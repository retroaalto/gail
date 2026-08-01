package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

func TestConvertDateFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"full datetime", "%Y-%m-%d-%H:%M:%S", "2006-01-02-15:04:05"},
		{"year month day", "%Y%m%d", "20060102"},
		{"12h with ampm", "%I:%M %p", "03:04 PM"},
		{"no specifiers", "hello-world", "hello-world"},
		{"default format", "060102-150405", "060102-150405"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertDateFormat(tt.input)
			if got != tt.expected {
				t.Errorf("convertDateFormat(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxWidth int
		expected string
	}{
		{"no trunc needed", "hello", 10, "hello"},
		{"exact fit", "hello", 5, "hello"},
		{"truncate ascii", "hello world", 5, "hello"},
		{"zero width", "hello", 0, ""},
		{"negative width", "hello", -1, ""},
		{"empty string", "", 10, ""},
		{"multi-byte chars", "héllo world", 7, "héllo "},
		{"cut mid rune boundary safety", "héllo", 2, "h"},
		{"unicode double-width", "abc世界def", 7, "abc世"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToWidth(tt.s, tt.maxWidth)
			if got != tt.expected {
				t.Errorf("truncateToWidth(%q, %d) = %q, want %q", tt.s, tt.maxWidth, got, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        time.Duration
		expected string
	}{
		{"just now", 500 * time.Millisecond, "just now"},
		{"seconds", 5 * time.Second, "5s"},
		{"seconds floor", 5*time.Second + 900*time.Millisecond, "5s"},
		{"minutes and seconds", 90 * time.Second, "1m 30s"},
		{"hours and minutes", 3661 * time.Second, "1h 1m"},
		{"zero", 0, "just now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.expected)
			}
		})
	}
}

func TestGetEventMessage(t *testing.T) {
	tests := []struct {
		evt      eventType
		expected string
	}{
		{eventTruncate, "--- FILE TRUNCATED ---"},
		{eventRecreate, "--- FILE RECREATED ---"},
		{eventRemove, "--- FILE REMOVED/RECREATED ---"},
		{eventRename, "--- FILE RENAMED/RECREATED ---"},
		{eventMarker, "MARKER"},
		{eventNone, "--- FILE EVENT ---"},
		{eventType(99), "--- FILE EVENT ---"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := getEventMessage(tt.evt)
			if got != tt.expected {
				t.Errorf("getEventMessage(%d) = %q, want %q", tt.evt, got, tt.expected)
			}
		})
	}
}

func TestGetEventSymbol(t *testing.T) {
	tests := []struct {
		evt      eventType
		expected string
	}{
		{eventTruncate, "T"},
		{eventRecreate, "R"},
		{eventRemove, "X"},
		{eventRename, "M"},
		{eventMarker, "★"},
		{eventNone, "!"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := getEventSymbol(tt.evt)
			if got != tt.expected {
				t.Errorf("getEventSymbol(%d) = %q, want %q", tt.evt, got, tt.expected)
			}
		})
	}
}

func TestAppendLine(t *testing.T) {
	t.Run("within maxLines", func(t *testing.T) {
		m := &model{allLines: make([]lineInfo, 0)}
		for i := 0; i < 100; i++ {
			m.appendLine(lineInfo{content: fmt.Sprintf("line %d", i)})
		}
		if len(m.allLines) != 100 {
			t.Errorf("expected 100 lines, got %d", len(m.allLines))
		}
		if m.verticalOffset != 0 {
			t.Errorf("verticalOffset should be 0, got %d", m.verticalOffset)
		}
	})

	t.Run("exceed maxLines trims from front", func(t *testing.T) {
		m := &model{allLines: make([]lineInfo, 0)}
		for i := 0; i < 15000; i++ {
			m.appendLine(lineInfo{content: fmt.Sprintf("line %d", i)})
		}
		if len(m.allLines) != maxLines {
			t.Errorf("expected %d lines, got %d", maxLines, len(m.allLines))
		}
		if m.allLines[0].content != fmt.Sprintf("line %d", 15000-maxLines) {
			t.Errorf("first line should be line %d, got %q", 15000-maxLines, m.allLines[0].content)
		}
		if m.allLines[len(m.allLines)-1].content != "line 14999" {
			t.Errorf("last line should be line 14999, got %q", m.allLines[len(m.allLines)-1].content)
		}
	})

	t.Run("trim adjusts verticalOffset", func(t *testing.T) {
		m := &model{allLines: make([]lineInfo, 0), verticalOffset: 500}
		for i := 0; i < 15000; i++ {
			m.appendLine(lineInfo{content: fmt.Sprintf("line %d", i)})
		}
		trimCount := 15000 - maxLines
		expectedOffset := 500 - trimCount
		if expectedOffset > 0 {
			t.Errorf("expectedOffset should be negative but got %d", expectedOffset)
		}
		if m.verticalOffset != 0 {
			t.Errorf("verticalOffset should be clamped to 0, got %d", m.verticalOffset)
		}
	})

	t.Run("trim does not make offset negative", func(t *testing.T) {
		m := &model{allLines: make([]lineInfo, 0), verticalOffset: 0}
		for i := 0; i < 15000; i++ {
			m.appendLine(lineInfo{content: fmt.Sprintf("line %d", i)})
		}
		if m.verticalOffset < 0 {
			t.Errorf("verticalOffset should not be negative, got %d", m.verticalOffset)
		}
	})
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}

