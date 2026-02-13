package editor

import (
	"fmt"
	"strings"

	"editor/buffer"
	"editor/config"
	"editor/highlight"
	"editor/lsp"
	"editor/ui"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// bufferColToDisplayCol converts a buffer column (rune index) to display column (with tabs expanded and wide chars)
func bufferColToDisplayCol(line string, bufCol int, tabSize int) int {
	displayCol := 0
	for i, r := range []rune(line) {
		if i >= bufCol {
			break
		}
		if r == '\t' {
			displayCol += tabSize - (displayCol % tabSize)
		} else {
			displayCol += runewidth.RuneWidth(r)
		}
	}
	return displayCol
}

// displayColToBufferCol converts a display column (visual position) to buffer column (rune index)
func displayColToBufferCol(line string, targetDisplayCol int, tabSize int) int {
	if targetDisplayCol <= 0 {
		return 0
	}

	displayCol := 0
	bufCol := 0
	lineRunes := []rune(line)

	for i, r := range lineRunes {
		if displayCol >= targetDisplayCol {
			return bufCol
		}

		if r == '\t' {
			displayCol += tabSize - (displayCol % tabSize)
		} else {
			displayCol += runewidth.RuneWidth(r)
		}

		// If this character spans the target position, return its buffer position
		if displayCol > targetDisplayCol {
			return i
		}

		bufCol++
	}

	// Target is beyond end of line
	return bufCol
}

func (e *Editor) render() {
	// Get theme first so we can set the screen style
	theme := e.cfg.GetTheme()

	// Set screen default style to use theme background, then clear
	// This ensures the background is properly colored when clearing
	defaultStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.Foreground)
	e.screen.SetStyle(defaultStyle)
	e.screen.Clear()

	screenW, screenH := e.screen.Size()

	// Update themes in components
	e.statusBar.Theme = theme
	e.tabBar.Theme = theme
	if e.fileTree != nil {
		e.fileTree.Theme = theme
	}
	if e.terminal != nil {
		e.terminal.Theme = theme
	}

	// File tree
	if e.treeOpen {
		e.fileTree.Render(e.screen, 0, 0, e.treeWidth, screenH-1)
	}

	// Tab bar
	left := e.treeLeft()
	e.tabBar.Render(e.screen, left, 0, screenW-left, 1)

	// Editor area or image viewer
	ex, ey, ew, eh := e.editorLayout()
	buf := e.activeBuffer()
	if buf != nil {
		if iv, ok := e.imageViews[buf]; ok && iv != nil {
			iv.SetTheme(theme)
			iv.Render(e.screen, ex, ey, ew, eh)
		} else {
			e.renderEditor(ex, ey, ew, eh)
		}
	} else {
		e.renderEditor(ex, ey, ew, eh)
	}

	// Terminal
	if e.termOpen && e.terminal != nil {
		tx, ty, tw, th := e.termLayout()
		e.terminal.Render(e.screen, tx, ty, tw, th)
	}

	// Status bar
	e.statusBar.Render(e.screen, 0, screenH-1, screenW, 1)

	// Dialog overlay
	if e.dialog != nil {
		// Pass theme to dialog
		theme := e.cfg.GetTheme()
		e.dialog.Theme = theme

		switch e.dialog.Type {
		case ui.DialogFind:
			h := 1
			if e.dialog.ReplaceMode {
				h = 2
			}
			e.dialog.Render(e.screen, ex, ey, ew, h)
		case ui.DialogGotoLine:
			e.dialog.Render(e.screen, ex, ey, ew, 1)
		case ui.DialogSaveAs:
			e.dialog.Render(e.screen, ex, ey, ew, 1)
		case ui.DialogInput:
			e.dialog.Render(e.screen, ex, ey, ew, 1)
		case ui.DialogSaveConfirm:
			e.dialog.Render(e.screen, 0, screenH-2, screenW, 1)
		case ui.DialogReloadConfirm:
			e.dialog.Render(e.screen, 0, screenH-2, screenW, 1)
		case ui.DialogHelp:
			e.dialog.Render(e.screen, 0, 0, screenW, screenH)
		case ui.DialogSettings:
			e.dialog.Render(e.screen, 0, 0, screenW, screenH)
		default:
			e.dialog.Render(e.screen, ex, ey, ew, 1)
		}
	}

	// Quick open overlay
	if e.quickOpen != nil {
		theme := e.cfg.GetTheme()
		e.quickOpen.Theme = theme
		e.quickOpen.Render(e.screen, 0, 0, screenW, screenH)
	}

	// Command palette overlay
	if e.commandPalette != nil {
		theme := e.cfg.GetTheme()
		e.commandPalette.Theme = theme
		e.commandPalette.Render(e.screen, 0, 0, screenW, screenH)
	}

	// Autocomplete popup overlay
	if e.autocomplete != nil && e.autocomplete.Visible {
		e.autocomplete.Theme = e.cfg.GetTheme()
		e.autocomplete.Render(e.screen, 0, 0, screenW, screenH)
	}

	// Show cursor in editor when focused (with blinking)
	_, isImageView := e.imageViews[buf]
	if e.focusTarget == "editor" && e.dialog == nil && e.quickOpen == nil && e.commandPalette == nil && !isImageView {
		view := e.activeView()
		cursorShown := false
		if buf != nil && view != nil && e.cursorVisible {
			gutterW := e.gutterWidth()
			if e.cfg.WordWrap {
				textW := ew - gutterW
				if textW > 0 {
					// Calculate visual cursor position with word wrap
					visualRow := 0
					for i := view.scrollY; i < buf.Cursor.Line && i < len(buf.Lines); i++ {
						lineLen := len([]rune(buf.Lines[i]))
						wrapRows := 1
						if lineLen > textW {
							wrapRows = (lineLen + textW - 1) / textW
						}
						visualRow += wrapRows
					}
					cursorWrapRow := buf.Cursor.Col / textW
					cursorWrapCol := buf.Cursor.Col % textW
					visualRow += cursorWrapRow

					cursorScreenX := ex + gutterW + cursorWrapCol
					cursorScreenY := ey + visualRow
					if buf.Cursor.Line >= view.scrollY &&
						cursorScreenX >= ex+gutterW && cursorScreenX < ex+ew &&
						cursorScreenY >= ey && cursorScreenY < ey+eh {
						e.screen.ShowCursor(cursorScreenX, cursorScreenY)
						cursorShown = true
					}
				}
			} else if buf.Cursor.Line >= 0 && buf.Cursor.Line < len(buf.Lines) {
				// Convert buffer column to display column for tabs
				cursorDisplayCol := bufferColToDisplayCol(buf.Lines[buf.Cursor.Line], buf.Cursor.Col, buf.TabSize)
				cursorScreenX := ex + gutterW + cursorDisplayCol - view.scrollX
				// Count visible lines between scrollY and cursor to get visual row
				visualRow := 0
				for i := view.scrollY; i < buf.Cursor.Line && i < len(buf.Lines); i++ {
					if !buf.IsHiddenByFold(i) {
						visualRow++
					}
				}
				cursorScreenY := ey + visualRow
				if buf.Cursor.Line >= view.scrollY &&
					cursorScreenX >= ex+gutterW && cursorScreenX < ex+ew &&
					cursorScreenY >= ey && cursorScreenY < ey+eh {
					e.screen.ShowCursor(cursorScreenX, cursorScreenY)
					cursorShown = true
				}
			}
		}
		if !cursorShown {
			e.screen.HideCursor()
		}
	} else if e.focusTarget != "terminal" {
		e.screen.HideCursor()
	}

	overlayVisible := e.dialog != nil || e.quickOpen != nil || e.commandPalette != nil || (e.autocomplete != nil && e.autocomplete.Visible)
	var protocolIV *ui.ImageView
	if buf != nil {
		if iv, ok := e.imageViews[buf]; ok && iv != nil && iv.NeedsProtocolRender() {
			protocolIV = iv
		}
	}
	if overlayVisible && protocolIV != nil && !e.protocolImageHidden {
		// Clear protocol image before Show/Sync so overlay text is drawn after it.
		protocolIV.ClearProtocolImage()
		e.protocolImageHidden = true
	}

	if e.needsSync {
		e.screen.Sync()
		e.needsSync = false
		// After Sync(), the sixel graphics plane is overwritten by text cells.
		// Mark the image as needing re-render to TTY.
		if protocolIV != nil {
			protocolIV.MarkDirty()
		}
	} else {
		e.screen.Show()
	}

	// Render protocol-based images after Show() since they write raw escape sequences.
	// Skip while overlays are visible so protocol output can't draw above dialogs/popups.
	if protocolIV != nil {
		if overlayVisible {
			// Force redraw once overlay closes in case the graphics plane changed.
			protocolIV.MarkDirty()
		} else {
			e.protocolImageHidden = false
			protocolIV.RenderProtocolImage()
		}
	} else {
		e.protocolImageHidden = false
	}
}

