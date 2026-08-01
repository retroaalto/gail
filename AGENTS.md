# Agent Notes: gail

Single-file Go TUI (`main.go`) — a `tail -f` viewer using Bubble Tea + Lipgloss + fsnotify.

## Build
- **Preferred:** `./build.sh` (injects version from git tags via `-X main.Version=…`)
- `make build` and `make install` do the same.
- Plain `go build -o gail` works but `gail --version` will show `dev`.

## Test
- **No Go tests exist** (`*_test.go` none). `make test` runs `go test -v ./...` against empty test suite.
- `test.sh` is an **interactive manual test**: it writes to `test.log` and expects you to run `./gail test.log` in another terminal. It exercises truncation, recreation, long lines, and partial-line buffering.
- Because it’s a full-screen TUI (`tea.WithAltScreen()`), automated testing needs terminal emulation (tmux / headless TUI tooling).

## Flags / CLI
- All flags accept both single and double dash (e.g. `-t` and `--timestamp`).
- `-t` / `--timestamp` — prepend timestamp. Default format is `060102-150405` (YYMMDD-hhmmss).
- `--timestamp-format +%Y-%m-%d-%H:%M:%S` — accepts `+`-prefixed `date`-style specifiers and converts them to Go time formats internally.
- `-debug` — writes `gail_debug.log` in cwd with microsecond timings.
- `--version` — requires a build via `build.sh` or `make` to show real tag.

## Code quirks
- **Single source file:** all logic, model, update, view, and helpers are in `main.go` (~1400 lines).
- `q` behavior depends on follow mode: quits when following, otherwise returns to follow mode. `esc` also returns to follow mode. `ctrl+c` always quits.
- Partial lines (no trailing newline) are tracked and rendered with an amber `◐` indicator while still incomplete.
- `fileReader` struct (with internal `sync.Mutex`) tracks file read position across `waitForFileChanges` goroutine and `checkFileCmd` polling tick — both call `readNewLines`.
- `allLines` buffer is bounded to 10,000 lines (`maxLines` constant). Oldest lines are trimmed when exceeded, and `verticalOffset` is adjusted to prevent jank.
- `truncateToWidth` is rune-aware (not byte-based) for status bar truncation.