func TestReadLastLines(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty file", func(t *testing.T) {
		path := writeTempFile(t, dir, "empty.log", "")
		r := &fileReader{}
		lines, err := readLastLines(path, r, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lines != nil {
			t.Errorf("expected nil lines for empty file, got %v", lines)
		}
		if r.readPos != 0 {
			t.Errorf("readPos should be 0 for empty file, got %d", r.readPos)
		}
	})

	t.Run("fewer lines than n", func(t *testing.T) {
		path := writeTempFile(t, dir, "few.log", "line1\nline2\nline3\n")
		r := &fileReader{}
		lines, err := readLastLines(path, r, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 3 {
			t.Errorf("expected 3 lines, got %d", len(lines))
		}
		if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
			t.Errorf("lines mismatch: %v", lines)
		}
	})

	t.Run("more lines than n returns last n", func(t *testing.T) {
		content := ""
		for i := 1; i <= 20; i++ {
			content += fmt.Sprintf("line%02d\n", i)
		}
		path := writeTempFile(t, dir, "many.log", content)
		r := &fileReader{}
		lines, err := readLastLines(path, r, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 10 {
			t.Errorf("expected 10 lines, got %d", len(lines))
		}
		if lines[0] != "line11" {
			t.Errorf("first line should be line11, got %q", lines[0])
		}
		if lines[9] != "line20" {
			t.Errorf("last line should be line20, got %q", lines[9])
		}
	})

	t.Run("file ending without newline sets partial", func(t *testing.T) {
		path := writeTempFile(t, dir, "nolf.log", "hello\nworld")
		r := &fileReader{}
		lines, err := readLastLines(path, r, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 1 {
			t.Errorf("expected 1 complete line, got %d", len(lines))
		}
		if lines[0] != "hello" {
			t.Errorf("expected complete line 'hello', got %q", lines[0])
		}
		if r.partialLine != "world" {
			t.Errorf("expected partialLine 'world', got %q", r.partialLine)
		}
		expectedLen := int64(len("hello\nworld"))
		if r.readPos != expectedLen {
			t.Errorf("readPos should be %d (EOF), got %d", expectedLen, r.readPos)
		}
	})

	t.Run("file ending with newline clears partial", func(t *testing.T) {
		r := &fileReader{partialLine: "old-partial"}
		path := writeTempFile(t, dir, "withnl.log", "hello\nworld\n")
		lines, err := readLastLines(path, r, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 2 {
			t.Errorf("expected 2 lines, got %d", len(lines))
		}
		if r.partialLine != "" {
			t.Errorf("expected partialLine to be cleared, got %q", r.partialLine)
		}
	})

	t.Run("only partial line no newline", func(t *testing.T) {
		path := writeTempFile(t, dir, "partialonly.log", "incomplete")
		r := &fileReader{}
		lines, err := readLastLines(path, r, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lines != nil {
			t.Errorf("expected nil complete lines, got %v", lines)
		}
		if r.partialLine != "incomplete" {
			t.Errorf("expected partialLine 'incomplete', got %q", r.partialLine)
		}
		if r.readPos != int64(len("incomplete")) {
			t.Errorf("readPos should be %d, got %d", len("incomplete"), r.readPos)
		}
	})
}

func TestReadAllLines(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty file", func(t *testing.T) {
		path := writeTempFile(t, dir, "empty.log", "")
		r := &fileReader{}
		lines, err := readAllLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 0 {
			t.Errorf("expected 0 lines, got %d", len(lines))
		}
		if r.readPos != 0 {
			t.Errorf("readPos should be 0, got %d", r.readPos)
		}
	})

	t.Run("file with newline", func(t *testing.T) {
		path := writeTempFile(t, dir, "nl.log", "a\nb\nc\n")
		r := &fileReader{}
		lines, err := readAllLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 3 {
			t.Errorf("expected 3 lines, got %d", len(lines))
		}
		if lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
			t.Errorf("lines mismatch: %v", lines)
		}
		if r.partialLine != "" {
			t.Errorf("partialLine should be empty, got %q", r.partialLine)
		}
	})

	t.Run("H4: readPos correct for file without trailing newline", func(t *testing.T) {
		content := "hello\nworld"
		path := writeTempFile(t, dir, "nolf.log", content)
		r := &fileReader{}
		lines, err := readAllLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 1 {
			t.Errorf("expected 1 complete line, got %d", len(lines))
		}
		if r.partialLine != "world" {
			t.Errorf("expected partialLine 'world', got %q", r.partialLine)
		}
		expectedPos := int64(len(content))
		if r.readPos != expectedPos {
			t.Errorf("H4 BUG: readPos = %d, want %d (len of entire content); partial line was already captured in partialLine", r.readPos, expectedPos)
		}
	})

	t.Run("H4: readPos with only partial line", func(t *testing.T) {
		content := "incomplete"
		path := writeTempFile(t, dir, "partialonly.log", content)
		r := &fileReader{}
		lines, err := readAllLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(lines) != 0 {
			t.Errorf("expected 0 complete lines, got %d", len(lines))
		}
		if r.partialLine != "incomplete" {
			t.Errorf("expected partialLine 'incomplete', got %q", r.partialLine)
		}
		if r.readPos != int64(len(content)) {
			t.Errorf("H4 BUG: readPos = %d, want %d", r.readPos, len(content))
		}
	})
}

