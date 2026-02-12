package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"editor/buffer"
	"editor/clipboardx"
	"editor/highlight"
	"editor/ui"

	"github.com/gdamore/tcell/v2"
)

func (e *Editor) handleKey(ev *tcell.EventKey) {
	// Reset cursor blink on any keypress
	e.cursorVisible = true
	e.lastBlinkTime = time.Now()

	// Reset force-quit state on any key except Ctrl+Q
	if ev.Key() != tcell.KeyCtrlQ {
		e.quitPending = false
	}

	// Check for Alt+, FIRST - it toggles settings dialog
	if ev.Key() == tcell.KeyRune && ev.Rune() == ',' && ev.Modifiers()&tcell.ModAlt != 0 {
		e.toggleSettingsDialog()
		return
	}

	// Ctrl+Shift+P opens command palette
	if ev.Key() == tcell.KeyRune && (ev.Rune() == 'P' || ev.Rune() == 'p') && ev.Modifiers()&tcell.ModCtrl != 0 && ev.Modifiers()&tcell.ModShift != 0 {
		e.openCommandPalette()
		return
	}

	// Quick open gets priority
	if e.quickOpen != nil {
		e.quickOpen.HandleKey(ev)
		return
	}

	// Command palette gets priority
	if e.commandPalette != nil {
		e.commandPalette.HandleKey(ev)
		return
	}

	// Dialog gets priority for other keys
	if e.dialog != nil {
		if e.dialog.HandleKey(ev) {
			// After typing in find dialog, update matches
			if e.dialog != nil && e.dialog.Type == ui.DialogFind {
				buf := e.activeBuffer()
				if buf != nil {
					e.dialog.FindMatches(buf.Lines)
				}
			}
			return
		}
	}

	// Autocomplete gets priority when visible
	if e.autocomplete != nil && e.autocomplete.Visible {
		if e.autocomplete.HandleKey(ev) {
			return
		}
		// Any other key closes autocomplete
		e.autocomplete = nil
	}

	// Global keybindings (always active)
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		e.handleQuit()
		return
	case tcell.KeyCtrlS:
		e.saveCurrentFile()
		return
	case tcell.KeyCtrlH, tcell.KeyF1:
		e.toggleHelpDialog()
		return
	case tcell.KeyCtrlT:
		e.toggleTerminal()
		return
	case tcell.KeyCtrlE:
		e.toggleTreeFocus()
		return
	case tcell.KeyCtrlP:
		e.openQuickOpen()
		return
	case tcell.KeyF12:
		e.gotoDefinition()
		return
	case tcell.KeyF2:
		e.renameSymbol()
		return
	}

	// Alt+Up/Down for terminal resizing OR moving lines (depends on focus)
	if ev.Modifiers()&tcell.ModAlt != 0 {
		if ev.Key() == tcell.KeyUp {
			if e.focusTarget == "terminal" || e.termOpen {
				// Resize terminal up (increase height)
				e.adjustTerminalHeight(0.05)
				return
			} else {
				buf := e.activeBuffer()
				if buf != nil {
					buf.MoveLineUp()
					e.markDirty()
				}
				return
			}
		} else if ev.Key() == tcell.KeyDown {
			if e.focusTarget == "terminal" || e.termOpen {
				// Resize terminal down (decrease height)
				e.adjustTerminalHeight(-0.05)
				return
			} else {
				buf := e.activeBuffer()
				if buf != nil {
					buf.MoveLineDown()
					e.markDirty()
				}
				return
			}
		} else if ev.Key() == tcell.KeyLeft {
			// Decrease tree width
			e.adjustTreeWidth(-4)
			return
		} else if ev.Key() == tcell.KeyRight {
			// Increase tree width
			e.adjustTreeWidth(4)
			return
		}
	}

	// Alt+Number for tab switching (1-9, 0 for tab 10)
	if ev.Key() == tcell.KeyRune && ev.Modifiers()&tcell.ModAlt != 0 {
		switch ev.Rune() {
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			tabIdx := int(ev.Rune() - '1') // Convert '1' to 0, '2' to 1, etc.
			if tabIdx < len(e.buffers) {
				e.switchTab(tabIdx)
			}
			return
		case '0':
			if len(e.buffers) >= 10 {
				e.switchTab(9) // 10th tab (0-indexed as 9)
			}
			return
		}
	}

	// Terminal gets all other keys when focused
	if e.focusTarget == "terminal" && e.terminal != nil {
		e.terminal.HandleKey(ev)
		return
	}

	// File tree gets keys when focused
	if e.focusTarget == "tree" && e.fileTree != nil {
		// Handle Escape to return to editor
		if ev.Key() == tcell.KeyEscape {
			e.focusTarget = "editor"
			e.updateFocus()
			return
		}
		if e.fileTree.HandleKey(ev) {
			return
		}
	}

	// Editor keybindings
	switch ev.Key() {
	case tcell.KeyCtrlB:
		e.toggleTree()
		return
	case tcell.KeyCtrlN:
		e.openEmptyBuffer()
		return
	case tcell.KeyCtrlW:
		e.closeTab(e.activeTab)
		return
	case tcell.KeyCtrlF:
		e.openFindDialog()
		return
	case tcell.KeyCtrlR:
		e.openFindReplaceDialog()
		return
	case tcell.KeyCtrlG:
		e.openGotoLineDialog()
		return
	case tcell.KeyCtrlZ:
		buf := e.activeBuffer()
		if buf != nil {
			// Ctrl+Shift+Z = Redo, Ctrl+Z = Undo
			if ev.Modifiers()&tcell.ModShift != 0 {
				buf.ApplyRedo()
			} else {
				buf.ApplyUndo()
			}
			e.markDirty()
			e.updateStatus()
		}
		return
	case tcell.KeyCtrlC:
		if e.focusTarget == "terminal" && e.terminal != nil {
			if e.terminal.CopySelection() {
				e.setTemporaryMessage("Copied")
			} else {
				e.terminal.HandleKey(ev)
			}
			return
		}
		e.copySelection()
		return
	case tcell.KeyCtrlX:
		e.cutSelection()
		return
	case tcell.KeyCtrlV:
		e.pasteClipboard()
		return
	case tcell.KeyCtrlA:
		buf := e.activeBuffer()
		if buf != nil {
			buf.ClearExtraCursors() // Clear multi-cursors before selecting all
			buf.SelectAll()
		}
		return
	case tcell.KeyCtrlD:
		buf := e.activeBuffer()
		if buf != nil {
			buf.SelectNextOccurrence()
		}
		return
	case tcell.KeyEscape:
		buf := e.activeBuffer()
		if buf != nil {
			buf.Selection = nil
			buf.ClearExtraCursors()
		}
		e.dialog = nil
		return
	case tcell.KeyTab:
		if ev.Modifiers()&tcell.ModShift != 0 {
			buf := e.activeBuffer()
			if buf != nil {
				buf.DedentSelection()
				e.markDirty()
			}
		} else {
			buf := e.activeBuffer()
			if buf != nil {
				buf.InsertTab()
				e.markDirty()
			}
		}
		return
	case tcell.KeyBacktab:
		buf := e.activeBuffer()
		if buf != nil {
			buf.DedentSelection()
			e.markDirty()
		}
		return
	}

	// Ctrl+Tab / Ctrl+Shift+Tab for tab switching
	if ev.Key() == tcell.KeyTab && ev.Modifiers()&tcell.ModCtrl != 0 {
		if ev.Modifiers()&tcell.ModShift != 0 {
			e.prevTab()
		} else {
			e.nextTab()
		}
		return
	}

	// Ctrl+/ for comment toggle
	if ev.Key() == tcell.KeyRune && ev.Rune() == '/' && ev.Modifiers()&tcell.ModCtrl != 0 {
		buf := e.activeBuffer()
		if buf != nil {
			buf.ToggleLineComment(e.commentString())
			e.markDirty()
		}
		return
	}

	// Ctrl+. for fold toggle
	if ev.Key() == tcell.KeyRune && ev.Rune() == '.' && ev.Modifiers()&tcell.ModCtrl != 0 {
		buf := e.activeBuffer()
		if buf != nil {
			buf.ToggleFold(buf.Cursor.Line)
		}
		return
	}

	// Ctrl+Space for command palette (toggle)
	if ev.Key() == tcell.KeyRune && ev.Rune() == ' ' && ev.Modifiers()&tcell.ModCtrl != 0 {
		if e.commandPalette != nil {
			// Already open - close it
			e.commandPalette = nil
		} else {
			// Open it
			e.openCommandPalette()
		}
		return
	}

	// Ctrl+] for jump to matching bracket
	if ev.Key() == tcell.KeyCtrlRightSq {
		buf := e.activeBuffer()
		if buf != nil {
			matchLine, matchCol := e.findMatchingBracket(buf, buf.Cursor.Line, buf.Cursor.Col)
			if matchLine >= 0 {
				buf.Cursor.Line = matchLine
				buf.Cursor.Col = matchCol
				buf.Selection = nil
			}
		}
		return
	}

	// Alt+Z for word wrap toggle (same as VS Code)
	if ev.Key() == tcell.KeyRune && ev.Rune() == 'z' && ev.Modifiers()&tcell.ModAlt != 0 {
		e.cfg.WordWrap = !e.cfg.WordWrap
		if e.cfg.WordWrap {
			e.setTemporaryMessage("Word wrap: ON")
		} else {
			e.setTemporaryMessage("Word wrap: OFF")
		}
		e.cfg.Save()
		return
	}

	// Arrow keys and movement
	buf := e.activeBuffer()
	if buf == nil {
		return
	}

	// Reset mouseScrolling on keyboard input so view snaps back to cursor
	e.mouseScrolling = false

	shift := ev.Modifiers()&tcell.ModShift != 0
	ctrl := ev.Modifiers()&tcell.ModCtrl != 0
	alt := ev.Modifiers()&tcell.ModAlt != 0
	wordMod := ctrl || alt // both Ctrl+Arrow and Alt+Arrow do word movement

	switch ev.Key() {
	case tcell.KeyUp:
		buf.ClearAutoClose()
		if shift {
			e.startOrExtendSelection(buf)
		} else {
			buf.Selection = nil
		}
		if buf.Cursor.Line > 0 {
			buf.Cursor.Line--
			// Skip lines hidden by folds
			for buf.Cursor.Line > 0 && buf.IsHiddenByFold(buf.Cursor.Line) {
				buf.Cursor.Line--
			}
			e.clampCol(buf)
		}
		if buf.HasExtraCursors() {
			buf.MoveCursorsUp()
		}
		if shift {
			e.extendSelection(buf)
		}

	case tcell.KeyDown:
		buf.ClearAutoClose()
		if shift {
			e.startOrExtendSelection(buf)
		} else {
			buf.Selection = nil
		}
		if buf.Cursor.Line < len(buf.Lines)-1 {
			buf.Cursor.Line++
			// Skip lines hidden by folds
			for buf.Cursor.Line < len(buf.Lines)-1 && buf.IsHiddenByFold(buf.Cursor.Line) {
				buf.Cursor.Line++
			}
			e.clampCol(buf)
		}
		if buf.HasExtraCursors() {
			buf.MoveCursorsDown()
		}
		if shift {
			e.extendSelection(buf)
		}

	case tcell.KeyLeft:
		buf.ClearAutoClose()
		if shift {
			e.startOrExtendSelection(buf)
		} else {
			buf.Selection = nil
		}
		if wordMod {
			buf.MoveWordLeft()
		} else if buf.Cursor.Col > 0 {
			buf.Cursor.Col--
		} else if buf.Cursor.Line > 0 {
			buf.Cursor.Line--
			// Validate before accessing
			if buf.Cursor.Line >= 0 && buf.Cursor.Line < len(buf.Lines) {
				buf.Cursor.Col = len(buf.Lines[buf.Cursor.Line])
			}
		}
		if buf.HasExtraCursors() {
			buf.MoveCursorsLeft()
		}
		if shift {
			e.extendSelection(buf)
		}

	case tcell.KeyRight:
		buf.ClearAutoClose()
		if shift {
			e.startOrExtendSelection(buf)
		} else {
			buf.Selection = nil
		}
		if wordMod {
			buf.MoveWordRight()
		} else if buf.Cursor.Line >= 0 && buf.Cursor.Line < len(buf.Lines) && buf.Cursor.Col < len(buf.Lines[buf.Cursor.Line]) {
			buf.Cursor.Col++
		} else if buf.Cursor.Line < len(buf.Lines)-1 {
			buf.Cursor.Line++
			buf.Cursor.Col = 0
		}
		if buf.HasExtraCursors() {
			buf.MoveCursorsRight()
		}
		if shift {
			e.extendSelection(buf)
		}

	case tcell.KeyHome:
		buf.ClearAutoClose()
		if shift {
			e.startOrExtendSelection(buf)
		} else {
			buf.Selection = nil
		}
		if ctrl {
			buf.Cursor.Line = 0
			buf.Cursor.Col = 0
		} else {
			buf.Cursor.Col = 0
		}
		if shift {
			e.extendSelection(buf)
		}

	case tcell.KeyEnd:
		buf.ClearAutoClose()
		if shift {
			e.startOrExtendSelection(buf)
		} else {
			buf.Selection = nil
		}
		if ctrl {
			buf.Cursor.Line = len(buf.Lines) - 1
			buf.Cursor.Col = len(buf.Lines[buf.Cursor.Line])
		} else {
			buf.Cursor.Col = len(buf.Lines[buf.Cursor.Line])
		}
		if shift {
			e.extendSelection(buf)
		}

	case tcell.KeyPgUp:
		buf.ClearAutoClose()
		_, _, _, h := e.editorLayout()
		buf.Cursor.Line -= h
		if buf.Cursor.Line < 0 {
			buf.Cursor.Line = 0
		}
		e.clampCol(buf)
		buf.Selection = nil

	case tcell.KeyPgDn:
		buf.ClearAutoClose()
		_, _, _, h := e.editorLayout()
		buf.Cursor.Line += h
		if buf.Cursor.Line >= len(buf.Lines) {
			buf.Cursor.Line = len(buf.Lines) - 1
		}
		e.clampCol(buf)
		buf.Selection = nil

	case tcell.KeyEnter:
		buf.ClearAutoClose()
		buf.InsertNewline()
		e.markDirty()

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if ctrl {
			buf.ClearAutoClose()
			buf.DeleteWordBackward()
		} else if buf.HasExtraCursors() {
			buf.ClearAutoClose()
			buf.DeleteCharMulti()
		} else {
			buf.Backspace()
		}
		e.markDirty()

	case tcell.KeyDelete:
		if ctrl {
			buf.ClearAutoClose()
			buf.DeleteWordForward()
		} else if buf.HasExtraCursors() {
			buf.ClearAutoClose()
			buf.DeleteForwardMulti()
		} else {
			buf.Delete()
		}
		e.markDirty()

	case tcell.KeyRune:
		if (ev.Rune() == '"' || ev.Rune() == '\'') && e.cfg.QuoteWrapSelection && buf.WrapSelectionWith(ev.Rune()) {
			e.markDirty()
			break
		}
		if buf.HasExtraCursors() {
			buf.InsertCharMulti(ev.Rune())
		} else {
			buf.InsertChar(ev.Rune())
		}
		e.markDirty()
	}

	e.updateStatus()
}

