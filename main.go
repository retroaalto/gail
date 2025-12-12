package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

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
	age       int // How many fade intervals have passed
	timestamp time.Time
	event     eventType // Special event marker
	isMarker  bool      // True if this is a user marker line
}

type model struct {
	filename         string
	lines            []lineInfo
	allLines         []lineInfo // Store all lines for scrolling
	partialLine      string     // Current incomplete line
	lastUpdate       time.Time
	err              error
	watcher          *fsnotify.Watcher
	file             *os.File
	showTimestamp    bool
	timestampFmt     string
	terminalHeight   int
	terminalWidth    int
	verticalOffset   int    // Vertical scroll offset
	horizontalOffset int    // Horizontal scroll offset
	autoFollow       bool   // Whether to follow new content
	promptingMarker  bool   // Whether we're prompting for a marker message
	markerInput      string // Input buffer for marker message
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

func waitForFileChanges(filename string, watcher *fsnotify.Watcher) tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-watcher.Events:
			// Handle write events
			if event.Op&fsnotify.Write == fsnotify.Write {
				result, err := readNewLines(filename)
				if err != nil {
					if errors.Is(err, errNoCompleteLines) {
						return nil
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

			// Handle file recreation/removal

			if event.Op&fsnotify.Remove == fsnotify.Remove {
				// File was removed, try to re-watch it
				watcher.Remove(filename)
				// Wait a bit for the file to be recreated
				time.Sleep(100 * time.Millisecond)
				if err := watcher.Add(filename); err == nil {
					return fileRecreatedMsg{eventType: eventRemove}
				}
			}

			if event.Op&fsnotify.Rename == fsnotify.Rename {
				// File was renamed, try to re-watch it
				watcher.Remove(filename)
				time.Sleep(100 * time.Millisecond)
				if err := watcher.Add(filename); err == nil {
					return fileRecreatedMsg{eventType: eventRename}
				}
			}

			// Handle file creation (in case we're watching the directory)
			if event.Op&fsnotify.Create == fsnotify.Create {
				return fileRecreatedMsg{eventType: eventRecreate}
			}

		case err := <-watcher.Errors:
			return errMsg{err}
		}
		return nil
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(fadeInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

var lastReadPosition int64
var lastPartialLine string     // Track partial line content for display
var lastPartialLineBytes int64 // Track how many bytes the partial line occupies in file

func readLastLines(filename string, n int) ([]string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	text := string(content)
	if len(text) == 0 {
		lastReadPosition = 0
		lastPartialLine = ""
		return nil, nil
	}

	// Check if file ends with newline
	endsWithNewline := strings.HasSuffix(text, "\n")
	allLines := strings.Split(text, "\n")

	var completeLines []string
	if endsWithNewline {
		// All lines are complete
		if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
			completeLines = allLines[:len(allLines)-1]
		} else {
			completeLines = allLines
		}
		lastReadPosition = int64(len(content))
		lastPartialLine = ""
	} else {
		// Last line is partial
		if len(allLines) > 1 {
			completeLines = allLines[:len(allLines)-1]
			lastPartialLine = allLines[len(allLines)-1]
			// Set position to END of file (including partial line)
			// so next read will only see new appends
			lastReadPosition = int64(len(content))
		} else {
			// Only partial line, no complete lines
			completeLines = []string{}
			lastPartialLine = allLines[0]
			lastReadPosition = int64(len(content))
		}
	}

	// Return last n lines
	if len(completeLines) == 0 {
		return nil, nil
	}

	start := 0
	if len(completeLines) > n {
		start = len(completeLines) - n
	}

	return completeLines[start:], nil
}

// Old ring buffer implementation (keeping for reference but unused now)
func readLastLinesOld(filename string, n int) ([]string, error) {
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
	lastReadPosition = fileSize

	reader := bufio.NewReader(file)
	ring := make([]string, n)
	count := 0

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			ring[count%n] = trimmed
			count++
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}

	available := min(count, n)
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
	partialLine string // Incomplete line without newline
	event       eventType
}

var errNoCompleteLines = errors.New("no complete lines available yet")

func readNewLines(filename string) (*fileReadResult, error) {
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

	if fileSize < lastReadPosition {
		lastReadPosition = 0
		lastPartialLine = ""
		lastPartialLineBytes = 0
		detectedEvent = eventTruncate
	}

	if fileSize <= lastReadPosition {
		return &fileReadResult{lines: nil, event: detectedEvent}, nil
	}

	_, err = file.Seek(lastReadPosition, io.SeekStart)
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(file)
	var lines []string
	currentPartialContent := lastPartialLine // Content to prepend to first line
	isFirstSegment := true                   // Track if this is the first segment we're reading
	bytesReadFromFile := int64(0)

	for {
		line, err := reader.ReadString('\n')

		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}

		bytesReadFromFile += int64(len(line))
		hasNewline := strings.HasSuffix(line, "\n")
		segment := strings.TrimRight(line, "\r\n")

		// On first segment, prepend the previous partial line content if any
		if isFirstSegment && currentPartialContent != "" {
			segment = currentPartialContent + segment
			isFirstSegment = false
		}

		if hasNewline {
			// Complete line found
			lines = append(lines, segment)
			if debugLog != nil {
				debugLog.Printf("readNewLines: complete line found: %q", segment)
			}
			lastPartialLine = ""
			lastPartialLineBytes = 0
			lastReadPosition += bytesReadFromFile
			bytesReadFromFile = 0
		} else {
			// Partial line - save content but advance position to end of partial
			// This way next read starts after the partial bytes
			lastPartialLine = segment
			lastPartialLineBytes = bytesReadFromFile
			lastReadPosition += bytesReadFromFile
			if debugLog != nil {
				debugLog.Printf("readNewLines: partial line saved: %q (pos=%d)", lastPartialLine, lastReadPosition)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				if lastPartialLine != "" {
					// Return the partial line for display
					if debugLog != nil {
						debugLog.Printf("readNewLines: returning partial line: %q", lastPartialLine)
					}
					return &fileReadResult{
						lines:       lines,
						partialLine: lastPartialLine,
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

	lastReadPosition = fileSize
	lastPartialLine = ""
	lastPartialLineBytes = 0
	return &fileReadResult{lines: lines, event: detectedEvent}, nil
}

func readAllLines(filename string) ([]string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	// Split by newlines
	text := string(content)
	var lines []string

	if len(text) == 0 {
		lastReadPosition = 0
		return lines, nil
	}

	// Split into lines
	allLines := strings.Split(text, "\n")

	// Check if file ends with newline
	endsWithNewline := strings.HasSuffix(text, "\n")

	if endsWithNewline {
		// All lines are complete (last element after split is empty string)
		// Don't include the empty string at the end
		if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
			lines = allLines[:len(allLines)-1]
		} else {
			lines = allLines
		}
		lastReadPosition = int64(len(content))
		lastPartialLine = "" // No partial line
	} else {
		// Last line is partial (no newline at end)
		// Include all lines except the last one
		if len(allLines) > 1 {
			lines = allLines[:len(allLines)-1]
			// Calculate position: length of complete lines
			completeContent := strings.Join(lines, "\n")
			if len(lines) > 0 {
				lastReadPosition = int64(len(completeContent) + 1) // +1 for the \n after last complete line
			} else {
				lastReadPosition = 0
			}
			// Store the partial line
			lastPartialLine = allLines[len(allLines)-1]
		} else {
			// Only one line and it's partial
			lines = []string{}
			lastReadPosition = 0
			lastPartialLine = allLines[0]
		}
	}

	return lines, nil
}

func initialModel(filename string, showTimestamp bool, timestampFmt string) model {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return model{err: err}
	}

	err = watcher.Add(filename)
	if err != nil {
		return model{err: err}
	}

	lines, err := readLastLines(filename, initialMaxLines)
	if err != nil {
		return model{err: err}
	}

	lineInfos := make([]lineInfo, len(lines))
	now := time.Now()
	for i, line := range lines {
		lineInfos[i] = lineInfo{
			content:   line,
			age:       fadeSteps, // Start faded (old lines)
			timestamp: now,
		}
	}

	return model{
		filename:       filename,
		lines:          lineInfos,
		allLines:       lineInfos,
		lastUpdate:     now,
		watcher:        watcher,
		showTimestamp:  showTimestamp,
		timestampFmt:   timestampFmt,
		autoFollow:     true, // Start in follow mode
		terminalWidth:  80,   // Default width until WindowSizeMsg is received
		terminalHeight: 24,   // Default height until WindowSizeMsg is received
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitForFileChanges(m.filename, m.watcher),
		checkFileCmd(m.filename),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle marker input mode
		if m.promptingMarker {
			switch msg.String() {
			case "enter":
				// Add marker with custom message
				markerLine := lineInfo{
					content:   "MARKER: " + m.markerInput,
					age:       0,
					timestamp: time.Now(),
					event:     eventMarker,
					isMarker:  true,
				}
				m.allLines = append(m.allLines, markerLine)

				// Auto-scroll to bottom if in follow mode
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

				// Exit prompt mode
				m.promptingMarker = false
				m.markerInput = ""
				return m, nil

			case "esc", "ctrl+c":
				// Cancel marker input
				m.promptingMarker = false
				m.markerInput = ""
				return m, nil

			case "backspace":
				if len(m.markerInput) > 0 {
					m.markerInput = m.markerInput[:len(m.markerInput)-1]
				}
				return m, nil

			default:
				// Add character to input
				if len(msg.String()) == 1 {
					m.markerInput += msg.String()
				}
				return m, nil
			}
		}

		// Normal mode key handling
		switch msg.String() {
		case "q":
			// In follow mode, q exits the program
			if m.autoFollow {
				if m.watcher != nil {
					m.watcher.Close()
				}
				return m, tea.Quit
			}
			// Not in follow mode, return to follow mode
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
			// ESC returns to follow mode (but doesn't exit)
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
			// In follow mode, ENTER adds a simple marker
			if m.autoFollow {
				markerLine := lineInfo{
					content:   "MARKER",
					age:       0,
					timestamp: time.Now(),
					event:     eventMarker,
					isMarker:  true,
				}
				m.allLines = append(m.allLines, markerLine)

				// Auto-scroll to bottom
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
			// Prompt for custom marker message
			m.promptingMarker = true
			m.markerInput = ""
			return m, nil

		// Vertical scrolling
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
				// At bottom, re-enable auto-follow
				m.autoFollow = true
			}

		// Page scrolling
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

		// Half-page scrolling
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

		// Home/End
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

		// Horizontal scrolling
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

		// Adjust vertical offset to maintain view position or stay at bottom in follow mode
		if m.autoFollow {
			// In follow mode, stay at the bottom
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
			// Not in follow mode, try to maintain relative position
			if oldHeight > 0 {
				// Calculate the line that was at the top of the screen
				// and try to keep it visible after resize
				availableHeight := m.terminalHeight - 1
				if availableHeight < 1 {
					availableHeight = 1
				}
				maxOffset := len(m.allLines) - availableHeight
				if maxOffset < 0 {
					maxOffset = 0
				}
				// Clamp the offset to valid range
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

		// Update partial line
		m.partialLine = msg.partialLine
		if debugLog != nil {
			debugLog.Printf("fileUpdateMsg: set m.partialLine=%q, lines=%d, autoFollow=%v", m.partialLine, len(msg.lines), m.autoFollow)
		}

		// If there's an event (like truncation), add a marker line
		if msg.event != eventNone {
			eventLine := lineInfo{
				content:   getEventMessage(msg.event),
				age:       0,
				timestamp: msg.eventTime,
				event:     msg.event,
			}
			m.allLines = append(m.allLines, eventLine)
		}

		// Add the actual content lines
		if len(msg.lines) > 0 {
			for _, line := range msg.lines {
				newLine := lineInfo{
					content:   line,
					age:       0,
					timestamp: m.lastUpdate,
					event:     eventNone,
				}
				m.allLines = append(m.allLines, newLine)
			}

			// Auto-scroll to bottom if in follow mode
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
		return m, waitForFileChanges(m.filename, m.watcher)

	case fileRecreatedMsg:
		m.lastUpdate = time.Now()

		// Add event marker line
		eventLine := lineInfo{
			content:   getEventMessage(msg.eventType),
			age:       0,
			timestamp: m.lastUpdate,
			event:     msg.eventType,
		}
		m.allLines = append(m.allLines, eventLine)

		// File was recreated, reload all lines
		lines, err := readAllLines(m.filename)
		if err == nil {
			for _, line := range lines {
				m.allLines = append(m.allLines, lineInfo{
					content:   line,
					age:       0,
					timestamp: m.lastUpdate,
					event:     eventNone,
				})
			}

			// Reset to bottom if in follow mode
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
		return m, waitForFileChanges(m.filename, m.watcher)

	case tickMsg:
		// Age all lines
		for i := range m.allLines {
			if m.allLines[i].age < fadeSteps {
				m.allLines[i].age++
			}
		}
		return m, tickCmd()

	case checkFileMsg:
		// Periodically check if there are new lines (in case events were missed)
		result, err := readNewLines(m.filename)
		if err == nil {
			// Update partial line
			m.partialLine = result.partialLine

			// Only update lastUpdate if there's actual new content or events
			hasChanges := result.event != eventNone || len(result.lines) > 0

			if hasChanges {
				m.lastUpdate = time.Now()
			}

			// Add event marker if detected
			if result.event != eventNone {
				eventLine := lineInfo{
					content:   getEventMessage(result.event),
					age:       0,
					timestamp: m.lastUpdate,
					event:     result.event,
				}
				m.allLines = append(m.allLines, eventLine)
			}

			// Add content lines
			if len(result.lines) > 0 {
				for _, line := range result.lines {
					newLine := lineInfo{
						content:   line,
						age:       0,
						timestamp: m.lastUpdate,
						event:     eventNone,
					}
					m.allLines = append(m.allLines, newLine)
				}
			}

			// Auto-scroll to bottom if in follow mode
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

	// DEBUG: Log terminal dimensions to a file
	// Uncomment to debug width issues
	// debugFile, _ := os.OpenFile("/tmp/gail_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	// if debugFile != nil {
	// 	fmt.Fprintf(debugFile, "Terminal: %dx%d\n", m.terminalWidth, m.terminalHeight)
	// 	debugFile.Close()
	// }

	// Styles
	signColors := []string{
		"#00FF00", // Bright green
		"#00DD00",
		"#00BB00",
		"#009900",
		"#006600",
		"#333333", // Dark gray
	}

	// Calculate available height for lines (terminal height - status bar)
	availableHeight := m.terminalHeight - 1
	if availableHeight < 1 {
		availableHeight = initialMaxLines
	}

	// Calculate available width for content (terminal width - sign - spaces)
	signWidth := 2 // "● "
	var timestampWidth int
	if m.showTimestamp {
		timestampWidth = len(m.timestampFmt) + 1 // +1 for space
	}
	availableWidth := m.terminalWidth - signWidth - timestampWidth
	if availableWidth < 10 {
		availableWidth = 10
	}

	// Reserve space for partial line if we're in follow mode and have one
	linesToDisplay := availableHeight
	if m.autoFollow && m.partialLine != "" {
		linesToDisplay = availableHeight - 1 // Reserve one line for partial
		if linesToDisplay < 0 {
			linesToDisplay = 0
		}
	}

	// Get the lines to display based on vertical offset
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

	// Display lines
	for _, line := range displayLines {
		var sign string
		var signStyle lipgloss.Style

		// Check if this is a marker line
		if line.isMarker {
			signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Bold(true) // Gold
			sign = getEventSymbol(eventMarker)
		} else if line.event != eventNone {
			// Event lines get special colored markers
			switch line.event {
			case eventTruncate:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true) // Red
			case eventRecreate:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true) // Orange
			case eventRemove:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF")).Bold(true) // Magenta
			case eventRename:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true) // Cyan
			default:
				signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Bold(true) // Yellow
			}
			sign = getEventSymbol(line.event)
		} else {
			// Normal lines get the fading dot
			colorIdx := line.age
			if colorIdx >= len(signColors) {
				colorIdx = len(signColors) - 1
			}
			signStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(signColors[colorIdx]))
			sign = "●"
		}

		// Add timestamp if enabled
		timestamp := ""
		if m.showTimestamp {
			timestamp = line.timestamp.Format(m.timestampFmt) + " "
		}

		// Apply horizontal scrolling and truncation
		// Convert to runes for proper character handling (not byte slicing)
		contentRunes := []rune(line.content)

		// Apply horizontal offset
		if m.horizontalOffset < len(contentRunes) {
			contentRunes = contentRunes[m.horizontalOffset:]
		} else {
			contentRunes = []rune{}
		}

		// Truncate if too long and add ellipsis
		// Reserve 3 characters for "..." if we need to truncate
		content := ""
		if len(contentRunes) > availableWidth {
			// Truncate to leave room for ellipsis
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

	// Display partial line if we're at the bottom and in follow mode
	linesRendered := len(displayLines)
	if m.autoFollow && m.partialLine != "" && linesRendered < availableHeight {
		if debugLog != nil {
			debugLog.Printf("View: rendering partial line: %q (autoFollow=%v, linesRendered=%d, availableHeight=%d)",
				m.partialLine, m.autoFollow, linesRendered, availableHeight)
		}
		// Use yellow/amber color for partial line indicator
		partialStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
		sign := "◐" // Half-circle to indicate incomplete

		timestamp := ""
		if m.showTimestamp {
			timestamp = time.Now().Format(m.timestampFmt) + " "
		}

		// Apply horizontal scrolling and truncation
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

	// Pad remaining lines if we don't have enough to fill the screen
	for i := linesRendered; i < availableHeight; i++ {
		b.WriteString("\n")
	}

	// Status bar
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#5555FF")).
		Padding(0, 1).
		Width(m.terminalWidth)

	elapsed := time.Since(m.lastUpdate)

	// Calculate position in buffer
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

	// Show marker input prompt if active
	if m.promptingMarker {
		statusText := fmt.Sprintf("Enter marker message: %s_", m.markerInput)
		b.WriteString(statusStyle.Render(statusText))
	} else {
		statusText := fmt.Sprintf("%s | Last update: %s ago | %s%s",
			m.filename,
			formatDuration(elapsed),
			positionInfo,
			followIndicator)
		b.WriteString(statusStyle.Render(statusText))
	}

	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

	// Initialize debug logging if requested
	if debug {
		debugFile, err := os.OpenFile("gail_debug.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open debug log: %v\n", err)
		} else {
			debugLog = log.New(debugFile, "", log.Ltime|log.Lmicroseconds)
			debugLog.Println("=== Debug logging started ===")
			defer debugFile.Close()
		}
	}

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: file '%s' does not exist\n", filename)
		os.Exit(1)
	}

	// Parse timestamp format
	if timestampFmt == "" {
		timestampFmt = "060102-150405" // Default: YYMMDD-hhmmss
	} else if strings.HasPrefix(timestampFmt, "+") {
		// Convert date command style format to Go format
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
	// Convert common date format specifiers to Go time format
	replacements := map[string]string{
		"%y": "06",   // Year (2 digits)
		"%Y": "2006", // Year (4 digits)
		"%m": "01",   // Month
		"%d": "02",   // Day
		"%H": "15",   // Hour (24h)
		"%M": "04",   // Minute
		"%S": "05",   // Second
		"%I": "03",   // Hour (12h)
		"%p": "PM",   // AM/PM
	}

	result := dateFormat
	for old, new := range replacements {
		result = strings.ReplaceAll(result, old, new)
	}
	return result
}