func TestReadNewLines(t *testing.T) {
	dir := t.TempDir()

	t.Run("no new data returns early", func(t *testing.T) {
		path := writeTempFile(t, dir, "uptodate.log", "hello\n")
		r := &fileReader{readPos: int64(len("hello\n"))}
		result, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.lines) != 0 {
			t.Errorf("expected 0 lines for no new data, got %d", len(result.lines))
		}
		if result.event != eventNone {
			t.Errorf("expected eventNone, got %v", result.event)
		}
	})

	t.Run("H1: early return preserves partialLine", func(t *testing.T) {
		content := "hello\nworld"
		path := writeTempFile(t, dir, "withpartial.log", content)
		r := &fileReader{
			readPos:     int64(len(content)),
			partialLine: "world",
		}
		result, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.partialLine != "world" {
			t.Errorf("H1 BUG: partialLine = %q, want %q (must preserve saved partial)", result.partialLine, "world")
		}
		if len(result.lines) != 0 {
			t.Errorf("expected 0 new complete lines, got %d", len(result.lines))
		}
	})

	t.Run("reads new complete lines", func(t *testing.T) {
		path := writeTempFile(t, dir, "increm.log", "hello\n")
		r := &fileReader{readPos: 0}
		result, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.lines) != 1 || result.lines[0] != "hello" {
			t.Errorf("expected ['hello'], got %v", result.lines)
		}
		if r.readPos != int64(len("hello\n")) {
			t.Errorf("readPos should be %d, got %d", len("hello\n"), r.readPos)
		}
		if result.partialLine != "" {
			t.Errorf("expected empty partialLine, got %q", result.partialLine)
		}
	})

	t.Run("reads new partial line", func(t *testing.T) {
		content := "incomplete"
		path := writeTempFile(t, dir, "partial.log", content)
		r := &fileReader{readPos: 0}
		result, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.lines) != 0 {
			t.Errorf("expected 0 complete lines, got %d: %v", len(result.lines), result.lines)
		}
		if result.partialLine != "incomplete" {
			t.Errorf("expected partialLine 'incomplete', got %q", result.partialLine)
		}
		if r.partialLine != "incomplete" {
			t.Errorf("reader.partialLine should be 'incomplete', got %q", r.partialLine)
		}
		if r.readPos != int64(len(content)) {
			t.Errorf("readPos should be %d, got %d", len(content), r.readPos)
		}
	})

	t.Run("completes previous partial line", func(t *testing.T) {
		content := "hello"
		path := writeTempFile(t, dir, "completeprev.log", content)
		r := &fileReader{readPos: 0, partialLine: "prev-"}

		appendContent(t, path, " world\n")

		result, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.lines) != 1 || result.lines[0] != "prev-hello world" {
			t.Errorf("expected completed line 'prev-hello world', got %v", result.lines)
		}
		if r.partialLine != "" {
			t.Errorf("partialLine should be empty, got %q", r.partialLine)
		}
	})

	t.Run("detects truncation", func(t *testing.T) {
		content := "aaaaaa\nbbbbbb\n"
		path := writeTempFile(t, dir, "trunc.log", content)
		r := &fileReader{readPos: int64(len(content))}

		os.WriteFile(path, []byte("new\n"), 0644)

		result, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.event != eventTruncate {
			t.Errorf("expected eventTruncate, got %v", result.event)
		}
		if len(result.lines) != 1 || result.lines[0] != "new" {
			t.Errorf("expected lines ['new'], got %v", result.lines)
		}
		if r.readPos != int64(len("new\n")) {
			t.Errorf("readPos should be %d, got %d", len("new\n"), r.readPos)
		}
	})

	t.Run("truncation then empty file with no new data", func(t *testing.T) {
		content := "aaaaaa\nbbbbbb\n"
		path := writeTempFile(t, dir, "trunc2.log", content)
		r := &fileReader{readPos: int64(len(content)), partialLine: "old-partial"}

		os.WriteFile(path, []byte(""), 0644)

		result, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// truncation detected: readPos reset to 0, then file is empty so returns with eventTruncate but 0 lines
		if result.event != eventTruncate {
			t.Errorf("expected eventTruncate, got %v", result.event)
		}
		if r.readPos != 0 {
			t.Errorf("after truncation to empty, readPos should be 0, got %d", r.readPos)
		}
		if r.partialLine != "" {
			t.Errorf("partialLine should be cleared after truncation, got %q", r.partialLine)
		}
	})
}