func (e *Editor) handleMouse(ev *tcell.EventMouse) {
	// Reset cursor blink on mouse activity
	e.cursorVisible = true
	e.lastBlinkTime = time.Now()

	mx, my := ev.Position()
	btn := ev.Buttons()
	screenW, screenH := e.screen.Size()
	_ = screenW

	// Always update tab bar hover state - reset if mouse is not on tab bar row
	if my != 0 {
		e.tabBar.HandleMouse(ev) // This will reset mouseX/mouseY to -1,-1
	}

	// Status bar click has no action.
	if my == screenH-1 {
		return
	}

	// File tree area
	if e.treeOpen {
		// Always update mouse position in tree to handle hover states properly
		// (clears hover when mouse moves out of tree)
		treeHandled := e.fileTree.HandleMouse(ev)

		if mx < e.treeWidth {
			if treeHandled {
				if btn == tcell.Button1 {
					e.focusTarget = "tree"
					e.updateFocus()
				}
				return
			}
			// If not handled but in tree area, consume event
			return
		}
	}

	// Tab bar
	if my == 0 {
		e.tabBar.HandleMouse(ev)
		return
	}

	// Terminal area
	if e.termOpen && e.terminal != nil {
		_, termY, _, termH := e.termLayout()
		if my >= termY && my < termY+termH {
			if btn == tcell.Button1 {
				e.focusTarget = "terminal"
				e.updateFocus()
			}
			e.terminal.HandleMouse(ev)
			return
		}
	}

	// Editor area
	e.focusTarget = "editor"
	e.updateFocus()
	e.handleEditorMouse(ev)
}

