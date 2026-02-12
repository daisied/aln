package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"editor/buffer"
	"editor/config"
	"editor/highlight"
	"editor/lsp"
	"editor/ui"

	"github.com/fsnotify/fsnotify"
	"github.com/gdamore/tcell/v2"
)

type Component interface {
	Render(screen tcell.Screen, x, y, width, height int)
	HandleKey(ev *tcell.EventKey) bool
	HandleMouse(ev *tcell.EventMouse) bool
	IsFocused() bool
	SetFocused(bool)
}

type Editor struct {
	screen    tcell.Screen
	buffers   []*buffer.Buffer
	activeTab int
	cfg       *config.Config

	fileTree       *ui.FileTree
	tabBar         *ui.TabBar
	terminal       *ui.Terminal
	statusBar      *ui.StatusBar
	dialog         *ui.Dialog
	quickOpen      *ui.QuickOpen
	commandPalette *ui.CommandPalette

	highlight *highlight.Highlighter

	termOpen  bool
	treeOpen  bool
	treeWidth int
	termRatio float64

	quit        bool
	quitPending bool   // true after first Ctrl+Q with unsaved changes
	focusTarget string // "editor", "tree", "terminal"

	// Editor view state per buffer
	views map[*buffer.Buffer]*EditorView

	// Mouse drag tracking
	mouseDown                bool
	mouseAnchor              buffer.Cursor
	mousePressX, mousePressY int // track where initial click happened

	// Middle mouse button multi-cursor tracking
	middleMouseDown   bool
	middleMouseAnchor buffer.Cursor
	middleMouseLine   int // track which line we last added a cursor on

	// Track whether scroll was caused by mouse wheel (skip ensureCursorVisible)
	mouseScrolling bool

	// Preview tab support (like VS Code: single-click opens preview, editing pins it)
	previewTab int // index of the preview tab, -1 if none

	// Git gutter
	gitGutter *GitGutter

	// File watching
	fileWatcher *fsnotify.Watcher
	watchedRoot string

	// LSP
	lspManager   *lsp.Manager
	autocomplete *ui.Autocomplete

	// Cursor blinking
	cursorVisible bool
	lastBlinkTime time.Time

	// Temporary status messages
	statusMessageTime    time.Time
	statusMessageIsError bool
}

type EditorView struct {
	scrollY int
	scrollX int
}

// FileWatchEvent carries file system change notifications to the main event loop.
type FileWatchEvent struct {
	tcell.EventTime
	Path string
	Op   fsnotify.Op
}

func New(cfg *config.Config) *Editor {
	return &Editor{
		cfg:         cfg,
		highlight:   highlight.New(),
		gitGutter:   NewGitGutter(),
		treeOpen:    true,
		treeWidth:   cfg.TreeWidth,
		termRatio:   cfg.TermRatio,
		focusTarget: "editor",
		views:       make(map[*buffer.Buffer]*EditorView),
		previewTab:  -1,
	}
}