func (e *Editor) renderEditor(x, y, w, h int) {
	buf := e.activeBuffer()
	if buf == nil {
		return
	}
	view := e.activeView()
	if view == nil {
		return
	}

	gutterW := e.gutterWidth()
	textW := w - gutterW

	if textW <= 0 {
		return
	}

	// Ensure cursor is visible (skip if user is scrolling with mouse wheel or moving mouse)
	if !e.mouseScrolling {
		if e.cfg.WordWrap {
			e.ensureCursorVisibleWrap(view, buf, textW, h)
		} else {
			e.ensureCursorVisible(view, buf, textW, h)
		}
	}

	// Get theme
	theme := e.cfg.GetTheme()

	// Styles
	gutterStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.LineNumber)
	activeGutterStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.LineNumberActive)
	lineStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.Foreground)
	selStyle := tcell.StyleDefault.Background(theme.Selection).Foreground(theme.Foreground)
	matchStyle := tcell.StyleDefault.Background(tcell.ColorYellow).Foreground(tcell.ColorBlack)
	emptyLineStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.LineNumber)
	bracketStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.Foreground).Bold(true).Underline(true)
	indentGuideStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.IndentGuide)
	extraCursorStyle := tcell.StyleDefault.Background(theme.Foreground).Foreground(theme.Background)
	_ = matchStyle

	// Compute matching bracket positions
	bracketLine1, bracketCol1 := -1, -1
	bracketLine2, bracketCol2 := -1, -1
	if e.focusTarget == "editor" {
		bracketLine1, bracketCol1 = e.bracketAtCursor(buf, buf.Cursor.Line, buf.Cursor.Col)
		if bracketLine1 >= 0 {
			bracketLine2, bracketCol2 = e.findMatchingBracket(buf, buf.Cursor.Line, buf.Cursor.Col)
		}
	}

	if e.cfg.WordWrap {
		e.renderEditorWrapped(x, y, w, h, buf, view, gutterW, textW, gutterStyle, activeGutterStyle, lineStyle, selStyle, matchStyle, emptyLineStyle, theme, bracketStyle, bracketLine1, bracketCol1, bracketLine2, bracketCol2)
		e.renderScrollbar(x, y, w, h, buf, view, theme)
		return
	}

	// Get diagnostics for this buffer
	var diagnostics []lsp.Diagnostic
	if e.lspManager != nil && buf.Path != "" {
		diagnostics = e.lspManager.GetDiagnostics(buf.Path)
	}

	// Get highlighted lines
	startLine := view.scrollY
	endLine := startLine + h
	if endLine > len(buf.Lines) {
		endLine = len(buf.Lines)
	}

	// Build visible lines list (skip lines hidden by folds)
	visibleLines := make([]int, 0, h)
	for lineIdx := startLine; lineIdx < len(buf.Lines) && len(visibleLines) < h; lineIdx++ {
		if buf.IsHiddenByFold(lineIdx) {
			continue
		}
		visibleLines = append(visibleLines, lineIdx)
	}

	// Expand endLine to cover all visible lines for highlighting
	if len(visibleLines) > 0 {
		lastVisible := visibleLines[len(visibleLines)-1]
		if lastVisible+1 > endLine {
			endLine = lastVisible + 1
		}
	}

	code := strings.Join(buf.Lines, "\n")
	var styledLines []highlight.StyledLine
	if buf.Language != "" {
		styledLines = e.highlight.HighlightLines(code, buf.Language, startLine, endLine)
	}

	foldStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.LineNumber)

	for row := 0; row < h; row++ {
		screenY := y + row

		if row >= len(visibleLines) {
			screen := e.screen
			for col := x; col < x+w; col++ {
				if col == x {
					screen.SetContent(col, screenY, '~', nil, emptyLineStyle)
				} else {
					screen.SetContent(col, screenY, ' ', nil, lineStyle)
				}
			}
			continue
		}

		lineIdx := visibleLines[row]

		// Git gutter indicator (first column when available)
		gitOffset := 0
		if e.gitGutter.available {
			gitOffset = 1
			gitStatus := e.gitGutter.StatusAt(lineIdx)
			gitCh := ' '
			gitStyle := gutterStyle
			switch gitStatus {
			case GitAdded:
				gitCh = '│'
				gitStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorGreen)
			case GitModified:
				gitCh = '│'
				gitStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorDarkCyan)
			case GitDeleted:
				gitCh = '▸'
				gitStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorRed)
			}
			e.screen.SetContent(x, screenY, gitCh, nil, gitStyle)
		}

		// Line number (gutter) with fold indicator
		lineNum := fmt.Sprintf("%*d", gutterW-1-gitOffset, lineIdx+1)
		currentGutterStyle := gutterStyle
		if lineIdx == buf.Cursor.Line {
			currentGutterStyle = activeGutterStyle
		}
		for i, ch := range lineNum {
			if x+gitOffset+i < x+gutterW-1 {
				e.screen.SetContent(x+gitOffset+i, screenY, ch, nil, currentGutterStyle)
			}
		}
		// Fold indicator in last gutter column (before the space)
		foldCh := ' '
		if buf.IsFolded(lineIdx) {
			foldCh = '▶'
		} else {
			s, _ := buf.FindFoldRange(lineIdx)
			if s >= 0 {
				foldCh = '▼'
			}
		}
		// Override with diagnostic gutter icon if present
		diagGutterStyle := currentGutterStyle
		if diagnostics != nil {
			for _, d := range diagnostics {
				if d.Range.Start.Line <= lineIdx && lineIdx <= d.Range.End.Line {
					if d.Severity == 1 {
						foldCh = '●'
						diagGutterStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorRed)
						break // error takes priority
					} else if d.Severity == 2 {
						foldCh = '▲'
						diagGutterStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorYellow)
					}
				}
			}
		}
		e.screen.SetContent(x+gutterW-1, screenY, foldCh, nil, diagGutterStyle)

		// Text content
		line := buf.Lines[lineIdx]
		styledIdx := lineIdx - startLine

		var tokens []highlight.Token
		if styledLines != nil && styledIdx >= 0 && styledIdx < len(styledLines) {
			tokens = styledLines[styledIdx].Tokens
		}

		col := 0        // buffer column position (byte-based)
		displayCol := 0 // visual column position (with tabs expanded)
		screenCol := x + gutterW

		if tokens != nil {
			for _, tok := range tokens {
				for _, ch := range tok.Text {
					if ch == '\t' {
						// Expand tab to spaces
						tabWidth := buf.TabSize - (displayCol % buf.TabSize)
						for i := 0; i < tabWidth; i++ {
							screenDisplayCol := displayCol - view.scrollX
							if screenDisplayCol >= 0 && screenDisplayCol < textW {
								style := tok.Style.Background(theme.Background)
								if e.isSelected(buf, lineIdx, col) {
									style = selStyle
								}
								if e.isSearchMatch(lineIdx, col) {
									style = matchStyle
								}
								if (lineIdx == bracketLine1 && col == bracketCol1) || (lineIdx == bracketLine2 && col == bracketCol2) {
									style = bracketStyle
								}
								e.screen.SetContent(screenCol+screenDisplayCol, screenY, ' ', nil, style)
							}
							displayCol++
						}
						col++ // tab is 1 character in buffer
					} else {
						screenDisplayCol := displayCol - view.scrollX
						if screenDisplayCol >= 0 && screenDisplayCol < textW {
							style := tok.Style.Background(theme.Background)
							if e.isSelected(buf, lineIdx, col) {
								style = selStyle
							}
							if e.isSearchMatch(lineIdx, col) {
								style = matchStyle
							}
							if (lineIdx == bracketLine1 && col == bracketCol1) || (lineIdx == bracketLine2 && col == bracketCol2) {
								style = bracketStyle
							}
							e.screen.SetContent(screenCol+screenDisplayCol, screenY, ch, nil, style)
						}
						w := runewidth.RuneWidth(ch)
						displayCol += w
						col++
					}
				}
			}
		} else {
			for _, ch := range line {
				if ch == '\t' {
					// Expand tab to spaces
					tabWidth := buf.TabSize - (displayCol % buf.TabSize)
					for i := 0; i < tabWidth; i++ {
						screenDisplayCol := displayCol - view.scrollX
						if screenDisplayCol >= 0 && screenDisplayCol < textW {
							style := lineStyle
							if e.isSelected(buf, lineIdx, col) {
								style = selStyle
							}
							if e.isSearchMatch(lineIdx, col) {
								style = matchStyle
							}
							if (lineIdx == bracketLine1 && col == bracketCol1) || (lineIdx == bracketLine2 && col == bracketCol2) {
								style = bracketStyle
							}
							e.screen.SetContent(screenCol+screenDisplayCol, screenY, ' ', nil, style)
						}
						displayCol++
					}
					col++ // tab is 1 character in buffer
				} else {
					screenDisplayCol := displayCol - view.scrollX
					if screenDisplayCol >= 0 && screenDisplayCol < textW {
						style := lineStyle
						if e.isSelected(buf, lineIdx, col) {
							style = selStyle
						}
						if e.isSearchMatch(lineIdx, col) {
							style = matchStyle
						}
						if (lineIdx == bracketLine1 && col == bracketCol1) || (lineIdx == bracketLine2 && col == bracketCol2) {
							style = bracketStyle
						}
						e.screen.SetContent(screenCol+screenDisplayCol, screenY, ch, nil, style)
					}
					w := runewidth.RuneWidth(ch)
					displayCol += w
					col++
				}
			}
		}

		// Clear rest of line
		startClear := displayCol - view.scrollX
		if startClear < 0 {
			startClear = 0
		}
		lineRuneLen := buffer.RuneLen(line)
		for c := startClear; c < textW; c++ {
			style := lineStyle
			// Past end of line, use rune length for selection check
			if e.isSelected(buf, lineIdx, lineRuneLen) {
				style = selStyle
			}
			e.screen.SetContent(screenCol+c, screenY, ' ', nil, style)
		}

		// Fold indicator after line content
		if buf.IsFolded(lineIdx) {
			foldCount := buf.FoldedLineCount(lineIdx)
			foldText := fmt.Sprintf(" ⋯ %d lines", foldCount)
			foldStartCol := displayCol - view.scrollX
			if foldStartCol < 0 {
				foldStartCol = 0
			}
			for i, ch := range foldText {
				dc := foldStartCol + i
				if dc >= 0 && dc < textW {
					e.screen.SetContent(screenCol+dc, screenY, ch, nil, foldStyle)
				}
			}
		}

		// Indent guides: draw │ at each tabSize boundary in leading whitespace
		// For empty/whitespace-only lines, look at surrounding context
		tabSize := buf.TabSize
		if tabSize > 0 {
			lineRunes := []rune(line)
			indentDisplayCol := 0 // visual indent position of current line

			// Calculate indent for current line
			for _, r := range lineRunes {
				if r == ' ' {
					indentDisplayCol++
				} else if r == '\t' {
					indentDisplayCol += tabSize - (indentDisplayCol % tabSize)
				} else {
					break
				}
			}

			// If line is empty or only whitespace, look at context
			isEmpty := len(strings.TrimSpace(line)) == 0
			if isEmpty && indentDisplayCol == 0 {
				// Look at previous non-empty line's indent
				for prevIdx := lineIdx - 1; prevIdx >= 0; prevIdx-- {
					prevLine := buf.Lines[prevIdx]
					if len(strings.TrimSpace(prevLine)) > 0 {
						// Found a non-empty line, use its indent
						prevRunes := []rune(prevLine)
						for _, r := range prevRunes {
							if r == ' ' {
								indentDisplayCol++
							} else if r == '\t' {
								indentDisplayCol += tabSize - (indentDisplayCol % tabSize)
							} else {
								break
							}
						}
						break
					}
				}
			}

			// Draw guides up to the indent level
			for c := tabSize; c < indentDisplayCol; c += tabSize {
				screenDisplayCol := c - view.scrollX
				if screenDisplayCol >= 0 && screenDisplayCol < textW {
					// Find buffer column at this display position for selection check
					bufCol := 0
					dispCol := 0
					for _, r := range lineRunes {
						if dispCol >= c {
							break
						}
						if r == '\t' {
							dispCol += tabSize - (dispCol % tabSize)
						} else {
							dispCol += runewidth.RuneWidth(r)
						}
						bufCol++
					}
					if !e.isSelected(buf, lineIdx, bufCol) {
						e.screen.SetContent(screenCol+screenDisplayCol, screenY, '│', nil, indentGuideStyle)
					}
				}
			}
		}

		// Render extra cursors on this line
		for _, ec := range buf.ExtraCursors {
			if ec.Line == lineIdx {
				ecDisplayCol := bufferColToDisplayCol(line, ec.Col, buf.TabSize)
				screenDisplayCol := ecDisplayCol - view.scrollX
				if screenDisplayCol >= 0 && screenDisplayCol < textW {
					ch := ' '
					lineRunes := []rune(line)
					if ec.Col < len(lineRunes) {
						ch = lineRunes[ec.Col]
					}
					e.screen.SetContent(screenCol+screenDisplayCol, screenY, ch, nil, extraCursorStyle)
				}
			}
		}

		// Render diagnostic underlines on this line
		if diagnostics != nil {
			for _, d := range diagnostics {
				if d.Range.Start.Line > lineIdx || d.Range.End.Line < lineIdx {
					continue
				}
				// Determine underline color by severity
				diagColor := tcell.ColorBlue
				if d.Severity == 1 {
					diagColor = tcell.ColorRed
				} else if d.Severity == 2 {
					diagColor = tcell.ColorYellow
				}
				// Calculate column range on this line
				startCol := 0
				if d.Range.Start.Line == lineIdx {
					startCol = d.Range.Start.Character
				}
				endCol := len([]rune(line))
				if d.Range.End.Line == lineIdx {
					endCol = d.Range.End.Character
				}
				if endCol <= startCol {
					endCol = startCol + 1
				}
				for c := startCol; c < endCol; c++ {
					dc := bufferColToDisplayCol(line, c, buf.TabSize) - view.scrollX
					if dc >= 0 && dc < textW {
						mainC, combC, st, _ := e.screen.GetContent(screenCol+dc, screenY)
						st = st.Foreground(diagColor).Underline(true)
						e.screen.SetContent(screenCol+dc, screenY, mainC, combC, st)
					}
				}
			}
		}
	}

	e.renderScrollbar(x, y, w, h, buf, view, theme)
}