func (e *Editor) handleEditorMouse(ev *tcell.EventMouse) {
	buf := e.activeBuffer()
	if buf == nil {
		return
	}
	view := e.activeView()
	if view == nil {
		return
	}

	mx, my := ev.Position()
	btn := ev.Buttons()
	ex, ey, _, eh := e.editorLayout()
	gutterW := e.gutterWidth()
	modifiers := ev.Modifiers()

	switch {
	case btn == tcell.WheelUp:
		if modifiers&tcell.ModShift != 0 {
			// Shift+WheelUp = scroll left
			view.scrollX -= 3
			if view.scrollX < 0 {
				view.scrollX = 0
			}
		} else {
			view.scrollY -= 3
			if view.scrollY < 0 {
				view.scrollY = 0
			}
		}
		e.mouseScrolling = true
	case btn == tcell.WheelDown:
		if modifiers&tcell.ModShift != 0 {
			// Shift+WheelDown = scroll right
			view.scrollX += 3
		} else {
			view.scrollY += 3
			maxScroll := len(buf.Lines) - eh + 1
			if maxScroll < 0 {
				maxScroll = 0
			}
			if view.scrollY > maxScroll {
				view.scrollY = maxScroll
			}
		}
		e.mouseScrolling = true
	case btn == tcell.WheelLeft:
		view.scrollX -= 3
		if view.scrollX < 0 {
			view.scrollX = 0
		}
		e.mouseScrolling = true
	case btn == tcell.WheelRight:
		view.scrollX += 3
		e.mouseScrolling = true
	case btn == tcell.Button1:
		buf.ClearAutoClose()
		// Record press position
		if !e.mouseDown {
			e.mousePressX, e.mousePressY = mx, my
		}

		// Convert screen coordinates to buffer coordinates
		visualRow := my - ey
		displayCol := mx - ex - gutterW + view.scrollX

		// Map visual row to actual buffer line, skipping folded lines
		line := view.scrollY
		rowCount := 0
		for line < len(buf.Lines) && rowCount < visualRow {
			if !buf.IsHiddenByFold(line) {
				rowCount++
			}
			line++
		}
		// Skip any hidden lines at the target position
		for line < len(buf.Lines) && buf.IsHiddenByFold(line) {
			line++
		}

		if line < 0 {
			line = 0
		}
		if line >= len(buf.Lines) {
			line = len(buf.Lines) - 1
		}

		// Additional safety check before accessing buf.Lines[line]
		if line < 0 || line >= len(buf.Lines) {
			return
		}

		// Convert display column to buffer column (handles tab expansion)
		if displayCol < 0 {
			displayCol = 0
		}
		col := displayColToBufferCol(buf.Lines[line], displayCol, buf.TabSize)
		if col > len(buf.Lines[line]) {
			col = len(buf.Lines[line])
		}

		if modifiers&tcell.ModShift != 0 {
			// Shift+click: extend selection
			buf.ClearExtraCursors()
			e.startOrExtendSelection(buf)
			buf.Cursor = buffer.Cursor{Line: line, Col: col}
			e.extendSelection(buf)
		} else if e.mouseDown {
			// Dragging with button held — extend selection from anchor
			newPos := buffer.Cursor{Line: line, Col: col}
			if !newPos.Equal(e.mouseAnchor) {
				sel := buffer.NewSelection(e.mouseAnchor, newPos)
				buf.Selection = &sel
				buf.Cursor = newPos
			}
		} else {
			// Regular click: place cursor, start drag tracking
			buf.Selection = nil
			buf.ClearExtraCursors()
			buf.Cursor = buffer.Cursor{Line: line, Col: col}
			e.mouseDown = true
			e.mouseAnchor = buf.Cursor
			e.mouseScrolling = false // Reset on click to allow ensureCursorVisible
		}
		e.updateStatus()

	case btn == tcell.Button2 || btn == tcell.Button3:
		// Middle mouse (Button2) or Right mouse (Button3): add cursor at this line (VSCode-style multi-cursor)
		// Note: Some systems report middle-click as Button3
		visualRow := my - ey

		line := view.scrollY
		rowCount := 0
		for line < len(buf.Lines) && rowCount < visualRow {
			if !buf.IsHiddenByFold(line) {
				rowCount++
			}
			line++
		}
		for line < len(buf.Lines) && buf.IsHiddenByFold(line) {
			line++
		}

		if line < 0 {
			line = 0
		}
		if line >= len(buf.Lines) {
			line = len(buf.Lines) - 1
		}

		if !e.middleMouseDown {
			// First middle/right click - start multi-cursor mode
			// Clear existing extra cursors
			buf.ClearExtraCursors()
			e.middleMouseDown = true

			// Record the anchor column from current cursor position
			e.middleMouseAnchor = buf.Cursor
			e.middleMouseLine = buf.Cursor.Line

			// Add cursor at clicked line, using anchor column
			col := e.middleMouseAnchor.Col
			if col > len(buf.Lines[line]) {
				col = len(buf.Lines[line])
			}
			buf.AddCursorAt(line, col)
		} else if line != e.middleMouseLine {
			// Dragging middle/right mouse - add cursor at each new line
			// Use the anchor column to keep cursors in a straight vertical line
			col := e.middleMouseAnchor.Col
			if col > len(buf.Lines[line]) {
				col = len(buf.Lines[line])
			}
			buf.AddCursorAt(line, col)
			e.middleMouseLine = line
		}
		e.updateStatus()

	case btn == tcell.ButtonNone:
		// Mouse release - check if it was a click (not a drag)
		if e.mouseDown && mx == e.mousePressX && my == e.mousePressY {
			// This was a click at the original press position
			// Check if it was on a fold indicator
			buf := e.activeBuffer()
			if buf != nil {
				view := e.activeView()
				if view != nil {
					ex, ey, _, _ := e.editorLayout()
					gutterW := e.gutterWidth()

					visualRow := my - ey

					// Map visual row to actual buffer line
					line := view.scrollY
					rowCount := 0
					for line < len(buf.Lines) && rowCount < visualRow {
						if !buf.IsHiddenByFold(line) {
							rowCount++
						}
						line++
					}
					for line < len(buf.Lines) && buf.IsHiddenByFold(line) {
						line++
					}

					// Check if click was in gutter
					gutterClickX := mx - ex
					if gutterClickX >= 0 && gutterClickX < gutterW && line >= 0 && line < len(buf.Lines) {
						if buf.IsFolded(line) || func() bool { s, _ := buf.FindFoldRange(line); return s >= 0 }() {
							buf.ToggleFold(line)
						}
					}
				}
			}
		}

		e.mouseDown = false
		e.middleMouseDown = false
		e.middleMouseLine = -1
		e.mouseScrolling = true
	}
}

