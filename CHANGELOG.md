# Changelog

## v0.2

### Unicode and text handling

- Cursor positions are rune-based instead of byte-based, so editing UTF-8 text works correctly.
- Wide characters (CJK) are rendered with proper display width (`go-runewidth`).
- UTF-8 BOM is preserved across load/save.
- Latin-1 files are decoded for editing and written back as Latin-1.
- Binary files are detected and opened read-only with a warning.
- Find/replace now maps byte offsets to rune offsets correctly.
- Fixed paste handling issues with multi-byte characters.

### Inline image viewer

- Added inline image tabs with automatic protocol selection:
  - Kitty (Kitty, WezTerm, Ghostty),
  - iTerm2 inline images (iTerm2, mintty),
  - SIXEL (foot, Konsole, Windows Terminal, xterm),
  - half-block fallback.
- Supported formats: PNG, JPEG, GIF, BMP, TIFF, WebP, ICO.
- Images scale to fit the editor area while preserving aspect ratio.
- Image tabs integrate with normal tab workflow and shortcuts.

### UI and workflow

- Settings moved from a centered modal to a right sidebar.
- Settings/command palette now close on `Ctrl+E` and `Ctrl+T` before those actions run.
- Command palette shortcuts updated:
  - `Ctrl+P` opens/toggles command palette,
  - `Ctrl+Shift+P` remains as an alternate,
  - `Ctrl+Space` binding removed.
- Quick Open is still available from inside the command palette.
- Palette shortcuts now work correctly in image tabs.

### Clipboard and terminal paste

- Added layered clipboard sync (`clipboardx`) across system clipboard, wl-copy/xclip/xsel, and OSC 52.
- Added terminal paste shortcuts: `Shift+Insert` and `Ctrl+Shift+V`.
- Editor and terminal now use the same clipboard layer.

### Installer

- Installer now always prints the `source` command needed to refresh `PATH`.
- Post-install next-step output was cleaned up and made more visible.

### Fixes

- Fixed find/replace not scrolling to matches in some cases.
- Fixed protocol image redraw issues around overlays:
  - overlays getting overpainted,
  - image erasure after closing overlays,
  - image bleed-through in blank overlay areas,
  - first-open overlay frame missing text/border.
- Fixed a sudo-save bug where password input could be written into the file.
  - Save now authenticates (`sudo -S -k -p '' -v`) and writes (`sudo -n tee`) in separate steps.

### Known issue

- SIXEL sizing over Windows Terminal + SSH can still be off in some environments.

## v0.1

Initial release.