// renderScrollbar draws a scrollbar on the rightmost column of the editor area.
func (e *Editor) renderScrollbar(x, y, w, h int, buf *buffer.Buffer, view *EditorView, theme *config.ColorScheme) {
	if h <= 0 || w <= 0 {
		return
	}

	totalLines := len(buf.Lines)
	if totalLines <= 0 {
		totalLines = 1
	}

	scrollCol := x + w - 1

	// Track style (dim background)
	trackStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.LineNumber)
	// Thumb style (lighter/brighter)
	thumbStyle := tcell.StyleDefault.Background(theme.Selection).Foreground(theme.Foreground)

	// Calculate thumb position and size
	thumbSize := h * h / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > h {
		thumbSize = h
	}

	thumbStart := 0
	if totalLines > h {
		thumbStart = view.scrollY * (h - thumbSize) / (totalLines - h)
	}
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart+thumbSize > h {
		thumbStart = h - thumbSize
	}

	// Collect search match lines
	matchLines := map[int]bool{}
	if e.dialog != nil && e.dialog.Type == ui.DialogFind {
		for _, m := range e.dialog.Matches {
			matchLines[m.Line] = true
		}
	}

	matchStyle := tcell.StyleDefault.Background(tcell.ColorYellow).Foreground(tcell.ColorYellow)

	for row := 0; row < h; row++ {
		screenY := y + row

		// Map this scrollbar row to a line range in the buffer
		lineStart := row * totalLines / h
		lineEnd := (row + 1) * totalLines / h

		// Check for search match in this range
		hasMatch := false
		for line := lineStart; line < lineEnd; line++ {
			if matchLines[line] {
				hasMatch = true
				break
			}
		}

		if hasMatch {
			e.screen.SetContent(scrollCol, screenY, '┃', nil, matchStyle)
		} else if row >= thumbStart && row < thumbStart+thumbSize {
			e.screen.SetContent(scrollCol, screenY, '┃', nil, thumbStyle)
		} else {
			e.screen.SetContent(scrollCol, screenY, ' ', nil, trackStyle)
		}
	}
}