func (e *Editor) handleQuit() {
	// Check for unsaved buffers
	for _, buf := range e.buffers {
		if buf.Dirty {
			if e.quitPending {
				e.quit = true // Second Ctrl+Q forces quit
				return
			}
			e.statusBar.Message = "Unsaved changes! Press Ctrl+Q again to force quit."
			e.quitPending = true
			return
		}
	}
	e.quit = true
}

// Selection helpers

var selectionAnchor *buffer.Cursor

func (e *Editor) startOrExtendSelection(buf *buffer.Buffer) {
	if buf.Selection == nil {
		selectionAnchor = &buffer.Cursor{Line: buf.Cursor.Line, Col: buf.Cursor.Col}
	}
}

func (e *Editor) extendSelection(buf *buffer.Buffer) {
	if selectionAnchor != nil {
		sel := buffer.NewSelection(*selectionAnchor, buf.Cursor)
		buf.Selection = &sel
	}
}

func (e *Editor) clampCol(buf *buffer.Buffer) {
	lineLen := len(buf.Lines[buf.Cursor.Line])
	if buf.Cursor.Col > lineLen {
		buf.Cursor.Col = lineLen
	}
}

func clipboardWrite(text string) {
	clipboardx.Write(text)
}

func clipboardRead() string {
	return clipboardx.Read()
}