func TestReadNewLinesIntegration(t *testing.T) {
	dir := t.TempDir()

	t.Run("simulate tail workflow", func(t *testing.T) {
		path := writeTempFile(t, dir, "workflow.log", "initial line\n")
		r := &fileReader{}

		t.Logf("initial content: 'initial line\\n'")

		result1, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 1 error: %v", err)
		}
		if len(result1.lines) != 1 || result1.lines[0] != "initial line" {
			t.Errorf("step 1: bad lines %v", result1.lines)
		}

		appendContent(t, path, "second line\n")
		result2, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 2 error: %v", err)
		}
		if len(result2.lines) != 1 || result2.lines[0] != "second line" {
			t.Errorf("step 2: bad lines %v", result2.lines)
		}

		appendContent(t, path, "partial")
		result3, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 3 error: %v", err)
		}
		if len(result3.lines) != 0 {
			t.Errorf("step 3: expected 0 lines, got %v", result3.lines)
		}
		if result3.partialLine != "partial" {
			t.Errorf("step 3: expected partialLine 'partial', got %q", result3.partialLine)
		}

		appendContent(t, path, " line\n")
		result4, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 4 error: %v", err)
		}
		if len(result4.lines) != 1 || result4.lines[0] != "partial line" {
			t.Errorf("step 4: expected 'partial line', got %v", result4.lines)
		}

		result5, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 5 error: %v", err)
		}
		if len(result5.lines) != 0 {
			t.Errorf("step 5: expected 0 new lines, got %d", len(result5.lines))
		}
		if result5.partialLine != "" {
			t.Errorf("step 5: expected empty partialLine, got %q", result5.partialLine)
		}
	})

	t.Run("H1: partial line preserved across polls with no new data", func(t *testing.T) {
		content := "hello\nincomplete"
		path := writeTempFile(t, dir, "h1test.log", content)
		r := &fileReader{}

		result1, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 1 error: %v", err)
		}
		if len(result1.lines) != 1 || result1.lines[0] != "hello" {
			t.Errorf("step 1: bad lines %v", result1.lines)
		}
		if result1.partialLine != "incomplete" {
			t.Errorf("step 1: expected partialLine 'incomplete', got %q", result1.partialLine)
		}

		result2, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 2 error: %v", err)
		}
		if len(result2.lines) != 0 {
			t.Errorf("step 2: expected 0 lines, got %v", result2.lines)
		}
		if result2.partialLine != "incomplete" {
			t.Errorf("H1 BUG: step 2 partialLine = %q, want 'incomplete' (poll should not clear partial)", result2.partialLine)
		}

		appendContent(t, path, " addition\n")
		result3, err := readNewLines(path, r)
		if err != nil {
			t.Fatalf("step 3 error: %v", err)
		}
		if len(result3.lines) != 1 {
			t.Errorf("step 3: expected 1 line, got %v", result3.lines)
		}
		if result3.lines[0] != "incomplete addition" {
			t.Errorf("step 3: expected 'incomplete addition', got %q", result3.lines[0])
		}
	})
}