// renderEditorWrapped renders the editor with soft word wrap enabled.
func (e *Editor) renderEditorWrapped(x, y, w, h int, buf *buffer.Buffer, view *EditorView,
	gutterW, textW int, gutterStyle, activeGutterStyle, lineStyle, selStyle, matchStyle, emptyLineStyle tcell.Style, theme *config.ColorScheme,
	bracketStyle tcell.Style, bracketLine1, bracketCol1, bracketLine2, bracketCol2 int) {

	// In word wrap mode, scrollY is still a buffer line index
	// We need to calculate visual rows from there

	// Get highlighted lines - request more than visible to handle wrap
	startLine := view.scrollY
	endLine := startLine + h // might need more, but this is a good estimate
	if endLine > len(buf.Lines) {
		endLine = len(buf.Lines)
	}

	code := strings.Join(buf.Lines, "\n")
	var styledLines []highlight.StyledLine
	if buf.Language != "" {
		styledLines = e.highlight.HighlightLines(code, buf.Language, startLine, endLine)
	}

	screenRow := 0
	for lineIdx := startLine; lineIdx < len(buf.Lines) && screenRow < h; lineIdx++ {
		if buf.IsHiddenByFold(lineIdx) {
			continue
		}
		line := buf.Lines[lineIdx]
		styledIdx := lineIdx - startLine

		var tokens []highlight.Token
		if styledLines != nil && styledIdx >= 0 && styledIdx < len(styledLines) {
			tokens = styledLines[styledIdx].Tokens
		}

		// Calculate wrap segments
		lineLen := len([]rune(line))
		if lineLen == 0 {
			lineLen = 1 // at least one visual row for empty lines
		}
		wrapRows := (lineLen + textW - 1) / textW
		if wrapRows == 0 {
			wrapRows = 1
		}

		for wrapIdx := 0; wrapIdx < wrapRows && screenRow < h; wrapIdx++ {
			screenY := y + screenRow

			// Git gutter indicator (first column when available)
			gitOffset := 0
			if e.gitGutter.available {
				gitOffset = 1
				gitCh := ' '
				gitStyle := gutterStyle
				if wrapIdx == 0 {
					gitStatus := e.gitGutter.StatusAt(lineIdx)
					switch gitStatus {
					case GitAdded:
						gitCh = '│'
						gitStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorGreen)
					case GitModified:
						gitCh = '│'
						gitStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorDarkCyan)
					case GitDeleted:
						gitCh = '▸'
						gitStyle = tcell.StyleDefault.Background(theme.Background).Foreground(tcell.ColorRed)
					}
				}
				e.screen.SetContent(x, screenY, gitCh, nil, gitStyle)
			}

			// Gutter: show line number only on first wrap row
			if wrapIdx == 0 {
				lineNum := fmt.Sprintf("%*d ", gutterW-1-gitOffset, lineIdx+1)
				currentGutterStyle := gutterStyle
				if lineIdx == buf.Cursor.Line {
					currentGutterStyle = activeGutterStyle
				}
				for i, ch := range lineNum {
					if x+gitOffset+i < x+gutterW {
						e.screen.SetContent(x+gitOffset+i, screenY, ch, nil, currentGutterStyle)
					}
				}
			} else {
				// Continuation row: empty gutter
				gs := gutterStyle
				if lineIdx == buf.Cursor.Line {
					gs = activeGutterStyle
				}
				for i := gitOffset; i < gutterW; i++ {
					e.screen.SetContent(x+i, screenY, ' ', nil, gs)
				}
			}

			// Render text segment for this wrap row
			colStart := wrapIdx * textW
			colEnd := colStart + textW
			screenCol := x + gutterW

			col := 0
			displayCol := 0

			if tokens != nil {
				for _, tok := range tokens {
					for _, ch := range tok.Text {
						if col >= colStart && col < colEnd {
							style := tok.Style.Background(theme.Background)
							if e.isSelected(buf, lineIdx, col) {
								style = selStyle
							}
							if e.isSearchMatch(lineIdx, col) {
								style = matchStyle
							}
							if (lineIdx == bracketLine1 && col == bracketCol1) || (lineIdx == bracketLine2 && col == bracketCol2) {
								style = bracketStyle
							}
							e.screen.SetContent(screenCol+displayCol, screenY, ch, nil, style)
							displayCol += runewidth.RuneWidth(ch)
						}
						col++
					}
				}
			} else {
				for _, ch := range line {
					if col >= colStart && col < colEnd {
						style := lineStyle
						if e.isSelected(buf, lineIdx, col) {
							style = selStyle
						}
						if e.isSearchMatch(lineIdx, col) {
							style = matchStyle
						}
						if (lineIdx == bracketLine1 && col == bracketCol1) || (lineIdx == bracketLine2 && col == bracketCol2) {
							style = bracketStyle
						}
						e.screen.SetContent(screenCol+displayCol, screenY, ch, nil, style)
						displayCol += runewidth.RuneWidth(ch)
					}
					col++
				}
			}

			// Clear rest of row
			for c := displayCol; c < textW; c++ {
				e.screen.SetContent(screenCol+c, screenY, ' ', nil, lineStyle)
			}

			screenRow++
		}
	}

	// Fill remaining rows
	for screenRow < h {
		screenY := y + screenRow
		for col := x; col < x+w; col++ {
			if col == x {
				e.screen.SetContent(col, screenY, '~', nil, emptyLineStyle)
			} else {
				e.screen.SetContent(col, screenY, ' ', nil, lineStyle)
			}
		}
		screenRow++
	}
}

