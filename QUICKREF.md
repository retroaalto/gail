# Gail - Quick Reference

## Keyboard Controls

```
Navigation:
  ↑  k          Scroll up one line
  ↓  j          Scroll down one line
  ←  h          Scroll left
  →  l          Scroll right
  
  PgUp  Ctrl+b  Scroll up one page
  PgDn  Ctrl+f  Scroll down one page
  
  g  Home       Go to top
  G  End        Go to bottom (enables follow mode)
  
Exit:
  q  Ctrl+c     Quit
```

## Status Bar Indicators

```
filename.log | Last update: 5s ago | 15-25/100 (25%) [FOLLOW]
└─────┬─────┘  └────────┬──────────┘  └─────┬─────┘  └───┬───┘
   file         time since update      position    follow mode
                                       in buffer    (auto-scroll)
```

## Usage Examples

```bash
# Basic - show last lines and follow
./gail application.log

# With timestamps
./gail -t application.log

# With custom timestamp format
./gail -t --timestamp-format +%Y-%m-%d-%H:%M:%S application.log
```

## Sign Column Markers

```
●  Green→Gray  Normal line (fades over time)
T  Red         File truncated
R  Orange      File recreated
X  Magenta     File removed/recreated
M  Cyan        File renamed/recreated
```

## Features

- Automatically follows new content when at bottom
- Disables follow when scrolling up
- Event markers for file truncation/recreation
- Truncates long lines with "..." 
- Shows position in buffer as percentage
- Adapts to terminal resize
- Fade effect on new lines (green → gray)
