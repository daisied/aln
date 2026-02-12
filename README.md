# ALN

<p align="center">
  <img src="https://placehold.co/1200x520?text=ALN+Editor+Hero+Screenshot" alt="ALN hero screenshot" />
</p>

<p align="center">
  A fast terminal-native editor with modern workflows: LSP, fuzzy navigation, integrated terminal, multi-cursor editing, and safe recovery/session restore.
</p>

---

## Quick setup

> Replace `<owner>/<repo>` with your GitHub repository path.

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/scripts/install.sh | ALN_REPO=<owner>/<repo> bash
```

This installs `aln` to `~/.local/bin` and configures your shell PATH if needed.

---

## Why ALN

ALN is a strong alternative when you want:
- **Nano simplicity** with significantly more power.
- **Vim-speed terminal workflow** without modal complexity.
- **VS Code-style conveniences** (quick open, command palette, search, tabs, preview tabs, LSP) directly in the terminal.

It is designed for developers who spend serious time in terminal sessions, SSH, tmux, or remote environments and still want modern editing features.

---

## Screenshots

<p align="center">
  <img src="https://placehold.co/1200x700?text=Editor+View+(Tree+Tabs+Status)" alt="Editor view screenshot" />
</p>

<p align="center">
  <img src="https://placehold.co/1200x700?text=Command+Palette+%2B+Project+Search" alt="Command palette screenshot" />
</p>

<p align="center">
  <img src="https://placehold.co/1200x700?text=Integrated+Terminal+%2B+Scrollback" alt="Terminal screenshot" />
</p>

---

## Feature highlights

### Editing
- Multi-tab editing with preview tabs (single-click opens preview, edits pin tab)
- Undo/redo with operation stack
- Multi-cursor editing:
  - Add next occurrence (`Ctrl+D`)
  - Vertical multi-cursor via middle/right drag
- Auto-close pairs for brackets/quotes with smart swallow behavior
- Wrap selected text with quotes (`'`, `"`, `` ` ``)
- Smart newline indentation and indentation detection
- Indent/dedent selection, move line up/down, duplicate line
- Toggle line comments based on detected language
- Code folding based on indentation (`Ctrl+.`)
- Word movement/deletion (`Ctrl/Alt+Arrow`, `Ctrl+Backspace`, `Ctrl+Delete`)
- Binary-file safety (opens read-only) and large-file warnings
- Line ending + encoding detection/preservation

### Navigation & search
- Quick Open (`Ctrl+P`) with fuzzy ranking
- Command Palette (`Ctrl+Shift+P` / `Ctrl+Space`)
- Project-wide search from palette with `%query` (ripgrep + grep fallback)
- Find (`Ctrl+F`) and Find/Replace (`Ctrl+R`) with regex support
- Match navigation (`F3`, `Shift+F3`)
- Go to line (`Ctrl+G`)
- Bracket matching jump (`Ctrl+]`)
- Per-tab view state persistence (scroll/cursor)

### IDE-like capabilities
- LSP integration for:
  - Completion popup
  - Diagnostics (errors/warnings in status)
  - Go to definition (`F12`)
  - Rename symbol (`F2`)
- Language-aware defaults for tab size and tabs/spaces
- `.editorconfig` support
- Syntax highlighting via Chroma

### UI & workflow
- Integrated file tree with:
  - Expand/collapse
  - New file/dir, rename, delete
  - Selection sync with active file
- Integrated terminal with PTY:
  - ANSI/CSI/OSC support
  - Alternate screen support (vim/htop)
  - Mouse selection + copy
  - Scrollback view
  - Resize + reflow behavior
- Configurable themes and settings dialog (`Alt+,`)
- Mouse support across editor, tabs, tree, and terminal

### Reliability features
- File watcher with external-change reload handling
- Autosave crash backups (`~/.local/share/aln/backups`)
- Session restore per working directory (`~/.local/share/aln/sessions`)
- Clean shutdown of terminal and language servers

---

## Installation

### Option 1: One-command installer (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/scripts/install.sh | ALN_REPO=<owner>/<repo> bash
```

Optional environment variables:
- `ALN_REPO` (required if script default is not updated)
- `ALN_VERSION` (example: `v1.2.3`, default: latest)
- `ALN_INSTALL_DIR` (default: `~/.local/bin`)
- `ALN_BINARY_NAME` (default: `aln`)

### Option 2: Build from source

```bash
git clone https://github.com/<owner>/<repo>.git
cd <repo>
make build
make install
```

### Option 3: Manual release install

1. Download the release asset for your OS/arch.
2. Make it executable: `chmod +x aln`
3. Move it to `~/.local/bin/aln`
4. Ensure `~/.local/bin` is on your `PATH`.

---

## Usage

```bash
aln                # Open in current directory
aln .              # Open in current directory
aln path/to/file   # Open specific file
aln path/to/dir    # Start in directory
```

---

## Keybindings

### Core
- `Ctrl+S` save
- `Ctrl+Q` quit (press twice if unsaved)
- `Ctrl+H` / `F1` help
- `Alt+,` settings

### Files / tabs
- `Ctrl+N` new file
- `Ctrl+W` close tab
- `Ctrl+Tab` / `Ctrl+Shift+Tab` next/prev tab
- `Alt+1..9`, `Alt+0` jump to tab

### Editing
- `Ctrl+Z` / `Ctrl+Shift+Z` undo/redo
- `Ctrl+C`, `Ctrl+X`, `Ctrl+V` copy/cut/paste
- `Ctrl+A` select all
- `Ctrl+D` add next occurrence (multi-cursor)
- `Ctrl+/` toggle comment
- `Alt+Up/Down` move line
- `Tab` / `Shift+Tab` indent/dedent
- `Ctrl+Backspace` / `Ctrl+Delete` delete word

### Navigation
- `Ctrl+F` find
- `Ctrl+R` find/replace
- `F3` / `Shift+F3` next/prev match
- `Ctrl+G` go to line
- `Ctrl+]` matching bracket
- `Ctrl/Alt+Left/Right` word movement

### Panels
- `Ctrl+B` toggle file tree
- `Ctrl+E` focus tree/editor
- `Ctrl+T` toggle terminal
- `Ctrl+Shift+P` or `Ctrl+Space` command palette
- `Ctrl+.` toggle fold
- `Alt+Z` toggle word wrap

---

## Configuration

Settings file: `~/.config/aln/settings.json`

Configurable options:
- Theme (`dark`, `light`, `monokai`, `nord`, `solarized-dark`, `gruvbox`, `gruvbox-light`, `dracula`, `one-dark`, `tokyo-night`, `catppuccin`)
- Space size (`2`, `4`, `8`)
- Tree width (`20`, `24`, `30`, `40`)
- Terminal ratio (`0.20`, `0.30`, `0.40`, `0.50`)
- Word wrap toggle
- Auto-close toggle
- Quote wrap selection toggle
- Trim trailing whitespace on save
- Insert final newline on save

---

## LSP server prerequisites (optional, for IDE features)

Install any language servers you use:
- Go: `gopls`
- Python: `pyright-langserver`
- TypeScript/JavaScript: `typescript-language-server`
- Rust: `rust-analyzer`
- C/C++: `clangd`

ALN starts available servers automatically.

---

## Development

```bash
make build
make install
make clean
go test ./...
```