// Clipboard operations

func (e *Editor) copySelection() {
	buf := e.activeBuffer()
	if buf == nil {
		return
	}

	var text string
	if buf.Selection != nil && !buf.Selection.Empty() {
		// Copy selection
		text = buf.GetSelectedText()
	} else {
		// No selection - copy entire current line including newline (VSCode behavior)
		if buf.Cursor.Line >= 0 && buf.Cursor.Line < len(buf.Lines) {
			text = buf.Lines[buf.Cursor.Line] + "\n"
		}
	}

	if text != "" {
		clipboardWrite(text)
		e.setTemporaryMessage("Copied")
	}
}

func (e *Editor) cutSelection() {
	buf := e.activeBuffer()
	if buf == nil {
		return
	}

	var text string
	if buf.Selection != nil && !buf.Selection.Empty() {
		// Cut selection
		text = buf.GetSelectedText()
		clipboardWrite(text)
		buf.DeleteSelection()
	} else {
		// No selection - cut entire current line (VSCode behavior)
		if buf.Cursor.Line >= 0 && buf.Cursor.Line < len(buf.Lines) {
			text = buf.Lines[buf.Cursor.Line] + "\n"
			clipboardWrite(text)
			// Delete the line
			buf.Lines = append(buf.Lines[:buf.Cursor.Line], buf.Lines[buf.Cursor.Line+1:]...)
			if len(buf.Lines) == 0 {
				buf.Lines = []string{""}
			}
			// Clamp cursor manually
			if buf.Cursor.Line >= len(buf.Lines) {
				buf.Cursor.Line = len(buf.Lines) - 1
			}
			if buf.Cursor.Line < 0 {
				buf.Cursor.Line = 0
			}
			lineLen := len(buf.Lines[buf.Cursor.Line])
			if buf.Cursor.Col > lineLen {
				buf.Cursor.Col = lineLen
			}
			buf.Dirty = true
		}
	}

	if text != "" {
		e.setTemporaryMessage("Cut")
		e.markDirty()
	}
}