func (e *Editor) Run(files []string, isDirOpen bool) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	// Don't defer Fini here - we'll call it manually with cleanup

	screen.EnableMouse()
	screen.EnablePaste()
	screen.SetStyle(tcell.StyleDefault)
	screen.Clear()

	e.screen = screen

	// Set up working directory
	cwd, _ := os.Getwd()

	// Initialize LSP manager
	e.lspManager = lsp.NewManager(cwd)

	// Initialize components
	e.tabBar = ui.NewTabBar()
	e.tabBar.OnSwitch = func(idx int) { e.switchTab(idx) }
	e.tabBar.OnClose = func(idx int) { e.closeTab(idx) }

	e.fileTree = ui.NewFileTree(cwd)
	e.fileTree.OnFileOpen = func(path string) {
		e.openFilePreview(path)
		// Switch focus to editor after opening file
		e.focusTarget = "editor"
		e.updateFocus()
	}
	e.fileTree.OnNewFile = func(dirPath string) {
		d := ui.NewInputDialog("New file: ")
		d.OnSubmit = func(name string) {
			if name == "" {
				e.dialog = nil
				return
			}
			path := filepath.Join(dirPath, name)
			f, err := os.Create(path)
			if err != nil {
				e.setTemporaryError("Error: " + err.Error())
			} else {
				f.Close()
				e.fileTree.Refresh()
				e.openFile(path)
				e.setTemporaryMessage("Created " + name)
			}
			e.dialog = nil
		}
		d.OnCancel = func() { e.dialog = nil }
		e.dialog = d
	}
	e.fileTree.OnNewDir = func(dirPath string) {
		d := ui.NewInputDialog("New directory: ")
		d.OnSubmit = func(name string) {
			if name == "" {
				e.dialog = nil
				return
			}
			path := filepath.Join(dirPath, name)
			err := os.MkdirAll(path, 0755)
			if err != nil {
				e.setTemporaryError("Error: " + err.Error())
			} else {
				e.fileTree.Refresh()
				e.setTemporaryMessage("Created " + name + "/")
			}
			e.dialog = nil
		}
		d.OnCancel = func() { e.dialog = nil }
		e.dialog = d
	}
	e.fileTree.OnDeleteFile = func(path string) {
		name := filepath.Base(path)
		d := ui.NewDeleteConfirmDialog(name)
		d.OnConfirm = func(answer rune) {
			if answer == 'y' {
				err := os.RemoveAll(path)
				if err != nil {
					e.setTemporaryError("Error: " + err.Error())
				} else {
					// Close buffer if open
					for i, buf := range e.buffers {
						if buf.Path == path {
							e.removeTab(i)
							break
						}
					}
					e.fileTree.Refresh()
					e.setTemporaryMessage("Deleted " + name)
				}
			}
			e.dialog = nil
		}
		e.dialog = d
	}
	e.fileTree.OnRenameFile = func(oldPath string) {
		d := ui.NewInputDialog("Rename: ")
		d.Input = filepath.Base(oldPath)
		d.Cursor = len([]rune(d.Input))
		d.OnSubmit = func(newName string) {
			if newName == "" || newName == filepath.Base(oldPath) {
				e.dialog = nil
				return
			}
			newPath := filepath.Join(filepath.Dir(oldPath), newName)
			err := os.Rename(oldPath, newPath)
			if err != nil {
				e.setTemporaryError("Error: " + err.Error())
			} else {
				// Update buffer path if open
				for i, buf := range e.buffers {
					if buf.Path == oldPath {
						buf.Path = newPath
						buf.Language = highlight.DetectLanguage(newPath)
						e.tabBar.Tabs[i].Title = newName
						e.tabBar.Tabs[i].Path = newPath
						break
					}
				}
				e.fileTree.Refresh()
				e.setTemporaryMessage("Renamed to " + newName)
			}
			e.dialog = nil
		}
		d.OnCancel = func() { e.dialog = nil }
		e.dialog = d
	}

	e.statusBar = ui.NewStatusBar()

	// Set up file watcher
	e.watchedRoot = cwd
	e.setupFileWatcher(screen)

	// Start auto-backup timer
	e.startBackupTimer()

	// Check for crash recovery backups
	backups := e.checkForBackups()
	if len(backups) > 0 {
		for _, info := range backups {
			e.recoverBackup(info)
		}
		e.statusBar.Message = fmt.Sprintf("Recovered %d backup(s) from previous session", len(backups))
	}

	// Open files from CLI args
	if len(files) > 0 {
		for _, f := range files {
			absPath, _ := filepath.Abs(f)
			e.openFile(absPath)
		}
	} else {
		// Try to restore previous session; fall back to empty buffer
		if !e.RestoreSession() {
			if !isDirOpen {
				e.openEmptyBuffer()
			} else {
				// We opened a directory but no specific files
				// Focus tree
				e.focusTarget = "tree"
			}
		}
	}

	e.updateFocus()

	// Initialize cursor blink
	e.cursorVisible = true
	e.lastBlinkTime = time.Now()
	blinkInterval := 500 * time.Millisecond

	// Main event loop with cursor blinking
	for !e.quit {
		// Clear expired status messages
		e.clearExpiredMessages()

		e.render()

		// Calculate time until next blink
		timeSinceBlink := time.Since(e.lastBlinkTime)
		timeUntilBlink := blinkInterval - timeSinceBlink
		if timeUntilBlink < 0 {
			timeUntilBlink = 0
		}

		// Poll with timeout for cursor blinking
		ev := screen.PollEvent()

		// Check if we should toggle cursor blink
		if time.Since(e.lastBlinkTime) >= blinkInterval {
			e.cursorVisible = !e.cursorVisible
			e.lastBlinkTime = time.Now()
		}

		switch ev := ev.(type) {
		case *tcell.EventResize:
			screen.Sync()
			if e.terminal != nil {
				_, _, termW, termH := e.termLayout()
				if termW > 0 && termH > 1 {
					e.terminal.Resize(termH-1, termW)
				}
			}
		case *tcell.EventKey:
			e.handleKey(ev)
		case *tcell.EventMouse:
			e.handleMouse(ev)
		case *ui.TermOutputEvent:
			if e.terminal != nil {
				e.terminal.ProcessOutput(ev.Data)
			}
		case *FileWatchEvent:
			e.handleFileWatchEvent(ev)
		}
	}

	// Save session before cleanup
	e.SaveSession()

	// Clean up file watcher
	if e.fileWatcher != nil {
		e.fileWatcher.Close()
	}

	// Clean up backups on clean exit
	e.cleanAllBackups()

	// Clean up terminal
	if e.terminal != nil {
		e.terminal.Close()
	}

	// Clean up LSP servers
	if e.lspManager != nil {
		e.lspManager.Close()
	}

	// Clear screen and reset terminal state before exiting
	screen.Clear()
	screen.Fini()

	return nil
}

func (e *Editor) openFile(path string) {
	// Check if already open
	for i, buf := range e.buffers {
		if buf.Path == path {
			e.switchTab(i)
			return
		}
	}

	// Check if file exists before loading
	fileExists := true
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fileExists = false
	}

	buf, err := buffer.NewBufferFromFile(path, e.cfg.TabSize)
	if err != nil {
		e.setTemporaryError("Error: " + err.Error())
		return
	}
	buf.Language = highlight.DetectLanguage(path)
	e.applyFileSettings(buf)
	e.buffers = append(e.buffers, buf)
	e.views[buf] = &EditorView{}
	e.tabBar.AddTab(path, false)
	e.activeTab = len(e.buffers) - 1
	e.gitGutter.Update(path)
	e.lspManager.DidOpen(buf.Language, path, strings.Join(buf.Lines, "\n"))

	// Sync file tree selection
	if e.treeOpen && e.fileTree != nil {
		e.fileTree.SelectPath(path)
	}

	// Set status message based on file state
	if !fileExists {
		e.statusBar.Message = fmt.Sprintf("New file: %s", filepath.Base(path))
	} else if buf.ReadOnly {
		e.statusBar.Message = "⚠ Binary file opened as read-only"
	} else if buf.FileSize > 10*1024*1024 {
		e.statusBar.Message = fmt.Sprintf("⚠ Large file (%d MB)", buf.FileSize/(1024*1024))
	} else {
		e.statusBar.Message = ""
	}
	e.updateStatus()
}