func TestModelView(t *testing.T) {
	t.Run("error view", func(t *testing.T) {
		m := model{err: fmt.Errorf("test error")}
		got := m.View()
		if !strings.HasPrefix(got, "Error:") {
			t.Errorf("expected error view, got %q", got)
		}
	})

	t.Run("view with lines", func(t *testing.T) {
		m := model{
			filename:       "/tmp/test.log",
			terminalHeight: 15,
			terminalWidth:  80,
			lastUpdate:     time.Now(),
			autoFollow:     true,
			allLines: []lineInfo{
				{content: "hello", age: 0, timestamp: time.Now()},
				{content: "world", age: 1, timestamp: time.Now()},
			},
		}
		output := m.View()
		if !strings.Contains(output, "hello") || !strings.Contains(output, "world") {
			t.Errorf("view missing content: %q", output)
		}
	})

	t.Run("view with partial line", func(t *testing.T) {
		m := model{
			filename:       "/tmp/test.log",
			terminalHeight: 10,
			terminalWidth:  80,
			lastUpdate:     time.Now(),
			autoFollow:     true,
			partialLine:    "incomplete line",
			allLines:       []lineInfo{},
		}
		output := m.View()
		if !strings.Contains(output, "incomplete line") {
			t.Errorf("view should render partial line, got: %q", output)
		}
	})

	t.Run("view with prompting marker", func(t *testing.T) {
		m := model{
			filename:        "/tmp/test.log",
			terminalHeight:  10,
			terminalWidth:   80,
			lastUpdate:      time.Now(),
			promptingMarker: true,
			markerInput:     "testing",
		}
		output := m.View()
		if !strings.Contains(output, "Enter marker message: testing_") {
			t.Errorf("view missing marker prompt, got: %q", output)
		}
	})

	t.Run("view with timestamps", func(t *testing.T) {
		now := time.Now()
		m := model{
			filename:       "/tmp/test.log",
			terminalHeight: 10,
			terminalWidth:  80,
			lastUpdate:     now,
			autoFollow:     true,
			showTimestamp:  true,
			timestampFmt:   "060102-150405",
			allLines: []lineInfo{
				{content: "hello", age: 0, timestamp: now},
			},
		}
		output := m.View()
		ts := now.Format("060102-150405")
		if !strings.Contains(output, ts) {
			t.Errorf("view missing timestamp %q, got: %q", ts, output)
		}
	})

	t.Run("view with follow indicator", func(t *testing.T) {
		m := model{
			filename:       "/tmp/test.log",
			terminalHeight: 10,
			terminalWidth:  80,
			lastUpdate:     time.Now(),
			autoFollow:     true,
		}
		output := m.View()
		if !strings.Contains(output, "[FOLLOW]") {
			t.Errorf("view missing FOLLOW indicator, got: %q", output)
		}
	})

	t.Run("view without follow indicator", func(t *testing.T) {
		m := model{
			filename:       "/tmp/test.log",
			terminalHeight: 10,
			terminalWidth:  80,
			lastUpdate:     time.Now(),
			autoFollow:     false,
		}
		output := m.View()
		if strings.Contains(output, "[FOLLOW]") {
			t.Errorf("view should not contain FOLLOW indicator, got: %q", output)
		}
	})
}