func (e *Editor) pasteClipboard() {
	text := clipboardRead()
	if text == "" {
		return
	}
	buf := e.activeBuffer()
	if buf == nil {
		return
	}

	// Just insert the text as-is - InsertText will handle it correctly
	buf.InsertText(text)
	e.markDirty()
}

func (e *Editor) markDirty() {
	buf := e.activeBuffer()
	if buf != nil {
		buf.RecomputeDirty()
		e.tabBar.SetModified(e.activeTab, buf.Dirty)
		e.highlight.InvalidateCache(buf.Path)
		// Pin preview tab on edit
		if e.previewTab == e.activeTab {
			e.tabBar.Tabs[e.activeTab].Preview = false
			e.previewTab = -1
		}
	}
}

// Tab navigation

func (e *Editor) nextTab() {
	if len(e.buffers) > 1 {
		e.switchTab((e.activeTab + 1) % len(e.buffers))
	}
}

func (e *Editor) prevTab() {
	if len(e.buffers) > 1 {
		idx := e.activeTab - 1
		if idx < 0 {
			idx = len(e.buffers) - 1
		}
		e.switchTab(idx)
	}
}

// Dialogs

func (e *Editor) openFindDialog() {
	d := ui.NewFindDialog()
	buf := e.activeBuffer()

	d.OnSubmit = func(value string) {
		// Jump to next match
		if len(d.Matches) > 0 {
			d.NextMatch()
			m := d.Matches[d.MatchIndex]
			if buf != nil {
				buf.Cursor = buffer.Cursor{Line: m.Line, Col: m.Col}
			}
		}
	}
	d.OnNavigate = func(line, col int) {
		if buf != nil {
			buf.Cursor = buffer.Cursor{Line: line, Col: col}
		}
	}
	d.OnCancel = func() {
		e.dialog = nil
	}
	e.dialog = d
}

func (e *Editor) openFindReplaceDialog() {
	d := ui.NewFindReplaceDialog()
	buf := e.activeBuffer()

	d.OnSubmit = func(value string) {
		if len(d.Matches) > 0 {
			d.NextMatch()
			m := d.Matches[d.MatchIndex]
			if buf != nil {
				buf.Cursor = buffer.Cursor{Line: m.Line, Col: m.Col}
			}
		}
	}
	d.OnNavigate = func(line, col int) {
		if buf != nil {
			buf.Cursor = buffer.Cursor{Line: line, Col: col}
		}
	}
	d.OnReplace = func(matchIdx int, replacement string) {
		if buf == nil || matchIdx < 0 || matchIdx >= len(d.Matches) {
			return
		}
		m := d.Matches[matchIdx]
		buf.ReplaceAt(m.Line, m.Col, m.Length, replacement)
		e.markDirty()
		// Re-search to update matches
		d.FindMatches(buf.Lines)
		// Navigate to next match
		if len(d.Matches) > 0 {
			if matchIdx >= len(d.Matches) {
				d.MatchIndex = 0
			} else {
				d.MatchIndex = matchIdx
			}
			m := d.Matches[d.MatchIndex]
			buf.Cursor = buffer.Cursor{Line: m.Line, Col: m.Col}
		}
		e.setTemporaryMessage("Replaced")
	}
	d.OnReplaceAll = func(find, replacement string) int {
		if buf == nil {
			return 0
		}
		count := buf.ReplaceAll(find, replacement)
		e.markDirty()
		d.FindMatches(buf.Lines)
		e.statusBar.Message = fmt.Sprintf("Replaced %d occurrences", count)
		return count
	}
	d.OnCancel = func() {
		e.dialog = nil
	}
	e.dialog = d
}