// ensureCursorVisibleWrap handles cursor visibility when word wrap is enabled.
func (e *Editor) ensureCursorVisibleWrap(view *EditorView, buf *buffer.Buffer, textW, textH int) {
	// Validate cursor is in bounds
	if buf.Cursor.Line >= len(buf.Lines) {
		buf.Cursor.Line = len(buf.Lines) - 1
	}
	if buf.Cursor.Line < 0 {
		buf.Cursor.Line = 0
	}

	// In word wrap mode, we don't scroll horizontally
	view.scrollX = 0

	// Ensure cursor line is visible
	if buf.Cursor.Line < view.scrollY {
		view.scrollY = buf.Cursor.Line
	}

	// Count visual rows from scrollY to cursor line
	visualRows := 0
	for i := view.scrollY; i <= buf.Cursor.Line && i < len(buf.Lines); i++ {
		lineLen := len([]rune(buf.Lines[i]))
		wrapRows := 1
		if textW > 0 && lineLen > textW {
			wrapRows = (lineLen + textW - 1) / textW
		}
		if i == buf.Cursor.Line {
			// Add the wrap row the cursor is on
			cursorWrapRow := buf.Cursor.Col / textW
			if cursorWrapRow >= wrapRows {
				cursorWrapRow = wrapRows - 1
			}
			visualRows += cursorWrapRow + 1
		} else {
			visualRows += wrapRows
		}
	}

	// If cursor is below visible area, scroll down
	for visualRows > textH {
		// Move scrollY forward by one line
		if view.scrollY < len(buf.Lines) {
			lineLen := len([]rune(buf.Lines[view.scrollY]))
			wrapRows := 1
			if textW > 0 && lineLen > textW {
				wrapRows = (lineLen + textW - 1) / textW
			}
			visualRows -= wrapRows
			view.scrollY++
		} else {
			break
		}
	}
}

