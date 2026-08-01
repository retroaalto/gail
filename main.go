package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

// Debug logger
var debugLog *log.Logger

// Version is set via ldflags during build
var Version = "dev"

const (
	initialMaxLines = 10
	maxLines        = 10000
	fadeSteps       = 5
	fadeInterval    = 500 * time.Millisecond
)

type eventType int

const (
	eventNone eventType = iota
	eventTruncate
	eventRecreate
	eventRemove
	eventRename
	eventMarker
)

type lineInfo struct {
	content   string
	age       int
	timestamp time.Time
	event     eventType
	isMarker  bool
}

// fileReader tracks file read position. Protected by internal mutex
// since it is accessed from both waitForFileChanges goroutine and
// the Bubble Tea Update loop (via checkFileCmd polling).
type fileReader struct {
	mu            sync.Mutex
	readPos       int64
	partialLine   string
	partialBytes  int64
}

type model struct {
	filename         string
	allLines         []lineInfo
	partialLine      string
	lastUpdate       time.Time
	err              error
	watcher          *fsnotify.Watcher
	reader           *fileReader
	showTimestamp    bool
	timestampFmt     string
	terminalHeight   int
	terminalWidth    int
	verticalOffset   int
	horizontalOffset int
	autoFollow       bool
	promptingMarker  bool
	markerInput      string
}

type fileUpdateMsg struct {
	lines       []string
	partialLine string
	event       eventType
	eventTime   time.Time
}

type tickMsg time.Time

type errMsg struct {
	err error
}

type fileRecreatedMsg struct {
	eventType eventType
}

type checkFileMsg time.Time

func checkFileCmd(filename string) tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return checkFileMsg(t)
	})
}

func waitForFileChanges(filename string, watcher *fsnotify.Watcher, reader *fileReader) tea.Cmd {
	return func() tea.Msg {
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Write == fsnotify.Write {
					result, err := readNewLines(filename, reader)
					if err != nil {
						if errors.Is(err, errNoCompleteLines) {
							continue
						}
						return errMsg{err}
					}
					return fileUpdateMsg{
						lines:       result.lines,
						partialLine: result.partialLine,
						event:       result.event,
						eventTime:   time.Now(),
					}
				}

				if event.Op&fsnotify.Remove == fsnotify.Remove {
					watcher.Remove(filename)
					for attempt := 0; attempt < 5; attempt++ {
						time.Sleep(100 * time.Millisecond)
						if err := watcher.Add(filename); err == nil {
							break
						}
					}
					if _, err := os.Stat(filename); os.IsNotExist(err) {
						time.Sleep(500 * time.Millisecond)
						if err := watcher.Add(filename); err != nil {
							return errMsg{fmt.Errorf("file removed and could not re-watch: %w", err)}
						}
					}
					return fileRecreatedMsg{eventType: eventRemove}
				}

				if event.Op&fsnotify.Rename == fsnotify.Rename {
					watcher.Remove(filename)
					for attempt := 0; attempt < 5; attempt++ {
						time.Sleep(100 * time.Millisecond)
						if err := watcher.Add(filename); err == nil {
							break
						}
					}
					if _, err := os.Stat(filename); os.IsNotExist(err) {
						time.Sleep(500 * time.Millisecond)
						if err := watcher.Add(filename); err != nil {
							return errMsg{fmt.Errorf("file renamed and could not re-watch: %w", err)}
						}
					}
					return fileRecreatedMsg{eventType: eventRename}
				}

				if event.Op&fsnotify.Create == fsnotify.Create {
					return fileRecreatedMsg{eventType: eventRecreate}
				}

			case err := <-watcher.Errors:
				return errMsg{err}
			}
		}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(fadeInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func readLastLines(filename string, reader *fileReader, n int) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileSize, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}

	br := bufio.NewReader(file)
	ring := make([]string, n)
	count := 0
	var lastPartial string

	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")

		if strings.HasSuffix(line, "\n") {
			ring[count%n] = trimmed
			count++
		} else if len(line) > 0 {
			lastPartial = line
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}

	reader.mu.Lock()
	reader.readPos = fileSize
	reader.partialLine = lastPartial
	reader.mu.Unlock()

	available := count
	if available > n {
		available = n
	}
	if available == 0 {
		return nil, nil
	}

	lines := make([]string, available)
	start := count - available
	for i := 0; i < available; i++ {
		lines[i] = ring[(start+i)%n]
	}

	return lines, nil
}