// openFilePreview opens a file in "preview" mode (like VS Code).
// If there's already a preview tab, it gets replaced. The tab is pinned
// when the user makes edits.
func (e *Editor) openFilePreview(path string) {
	// Check if already open
	for i, buf := range e.buffers {
		if buf.Path == path {
			e.switchTab(i)
			return
		}
	}

	// Check if file exists before loading
	fileExists := true
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fileExists = false
	}

	// If there's an existing preview tab, replace it
	if e.previewTab >= 0 && e.previewTab < len(e.buffers) {
		oldBuf := e.buffers[e.previewTab]
		if !oldBuf.Dirty {
			// Replace the preview tab content
			newBuf, err := buffer.NewBufferFromFile(path, e.cfg.TabSize)
			if err != nil {
				e.setTemporaryError("Error: " + err.Error())
				return
			}
			newBuf.Language = highlight.DetectLanguage(path)
			e.applyFileSettings(newBuf)
			delete(e.views, oldBuf)
			e.highlight.InvalidateCache(oldBuf.Path)
			e.buffers[e.previewTab] = newBuf
			e.views[newBuf] = &EditorView{}
			e.tabBar.Tabs[e.previewTab].Title = filepath.Base(path)
			e.tabBar.Tabs[e.previewTab].Path = path
			e.tabBar.Tabs[e.previewTab].Preview = true
			e.switchTab(e.previewTab)

			// Set status message
			if !fileExists {
				e.statusBar.Message = fmt.Sprintf("New file: %s", filepath.Base(path))
			} else {
				e.statusBar.Message = ""
			}
			return
		}
	}

	// Open as new preview tab
	buf, err := buffer.NewBufferFromFile(path, e.cfg.TabSize)
	if err != nil {
		e.setTemporaryError("Error: " + err.Error())
		return
	}
	buf.Language = highlight.DetectLanguage(path)
	e.applyFileSettings(buf)
	e.buffers = append(e.buffers, buf)
	e.views[buf] = &EditorView{}
	e.tabBar.AddTab(path, false)
	e.tabBar.Tabs[len(e.tabBar.Tabs)-1].Preview = true
	e.previewTab = len(e.buffers) - 1
	e.activeTab = e.previewTab
	e.gitGutter.Update(path)
	e.lspManager.DidOpen(buf.Language, path, strings.Join(buf.Lines, "\n"))

	// Sync file tree selection
	if e.treeOpen && e.fileTree != nil {
		e.fileTree.SelectPath(path)
	}

	// Set status message
	if !fileExists {
		e.statusBar.Message = fmt.Sprintf("New file: %s", filepath.Base(path))
	} else {
		e.statusBar.Message = ""
	}
	e.updateStatus()
}

func (e *Editor) openEmptyBuffer() {
	buf := buffer.NewBuffer(e.cfg.TabSize)
	buf.AutoCloseEnabled = e.cfg.AutoClose
	e.buffers = append(e.buffers, buf)
	e.views[buf] = &EditorView{}
	e.tabBar.AddTab("", false)
	e.activeTab = len(e.buffers) - 1
}

func (e *Editor) switchTab(idx int) {
	if idx >= 0 && idx < len(e.buffers) {
		// Clear multi-cursor state from previous buffer
		if e.activeTab >= 0 && e.activeTab < len(e.buffers) {
			e.buffers[e.activeTab].ClearExtraCursors()
		}

		e.activeTab = idx
		e.tabBar.Active = idx
		e.gitGutter.Update(e.buffers[idx].Path)
		e.updateStatus()

		// Sync file tree selection
		if e.treeOpen && e.fileTree != nil {
			e.fileTree.SelectPath(e.buffers[idx].Path)
		}
	}
}

func (e *Editor) closeTab(idx int) {
	if idx < 0 || idx >= len(e.buffers) {
		return
	}
	buf := e.buffers[idx]
	if buf.Dirty {
		name := filepath.Base(buf.Path)
		if name == "." || name == "" {
			name = "untitled"
		}
		e.dialog = ui.NewSaveConfirmDialog(name)
		e.dialog.OnConfirm = func(answer rune) {
			switch answer {
			case 'y':
				buf.SaveWithOptions(e.cfg.TrimTrailingSpace, e.cfg.InsertFinalNewline)
				buf.ExternallyModified = false
				e.removeTab(idx)
			case 'n':
				e.removeTab(idx)
			case 'c':
				// Cancel - do nothing
			}
			e.dialog = nil
		}
		return
	}
	e.removeTab(idx)
}

func (e *Editor) removeTab(idx int) {
	if idx < 0 || idx >= len(e.buffers) {
		return
	}
	buf := e.buffers[idx]
	delete(e.views, buf)
	e.highlight.InvalidateCache(buf.Path)
	e.buffers = append(e.buffers[:idx], e.buffers[idx+1:]...)
	e.tabBar.RemoveTab(idx)

	// Update preview tab index
	if e.previewTab == idx {
		e.previewTab = -1
	} else if e.previewTab > idx {
		e.previewTab--
	}

	if len(e.buffers) == 0 {
		e.quit = true
		return
	}
	if e.activeTab >= len(e.buffers) {
		e.activeTab = len(e.buffers) - 1
	}
	e.tabBar.Active = e.activeTab
	e.updateStatus()
}

func (e *Editor) activeBuffer() *buffer.Buffer {
	if e.activeTab >= 0 && e.activeTab < len(e.buffers) {
		return e.buffers[e.activeTab]
	}
	return nil
}

func (e *Editor) activeView() *EditorView {
	buf := e.activeBuffer()
	if buf == nil {
		return nil
	}
	return e.views[buf]
}