func (e *Editor) gutterWidth() int {
	buf := e.activeBuffer()
	if buf == nil {
		return 2
	}

	digits := 1
	for lines := len(buf.Lines); lines >= 10; lines /= 10 {
		digits++
	}
	w := digits + 1 // digits + indicator column
	if e.gitGutter.available {
		w++ // extra column for git indicators
	}
	return w
}

func (e *Editor) ensureCursorVisible(view *EditorView, buf *buffer.Buffer, textW, textH int) {
	const scrollMargin = 5 // keep cursor this many lines from edge

	// Validate cursor is in bounds
	if buf.Cursor.Line >= len(buf.Lines) {
		buf.Cursor.Line = len(buf.Lines) - 1
	}
	if buf.Cursor.Line < 0 {
		buf.Cursor.Line = 0
	}

	// Vertical - account for folded lines
	// Calculate effective margin (can't be more than half the screen)
	margin := scrollMargin
	if margin > textH/2 {
		margin = textH / 2
	}

	// Scroll up if cursor is too close to the top
	visibleAbove := 0
	for i := view.scrollY; i < buf.Cursor.Line && i < len(buf.Lines); i++ {
		if !buf.IsHiddenByFold(i) {
			visibleAbove++
		}
	}
	if visibleAbove < margin {
		// Need to scroll up
		target := buf.Cursor.Line
		count := 0
		for target > 0 && count < margin {
			target--
			if !buf.IsHiddenByFold(target) {
				count++
			}
		}
		view.scrollY = target
	}

	// Scroll down if cursor is too close to the bottom
	visibleCount := 0
	for i := view.scrollY; i <= buf.Cursor.Line && i < len(buf.Lines); i++ {
		if !buf.IsHiddenByFold(i) {
			visibleCount++
		}
	}
	for visibleCount > textH-margin {
		view.scrollY++
		for view.scrollY < len(buf.Lines) && buf.IsHiddenByFold(view.scrollY) {
			view.scrollY++
		}
		visibleCount--
	}

	// Still ensure cursor is at least visible (fallback for very small screens)
	if buf.Cursor.Line < view.scrollY {
		view.scrollY = buf.Cursor.Line
	}

	// Horizontal — scrollX is in display columns
	cursorDisplayCol := bufferColToDisplayCol(buf.Lines[buf.Cursor.Line], buf.Cursor.Col, buf.TabSize)
	if cursorDisplayCol < view.scrollX {
		view.scrollX = cursorDisplayCol
	}
	rightLimit := (textW * 7) / 10
	if rightLimit < 1 {
		rightLimit = 1
	}
	if rightLimit >= textW {
		rightLimit = textW - 1
	}
	if cursorDisplayCol > view.scrollX+rightLimit {
		view.scrollX = cursorDisplayCol - rightLimit
	}
}