type fileReadResult struct {
	lines       []string
	partialLine string
	event       eventType
}

var errNoCompleteLines = errors.New("no complete lines available yet")

func readNewLines(filename string, reader *fileReader) (*fileReadResult, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	fileSize := stat.Size()
	detectedEvent := eventNone

	reader.mu.Lock()
	defer reader.mu.Unlock()

	if fileSize < reader.readPos {
		reader.readPos = 0
		reader.partialLine = ""
		reader.partialBytes = 0
		detectedEvent = eventTruncate
	}

	if fileSize <= reader.readPos && detectedEvent == eventNone {
		return &fileReadResult{
			lines:       nil,
			partialLine: reader.partialLine,
			event:       eventNone,
		}, nil
	}

	seekPos := reader.readPos
	currentPartialContent := reader.partialLine

	_, err = file.Seek(seekPos, io.SeekStart)
	if err != nil {
		return nil, err
	}

	bufReader := bufio.NewReader(file)
	var lines []string
	isFirstSegment := true
	bytesReadFromFile := int64(0)

	for {
		line, err := bufReader.ReadString('\n')

		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}

		bytesReadFromFile += int64(len(line))
		hasNewline := strings.HasSuffix(line, "\n")
		segment := strings.TrimRight(line, "\r\n")

		if isFirstSegment && currentPartialContent != "" {
			segment = currentPartialContent + segment
			isFirstSegment = false
		}

		if hasNewline {
			lines = append(lines, segment)
			if debugLog != nil {
				debugLog.Printf("readNewLines: complete line found: %q", segment)
			}
			reader.partialLine = ""
			reader.partialBytes = 0
			reader.readPos += bytesReadFromFile
			bytesReadFromFile = 0
		} else {
			reader.partialLine = segment
			reader.partialBytes = bytesReadFromFile
			reader.readPos += bytesReadFromFile
			if debugLog != nil {
				debugLog.Printf("readNewLines: partial line saved: %q (pos=%d)", reader.partialLine, reader.readPos)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				if reader.partialLine != "" {
					if debugLog != nil {
						debugLog.Printf("readNewLines: returning partial line: %q", reader.partialLine)
					}
					return &fileReadResult{
						lines:       lines,
						partialLine: reader.partialLine,
						event:       detectedEvent,
					}, nil
				}
				if len(lines) == 0 {
					return nil, errNoCompleteLines
				}
				return &fileReadResult{lines: lines, event: detectedEvent}, nil
			}
			return nil, err
		}
	}

	reader.readPos = fileSize
	reader.partialLine = ""
	reader.partialBytes = 0
	return &fileReadResult{lines: lines, event: detectedEvent}, nil
}

func readAllLines(filename string, reader *fileReader) ([]string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	text := string(content)
	var lines []string

	if len(text) == 0 {
		reader.mu.Lock()
		reader.readPos = 0
		reader.mu.Unlock()
		return lines, nil
	}

	allLines := strings.Split(text, "\n")
	endsWithNewline := strings.HasSuffix(text, "\n")

	reader.mu.Lock()
	if endsWithNewline {
		if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
			lines = allLines[:len(allLines)-1]
		} else {
			lines = allLines
		}
		reader.readPos = int64(len(content))
		reader.partialLine = ""
	} else {
		if len(allLines) > 1 {
			lines = allLines[:len(allLines)-1]
			reader.readPos = int64(len(content))
			reader.partialLine = allLines[len(allLines)-1]
		} else {
			lines = []string{}
			reader.readPos = int64(len(content))
			reader.partialLine = allLines[0]
		}
	}
	reader.mu.Unlock()

	return lines, nil
}