func (e *Editor) saveCurrentFile() {
	buf := e.activeBuffer()
	if buf == nil {
		return
	}
	if buf.Path == "" {
		e.openSaveAsDialog()
		return
	}
	err := buf.SaveWithOptions(e.cfg.TrimTrailingSpace, e.cfg.InsertFinalNewline)
	if err != nil {
		if os.IsPermission(err) {
			e.promptSudoSave(buf, buf.Path, nil)
			return
		}
		e.setTemporaryError("Error saving: " + err.Error())
	} else {
		e.onSaveSuccess(buf, "Saved "+filepath.Base(buf.Path))
	}
}

func (e *Editor) onSaveSuccess(buf *buffer.Buffer, message string) {
	e.setTemporaryMessage(message)
	buf.ExternallyModified = false
	e.tabBar.SetModified(e.activeTab, false)
	e.tabBar.SetExternallyModified(e.activeTab, false)
	e.cleanBackup(buf.Path)
	e.gitGutter.Update(buf.Path)
	e.lspManager.DidSave(buf.Path)
}

func (e *Editor) promptSudoSave(buf *buffer.Buffer, path string, onSuccess func()) {
	d := ui.NewInputDialog("sudo password: ")
	d.MaskInput = true
	d.OnSubmit = func(password string) {
		e.dialog = nil
		if password == "" {
			e.setTemporaryError("Save cancelled")
			return
		}
		if err := e.saveWithSudo(buf, path, password); err != nil {
			e.setTemporaryError("Error saving with sudo: " + err.Error())
			return
		}
		e.onSaveSuccess(buf, "Saved "+filepath.Base(path)+" (sudo)")
		if onSuccess != nil {
			onSuccess()
		}
	}
	d.OnCancel = func() { e.dialog = nil }
	e.dialog = d
}

func (e *Editor) saveWithSudo(buf *buffer.Buffer, path, password string) error {
	content := buf.BuildSaveContent(e.cfg.TrimTrailingSpace, e.cfg.InsertFinalNewline)

	cmd := exec.Command("sudo", "-S", "tee", path)
	cmd.Stdin = bytes.NewBufferString(password + "\n" + content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return errors.New(msg)
	}

	buf.MarkSaved()
	buf.LastSaveTime = time.Now()
	return nil
}

func (e *Editor) reloadFile() {
	buf := e.activeBuffer()
	if buf == nil {
		return
	}
	if buf.Path == "" {
		e.setTemporaryError("Cannot reload: no file path")
		return
	}

	// Check if file exists on disk
	if _, err := os.Stat(buf.Path); os.IsNotExist(err) {
		e.setTemporaryError("Cannot reload: file doesn't exist on disk")
		return
	}

	// If buffer has unsaved changes, prompt for confirmation
	if buf.Dirty {
		d := ui.NewReloadConfirmDialog(filepath.Base(buf.Path))
		d.OnConfirm = func(answer rune) {
			e.dialog = nil
			if answer == 'y' {
				e.performReload()
			}
		}
		e.dialog = d
	} else {
		// No unsaved changes, reload directly
		e.performReload()
	}
}

func (e *Editor) performReload() {
	buf := e.activeBuffer()
	if buf == nil || buf.Path == "" {
		return
	}

	// Save cursor position
	oldLine := buf.Cursor.Line
	oldCol := buf.Cursor.Col

	// Reload from disk
	newBuf, err := buffer.NewBufferFromFile(buf.Path, e.cfg.TabSize)
	if err != nil {
		e.setTemporaryError("Error reloading: " + err.Error())
		return
	}

	// Copy properties
	newBuf.Language = buf.Language
	e.applyFileSettings(newBuf)

	// Replace buffer in place
	*buf = *newBuf

	// Restore cursor position (clamped to new content)
	buf.Cursor.Line = oldLine
	buf.Cursor.Col = oldCol
	if buf.Cursor.Line >= len(buf.Lines) {
		buf.Cursor.Line = len(buf.Lines) - 1
	}
	if buf.Cursor.Line >= 0 && buf.Cursor.Line < len(buf.Lines) {
		if buf.Cursor.Col > len(buf.Lines[buf.Cursor.Line]) {
			buf.Cursor.Col = len(buf.Lines[buf.Cursor.Line])
		}
	}

	// Clear flags
	buf.Dirty = false
	buf.ExternallyModified = false

	// Update UI
	e.tabBar.SetModified(e.activeTab, false)
	e.tabBar.SetExternallyModified(e.activeTab, false)
	e.highlight.InvalidateCache(buf.Path)
	e.gitGutter.Update(buf.Path)
	e.setTemporaryMessage("Reloaded " + filepath.Base(buf.Path))
}

func (e *Editor) gotoDefinition() {
	buf := e.activeBuffer()
	if buf == nil || buf.Path == "" || e.lspManager == nil {
		return
	}
	loc := e.lspManager.Definition(buf.Language, buf.Path, buf.Cursor.Line, buf.Cursor.Col)
	if loc == nil {
		e.setTemporaryError("No definition found")
		return
	}
	path := lsp.URIToPath(loc.URI)
	if path == buf.Path {
		// Same file — just jump
		buf.Cursor.Line = loc.Range.Start.Line
		buf.Cursor.Col = loc.Range.Start.Character
		buf.Selection = nil
	} else {
		e.openFile(path)
		newBuf := e.activeBuffer()
		if newBuf != nil {
			newBuf.Cursor.Line = loc.Range.Start.Line
			newBuf.Cursor.Col = loc.Range.Start.Character
		}
	}
}

