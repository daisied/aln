# aln - Terminal Text Editor

A terminal-based text editor written in Go using tcell for rendering and terminal emulation.

## Build Commands

```bash
# Build the editor binary
make build

# Install to ~/.local/bin/aln
make install

# Clean build artifacts
make clean

# Direct build without make
go build -o editor-bin .
```

## Usage

```bash
aln [file...]        # Open specific file(s)
aln [directory]      # Open editor in directory
aln .                # Open editor in current directory
aln                  # Open editor in current directory
```

## Architecture Overview

### Core Components

The editor follows a component-based architecture with clear separation of concerns:

1. **Editor Layer** (`editor/`)
   - `editor.go`: Main editor orchestration, file watching, component lifecycle
   - `input.go`: Keyboard and mouse input routing to components
   - `render.go`: Screen rendering and layout calculations
   - Manages focus between file tree, editor, and terminal

2. **Buffer Layer** (`buffer/`)
   - `buffer.go`: Text manipulation, file I/O, content operations
   - `cursor.go`: Cursor positioning and selection management
   - `undo.go`: Undo/redo stack implementation
   - Each open file has its own Buffer instance

3. **UI Components** (`ui/`)
   - `terminal.go`: Embedded PTY-based terminal with ANSI parsing
   - `filetree.go`: File browser sidebar
   - `tabbar.go`: Tab management UI
   - `statusbar.go`: Status line display
   - `dialog.go`: Modal dialogs (find, goto, save confirm, help)

4. **Supporting Systems**
   - `highlight/`: Syntax highlighting using Chroma lexer
   - `config/`: Editor configuration (tab size, shell, layout)

### Component Interface

All UI components implement this interface (defined in `editor/editor.go`):

```go
type Component interface {
    Render(screen tcell.Screen, x, y, width, height int)
    HandleKey(ev *tcell.EventKey) bool
    HandleMouse(ev *tcell.EventMouse) bool
    IsFocused() bool
    SetFocused(bool)
}
```

### Event Flow

The editor uses tcell's event loop pattern:
- Main loop in `editor.Run()` polls for events
- Events are routed based on priority: Dialog → Terminal (if focused) → File tree (if focused) → Editor
- Custom events (`FileWatchEvent`, `TermOutputEvent`) integrate with tcell's event system via `PostEvent()`

### View State Management

Each buffer has an associated `EditorView` that tracks:
- Scroll position (scrollY, scrollX)
- Stored in `Editor.views` map, keyed by buffer pointer
- View state persists when switching tabs

### File Watching

The editor implements automatic file reload using fsnotify:
- Watches the working directory recursively (excludes `.git`, `node_modules`, etc.)
- Debounces events (100ms) to reduce noise
- Auto-reloads files when modified externally (if buffer is clean)
- Shows warning if file changes when buffer has unsaved changes
- Auto-refreshes file tree on filesystem changes

## Key Conventions

### Focus Management

Focus is tracked via `Editor.focusTarget` string: `"editor"`, `"tree"`, or `"terminal"`. Components query `IsFocused()` to determine behavior. Toggle keys:
- `Ctrl+B`: Toggle file tree visibility
- `Ctrl+T`: Toggle terminal visibility
- `Ctrl+E`: Toggle focus between file tree and editor (opens tree if closed)
- `Esc`: Return focus to editor (from tree/terminal)

When a file is opened via the file tree (Enter key), focus automatically switches back to the editor.

### Buffer Dirty State

`Buffer.Dirty` tracks unsaved changes. When closing a tab with dirty buffer, `ui.SaveConfirmDialog` prompts user (y/n/cancel). Tab bar shows modified indicator.

### Undo Operations

Operations are recorded as `OpInsert` or `OpDelete` with position and text. Undo/redo applies inverse operations. See `buffer/undo.go` for stack implementation.

### Syntax Highlighting Cache

`Highlighter.cache` uses key format: `"lang:startLine:endLine:contentHash"`. Cache is invalidated on buffer modification. Highlighting is viewport-based (only visible lines + context).

### Terminal PTY Integration