func (e *Editor) openSaveAsDialog() {
	d := ui.NewSaveAsDialog()
	cwd, _ := os.Getwd()
	d.Input = cwd + string(os.PathSeparator)
	d.Cursor = len([]rune(d.Input))
	d.OnSubmit = func(value string) {
		if value == "" {
			e.dialog = nil
			return
		}
		absPath, _ := filepath.Abs(value)
		buf := e.activeBuffer()
		if buf != nil {
			buf.Path = absPath
			buf.Language = highlight.DetectLanguage(absPath)
			err := buf.SaveWithOptions(e.cfg.TrimTrailingSpace, e.cfg.InsertFinalNewline)
			if err != nil {
				if os.IsPermission(err) {
					e.promptSudoSave(buf, absPath, func() {
						e.tabBar.Tabs[e.activeTab].Title = filepath.Base(absPath)
						e.tabBar.Tabs[e.activeTab].Path = absPath
						e.updateStatus()
					})
					return
				}
				e.setTemporaryError("Error saving: " + err.Error())
			} else {
				e.onSaveSuccess(buf, "Saved "+filepath.Base(absPath))
				e.tabBar.Tabs[e.activeTab].Title = filepath.Base(absPath)
				e.tabBar.Tabs[e.activeTab].Path = absPath
				e.updateStatus()
			}
		}
		e.dialog = nil
	}
	d.OnCancel = func() {
		e.dialog = nil
	}
	e.dialog = d
}

func (e *Editor) openGotoLineDialog() {
	d := ui.NewGotoLineDialog()
	d.OnSubmit = func(value string) {
		lineNum, err := strconv.Atoi(value)
		if err != nil {
			e.setTemporaryError("Invalid line number")
			e.dialog = nil
			return
		}
		if lineNum <= 0 {
			e.setTemporaryError("Line number must be positive")
			e.dialog = nil
			return
		}
		buf := e.activeBuffer()
		if buf != nil {
			lineNum-- // convert to 0-indexed
			if lineNum >= len(buf.Lines) {
				e.setTemporaryError(fmt.Sprintf("Line %d exceeds file length (%d lines)", lineNum+1, len(buf.Lines)))
				lineNum = len(buf.Lines) - 1
			}
			buf.Cursor = buffer.Cursor{Line: lineNum, Col: 0}
			buf.Selection = nil
		}
		e.dialog = nil
	}
	d.OnCancel = func() {
		e.dialog = nil
	}
	e.dialog = d
}

func (e *Editor) toggleHelpDialog() {
	// If help dialog is already open, close it
	if e.dialog != nil && e.dialog.Type == ui.DialogHelp {
		e.dialog = nil
		return
	}

	// Otherwise, open the help dialog
	d := ui.NewHelpDialog()
	d.OnCancel = func() {
		e.dialog = nil
	}
	e.dialog = d
}

func (e *Editor) openHelpDialog() {
	e.toggleHelpDialog()
}

func (e *Editor) toggleSettingsDialog() {
	// If settings dialog is already open, close it
	if e.dialog != nil && e.dialog.Type == ui.DialogSettings {
		e.cfg.Save() // Save settings before closing
		e.dialog = nil
		return
	}

	// Otherwise, open the settings dialog
	e.openSettingsDialog()
}

func (e *Editor) openSettingsDialog() {
	options := []string{
		"Theme",
		"Space Size",
		"Tree Width",
		"Terminal Ratio",
		"Word Wrap",
		"Auto Close",
		"Quote Wrap Selection",
		"Trim Trailing Whitespace",
		"Insert Final Newline",
	}
	values := []string{
		e.cfg.Theme,
		strconv.Itoa(e.cfg.TabSize),
		strconv.Itoa(e.cfg.TreeWidth),
		fmt.Sprintf("%.2f", e.cfg.TermRatio),
		boolSettingValue(e.cfg.WordWrap),
		boolSettingValue(e.cfg.AutoClose),
		boolSettingValue(e.cfg.QuoteWrapSelection),
		boolSettingValue(e.cfg.TrimTrailingSpace),
		boolSettingValue(e.cfg.InsertFinalNewline),
	}

	d := ui.NewSettingsDialog(options, values)
	d.OnCancel = func() {
		e.cfg.Save() // Save settings when closing with ESC
		e.dialog = nil
	}
	d.OnSettingChange = func(index int, currentValue string) {
		e.applySettingChange(index, currentValue, d)
	}
	d.OnSettingChangeReverse = func(index int, currentValue string) {
		e.applySettingChangeReverse(index, currentValue, d)
	}
	e.dialog = d
}

func (e *Editor) applySettingChange(index int, currentValue string, d *ui.Dialog) {
	e.applySettingByDirection(index, 1, d)
}

func (e *Editor) applySettingChangeReverse(index int, currentValue string, d *ui.Dialog) {
	e.applySettingByDirection(index, -1, d)
}