func (e *Editor) renameSymbol() {
	buf := e.activeBuffer()
	if buf == nil || buf.Path == "" || e.lspManager == nil {
		return
	}
	word := buf.WordAtCursor()
	if word == "" {
		e.setTemporaryError("No symbol under cursor")
		return
	}
	d := ui.NewInputDialog("Rename: ")
	d.Input = word
	d.Cursor = len([]rune(word))
	d.OnSubmit = func(newName string) {
		e.dialog = nil
		if newName == "" || newName == word {
			return
		}
		edit := e.lspManager.Rename(buf.Language, buf.Path, buf.Cursor.Line, buf.Cursor.Col, newName)
		if edit == nil || len(edit.Changes) == 0 {
			e.setTemporaryError("Rename failed")
			return
		}
		e.applyWorkspaceEdit(edit)
		e.statusBar.Message = fmt.Sprintf("Renamed '%s' to '%s'", word, newName)
	}
	d.OnCancel = func() {
		e.dialog = nil
	}
	e.dialog = d
}

func (e *Editor) applyWorkspaceEdit(edit *lsp.WorkspaceEdit) {
	for uri, edits := range edit.Changes {
		path := lsp.URIToPath(uri)
		var buf *buffer.Buffer
		for _, b := range e.buffers {
			if b.Path == path {
				buf = b
				break
			}
		}
		if buf == nil {
			e.openFile(path)
			buf = e.activeBuffer()
		}
		if buf == nil {
			continue
		}

		// Sort edits bottom-to-top to preserve positions
		sort.Slice(edits, func(i, j int) bool {
			if edits[i].Range.Start.Line != edits[j].Range.Start.Line {
				return edits[i].Range.Start.Line > edits[j].Range.Start.Line
			}
			return edits[i].Range.Start.Character > edits[j].Range.Start.Character
		})

		for _, te := range edits {
			startPos := buffer.Cursor{Line: te.Range.Start.Line, Col: te.Range.Start.Character}
			endPos := buffer.Cursor{Line: te.Range.End.Line, Col: te.Range.End.Character}
			oldText := buf.GetTextInRange(startPos, endPos)
			if oldText != "" {
				buf.RemoveTextAt(startPos, oldText)
			}
			if te.NewText != "" {
				buf.InsertTextAtPos(startPos, te.NewText)
			}
		}
		buf.RecomputeDirty()
		e.highlight.InvalidateCache(path)
		// Update tab modified indicator
		for i, b := range e.buffers {
			if b == buf {
				e.tabBar.SetModified(i, buf.Dirty)
				break
			}
		}
	}
}

func (e *Editor) showHoverInfo() {
	buf := e.activeBuffer()
	if buf == nil || buf.Path == "" || e.lspManager == nil {
		return
	}
	info := e.lspManager.Hover(buf.Language, buf.Path, buf.Cursor.Line, buf.Cursor.Col)
	if info == "" {
		e.statusBar.Message = "No hover info"
	} else {
		// Truncate for status bar display
		if len(info) > 120 {
			info = info[:120] + "…"
		}
		// Replace newlines with spaces for status bar
		for i, ch := range info {
			if ch == '\n' || ch == '\r' {
				// Safe slicing - check bounds before accessing i+1
				if i+1 < len(info) {
					info = info[:i] + " " + info[i+1:]
				} else {
					info = info[:i] + " "
				}
			}
		}
		e.statusBar.Message = info
	}
}

func (e *Editor) toggleTerminal() {
	e.termOpen = !e.termOpen
	if e.termOpen && e.terminal == nil {
		_, _, w, h := e.termLayout()
		if h < 3 {
			h = 3
		}
		if w < 10 {
			w = 10
		}
		e.terminal = ui.NewTerminal(e.screen, e.cfg.Shell, h-1, w)
	}
	if e.termOpen {
		e.focusTarget = "terminal"
	} else {
		e.focusTarget = "editor"
	}
	e.updateFocus()
}

func (e *Editor) toggleTree() {
	e.treeOpen = !e.treeOpen
	if !e.treeOpen && e.focusTarget == "tree" {
		e.focusTarget = "editor"
	}
	e.updateFocus()
}

func (e *Editor) toggleTreeFocus() {
	if !e.treeOpen {
		// If tree is closed, open it and focus it
		e.treeOpen = true
		e.focusTarget = "tree"
	} else if e.focusTarget == "tree" {
		// If tree is focused, switch to editor
		e.focusTarget = "editor"
	} else {
		// If editor is focused, switch to tree
		e.focusTarget = "tree"
	}
	e.updateFocus()
}

func (e *Editor) adjustTerminalHeight(delta float64) {
	newRatio := e.termRatio + delta
	// Constrain between 10% and 100%
	if newRatio < 0.10 {
		newRatio = 0.10
	} else if newRatio > 1.0 {
		newRatio = 1.0
	}
	e.termRatio = newRatio
	e.cfg.TermRatio = newRatio

	// Resize terminal if it exists
	if e.terminal != nil {
		_, _, termW, termH := e.termLayout()
		if termW > 0 && termH > 1 {
			e.terminal.Resize(termH-1, termW)
		}
	}

	// Save config
	e.cfg.Save()
}

func (e *Editor) adjustTreeWidth(delta int) {
	newWidth := e.treeWidth + delta
	w, _ := e.screen.Size()
	maxWidth := w / 2 // 50% max width

	// Constrain between 16 and 50% of screen width
	if newWidth < 16 {
		newWidth = 16
	} else if newWidth > maxWidth {
		newWidth = maxWidth
	}

	e.treeWidth = newWidth
	e.cfg.TreeWidth = newWidth

	// Save config
	e.cfg.Save()
}

func (e *Editor) updateFocus() {
	if e.fileTree != nil {
		e.fileTree.SetFocused(e.focusTarget == "tree")
	}
	if e.terminal != nil {
		e.terminal.SetFocused(e.focusTarget == "terminal")
	}
}