func TestModelUpdateKeyHandling(t *testing.T) {
	t.Run("q quits when autoFollow", func(t *testing.T) {
		m := model{terminalHeight: 24, autoFollow: true}
		w := &fakeWatcher{}
		m.watcher = w.watcher()
		_, cmd := m.Update(teaKeyMsg("q"))
		if !isQuitCmd(cmd) {
			t.Error("q should quit when following")
		}
	})

	t.Run("q resumes follow when scrolled up", func(t *testing.T) {
		m := model{
			terminalHeight: 24,
			autoFollow:     false,
			verticalOffset: 5,
			allLines:       makeLines(30),
		}
		newM, cmd := m.Update(teaKeyMsg("q"))
		nm := newM.(model)
		if !nm.autoFollow {
			t.Errorf("q should re-enable autoFollow")
		}
		if cmd != nil {
			t.Errorf("q should not quit when not following, got cmd")
		}
	})

	t.Run("ctrl+c quits", func(t *testing.T) {
		m := model{terminalHeight: 24}
		w := &fakeWatcher{}
		m.watcher = w.watcher()
		_, cmd := m.Update(teaKeyMsg("ctrl+c"))
		if !isQuitCmd(cmd) {
			t.Error("ctrl+c should quit")
		}
	})

	t.Run("up scroll disables follow", func(t *testing.T) {
		m := model{terminalHeight: 24, verticalOffset: 10, autoFollow: true, allLines: makeLines(50)}
		newM, _ := m.Update(teaKeyMsg("up"))
		nm := newM.(model)
		if nm.autoFollow {
			t.Error("up should disable autoFollow")
		}
		if nm.verticalOffset != 9 {
			t.Errorf("up should decrement offset: got %d", nm.verticalOffset)
		}
	})

	t.Run("down scroll re-enables follow at bottom", func(t *testing.T) {
		m := model{
			terminalHeight: 3,
			verticalOffset: 10,
			autoFollow:     false,
			allLines:       makeLines(12),
		}
		newM, _ := m.Update(teaKeyMsg("down"))
		nm := newM.(model)
		if !nm.autoFollow {
			t.Error("down should re-enable autoFollow at bottom")
		}
	})

	t.Run("left horizontal scroll clamp", func(t *testing.T) {
		m := model{horizontalOffset: 0}
		newM, _ := m.Update(teaKeyMsg("left"))
		nm := newM.(model)
		if nm.horizontalOffset != 0 {
			t.Errorf("left should not go below 0, got %d", nm.horizontalOffset)
		}
	})

	t.Run("right horizontal scroll", func(t *testing.T) {
		m := model{horizontalOffset: 0}
		newM, _ := m.Update(teaKeyMsg("right"))
		nm := newM.(model)
		if nm.horizontalOffset != 1 {
			t.Errorf("right should increment, got %d", nm.horizontalOffset)
		}
	})

	t.Run("esc resumes follow mode", func(t *testing.T) {
		m := model{
			terminalHeight: 24,
			autoFollow:     false,
			verticalOffset: 5,
			allLines:       makeLines(50),
		}
		newM, _ := m.Update(teaKeyMsg("esc"))
		nm := newM.(model)
		if !nm.autoFollow {
			t.Error("esc should re-enable autoFollow")
		}
	})

	t.Run("g goes to top", func(t *testing.T) {
		m := model{
			terminalHeight: 24,
			verticalOffset: 10,
			autoFollow:     true,
			allLines:       makeLines(50),
		}
		newM, _ := m.Update(teaKeyMsg("g"))
		nm := newM.(model)
		if nm.verticalOffset != 0 {
			t.Errorf("g should go to top, got offset %d", nm.verticalOffset)
		}
		if nm.autoFollow {
			t.Error("g should disable autoFollow")
		}
	})

	t.Run("G goes to bottom", func(t *testing.T) {
		m := model{
			terminalHeight: 10,
			verticalOffset: 0,
			autoFollow:     false,
			allLines:       makeLines(50),
		}
		newM, _ := m.Update(teaKeyMsg("G"))
		nm := newM.(model)
		maxOffset := 50 - 9
		if nm.verticalOffset != maxOffset {
			t.Errorf("G should go to bottom, expected offset %d, got %d", maxOffset, nm.verticalOffset)
		}
		if !nm.autoFollow {
			t.Error("G should enable autoFollow")
		}
	})

	t.Run("enter adds marker in follow mode", func(t *testing.T) {
		m := model{
			terminalHeight: 24,
			autoFollow:     true,
		}
		newM, _ := m.Update(teaKeyMsg("enter"))
		nm := newM.(model)
		if len(nm.allLines) != 1 {
			t.Fatalf("expected 1 line, got %d", len(nm.allLines))
		}
		if nm.allLines[0].content != "MARKER" {
			t.Errorf("expected 'MARKER', got %q", nm.allLines[0].content)
		}
	})

	t.Run("enter does nothing without follow", func(t *testing.T) {
		m := model{
			terminalHeight: 24,
			autoFollow:     false,
		}
		newM, _ := m.Update(teaKeyMsg("enter"))
		nm := newM.(model)
		if len(nm.allLines) != 0 {
			t.Errorf("enter without follow should not add marker, got %d lines", len(nm.allLines))
		}
	})

	t.Run("m activates marker prompt", func(t *testing.T) {
		m := model{}
		newM, _ := m.Update(teaKeyMsg("m"))
		nm := newM.(model)
		if !nm.promptingMarker {
			t.Error("m should activate marker prompt")
		}
	})
}