func (e *Editor) isSelected(buf *buffer.Buffer, line, col int) bool {
	if buf.Selection == nil {
		return false
	}
	sel := *buf.Selection
	pos := buffer.Cursor{Line: line, Col: col}
	return sel.Contains(pos) && !pos.Equal(sel.End)
}

// findMatchingBracket finds the matching bracket for the character at or just
// before the cursor. It returns the position of the matching bracket or (-1,-1).
func (e *Editor) findMatchingBracket(buf *buffer.Buffer, line, col int) (int, int) {
	openers := map[rune]rune{'(': ')', '[': ']', '{': '}'}
	closers := map[rune]rune{')': '(', ']': '[', '}': '{'}

	getRune := func(l, c int) rune {
		if l < 0 || l >= len(buf.Lines) {
			return 0
		}
		runes := []rune(buf.Lines[l])
		if c < 0 || c >= len(runes) {
			return 0
		}
		return runes[c]
	}

	// Check character at cursor, then one before cursor
	positions := []int{col, col - 1}
	for _, pos := range positions {
		ch := getRune(line, pos)
		if ch == 0 {
			continue
		}
		if closer, ok := openers[ch]; ok {
			// Scan forward for matching closer
			depth := 1
			l, c := line, pos+1
			for l < len(buf.Lines) {
				runes := []rune(buf.Lines[l])
				for c < len(runes) {
					if runes[c] == ch {
						depth++
					} else if runes[c] == closer {
						depth--
						if depth == 0 {
							return l, c
						}
					}
					c++
				}
				l++
				c = 0
			}
			return -1, -1
		}
		if opener, ok := closers[ch]; ok {
			// Scan backward for matching opener
			depth := 1
			l, c := line, pos-1
			for l >= 0 {
				runes := []rune(buf.Lines[l])
				if c < 0 {
					c = len(runes) - 1
				}
				for c >= 0 {
					if runes[c] == ch {
						depth++
					} else if runes[c] == opener {
						depth--
						if depth == 0 {
							return l, c
						}
					}
					c--
				}
				l--
				if l >= 0 {
					c = len([]rune(buf.Lines[l])) - 1
				}
			}
			return -1, -1
		}
	}
	return -1, -1
}

// bracketAtCursor returns the position of the bracket under/before the cursor
// that has a match, so both ends can be highlighted.
func (e *Editor) bracketAtCursor(buf *buffer.Buffer, line, col int) (int, int) {
	brackets := map[rune]bool{'(': true, ')': true, '[': true, ']': true, '{': true, '}': true}
	getRune := func(l, c int) rune {
		if l < 0 || l >= len(buf.Lines) {
			return 0
		}
		runes := []rune(buf.Lines[l])
		if c < 0 || c >= len(runes) {
			return 0
		}
		return runes[c]
	}
	ch := getRune(line, col)
	if brackets[ch] {
		return line, col
	}
	ch = getRune(line, col-1)
	if brackets[ch] {
		return line, col - 1
	}
	return -1, -1
}

func (e *Editor) isSearchMatch(line, col int) bool {
	if e.dialog == nil || e.dialog.Type != ui.DialogFind {
		return false
	}
	for _, m := range e.dialog.Matches {
		if m.Line == line && col >= m.Col && col < m.Col+m.Length {
			return true
		}
	}
	return false
}