// applyFileSettings applies per-language defaults and .editorconfig to a buffer.
func (e *Editor) applyFileSettings(buf *buffer.Buffer) {
	// Apply per-language defaults
	buf.TabSize = e.cfg.LanguageTabSize(buf.Language)
	buf.UseTabs = e.cfg.LanguageUseTabs(buf.Language)
	buf.AutoCloseEnabled = e.cfg.AutoClose

	// Override with .editorconfig if present
	if buf.Path != "" {
		if ec := config.FindEditorConfig(buf.Path); ec != nil {
			if ec.IndentSize > 0 {
				buf.TabSize = ec.IndentSize
			}
			if ec.TabWidth > 0 {
				buf.TabSize = ec.TabWidth
			}
			if ec.IndentStyle == "tab" {
				buf.UseTabs = true
			} else if ec.IndentStyle == "space" {
				buf.UseTabs = false
			}
			if ec.EndOfLine == "crlf" {
				buf.LineEnding = "CRLF"
			} else if ec.EndOfLine == "lf" {
				buf.LineEnding = "LF"
			}
		}
	}
}

func (e *Editor) updateStatus() {
	buf := e.activeBuffer()
	if buf == nil {
		return
	}
	e.statusBar.Filename = filepath.Base(buf.Path)
	if e.statusBar.Filename == "." {
		e.statusBar.Filename = "untitled"
	}
	e.statusBar.Line = buf.Cursor.Line
	e.statusBar.Col = buf.Cursor.Col
	e.statusBar.Language = buf.Language
	e.statusBar.LineEnd = buf.LineEnding
	e.statusBar.Encoding = buf.Encoding
	if e.statusBar.Encoding == "" {
		e.statusBar.Encoding = "UTF-8"
	}
	if e.focusTarget == "terminal" {
		e.statusBar.Mode = "TERM"
	} else {
		e.statusBar.Mode = "EDIT"
	}
	// Selection info
	if buf.Selection != nil && !buf.Selection.Empty() {
		text := buf.GetSelectedText()
		e.statusBar.SelChars = len([]rune(text))
		e.statusBar.SelLines = buf.Selection.End.Line - buf.Selection.Start.Line + 1
	} else {
		e.statusBar.SelChars = 0
		e.statusBar.SelLines = 0
	}

	// LSP diagnostics
	e.statusBar.DiagErrors = 0
	e.statusBar.DiagWarnings = 0
	if e.lspManager != nil && buf.Path != "" {
		diags := e.lspManager.GetDiagnostics(buf.Path)
		for _, d := range diags {
			if d.Severity == 1 {
				e.statusBar.DiagErrors++
			} else if d.Severity == 2 {
				e.statusBar.DiagWarnings++
			}
		}
		// Show diagnostic message for cursor line
		if e.statusBar.Message == "" {
			for _, d := range diags {
				if d.Range.Start.Line <= buf.Cursor.Line && buf.Cursor.Line <= d.Range.End.Line {
					prefix := "ⓘ"
					if d.Severity == 1 {
						prefix = "● Error"
					} else if d.Severity == 2 {
						prefix = "▲ Warning"
					}
					src := ""
					if d.Source != "" {
						src = " [" + d.Source + "]"
					}
					e.statusBar.Message = prefix + ": " + d.Message + src
					break
				}
			}
		}
	}

	// Tab/Space indicator
	if buf.UseTabs {
		e.statusBar.TabInfo = "Tabs"
	} else {
		e.statusBar.TabInfo = fmt.Sprintf("Spaces: %d", buf.TabSize)
	}
}

// Layout helpers

func (e *Editor) treeLeft() int {
	if e.treeOpen {
		return e.treeWidth
	}
	return 0
}

func (e *Editor) termLayout() (x, y, w, h int) {
	screenW, screenH := e.screen.Size()
	left := e.treeLeft()
	w = screenW - left
	h = int(float64(screenH-2) * e.termRatio) // -2 for tab bar and status bar
	if h < 3 {
		h = 3
	}
	x = left
	y = screenH - 1 - h // -1 for status bar
	return
}

func (e *Editor) editorLayout() (x, y, w, h int) {
	screenW, screenH := e.screen.Size()
	left := e.treeLeft()
	x = left
	y = 1 // below tab bar
	w = screenW - left
	h = screenH - 2 // -1 tab bar, -1 status bar
	if e.termOpen {
		_, termY, _, _ := e.termLayout()
		h = termY - y
	}
	return
}

// setStatusMessage sets a permanent status message (won't auto-clear)
func (e *Editor) setStatusMessage(msg string) {
	e.statusBar.Message = msg
	e.statusMessageTime = time.Time{} // zero time = permanent
	e.statusMessageIsError = false
}

// setTemporaryMessage sets a message that will auto-clear after 5 seconds
func (e *Editor) setTemporaryMessage(msg string) {
	e.statusBar.Message = msg
	e.statusMessageTime = time.Now()
	e.statusMessageIsError = false
}

// setTemporaryError sets an error message that will auto-clear after 5 seconds
func (e *Editor) setTemporaryError(msg string) {
	e.statusBar.Message = msg
	e.statusMessageTime = time.Now()
	e.statusMessageIsError = true
}

// clearExpiredMessages clears status messages that have expired
func (e *Editor) clearExpiredMessages() {
	if !e.statusMessageTime.IsZero() && time.Since(e.statusMessageTime) > 5*time.Second {
		e.statusBar.Message = ""
		e.statusMessageTime = time.Time{}
		e.statusMessageIsError = false
	}
}

func (e *Editor) openQuickOpen() {
	cwd, _ := os.Getwd()
	files := ui.CollectFiles(cwd)
	theme := e.cfg.GetTheme()
	qo := ui.NewQuickOpen(files, theme)
	qo.OnSelect = func(relPath string) {
		absPath := filepath.Join(cwd, relPath)
		e.openFile(absPath)
		e.quickOpen = nil
	}
	qo.OnClose = func() {
		e.quickOpen = nil
	}
	e.quickOpen = qo
}