func TestModelUpdateMarkerPrompt(t *testing.T) {
	t.Run("typing in marker prompt", func(t *testing.T) {
		m := model{promptingMarker: true, markerInput: ""}
		newM, _ := m.Update(teaKeyMsg("a"))
		nm := newM.(model)
		if nm.markerInput != "a" {
			t.Errorf("expected 'a', got %q", nm.markerInput)
		}
	})

	t.Run("L1: multi-byte char accepted in marker prompt", func(t *testing.T) {
		m := model{promptingMarker: true, markerInput: ""}
		newM, _ := m.Update(teaKeyMsg("é"))
		nm := newM.(model)
		if nm.markerInput != "é" {
			t.Errorf("L1 BUG: multi-byte char 'é' should be accepted, got %q", nm.markerInput)
		}
	})

	t.Run("backspace removes character", func(t *testing.T) {
		m := model{promptingMarker: true, markerInput: "abc"}
		newM, _ := m.Update(teaKeyMsg("backspace"))
		nm := newM.(model)
		if nm.markerInput != "ab" {
			t.Errorf("expected 'ab', got %q", nm.markerInput)
		}
	})

	t.Run("backspace on multi-byte char", func(t *testing.T) {
		m := model{promptingMarker: true, markerInput: "héllo"}
		newM, _ := m.Update(teaKeyMsg("backspace"))
		nm := newM.(model)
		if nm.markerInput != "héll" {
			t.Errorf("expected 'héll', got %q", nm.markerInput)
		}
	})

	t.Run("enter confirms marker", func(t *testing.T) {
		m := model{
			promptingMarker:  true,
			markerInput:      "my marker",
			autoFollow:       true,
			terminalHeight:   24,
		}
		newM, _ := m.Update(teaKeyMsg("enter"))
		nm := newM.(model)
		if nm.promptingMarker {
			t.Error("prompting should be false after confirm")
		}
		if len(nm.allLines) != 1 {
			t.Fatalf("expected 1 line, got %d", len(nm.allLines))
		}
		if nm.allLines[0].content != "MARKER: my marker" {
			t.Errorf("expected 'MARKER: my marker', got %q", nm.allLines[0].content)
		}
	})

	t.Run("esc cancels marker", func(t *testing.T) {
		m := model{promptingMarker: true, markerInput: "cancel this"}
		newM, _ := m.Update(teaKeyMsg("esc"))
		nm := newM.(model)
		if nm.promptingMarker {
			t.Error("prompting should be false after cancel")
		}
		if nm.markerInput != "" {
			t.Errorf("markerInput should be cleared, got %q", nm.markerInput)
		}
		if len(nm.allLines) != 0 {
			t.Errorf("no marker should be added, got %d lines", len(nm.allLines))
		}
	})
}