func (m *model) appendLine(l lineInfo) {
	m.allLines = append(m.allLines, l)
	if len(m.allLines) > maxLines {
		trimCount := len(m.allLines) - maxLines
		m.allLines = m.allLines[trimCount:]
		m.verticalOffset -= trimCount
		if m.verticalOffset < 0 {
			m.verticalOffset = 0
		}
	}
}

func initialModel(filename string, showTimestamp bool, timestampFmt string) model {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return model{err: err}
	}

	err = watcher.Add(filename)
	if err != nil {
		watcher.Close()
		return model{err: err}
	}

	reader := &fileReader{}
	lines, err := readLastLines(filename, reader, initialMaxLines)
	if err != nil {
		watcher.Close()
		return model{err: err}
	}

	lineInfos := make([]lineInfo, len(lines))
	now := time.Now()
	for i, line := range lines {
		lineInfos[i] = lineInfo{
			content:   line,
			age:       fadeSteps,
			timestamp: now,
		}
	}

	return model{
		filename:       filename,
		allLines:       lineInfos,
		lastUpdate:     now,
		watcher:        watcher,
		reader:         reader,
		showTimestamp:  showTimestamp,
		timestampFmt:   timestampFmt,
		autoFollow:     true,
		terminalWidth:  80,
		terminalHeight: 24,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitForFileChanges(m.filename, m.watcher, m.reader),
		checkFileCmd(m.filename),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.promptingMarker {
			switch msg.String() {
			case "enter":
				markerLine := lineInfo{
					content:   "MARKER: " + m.markerInput,
					age:       0,
					timestamp: time.Now(),
					event:     eventMarker,
					isMarker:  true,
				}
				m.appendLine(markerLine)

				if m.autoFollow {
					availableHeight := m.terminalHeight - 1
					if availableHeight < 1 {
						availableHeight = 1
					}
					maxOffset := len(m.allLines) - availableHeight
					if maxOffset < 0 {
						maxOffset = 0
					}
					m.verticalOffset = maxOffset
				}

				m.promptingMarker = false
				m.markerInput = ""
				return m, nil

			case "esc", "ctrl+c":
				m.promptingMarker = false
				m.markerInput = ""
				return m, nil

			case "backspace":
				if len(m.markerInput) > 0 {
					runes := []rune(m.markerInput)
					m.markerInput = string(runes[:len(runes)-1])
				}
				return m, nil

			default:
				if utf8.RuneCountInString(msg.String()) == 1 {
					m.markerInput += msg.String()
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "q":
			if m.autoFollow {
				if m.watcher != nil {
					m.watcher.Close()
				}
				return m, tea.Quit
			}
			m.autoFollow = true
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			m.verticalOffset = maxOffset
			return m, nil

		case "esc":
			if !m.autoFollow {
				m.autoFollow = true
				availableHeight := m.terminalHeight - 1
				if availableHeight < 1 {
					availableHeight = 1
				}
				maxOffset := len(m.allLines) - availableHeight
				if maxOffset < 0 {
					maxOffset = 0
				}
				m.verticalOffset = maxOffset
			}
			return m, nil

		case "ctrl+c":
			if m.watcher != nil {
				m.watcher.Close()
			}
			return m, tea.Quit

		case "enter":
			if m.autoFollow {
				markerLine := lineInfo{
					content:   "MARKER",
					age:       0,
					timestamp: time.Now(),
					event:     eventMarker,
					isMarker:  true,
				}
				m.appendLine(markerLine)

				availableHeight := m.terminalHeight - 1
				if availableHeight < 1 {
					availableHeight = 1
				}
				maxOffset := len(m.allLines) - availableHeight
				if maxOffset < 0 {
					maxOffset = 0
				}
				m.verticalOffset = maxOffset
			}
			return m, nil

		case "m":
			m.promptingMarker = true
			m.markerInput = ""
			return m, nil

		case "up", "k":
			if m.verticalOffset > 0 {
				m.verticalOffset--
				m.autoFollow = false
			}
		case "down", "j":
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			if m.verticalOffset < maxOffset {
				m.verticalOffset++
			} else {
				m.autoFollow = true
			}

		case "pgup", "ctrl+b":
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			m.verticalOffset -= availableHeight
			if m.verticalOffset < 0 {
				m.verticalOffset = 0
			}
			m.autoFollow = false

		case "pgdown", "ctrl+f":
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			m.verticalOffset += availableHeight
			if m.verticalOffset >= maxOffset {
				m.verticalOffset = maxOffset
				m.autoFollow = true
			}

		case "ctrl+u":
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			halfPage := availableHeight / 2
			if halfPage < 1 {
				halfPage = 1
			}
			m.verticalOffset -= halfPage
			if m.verticalOffset < 0 {
				m.verticalOffset = 0
			}
			m.autoFollow = false

		case "ctrl+d":
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			halfPage := availableHeight / 2
			if halfPage < 1 {
				halfPage = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			m.verticalOffset += halfPage
			if m.verticalOffset >= maxOffset {
				m.verticalOffset = maxOffset
				m.autoFollow = true
			}

		case "g", "home":
			m.verticalOffset = 0
			m.autoFollow = false

		case "G", "end":
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			m.verticalOffset = maxOffset
			m.autoFollow = true

		case "left", "h":
			if m.horizontalOffset > 0 {
				m.horizontalOffset--
			}
		case "right", "l":
			m.horizontalOffset++
		}

	case tea.WindowSizeMsg:
		oldHeight := m.terminalHeight
		m.terminalHeight = msg.Height
		m.terminalWidth = msg.Width

		if m.autoFollow {
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			m.verticalOffset = maxOffset
		} else {
			if oldHeight > 0 {
				availableHeight := m.terminalHeight - 1
				if availableHeight < 1 {
					availableHeight = 1
				}
				maxOffset := len(m.allLines) - availableHeight
				if maxOffset < 0 {
					maxOffset = 0
				}
				if m.verticalOffset > maxOffset {
					m.verticalOffset = maxOffset
				}
				if m.verticalOffset < 0 {
					m.verticalOffset = 0
				}
			}
		}

	case fileUpdateMsg:
		m.lastUpdate = time.Now()

		m.partialLine = msg.partialLine
		if debugLog != nil {
			debugLog.Printf("fileUpdateMsg: set m.partialLine=%q, lines=%d, autoFollow=%v", m.partialLine, len(msg.lines), m.autoFollow)
		}

		if msg.event != eventNone {
			eventLine := lineInfo{
				content:   getEventMessage(msg.event),
				age:       0,
				timestamp: msg.eventTime,
				event:     msg.event,
			}
			m.appendLine(eventLine)
		}

		if len(msg.lines) > 0 {
			for _, line := range msg.lines {
				newLine := lineInfo{
					content:   line,
					age:       0,
					timestamp: m.lastUpdate,
					event:     eventNone,
				}
				m.appendLine(newLine)
			}

			if m.autoFollow {
				availableHeight := m.terminalHeight - 1
				if availableHeight < 1 {
					availableHeight = 1
				}
				maxOffset := len(m.allLines) - availableHeight
				if maxOffset < 0 {
					maxOffset = 0
				}
				m.verticalOffset = maxOffset
			}
		}
		return m, waitForFileChanges(m.filename, m.watcher, m.reader)

	case fileRecreatedMsg:
		m.lastUpdate = time.Now()

		eventLine := lineInfo{
			content:   getEventMessage(msg.eventType),
			age:       0,
			timestamp: m.lastUpdate,
			event:     msg.eventType,
		}
		m.appendLine(eventLine)

		lines, err := readAllLines(m.filename, m.reader)
		if err != nil {
			m.err = fmt.Errorf("error reading recreated file: %w", err)
			return m, tea.Quit
		}
		for _, line := range lines {
			m.appendLine(lineInfo{
				content:   line,
				age:       0,
				timestamp: m.lastUpdate,
				event:     eventNone,
			})
		}

		if m.autoFollow {
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			m.verticalOffset = maxOffset
		}
		return m, waitForFileChanges(m.filename, m.watcher, m.reader)

	case tickMsg:
		for i := range m.allLines {
			if m.allLines[i].age < fadeSteps {
				m.allLines[i].age++
			}
		}
		return m, tickCmd()

	case checkFileMsg:
		result, err := readNewLines(m.filename, m.reader)
		if err != nil {
			if debugLog != nil {
				debugLog.Printf("checkFileMsg: readNewLines error: %v", err)
			}
			return m, checkFileCmd(m.filename)
		}

		m.partialLine = result.partialLine

		hasChanges := result.event != eventNone || len(result.lines) > 0
		if hasChanges {
			m.lastUpdate = time.Now()
		}

		if result.event != eventNone {
			eventLine := lineInfo{
				content:   getEventMessage(result.event),
				age:       0,
				timestamp: m.lastUpdate,
				event:     result.event,
			}
			m.appendLine(eventLine)
		}

		if len(result.lines) > 0 {
			for _, line := range result.lines {
				newLine := lineInfo{
					content:   line,
					age:       0,
					timestamp: m.lastUpdate,
					event:     eventNone,
				}
				m.appendLine(newLine)
			}
		}

		if m.autoFollow {
			availableHeight := m.terminalHeight - 1
			if availableHeight < 1 {
				availableHeight = 1
			}
			maxOffset := len(m.allLines) - availableHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			m.verticalOffset = maxOffset
		}
		return m, checkFileCmd(m.filename)

	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

func getEventMessage(evt eventType) string {
	switch evt {
	case eventTruncate:
		return "--- FILE TRUNCATED ---"
	case eventRecreate:
		return "--- FILE RECREATED ---"
	case eventRemove:
		return "--- FILE REMOVED/RECREATED ---"
	case eventRename:
		return "--- FILE RENAMED/RECREATED ---"
	case eventMarker:
		return "MARKER"
	default:
		return "--- FILE EVENT ---"
	}
}

func getEventSymbol(evt eventType) string {
	switch evt {
	case eventTruncate:
		return "T"
	case eventRecreate:
		return "R"
	case eventRemove:
		return "X"
	case eventRename:
		return "M"
	case eventMarker:
		return "★"
	default:
		return "!"
	}
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	var b strings.Builder

	signColors := []string{
		"#00FF00",
		"#00DD00",
		"#00BB00",
		"#009900",
		"#006600",
		"#333333",
	}

	availableHeight := m.terminalHeight - 1
	if availableHeight < 1 {
		availableHeight = initialMaxLines
	}

	signWidth := 2
	var timestampWidth int
	if m.showTimestamp {
		timestampWidth = len(m.lastUpdate.Format(m.timestampFmt)) + 1
	}
	availableWidth := m.terminalWidth - signWidth - timestampWidth
	if availableWidth < 10 {
		availableWidth = 10
	}

	linesToDisplay := availableHeight
	if m.autoFollow && m.partialLine != "" {
		linesToDisplay = availableHeight - 1
		if linesToDisplay < 0 {
			linesToDisplay = 0
		}
	}

	startIdx := m.verticalOffset
	endIdx := startIdx + linesToDisplay
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(m.allLines) {
		endIdx = len(m.allLines)
	}
	if startIdx >= len(m.allLines) {
		startIdx = 0
		endIdx = 0
	}

	displayLines := []lineInfo{}
	if startIdx < endIdx {
		displayLines = m.allLines[startIdx:endIdx]
	}

	for _, line := range displayLines {
		var sign string
		var signStyle lipgloss.Style

		if line.isMarker {
			signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Bold(true)
			sign = getEventSymbol(eventMarker)
		} else if line.event != eventNone {
			switch line.event {
			case eventTruncate:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
			case eventRecreate:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true)
			case eventRemove:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF")).Bold(true)
			case eventRename:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)
			default:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Bold(true)
			}
			sign = getEventSymbol(line.event)
		} else {
			colorIdx := line.age
			if colorIdx >= len(signColors) {
				colorIdx = len(signColors) - 1
			}
			signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(signColors[colorIdx]))
			sign = "●"
		}

		timestamp := ""
		if m.showTimestamp {
			timestamp = line.timestamp.Format(m.timestampFmt) + " "
		}

		contentRunes := []rune(line.content)

		if m.horizontalOffset < len(contentRunes) {
			contentRunes = contentRunes[m.horizontalOffset:]
		} else {
			contentRunes = []rune{}
		}

		content := ""
		if len(contentRunes) > availableWidth {
			truncateAt := availableWidth - 3
			if truncateAt < 1 {
				truncateAt = 1
			}
			content = string(contentRunes[:truncateAt]) + "..."
		} else {
			content = string(contentRunes)
		}

		b.WriteString(signStyle.Render(sign) + " " + timestamp + content + "\n")
	}

	linesRendered := len(displayLines)
	if m.autoFollow && m.partialLine != "" && linesRendered < availableHeight {
		if debugLog != nil {
			debugLog.Printf("View: rendering partial line: %q (autoFollow=%v, linesRendered=%d, availableHeight=%d)",
				m.partialLine, m.autoFollow, linesRendered, availableHeight)
		}
		partialStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
		sign := "◐"

		timestamp := ""
		if m.showTimestamp {
			timestamp = time.Now().Format(m.timestampFmt) + " "
		}

		contentRunes := []rune(m.partialLine)
		if m.horizontalOffset < len(contentRunes) {
			contentRunes = contentRunes[m.horizontalOffset:]
		} else {
			contentRunes = []rune{}
		}

		content := ""
		if len(contentRunes) > availableWidth {
			truncateAt := availableWidth - 3
			if truncateAt < 1 {
				truncateAt = 1
			}
			content = string(contentRunes[:truncateAt]) + "..."
		} else {
			content = string(contentRunes)
		}

		b.WriteString(partialStyle.Render(sign) + " " + timestamp + content + "\n")
		linesRendered++
	} else if debugLog != nil && m.partialLine != "" {
		debugLog.Printf("View: NOT rendering partial line %q: autoFollow=%v, linesRendered=%d, availableHeight=%d",
			m.partialLine, m.autoFollow, linesRendered, availableHeight)
	}

	for i := linesRendered; i < availableHeight; i++ {
		b.WriteString("\n")
	}

	elapsed := time.Since(m.lastUpdate)

	totalLines := len(m.allLines)
	var positionInfo string
	if totalLines == 0 {
		positionInfo = "0/0 (0%)"
	} else {
		currentLine := m.verticalOffset + 1
		if currentLine > totalLines {
			currentLine = totalLines
		}
		percentage := 100
		if totalLines > 0 {
			percentage = (m.verticalOffset + availableHeight) * 100 / totalLines
			if percentage > 100 {
				percentage = 100
			}
		}
		positionInfo = fmt.Sprintf("%d-%d/%d (%d%%)",
			currentLine,
			min(m.verticalOffset+availableHeight, totalLines),
			totalLines,
			percentage)
	}

	followIndicator := ""
	if m.autoFollow {
		followIndicator = " [FOLLOW]"
	}

	var statusText string

	if m.promptingMarker {
		statusText = fmt.Sprintf("Enter marker message: %s_", m.markerInput)
	} else {
		displayName := filepath.Base(m.filename)
		timeStr := formatDuration(elapsed)

		maxWidth := m.terminalWidth - 2
		if maxWidth < 10 {
			maxWidth = 10
		}

		fullPath := fmt.Sprintf("%s | Last update: %s | %s%s",
			m.filename, timeStr, positionInfo, followIndicator)

		withLastUpdate := fmt.Sprintf("%s | Last update: %s | %s%s",
			displayName, timeStr, positionInfo, followIndicator)

		compact := fmt.Sprintf("%s | %s | %s%s",
			displayName, timeStr, positionInfo, followIndicator)

		if len(fullPath) <= maxWidth {
			statusText = fullPath
		} else if len(withLastUpdate) <= maxWidth {
			statusText = withLastUpdate
		} else if len(compact) <= maxWidth {
			statusText = compact
		} else {
			suffix := fmt.Sprintf(" | %s | %s%s", timeStr, positionInfo, followIndicator)
			maxNameLen := maxWidth - len(suffix) - 3

			if maxNameLen < 1 {
				statusText = fmt.Sprintf("%s%s", positionInfo, followIndicator)
				if len(statusText) > maxWidth {
					statusText = truncateToWidth(statusText, maxWidth)
				}
			} else {
				truncName := displayName
				if len(truncName) > maxNameLen {
					truncName = truncateToWidth(truncName, maxNameLen) + "..."
				}
				statusText = truncName + suffix
			}
		}

		if len(statusText) > maxWidth {
			statusText = truncateToWidth(statusText, maxWidth)
		}
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#5555FF")).
		Padding(0, 1).
		Width(m.terminalWidth)

	b.WriteString(statusStyle.Render(statusText))

	return b.String()
}

func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if len(s) <= maxWidth {
		return s
	}
	width := 0
	for i, r := range s {
		rl := utf8.RuneLen(r)
		if width+rl > maxWidth {
			return s[:i]
		}
		width += rl
	}
	return s
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "just now"
	} else if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func main() {
	var showTimestamp bool
	var timestampFmt string
	var debug bool
	var showVersion bool

	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&showVersion, "V", false, "Show version information")
	flag.BoolVar(&showTimestamp, "t", false, "Show timestamp for each line")
	flag.BoolVar(&showTimestamp, "timestamp", false, "Show timestamp for each line")
	flag.StringVar(&timestampFmt, "timestamp-format", "", "Custom timestamp format (e.g., +%y%m%d-%H%M%S)")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging to gail_debug.log")
	flag.Parse()

	if showVersion {
		fmt.Printf("gail version %s\n", Version)
		os.Exit(0)
	}

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: gail [OPTIONS] <filename>\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	filename := flag.Arg(0)

	if debug {
		debugFile, err := os.OpenFile("gail_debug.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open debug log: %v\n", err)
		} else {
			defer debugFile.Close()
			debugLog = log.New(debugFile, "", log.Ltime|log.Lmicroseconds)
			debugLog.Println("=== Debug logging started ===")
		}
	}

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: file '%s' does not exist\n", filename)
		os.Exit(1)
	}

	if timestampFmt == "" {
		timestampFmt = "060102-150405"
	} else if strings.HasPrefix(timestampFmt, "+") {
		timestampFmt = convertDateFormat(timestampFmt[1:])
	}

	m := initialModel(filename, showTimestamp, timestampFmt)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func convertDateFormat(dateFormat string) string {
	replacements := map[string]string{
		"%y": "06",
		"%Y": "2006",
		"%m": "01",
		"%d": "02",
		"%H": "15",
		"%M": "04",
		"%S": "05",
		"%I": "03",
		"%p": "PM",
	}

	result := dateFormat
	for old, new := range replacements {
		result = strings.ReplaceAll(result, old, new)
	}
	return result
}