func (e *Editor) openCommandPalette() {
	theme := e.cfg.GetTheme()
	commands := []ui.Command{
		{Name: "Save", Shortcut: "Ctrl+S", Action: func() { e.saveCurrentFile() }},
		{Name: "Save As", Shortcut: "", Action: func() { e.openSaveAsDialog() }},
		{Name: "Reload", Shortcut: "", Action: func() { e.reloadFile() }},
		{Name: "New File", Shortcut: "Ctrl+N", Action: func() { e.openEmptyBuffer() }},
		{Name: "Close Tab", Shortcut: "Ctrl+W", Action: func() { e.closeTab(e.activeTab) }},
		{Name: "Find", Shortcut: "Ctrl+F", Action: func() { e.openFindDialog() }},
		{Name: "Find and Replace", Shortcut: "Ctrl+R", Action: func() { e.openFindReplaceDialog() }},
		{Name: "Go to Line", Shortcut: "Ctrl+G", Action: func() { e.openGotoLineDialog() }},
		{Name: "Quick Open", Shortcut: "Ctrl+P", Action: func() { e.openQuickOpen() }},
		{Name: "Toggle Word Wrap", Shortcut: "Alt+Z", Action: func() {
			e.cfg.WordWrap = !e.cfg.WordWrap
			if e.cfg.WordWrap {
				e.setTemporaryMessage("Word wrap: ON")
			} else {
				e.setTemporaryMessage("Word wrap: OFF")
			}
			e.cfg.Save()
		}},
		{Name: "Toggle File Tree", Shortcut: "Ctrl+B", Action: func() { e.toggleTree() }},
		{Name: "Toggle Terminal", Shortcut: "Ctrl+T", Action: func() { e.toggleTerminal() }},
		{Name: "Toggle Code Fold", Shortcut: "Ctrl+.", Action: func() {
			buf := e.activeBuffer()
			if buf != nil {
				buf.ToggleFold(buf.Cursor.Line)
			}
		}},
		{Name: "Next Tab", Shortcut: "Ctrl+Tab", Action: func() { e.nextTab() }},
		{Name: "Previous Tab", Shortcut: "Ctrl+Shift+Tab", Action: func() { e.prevTab() }},
		{Name: "Undo", Shortcut: "Ctrl+Z", Action: func() {
			buf := e.activeBuffer()
			if buf != nil {
				buf.ApplyUndo()
				e.updateStatus()
			}
		}},
		{Name: "Redo", Shortcut: "Ctrl+Shift+Z", Action: func() {
			buf := e.activeBuffer()
			if buf != nil {
				buf.ApplyRedo()
				e.updateStatus()
			}
		}},
		{Name: "Copy", Shortcut: "Ctrl+C", Action: func() { e.copySelection() }},
		{Name: "Paste", Shortcut: "Ctrl+V", Action: func() { e.pasteClipboard() }},
		{Name: "Cut", Shortcut: "Ctrl+X", Action: func() { e.cutSelection() }},
		{Name: "Select All", Shortcut: "Ctrl+A", Action: func() {
			buf := e.activeBuffer()
			if buf != nil {
				buf.SelectAll()
			}
		}},
		{Name: "Toggle Line Comment", Shortcut: "Ctrl+/", Action: func() {
			buf := e.activeBuffer()
			if buf != nil {
				buf.ToggleLineComment(e.commentString())
				e.markDirty()
			}
		}},
		{Name: "Duplicate Line", Shortcut: "", Action: func() {
			buf := e.activeBuffer()
			if buf != nil {
				buf.DuplicateLine()
				e.markDirty()
			}
		}},
		{Name: "Settings", Shortcut: "Alt+,", Action: func() { e.toggleSettingsDialog() }},
		{Name: "Help", Shortcut: "Ctrl+H", Action: func() { e.openHelpDialog() }},
		{Name: "Quit", Shortcut: "Ctrl+Q", Action: func() { e.handleQuit() }},
	}
	cp := ui.NewCommandPalette(commands, theme)
	cp.OnClose = func() {
		e.commandPalette = nil
	}
	cp.OnNavigate = func(path string, line int) {
		e.navigateToLocation(path, line)
	}
	// Set working directory for search
	if e.fileTree != nil {
		cp.SetWorkDir(e.fileTree.GetRoot())
	}
	e.commandPalette = cp
}

func (e *Editor) navigateToLocation(path string, line int) {
	// Make path absolute if it's relative
	absPath := path
	if !filepath.IsAbs(path) && e.fileTree != nil {
		absPath = filepath.Join(e.fileTree.GetRoot(), path)
	}

	// Open the file (or switch to it if already open)
	tabIdx := -1
	for i, buf := range e.buffers {
		if buf.Path == absPath {
			tabIdx = i
			break
		}
	}

	if tabIdx == -1 {
		// File not open, so open it as a preview tab.
		e.openFilePreview(absPath)
		tabIdx = e.activeTab
	} else {
		// Switch to existing tab
		e.switchTab(tabIdx)
	}

	// Navigate to the line
	if tabIdx >= 0 && tabIdx < len(e.buffers) {
		buf := e.buffers[tabIdx]
		if line > 0 && line <= len(buf.Lines) {
			buf.Cursor.Line = line - 1 // Convert to 0-based
			buf.Cursor.Col = 0
			buf.Selection = nil // Clear selection

			// Ensure cursor is visible
			view := e.views[buf]
			if view != nil {
				// Get text dimensions
				_, _, ew, eh := e.editorLayout()
				gutterW := e.gutterWidth()
				textW := ew - gutterW

				if e.cfg.WordWrap {
					e.ensureCursorVisibleWrap(view, buf, textW, eh)
				} else {
					e.ensureCursorVisible(view, buf, textW, eh)
				}
			}
		}
	}
}