func TestModelUpdateFileUpdate(t *testing.T) {
	t.Run("fileUpdateMsg appends lines", func(t *testing.T) {
		w := &fakeWatcher{}
		m := model{
			filename:       "/tmp/test.log",
			terminalHeight: 24,
			autoFollow:     true,
			watcher:        w.watcher(),
		}
		msg := fileUpdateMsg{
			lines:       []string{"new line 1", "new line 2"},
			partialLine: "",
			event:       eventNone,
			eventTime:   time.Now(),
		}
		newM, cmd := m.Update(msg)
		nm := newM.(model)
		if len(nm.allLines) != 2 {
			t.Errorf("expected 2 lines, got %d", len(nm.allLines))
		}
		if nm.allLines[0].content != "new line 1" {
			t.Errorf("expected 'new line 1', got %q", nm.allLines[0].content)
		}
		if cmd == nil {
			t.Error("should return waitForFileChanges cmd")
		}
	})

	t.Run("fileUpdateMsg sets partial line", func(t *testing.T) {
		m := model{terminalHeight: 24}
		w := &fakeWatcher{}
		m.watcher = w.watcher()
		msg := fileUpdateMsg{
			lines:       nil,
			partialLine: "incomplete",
			event:       eventNone,
			eventTime:   time.Now(),
		}
		newM, _ := m.Update(msg)
		nm := newM.(model)
		if nm.partialLine != "incomplete" {
			t.Errorf("partialLine should be 'incomplete', got %q", nm.partialLine)
		}
	})

	t.Run("fileUpdateMsg with truncation event", func(t *testing.T) {
		m := model{terminalHeight: 24}
		w := &fakeWatcher{}
		m.watcher = w.watcher()
		msg := fileUpdateMsg{
			lines:       []string{"after trunc"},
			partialLine: "",
			event:       eventTruncate,
			eventTime:   time.Now(),
		}
		newM, _ := m.Update(msg)
		nm := newM.(model)
		hasTruncation := false
		for _, l := range nm.allLines {
			if l.event == eventTruncate {
				hasTruncation = true
				break
			}
		}
		if !hasTruncation {
			t.Error("truncation event marker not found in lines")
		}
	})
}

func TestModelUpdateWindowSize(t *testing.T) {
	t.Run("window size with autoFollow", func(t *testing.T) {
		m := model{
			terminalHeight: 24,
			autoFollow:     true,
			verticalOffset: 10,
			allLines:       makeLines(100),
		}
		newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		nm := newM.(model)
		if nm.terminalHeight != 40 {
			t.Errorf("height should be 40, got %d", nm.terminalHeight)
		}
		if nm.terminalWidth != 100 {
			t.Errorf("width should be 100, got %d", nm.terminalWidth)
		}
		expectedOffset := 100 - (40 - 1)
		if nm.verticalOffset != expectedOffset {
			t.Errorf("autoFollow offset should be %d, got %d", expectedOffset, nm.verticalOffset)
		}
	})

	t.Run("window size without autoFollow clamps offset", func(t *testing.T) {
		m := model{
			terminalHeight: 24,
			autoFollow:     false,
			verticalOffset: 200,
			allLines:       makeLines(100),
		}
		newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
		nm := newM.(model)
		maxOffset := 100 - 9
		if nm.verticalOffset != maxOffset {
			t.Errorf("offset should be clamped to %d, got %d", maxOffset, nm.verticalOffset)
		}
	})
}

func appendContent(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("appendContent: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("appendContent write: %v", err)
	}
}

func makeLines(n int) []lineInfo {
	lines := make([]lineInfo, n)
	for i := range lines {
		lines[i] = lineInfo{content: fmt.Sprintf("line %d", i), age: 1}
	}
	return lines
}

func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	return isQuitMsg(msg)
}

func isQuitMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func teaKeyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

type fakeWatcher struct {
	w *fsnotify.Watcher
}

func (f *fakeWatcher) watcher() *fsnotify.Watcher {
	if f.w == nil {
		w, _ := fsnotify.NewWatcher()
		f.w = w
	}
	return f.w
}
