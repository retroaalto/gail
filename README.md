# Gail

A graphical TUI tool similar to `tail -f` written in Go using [Bubbletea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).

## Features

- **Follow mode**: Watches files for changes and displays new lines in real-time
- **Visual indicators**: Sign column with colored dots that fade over time to highlight new entries
- **Event markers**: Special indicators for file events (truncation, recreation, etc.)
- **Scrolling**: Full vertical and horizontal scrolling support with vim keybindings
- **Dynamic resizing**: Automatically adapts to terminal size changes
- **Long line handling**: Truncates long lines with "..." and allows horizontal scrolling
- **Buffer navigation**: Shows current position in file with percentage
- **Status bar**: Shows filename, last update time, and buffer position
- **Timestamps**: Optional timestamp display with customizable format
- **Clean TUI**: Full-screen terminal interface with proper rendering

## Installation

### Homebrew

```bash
brew tap retroaalto/gail https://github.com/retroaalto/gail
brew trust retroaalto/gail
brew install gail
```

### From Source

Build with version information using the build script:

```bash
./build.sh build    # or just ./build.sh
```

Or install directly to your GOBIN:

```bash
./build.sh install
```

Alternative: Use make if you prefer:

```bash
make build          # make install
```

Manual build (without version info):

```bash
go build -o gail
```

## Usage

Check version:

```bash
gail --version
```

Basic usage:

```bash
gail filename.log
```

With timestamps (default format YYMMDD-hhmmss):

```bash
gail -t filename.log
gail --timestamp filename.log
```

With custom timestamp format:

```bash
gail -t --timestamp-format +%Y-%m-%d-%H:%M:%S filename.log
```

Note: All flags support both single dash (e.g., `-debug`) and double dash (e.g., `--debug`) formats.

### Timestamp Format

The timestamp format follows Go's time format conventions but accepts date command-style specifiers:

- `%y` - Year (2 digits)
- `%Y` - Year (4 digits)
- `%m` - Month
- `%d` - Day
- `%H` - Hour (24h)
- `%M` - Minute
- `%S` - Second
- `%I` - Hour (12h)
- `%p` - AM/PM

Default format: `YYMMDD-hhmmss` (060102-150405)

## Controls

### Navigation

**Vertical Scrolling:**
- `↑` or `k` - Scroll up one line
- `↓` or `j` - Scroll down one line
- `PgUp` or `Ctrl+b` - Scroll up one page
- `PgDn` or `Ctrl+f` - Scroll down one page
- `Ctrl+u` - Scroll up half a page
- `Ctrl+d` - Scroll down half a page
- `g` or `Home` - Go to top of buffer
- `G` or `End` - Go to bottom of buffer (re-enables follow mode)

**Horizontal Scrolling:**
- `←` or `h` - Scroll left
- `→` or `l` - Scroll right

**Markers:**
- `m` - Prompt for custom marker text
- `Enter` - Insert marker at current position (follow mode only)

**Other:**
- `q` - Quit (when following), resume follow mode (when scrolled up)
- `Esc` - Resume follow mode
- `Ctrl+C` - Quit

### Marker Prompt

When inserting a marker:
- Type text, then press `Enter` to confirm
- `Esc` or `Ctrl+C` to cancel
- `Backspace` to delete last character

### Follow Mode

The application starts in "follow mode" where it automatically scrolls to show new lines as they are added. When you scroll up manually, follow mode is disabled and indicated by the absence of `[FOLLOW]` in the status bar. To re-enable follow mode, press `Esc`, scroll to the bottom using `G` or `End`, or press `↓`/`j` until you reach the end.

## Event Markers

The sign column displays special indicators for file events:

| Marker | Color | Event |
|--------|-------|-------|
| `T` | Red | File truncated |
| `R` | Orange | File recreated |
| `X` | Magenta | File removed and recreated |
| `M` | Cyan | File renamed and recreated |
| `●` | Green→Gray | Normal line (fades over time) |

Event markers appear when:
- The log file is truncated (e.g., `> file.log`)
- The file is deleted and recreated
- Log rotation occurs
- The file is renamed

## How It Works

1. **Initial Display**: Shows the last 10 lines of the file
2. **File Watching**: Uses `fsnotify` to watch for file changes, with periodic polling as backup
3. **File Recreation**: Automatically handles file truncation and recreation (e.g., when log rotation occurs)
4. **New Line Highlighting**: New lines appear with bright green dots in the sign column
5. **Fade Effect**: Dots gradually fade to dark gray over 2.5 seconds (5 steps × 500ms)
6. **Buffer Management**: Stores all lines for scrolling through history
7. **Smart Truncation**: Long lines are truncated to fit terminal width with "..." indicator
8. **Status Bar**: Shows filename, last update time, current position (e.g., "15-25/100 (25%)"), and follow mode indicator

## Example

### Quick Start

```bash
# Build gail
go build -o gail

# Run the comprehensive test
./test.sh
# In another terminal: ./gail test.log
```

The test script will demonstrate all features:
- Normal line following with fade effects
- File truncation (RED T marker)
- File recreation (ORANGE R/MAGENTA X/CYAN M markers)
- Horizontal scrolling with long lines
- Vertical scrolling and buffer navigation

### Manual Test

```bash
# Create a test log file
echo "Line 1" > test.log

# Start gail in one terminal
gail -t test.log

# In another terminal, add lines
echo "Line 2" >> test.log
echo "Line 3" >> test.log
```

You'll see the new lines appear with bright green indicators that fade over time.

## Building from Source

```bash
git clone <repository>
cd gail
go mod download
./build.sh
```

The build script automatically injects version information from git tags. To create a tagged release:

```bash
git tag v0.1
git push --tags
./build.sh
./gail --version  # Shows: gail version v0.1
```

Build script commands:
```bash
./build.sh build    # Build the binary (default)
./build.sh install  # Install to GOBIN
./build.sh clean    # Remove build artifacts
./build.sh version  # Show current version
./build.sh help     # Show help
```

## Dependencies

- [Bubbletea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Style definitions
- [fsnotify](https://github.com/fsnotify/fsnotify) - File system notifications

## Troubleshooting

### File Not Updating After Recreation

`gail` automatically handles file recreation and truncation. The application:
- Detects when a file is truncated (file size becomes smaller)
- Handles file removal and recreation events
- Polls the file every 2 seconds as a backup mechanism

If you recreate a log file (e.g., `cat > file.log`), gail will automatically reload the content.

### Testing All Features

Use the comprehensive test script:
```bash
./test.sh
# In another terminal: ./gail test.log
```

This script tests all features in sequence:
1. Initial file creation and normal appends
2. File truncation with RED T marker
3. File recreation with ORANGE R/MAGENTA X/CYAN M markers
4. Long lines for horizontal scrolling
5. Multiple entries for vertical scrolling and buffer navigation

## License

MIT