func (e *Editor) commentString() string {
	buf := e.activeBuffer()
	if buf == nil {
		return "//"
	}
	lang := strings.ToLower(buf.Language)
	switch lang {
	case "python", "ruby", "perl", "bash", "shell", "sh", "zsh", "fish",
		"yaml", "toml", "r", "julia", "elixir", "nim", "tcl", "conf", "cfg",
		"ini", "dockerfile", "makefile", "cmake":
		return "#"
	case "lua", "haskell", "sql", "ada", "applescript", "vhdl", "verilog":
		return "--"
	case "lisp", "scheme", "clojure", "racket", "elisp":
		return ";"
	case "vim":
		return "\""
	case "html", "xml", "svg":
		return "<!--"
	case "css", "scss", "sass", "less":
		return "/*"
	default:
		// C-style languages: C, C++, C#, Java, JavaScript, TypeScript, Go, Rust, Swift, Kotlin, etc.
		return "//"
	}
}

// File watching

func (e *Editor) setupFileWatcher(screen tcell.Screen) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		// Graceful degradation - continue without watching
		return
	}
	e.fileWatcher = watcher

	// Watch root directory recursively
	e.addWatchRecursive(e.watchedRoot)

	// Start watcher goroutine
	go func() {
		// Debounce: collect events and send after quiet period
		debounceTimer := time.NewTimer(100 * time.Millisecond)
		debounceTimer.Stop()
		var pendingEvents []fsnotify.Event

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Ignore hidden files and common build directories
				if e.shouldIgnorePath(event.Name) {
					continue
				}

				// Collect event
				pendingEvents = append(pendingEvents, event)
				debounceTimer.Reset(100 * time.Millisecond)

			case <-debounceTimer.C:
				// Send all pending events
				for _, event := range pendingEvents {
					ev := &FileWatchEvent{
						Path: event.Name,
						Op:   event.Op,
					}
					ev.SetEventNow()
					screen.PostEvent(ev)

					// If a directory was created, add it to the watch list
					if event.Op&fsnotify.Create != 0 {
						if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
							e.addWatchRecursive(event.Name)
						}
					}
				}
				pendingEvents = nil

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				_ = err
			}
		}
	}()
}

func (e *Editor) addWatchRecursive(root string) {
	if e.fileWatcher == nil {
		return
	}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Ignore certain directories
			if e.shouldIgnorePath(path) {
				return filepath.SkipDir
			}
			e.fileWatcher.Add(path)
		}
		return nil
	})
}

func (e *Editor) shouldIgnorePath(path string) bool {
	base := filepath.Base(path)
	// Ignore hidden files/dirs, build artifacts, version control
	ignore := []string{".git", ".hg", ".svn", "node_modules", "target", "build", "dist", "__pycache__", ".idea", ".vscode"}
	for _, pattern := range ignore {
		if base == pattern || strings.HasPrefix(base, ".") && base != "." {
			return true
		}
	}
	return false
}

func (e *Editor) handleFileWatchEvent(ev *FileWatchEvent) {
	// Check if this file is open in a buffer
	var affectedBuf *buffer.Buffer
	var bufIdx int
	for i, buf := range e.buffers {
		if buf.Path == ev.Path {
			affectedBuf = buf
			bufIdx = i
			break
		}
	}

	if affectedBuf != nil {
		// File is open in editor
		switch {
		case ev.Op&fsnotify.Remove != 0:
			// File was deleted
			e.statusBar.Message = "Warning: " + filepath.Base(ev.Path) + " was deleted externally"

		case ev.Op&fsnotify.Write != 0 || ev.Op&fsnotify.Create != 0:
			// File was modified externally
			// Check if we just saved it (to avoid reload loop)
			info, err := os.Stat(ev.Path)
			if err != nil {
				return
			}
			modTime := info.ModTime()

			// Allow 1 second grace period after our last save
			if affectedBuf.LastSaveTime.IsZero() || modTime.Sub(affectedBuf.LastSaveTime) > time.Second {
				if affectedBuf.Dirty {
					// Buffer has unsaved changes - mark as conflict
					affectedBuf.ExternallyModified = true
					e.tabBar.SetExternallyModified(bufIdx, true)
					e.statusBar.Message = "⚠ " + filepath.Base(ev.Path) + " was modified externally! (unsaved changes)"
				} else {
					// Buffer is clean - reload silently
					newBuf, err := buffer.NewBufferFromFile(ev.Path, e.cfg.TabSize)
					if err == nil {
						// Preserve cursor position if possible
						oldCursor := affectedBuf.Cursor
						newBuf.Language = affectedBuf.Language
						e.applyFileSettings(newBuf)
						newBuf.LastSaveTime = modTime

						// Replace buffer
						e.buffers[bufIdx] = newBuf
						e.views[newBuf] = e.views[affectedBuf]
						delete(e.views, affectedBuf)

						// Restore cursor if still valid
						if oldCursor.Line < len(newBuf.Lines) {
							newBuf.Cursor = oldCursor
							if newBuf.Cursor.Col > len(newBuf.Lines[newBuf.Cursor.Line]) {
								newBuf.Cursor.Col = len(newBuf.Lines[newBuf.Cursor.Line])
							}
						}

						e.highlight.InvalidateCache(ev.Path)
						e.statusBar.Message = "↻ " + filepath.Base(ev.Path) + " (reloaded)"
					}
				}
			}
		}
	}

	// Refresh file tree if event is in watched directory
	if strings.HasPrefix(ev.Path, e.watchedRoot) {
		if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
			// File/directory was added, removed, or renamed
			e.fileTree.Refresh()
		}
	}
}