Terminal component manages a PTY subprocess:
- Runs shell from `Config.Shell` (defaults to `$SHELL`)
- Custom ANSI parser handles escape sequences
- Output is posted to main event loop as `TermOutputEvent`
- Supports alternate screen buffer, scrollback, bracketed paste

### Comment Toggling

`Editor.commentString()` returns language-specific comment prefix based on `Buffer.Language`. Used by `Ctrl+/` to toggle line comments. Supports `//`, `#`, `--`, `;`, `<!--`, `/*` based on file type.

### Language Detection

`highlight.DetectLanguage(path)` uses file extension to determine lexer. Language name is stored in `Buffer.Language` and displayed in status bar.

## Keybindings

### Global
- `Ctrl+Q`: Quit (prompts if unsaved changes)
- `Ctrl+S`: Save current file
- `Ctrl+H` / `F1`: Help dialog
- `Ctrl+,`: Toggle settings dialog (opens/closes)

### Navigation
- `Ctrl+B`: Toggle file tree visibility
- `Ctrl+E`: Switch focus between file tree and editor
- `Ctrl+T`: Toggle terminal
- Arrow keys: Navigate in editor/tree/terminal
- `Ctrl+Left/Right`: Word movement
- `Ctrl+G`: Go to line dialog

### Editing
- `Ctrl+Z`: Undo
- `Ctrl+Y`: Redo
- `Ctrl+C`: Copy selection
- `Ctrl+V`: Paste
- `Ctrl+X`: Cut selection
- `Ctrl+A`: Select all
- `Ctrl+/`: Toggle line comment
- `Ctrl+D`: Duplicate line
- `Ctrl+Backspace`: Delete word backward
- `Ctrl+Delete`: Delete word forward
- `Shift+Arrows`: Create/extend character selection
- `Ctrl+Shift+Left/Right`: Create/extend word selection
- `Tab`: Indent (or insert spaces if no selection)
- `Shift+Tab`: Dedent selection

### Tabs
- `Ctrl+N`: New buffer
- `Ctrl+W`: Close current tab
- `Ctrl+Tab`: Next tab
- `Ctrl+Shift+Tab`: Previous tab
- Middle-click on tab: Close tab

### File Tree
- `Ctrl+E`: Switch focus to/from file tree
- `Up/Down`: Navigate through files and directories
- `Enter`: Open file (switches focus to editor) or toggle directory
- `Left/Right`: Collapse/expand directories
- `Space`: Toggle directory expansion
- `Esc`: Return focus to editor
- Mouse click: Select/open

### Find Dialog
- `Ctrl+F`: Open find dialog
- `F3` / `Ctrl+N`: Next match
- `Shift+F3` / `Ctrl+P`: Previous match
- `Esc`: Close dialog

## Module Structure

Module name: `editor`

Internal imports use `editor/` prefix:
```go
import (
    "editor/buffer"
    "editor/config"
    "editor/editor"
    "editor/highlight"
    "editor/ui"
)
```

## Configuration

Settings are stored in `~/.config/aln/settings.json` and loaded at startup. Configuration includes:
- **Theme**: Color scheme (dark, light, monokai, nord, solarized-dark)
- **Space Size**: Number of spaces for indentation (2, 4, 8)
- **Tree Width**: File tree width in characters (20, 24, 30, 40)
- **Terminal Ratio**: Terminal height as fraction of screen (0.20-0.50)

Access via **Ctrl+,** to toggle settings dialog. Use **←/→ arrow keys** or **Enter** to cycle through values. Use **↑/↓** to navigate between settings. Changes save immediately. Press **ESC** or **Ctrl+,** again to close.

The `config.ColorScheme` type defines all UI colors used throughout the editor.

## Third-Party Dependencies

Key libraries:
- `github.com/gdamore/tcell/v2`: Terminal rendering and input
- `github.com/alecthomas/chroma/v2`: Syntax highlighting lexers
- `github.com/creack/pty`: PTY management for embedded terminal
- `github.com/fsnotify/fsnotify`: File system event monitoring
- `github.com/atotto/clipboard`: System clipboard integration