func boolSettingValue(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func (e *Editor) applySettingByDirection(index int, direction int, d *ui.Dialog) {
	switch index {
	case 0: // Theme
		themeList := []string{"dark", "light", "monokai", "nord", "solarized-dark", "gruvbox", "gruvbox-light", "dracula", "one-dark", "tokyo-night", "catppuccin"}
		e.cfg.Theme = cycleString(themeList, e.cfg.Theme, direction)
		d.SettingsValues[0] = e.cfg.Theme
	case 1: // Space Size
		sizes := []int{2, 4, 8}
		e.cfg.TabSize = cycleInt(sizes, e.cfg.TabSize, direction)
		d.SettingsValues[1] = strconv.Itoa(e.cfg.TabSize)
	case 2: // Tree Width
		widths := []int{20, 24, 30, 40}
		e.cfg.TreeWidth = cycleInt(widths, e.cfg.TreeWidth, direction)
		e.treeWidth = e.cfg.TreeWidth
		d.SettingsValues[2] = strconv.Itoa(e.cfg.TreeWidth)
	case 3: // Terminal Ratio
		ratios := []float64{0.20, 0.30, 0.40, 0.50}
		e.cfg.TermRatio = cycleFloat(ratios, e.cfg.TermRatio, direction)
		e.termRatio = e.cfg.TermRatio
		d.SettingsValues[3] = fmt.Sprintf("%.2f", e.cfg.TermRatio)

		// Resize terminal if it's open
		if e.termOpen && e.terminal != nil {
			_, _, termW, termH := e.termLayout()
			if termW > 0 && termH > 1 {
				e.terminal.Resize(termH-1, termW)
			}
		}
	case 4: // Word Wrap
		e.cfg.WordWrap = !e.cfg.WordWrap
		d.SettingsValues[4] = boolSettingValue(e.cfg.WordWrap)
	case 5: // Auto Close
		e.cfg.AutoClose = !e.cfg.AutoClose
		for _, b := range e.buffers {
			b.AutoCloseEnabled = e.cfg.AutoClose
			if !e.cfg.AutoClose {
				b.ClearAutoClose()
			}
		}
		d.SettingsValues[5] = boolSettingValue(e.cfg.AutoClose)
	case 6: // Quote Wrap Selection
		e.cfg.QuoteWrapSelection = !e.cfg.QuoteWrapSelection
		d.SettingsValues[6] = boolSettingValue(e.cfg.QuoteWrapSelection)
	case 7: // Trim Trailing Whitespace
		e.cfg.TrimTrailingSpace = !e.cfg.TrimTrailingSpace
		d.SettingsValues[7] = boolSettingValue(e.cfg.TrimTrailingSpace)
	case 8: // Insert Final Newline
		e.cfg.InsertFinalNewline = !e.cfg.InsertFinalNewline
		d.SettingsValues[8] = boolSettingValue(e.cfg.InsertFinalNewline)
	}
	e.cfg.Save()
}

func cycleString(values []string, current string, direction int) string {
	currentIdx := 0
	for i, value := range values {
		if value == current {
			currentIdx = i
			break
		}
	}
	next := (currentIdx + direction + len(values)) % len(values)
	return values[next]
}

func cycleInt(values []int, current int, direction int) int {
	currentIdx := 0
	for i, value := range values {
		if value == current {
			currentIdx = i
			break
		}
	}
	next := (currentIdx + direction + len(values)) % len(values)
	return values[next]
}

func cycleFloat(values []float64, current float64, direction int) float64 {
	currentIdx := 0
	for i, value := range values {
		if fmt.Sprintf("%.2f", value) == fmt.Sprintf("%.2f", current) {
			currentIdx = i
			break
		}
	}
	next := (currentIdx + direction + len(values)) % len(values)
	return values[next]
}

func (e *Editor) triggerAutocomplete() {
	buf := e.activeBuffer()
	if buf == nil || e.lspManager == nil {
		return
	}

	lspItems := e.lspManager.Completion(buf.Language, buf.Path, buf.Cursor.Line, buf.Cursor.Col)
	if len(lspItems) == 0 {
		e.setTemporaryMessage("No completions")
		return
	}

	// Convert lsp.CompletionItem to ui.CompletionItem
	items := make([]ui.CompletionItem, len(lspItems))
	for i, li := range lspItems {
		items[i] = ui.CompletionItem{
			Label:      li.Label,
			Detail:     li.Detail,
			InsertText: li.InsertText,
			Kind:       li.Kind,
		}
	}

	// Calculate screen position for the popup
	view := e.activeView()
	if view == nil {
		return
	}
	ex, ey, _, _ := e.editorLayout()
	gutterW := e.gutterWidth()

	screenX := ex + gutterW + buf.Cursor.Col - view.scrollX
	// Count visible lines from scrollY to cursor line
	visualRow := 0
	for i := view.scrollY; i < buf.Cursor.Line && i < len(buf.Lines); i++ {
		if !buf.IsHiddenByFold(i) {
			visualRow++
		}
	}
	screenY := ey + visualRow

	theme := e.cfg.GetTheme()
	ac := ui.NewAutocomplete(items, screenX, screenY, theme)
	ac.OnSelect = func(item ui.CompletionItem) {
		text := item.InsertText
		if text == "" {
			text = item.Label
		}
		buf.InsertText(text)
		e.markDirty()
		e.autocomplete = nil
	}
	ac.OnClose = func() {
		e.autocomplete = nil
	}
	e.autocomplete = ac
}
