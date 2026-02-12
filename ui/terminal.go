package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode/utf8"

	"editor/config"
	"github.com/atotto/clipboard"
	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
)

// TermOutputEvent carries PTY output to the main event loop.
type TermOutputEvent struct {
	tcell.EventTime
	Data []byte
}

type Cell struct {
	Ch    rune
	Style tcell.Style
}

type scrollbackLine struct {
	cells   []Cell
	wrapped bool // true if this line was soft-wrapped (not a hard newline)
}

var terminalClipboard string

type ansiState int

const (
	stateNormal ansiState = iota
	stateEscape
	stateCSI
	stateOSC
)

type Terminal struct {
	ptyFile    *os.File
	cmd        *exec.Cmd
	cells      [][]Cell
	curRow     int
	curCol     int
	rows       int
	cols       int
	scrollTop  int
	scrollBot  int
	focused    bool
	x, y, w, h int
	screen     tcell.Screen
	Theme      *config.ColorScheme

	// ANSI parser state
	state    ansiState
	csiBuf   []byte
	oscBuf   []byte
	curStyle tcell.Style

	// Scrollback
	scrollback []scrollbackLine
	viewOffset int    // 0 = live, >0 = scrolled up into scrollback
	wrapped    []bool // per-row: true = this row soft-wrapped (no hard newline)

	// Alternate screen buffer
	mainCells    [][]Cell
	altCells     [][]Cell
	altActive    bool
	cursorHidden bool

	// Saved cursor state (DECSC/DECRC)
	savedCurRow int
	savedCurCol int
	savedStyle  tcell.Style

	// Modes
	bracketedPaste bool
	appCursorKeys  bool // DECCKM: application cursor key mode

	// Terminal title (from OSC)
	Title string

	// Mouse selection
	selecting   bool
	selStartRow int
	selStartCol int
	selEndRow   int
	selEndCol   int

	mu sync.Mutex
}

func NewTerminal(screen tcell.Screen, shell string, rows, cols int) *Terminal {
	t := &Terminal{
		rows:      rows,
		cols:      cols,
		screen:    screen,
		curStyle:  tcell.StyleDefault,
		scrollTop: 0,
		scrollBot: rows - 1,
	}
	t.initCells()

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return t
	}

	t.cmd = cmd
	t.ptyFile = ptmx

	// Read PTY output and post events
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			data := make([]byte, n)
			copy(data, buf[:n])
			ev := &TermOutputEvent{Data: data}
			ev.SetEventNow()
			// Use PostEventWait to guarantee no data is lost when the
			// event queue is full (e.g. during heavy output from long commands)
			screen.PostEventWait(ev)
		}
	}()

	return t
}

func (t *Terminal) initCells() {
	t.cells = make([][]Cell, t.rows)
	for i := range t.cells {
		t.cells[i] = make([]Cell, t.cols)
		for j := range t.cells[i] {
			t.cells[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}
	t.wrapped = make([]bool, t.rows)
}

func (t *Terminal) Resize(rows, cols int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldRows := t.rows
	oldCols := t.cols

	if oldCols == cols && oldRows == rows {
		return // No change
	}

	// Save old scroll state
	oldViewOffset := t.viewOffset

	t.rows = rows
	t.cols = cols
	t.scrollTop = 0
	t.scrollBot = rows - 1

	if t.altActive {
		// Alt screen: simple resize without reflow (like vim, htop)
		t.resizeSimple(rows, cols, oldRows, oldCols)
	} else {
		// Main screen: reflow content
		t.resizeWithReflow(rows, cols, oldRows, oldCols)
	}

	// Preserve scroll position (adjusted for row count changes)
	if oldViewOffset > 0 {
		// Adjust viewOffset: if scrollback size changed, scale proportionally
		maxOffset := len(t.scrollback) + t.rows
		if oldViewOffset > maxOffset {
			t.viewOffset = maxOffset
		} else {
			t.viewOffset = oldViewOffset
		}
	}

	// Adjust cursor position
	if t.curRow >= rows {
		t.curRow = rows - 1
	}
	if t.curCol >= cols {
		t.curCol = cols - 1
	}

	if t.ptyFile != nil {
		pty.Setsize(t.ptyFile, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}
}

// resizeSimple does a simple cell copy without reflow (used for alt screen)
func (t *Terminal) resizeSimple(rows, cols, oldRows, oldCols int) {
	newCells := make([][]Cell, rows)
	for i := range newCells {
		newCells[i] = make([]Cell, cols)
		for j := range newCells[i] {
			newCells[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}

	copyRows := oldRows
	if copyRows > rows {
		copyRows = rows
	}
	for i := 0; i < copyRows && i < len(t.cells); i++ {
		copyCols := oldCols
		if copyCols > cols {
			copyCols = cols
		}
		for j := 0; j < copyCols && j < len(t.cells[i]); j++ {
			newCells[i][j] = t.cells[i][j]
		}
	}
	t.cells = newCells
	t.wrapped = make([]bool, rows)

	// Also resize main cells
	if t.mainCells != nil {
		newMain := make([][]Cell, rows)
		for i := range newMain {
			newMain[i] = make([]Cell, cols)
			for j := range newMain[i] {
				newMain[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
			}
		}
		for i := 0; i < len(t.mainCells) && i < rows; i++ {
			for j := 0; j < len(t.mainCells[i]) && j < cols; j++ {
				newMain[i][j] = t.mainCells[i][j]
			}
		}
		t.mainCells = newMain
	}
}

// logicalLine represents a full line of terminal content (may span multiple screen rows)
type logicalLine struct {
	cells []Cell
}

// resizeWithReflow reconstructs logical lines from scrollback+cells and re-wraps at new width
func (t *Terminal) resizeWithReflow(rows, cols, oldRows, oldCols int) {
	// Step 1: Find how many live rows actually have content or are before cursor
	// We need to include all rows up to and including the cursor row
	liveRows := t.curRow + 1
	if liveRows > len(t.cells) {
		liveRows = len(t.cells)
	}
	// Also include any rows below cursor that have content
	for r := liveRows; r < len(t.cells) && r < oldRows; r++ {
		hasContent := false
		for c := 0; c < len(t.cells[r]); c++ {
			if t.cells[r][c].Ch != ' ' && t.cells[r][c].Ch != 0 {
				hasContent = true
				break
			}
		}
		if hasContent {
			liveRows = r + 1
		}
	}

	// Step 2: Build logical lines from scrollback + live cells
	var logLines []logicalLine

	// Process scrollback
	var currentLine []Cell
	for _, sb := range t.scrollback {
		currentLine = append(currentLine, trimTrailingSpaces(sb.cells)...)
		if !sb.wrapped {
			logLines = append(logLines, logicalLine{cells: currentLine})
			currentLine = nil
		}
	}

	// Process live cells, joining with any pending wrapped scrollback line
	for r := 0; r < liveRows; r++ {
		rowCells := make([]Cell, len(t.cells[r]))
		copy(rowCells, t.cells[r])
		currentLine = append(currentLine, trimTrailingSpaces(rowCells)...)

		isWrapped := false
		if r < len(t.wrapped) {
			isWrapped = t.wrapped[r]
		}

		if !isWrapped {
			logLines = append(logLines, logicalLine{cells: currentLine})
			currentLine = nil
		}
	}
	// Flush any remaining content
	if len(currentLine) > 0 {
		logLines = append(logLines, logicalLine{cells: currentLine})
	}

	// Step 3: Compute cursor position in the logical line stream
	// Figure out the logical line index and character offset for cursor
	// by replaying the scrollback+live assembly
	sbLogLines := 0
	var pendingLen int
	for _, sb := range t.scrollback {
		pendingLen += len(trimTrailingSpaces(sb.cells))
		if !sb.wrapped {
			sbLogLines++
			pendingLen = 0
		}
	}

	cursorLogLine := sbLogLines
	cursorLogCol := pendingLen // carry over from wrapped scrollback
	for r := 0; r < liveRows; r++ {
		rowLen := len(trimTrailingSpaces(t.cells[r]))
		if r == t.curRow {
			cursorLogCol += t.curCol
			break
		}
		cursorLogCol += rowLen
		isWrapped := false
		if r < len(t.wrapped) {
			isWrapped = t.wrapped[r]
		}
		if !isWrapped {
			cursorLogLine++
			cursorLogCol = 0
		}
	}

	// Step 4: Re-wrap logical lines at new width
	var newScrollback []scrollbackLine
	var newCellRows [][]Cell
	var newWrapped []bool

	// Track cursor position in new layout
	newCurRow := 0
	newCurCol := 0
	cursorFound := false
	currentLogIdx := 0

	for _, ll := range logLines {
		lineLen := len(ll.cells)
		if lineLen == 0 {
			// Empty line
			emptyRow := make([]Cell, cols)
			for j := range emptyRow {
				emptyRow[j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
			}

			// Check cursor
			if !cursorFound && currentLogIdx == cursorLogLine {
				newCurRow = len(newScrollback) + len(newCellRows)
				newCurCol = 0
				cursorFound = true
			}

			newCellRows = append(newCellRows, emptyRow)
			newWrapped = append(newWrapped, false)
			currentLogIdx++
			continue
		}

		// Break this logical line into rows of 'cols' width
		charIdx := 0
		for charIdx < lineLen {
			endIdx := charIdx + cols
			if endIdx > lineLen {
				endIdx = lineLen
			}

			row := make([]Cell, cols)
			for j := range row {
				row[j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
			}
			copy(row, ll.cells[charIdx:endIdx])

			isWrapped := endIdx < lineLen // more content follows = this row is wrapped

			// Check if cursor falls in this row
			if !cursorFound && currentLogIdx == cursorLogLine {
				if cursorLogCol >= charIdx && cursorLogCol < endIdx {
					newCurRow = len(newScrollback) + len(newCellRows)
					newCurCol = cursorLogCol - charIdx
					cursorFound = true
				} else if cursorLogCol >= lineLen {
					// Cursor is at end of this logical line
					if !isWrapped {
						newCurRow = len(newScrollback) + len(newCellRows)
						newCurCol = lineLen - charIdx
						if newCurCol > cols {
							newCurCol = cols
						}
						cursorFound = true
					}
				}
			}

			newCellRows = append(newCellRows, row)
			newWrapped = append(newWrapped, isWrapped)

			charIdx = endIdx
		}
		currentLogIdx++
	}

	// If cursor wasn't found, put it at the end
	if !cursorFound {
		newCurRow = len(newCellRows) - 1
		if newCurRow < 0 {
			newCurRow = 0
		}
		newCurCol = 0
	}

	// Step 5: Split into scrollback and screen cells
	// We want the last 'rows' worth of cell rows on screen, rest goes to scrollback
	totalRows := len(newCellRows)

	// Ensure cursor is visible on screen
	// The screen should show rows such that cursor is visible
	screenStart := totalRows - rows
	if screenStart < 0 {
		screenStart = 0
	}
	// Make sure cursor row is on screen
	if newCurRow < screenStart {
		screenStart = newCurRow
	}
	if newCurRow >= screenStart+rows {
		screenStart = newCurRow - rows + 1
	}

	// Everything before screenStart goes to scrollback
	newScrollback = nil
	for i := 0; i < screenStart; i++ {
		w := false
		if i < len(newWrapped) {
			w = newWrapped[i]
		}
		newScrollback = append(newScrollback, scrollbackLine{cells: newCellRows[i], wrapped: w})
	}

	// Build new screen
	newCells := make([][]Cell, rows)
	finalWrapped := make([]bool, rows)
	for i := 0; i < rows; i++ {
		srcIdx := screenStart + i
		if srcIdx < totalRows {
			newCells[i] = newCellRows[srcIdx]
			if srcIdx < len(newWrapped) {
				finalWrapped[i] = newWrapped[srcIdx]
			}
		} else {
			newCells[i] = make([]Cell, cols)
			for j := range newCells[i] {
				newCells[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
			}
		}
	}

	// Append old scrollback entries that weren't part of our logical lines
	// (they were already included in logLines construction)
	t.scrollback = newScrollback
	t.cells = newCells
	t.wrapped = finalWrapped

	// Adjust cursor to screen-relative position
	t.curRow = newCurRow - screenStart
	t.curCol = newCurCol
	if t.curRow < 0 {
		t.curRow = 0
	}
	if t.curRow >= rows {
		t.curRow = rows - 1
	}
	if t.curCol >= cols {
		t.curCol = cols - 1
	}
	if t.curCol < 0 {
		t.curCol = 0
	}
}

// trimTrailingSpaces removes trailing space cells from a row
func trimTrailingSpaces(cells []Cell) []Cell {
	end := len(cells)
	for end > 0 && (cells[end-1].Ch == ' ' || cells[end-1].Ch == 0) {
		// Check if style is non-default (colored space should be preserved)
		if cells[end-1].Style != tcell.StyleDefault {
			break
		}
		end--
	}
	result := make([]Cell, end)
	copy(result, cells[:end])
	return result
}

func (t *Terminal) ProcessOutput(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Snap back to live view on new output ONLY if user is already at live view
	// This way: resize output won't cause unwanted snapping, and users scrolled
	// up in history won't be interrupted by new output
	if t.viewOffset == 0 {
		// User is at live view, keep them there as new output arrives
		// (viewOffset is already 0, but this documents the intent)
	}
	// If viewOffset > 0, user has scrolled up - don't interrupt them

	i := 0
	for i < len(data) {
		b := data[i]
		switch t.state {
		case stateNormal:
			switch b {
			case 0x1b: // ESC
				t.state = stateEscape
			case '\r':
				t.curCol = 0
			case '\n':
				t.lineFeed()
			case '\b':
				if t.curCol > 0 {
					t.curCol--
				}
			case '\t':
				next := ((t.curCol / 8) + 1) * 8
				if next >= t.cols {
					next = t.cols - 1
				}
				t.curCol = next
			case 0x07: // BEL - ignore
			case 0x00, 0x0e, 0x0f: // NUL, SO, SI - ignore
			default:
				if b >= 0x20 || b == 0x0d {
					// Decode UTF-8
					r, size := utf8.DecodeRune(data[i:])
					if r != utf8.RuneError && r >= 0x20 {
						t.putChar(r)
						i += size - 1 // -1 because loop increments
					}
				}
			}
		case stateEscape:
			switch b {
			case '[':
				t.state = stateCSI
				t.csiBuf = t.csiBuf[:0]
			case ']':
				t.state = stateOSC
				t.oscBuf = t.oscBuf[:0]
			case '(':
				// Charset designation, skip next byte
				i++
				t.state = stateNormal
			case 'M': // Reverse index
				t.reverseIndex()
				t.state = stateNormal
			case '7': // DECSC - save cursor
				t.savedCurRow = t.curRow
				t.savedCurCol = t.curCol
				t.savedStyle = t.curStyle
				t.state = stateNormal
			case '8': // DECRC - restore cursor
				t.curRow = t.savedCurRow
				t.curCol = t.savedCurCol
				t.curStyle = t.savedStyle
				if t.curRow >= t.rows {
					t.curRow = t.rows - 1
				}
				if t.curCol >= t.cols {
					t.curCol = t.cols - 1
				}
				t.state = stateNormal
			case '=', '>': // Keypad modes - ignore
				t.state = stateNormal
			default:
				t.state = stateNormal
			}
		case stateCSI:
			if b >= 0x40 && b <= 0x7e {
				// Final byte
				t.csiBuf = append(t.csiBuf, b)
				t.processCSI()
				t.state = stateNormal
			} else {
				t.csiBuf = append(t.csiBuf, b)
			}
		case stateOSC:
			if b == 0x07 || b == 0x1b {
				// End of OSC sequence (BEL or ESC)
				if b == 0x1b && i+1 < len(data) && data[i+1] == '\\' {
					i++ // skip ST
				}
				t.processOSC()
				t.state = stateNormal
			} else {
				t.oscBuf = append(t.oscBuf, b)
			}
		}
		i++
	}
}

func (t *Terminal) putChar(ch rune) {
	if t.curRow < 0 || t.curRow >= t.rows || t.curCol < 0 {
		return
	}
	if t.curCol >= t.cols {
		// Mark current row as soft-wrapped
		if t.curRow >= 0 && t.curRow < len(t.wrapped) {
			t.wrapped[t.curRow] = true
		}
		t.curCol = 0
		t.lineFeed()
	}
	t.cells[t.curRow][t.curCol] = Cell{Ch: ch, Style: t.curStyle}
	t.curCol++
}

func (t *Terminal) lineFeed() {
	// Current row has a hard newline (not wrapped)
	if t.curRow >= 0 && t.curRow < len(t.wrapped) {
		t.wrapped[t.curRow] = false
	}
	if t.curRow == t.scrollBot {
		t.scrollUp()
	} else if t.curRow < t.rows-1 {
		t.curRow++
	}
}

func (t *Terminal) reverseIndex() {
	if t.curRow == t.scrollTop {
		t.scrollDown()
	} else if t.curRow > 0 {
		t.curRow--
	}
}

func (t *Terminal) scrollUp() {
	// Save top line to scrollback (only in main screen, not alt screen)
	if !t.altActive {
		saved := make([]Cell, t.cols)
		copy(saved, t.cells[t.scrollTop])
		wasWrapped := false
		if t.scrollTop < len(t.wrapped) {
			wasWrapped = t.wrapped[t.scrollTop]
		}
		t.scrollback = append(t.scrollback, scrollbackLine{cells: saved, wrapped: wasWrapped})

		if len(t.scrollback) > 10000 {
			t.scrollback = t.scrollback[1:]
		}
	}

	for i := t.scrollTop; i < t.scrollBot; i++ {
		t.cells[i] = t.cells[i+1]
		if i < len(t.wrapped) && i+1 < len(t.wrapped) {
			t.wrapped[i] = t.wrapped[i+1]
		}
	}
	t.cells[t.scrollBot] = make([]Cell, t.cols)
	for j := range t.cells[t.scrollBot] {
		t.cells[t.scrollBot][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
	}
	if t.scrollBot < len(t.wrapped) {
		t.wrapped[t.scrollBot] = false
	}
}

func (t *Terminal) scrollDown() {
	for i := t.scrollBot; i > t.scrollTop; i-- {
		t.cells[i] = t.cells[i-1]
		if i < len(t.wrapped) && i-1 < len(t.wrapped) {
			t.wrapped[i] = t.wrapped[i-1]
		}
	}
	t.cells[t.scrollTop] = make([]Cell, t.cols)
	for j := range t.cells[t.scrollTop] {
		t.cells[t.scrollTop][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
	}
	if t.scrollTop < len(t.wrapped) {
		t.wrapped[t.scrollTop] = false
	}
}

func (t *Terminal) processCSI() {
	if len(t.csiBuf) == 0 {
		return
	}
	final := t.csiBuf[len(t.csiBuf)-1]
	params := string(t.csiBuf[:len(t.csiBuf)-1])

	switch final {
	case 'm': // SGR
		t.processSGR(params)
	case 'A': // Cursor up
		n := parseParam(params, 1)
		t.curRow -= n
		if t.curRow < 0 {
			t.curRow = 0
		}
	case 'B': // Cursor down
		n := parseParam(params, 1)
		t.curRow += n
		if t.curRow >= t.rows {
			t.curRow = t.rows - 1
		}
	case 'C': // Cursor forward
		n := parseParam(params, 1)
		t.curCol += n
		if t.curCol >= t.cols {
			t.curCol = t.cols - 1
		}
	case 'D': // Cursor back
		n := parseParam(params, 1)
		t.curCol -= n
		if t.curCol < 0 {
			t.curCol = 0
		}
	case 'H', 'f': // Cursor position
		row, col := parseParamPair(params, 1, 1)
		t.curRow = row - 1
		t.curCol = col - 1
		if t.curRow < 0 {
			t.curRow = 0
		}
		if t.curRow >= t.rows {
			t.curRow = t.rows - 1
		}
		if t.curCol < 0 {
			t.curCol = 0
		}
		if t.curCol >= t.cols {
			t.curCol = t.cols - 1
		}
	case 'J': // Erase display
		n := parseParam(params, 0)
		t.eraseDisplay(n)
	case 'K': // Erase line
		n := parseParam(params, 0)
		t.eraseLine(n)
	case 'r': // Set scroll region
		top, bot := parseParamPair(params, 1, t.rows)
		t.scrollTop = top - 1
		t.scrollBot = bot - 1
		if t.scrollTop < 0 {
			t.scrollTop = 0
		}
		if t.scrollBot >= t.rows {
			t.scrollBot = t.rows - 1
		}
	case 'L': // Insert lines
		n := parseParam(params, 1)
		for i := 0; i < n; i++ {
			t.scrollDown()
		}
	case 'M': // Delete lines
		n := parseParam(params, 1)
		for i := 0; i < n; i++ {
			t.scrollUp()
		}
	case 'G': // Cursor horizontal absolute
		n := parseParam(params, 1)
		t.curCol = n - 1
		if t.curCol < 0 {
			t.curCol = 0
		}
		if t.curCol >= t.cols {
			t.curCol = t.cols - 1
		}
	case 'd': // Cursor vertical absolute
		n := parseParam(params, 1)
		t.curRow = n - 1
		if t.curRow < 0 {
			t.curRow = 0
		}
		if t.curRow >= t.rows {
			t.curRow = t.rows - 1
		}
	case 'h', 'l': // Set/reset mode
		set := final == 'h'
		t.processMode(params, set)
	case 's': // Save cursor position (ANSI)
		t.savedCurRow = t.curRow
		t.savedCurCol = t.curCol
		t.savedStyle = t.curStyle
	case 'u': // Restore cursor position (ANSI)
		t.curRow = t.savedCurRow
		t.curCol = t.savedCurCol
		t.curStyle = t.savedStyle
		if t.curRow >= t.rows {
			t.curRow = t.rows - 1
		}
		if t.curCol >= t.cols {
			t.curCol = t.cols - 1
		}
	case 'S': // Scroll up N lines
		n := parseParam(params, 1)
		for i := 0; i < n; i++ {
			t.scrollUp()
		}
	case 'T': // Scroll down N lines
		n := parseParam(params, 1)
		for i := 0; i < n; i++ {
			t.scrollDown()
		}
	case 'E': // Cursor next line
		n := parseParam(params, 1)
		t.curRow += n
		if t.curRow >= t.rows {
			t.curRow = t.rows - 1
		}
		t.curCol = 0
	case 'F': // Cursor previous line
		n := parseParam(params, 1)
		t.curRow -= n
		if t.curRow < 0 {
			t.curRow = 0
		}
		t.curCol = 0
	case 'n': // Device status report
		if parseParam(params, 0) == 6 {
			// Respond with cursor position
			response := fmt.Sprintf("\x1b[%d;%dR", t.curRow+1, t.curCol+1)
			if t.ptyFile != nil {
				t.ptyFile.Write([]byte(response))
			}
		}
	case 'P': // Delete chars
		n := parseParam(params, 1)
		row := t.cells[t.curRow]
		for i := t.curCol; i < t.cols-n; i++ {
			row[i] = row[i+n]
		}
		for i := t.cols - n; i < t.cols; i++ {
			if i >= 0 {
				row[i] = Cell{Ch: ' ', Style: tcell.StyleDefault}
			}
		}
	case '@': // Insert chars
		n := parseParam(params, 1)
		row := t.cells[t.curRow]
		for i := t.cols - 1; i >= t.curCol+n; i-- {
			row[i] = row[i-n]
		}
		for i := t.curCol; i < t.curCol+n && i < t.cols; i++ {
			row[i] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	case 'X': // Erase chars
		n := parseParam(params, 1)
		for i := t.curCol; i < t.curCol+n && i < t.cols; i++ {
			t.cells[t.curRow][i] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}
}

func (t *Terminal) processMode(params string, set bool) {
	// Check for DEC private mode (starts with ?)
	if strings.HasPrefix(params, "?") {
		codes := splitParams(params[1:])
		for _, code := range codes {
			switch code {
			case 1: // DECCKM - application cursor keys
				t.appCursorKeys = set
			case 25: // DECTCEM - cursor visibility
				t.cursorHidden = !set
			case 47: // Alternate screen buffer (old style)
				if set {
					t.enterAltScreen()
				} else {
					t.exitAltScreen()
				}
			case 1047: // Alternate screen buffer
				if set {
					t.enterAltScreen()
				} else {
					t.exitAltScreen()
				}
			case 1049: // Alternate screen buffer with save/restore cursor
				if set {
					t.savedCurRow = t.curRow
					t.savedCurCol = t.curCol
					t.savedStyle = t.curStyle
					t.enterAltScreen()
				} else {
					t.exitAltScreen()
					t.curRow = t.savedCurRow
					t.curCol = t.savedCurCol
					t.curStyle = t.savedStyle
					if t.curRow >= t.rows {
						t.curRow = t.rows - 1
					}
					if t.curCol >= t.cols {
						t.curCol = t.cols - 1
					}
				}
			case 2004: // Bracketed paste mode
				t.bracketedPaste = set
			}
		}
		return
	}
	// Standard (non-private) modes are currently unsupported.
}

func (t *Terminal) enterAltScreen() {
	if t.altActive {
		return
	}
	// Save main screen
	t.mainCells = t.cells

	// Create fresh alt screen
	t.altCells = make([][]Cell, t.rows)
	for i := range t.altCells {
		t.altCells[i] = make([]Cell, t.cols)
		for j := range t.altCells[i] {
			t.altCells[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}
	t.cells = t.altCells
	t.altActive = true
	t.scrollTop = 0
	t.scrollBot = t.rows - 1
}

func (t *Terminal) exitAltScreen() {
	if !t.altActive {
		return
	}
	// Restore main screen
	t.cells = t.mainCells
	t.mainCells = nil
	t.altCells = nil
	t.altActive = false
	t.scrollTop = 0
	t.scrollBot = t.rows - 1
}

func (t *Terminal) processOSC() {
	s := string(t.oscBuf)
	// OSC format: "code;content"
	idx := strings.IndexByte(s, ';')
	if idx < 0 || idx+1 > len(s) {
		return
	}
	code := s[:idx]
	content := s[idx+1:]
	switch code {
	case "0", "2": // Set window title
		t.Title = content
	case "1": // Set icon name (treat as title)
		t.Title = content
	}
}

func (t *Terminal) processSGR(params string) {
	if params == "" || params == "0" {
		t.curStyle = tcell.StyleDefault
		return
	}

	codes := splitParams(params)
	i := 0
	for i < len(codes) {
		c := codes[i]
		switch {
		case c == 0:
			t.curStyle = tcell.StyleDefault
		case c == 1:
			t.curStyle = t.curStyle.Bold(true)
		case c == 2:
			t.curStyle = t.curStyle.Dim(true)
		case c == 3:
			t.curStyle = t.curStyle.Italic(true)
		case c == 4:
			t.curStyle = t.curStyle.Underline(true)
		case c == 7:
			t.curStyle = t.curStyle.Reverse(true)
		case c == 22:
			t.curStyle = t.curStyle.Bold(false).Dim(false)
		case c == 23:
			t.curStyle = t.curStyle.Italic(false)
		case c == 24:
			t.curStyle = t.curStyle.Underline(false)
		case c == 27:
			t.curStyle = t.curStyle.Reverse(false)
		case c >= 30 && c <= 37:
			t.curStyle = t.curStyle.Foreground(ansiColor(c - 30))
		case c == 38:
			if i+1 < len(codes) && codes[i+1] == 5 && i+2 < len(codes) {
				t.curStyle = t.curStyle.Foreground(tcell.PaletteColor(int(codes[i+2])))
				i += 2
			} else if i+1 < len(codes) && codes[i+1] == 2 && i+4 < len(codes) {
				r, g, b := codes[i+2], codes[i+3], codes[i+4]
				t.curStyle = t.curStyle.Foreground(tcell.NewRGBColor(int32(r), int32(g), int32(b)))
				i += 4
			}
		case c == 39:
			t.curStyle = t.curStyle.Foreground(tcell.ColorDefault)
		case c >= 40 && c <= 47:
			t.curStyle = t.curStyle.Background(ansiColor(c - 40))
		case c == 48:
			if i+1 < len(codes) && codes[i+1] == 5 && i+2 < len(codes) {
				t.curStyle = t.curStyle.Background(tcell.PaletteColor(int(codes[i+2])))
				i += 2
			} else if i+1 < len(codes) && codes[i+1] == 2 && i+4 < len(codes) {
				r, g, b := codes[i+2], codes[i+3], codes[i+4]
				t.curStyle = t.curStyle.Background(tcell.NewRGBColor(int32(r), int32(g), int32(b)))
				i += 4
			}
		case c == 49:
			t.curStyle = t.curStyle.Background(tcell.ColorDefault)
		case c >= 90 && c <= 97:
			t.curStyle = t.curStyle.Foreground(ansiBrightColor(c - 90))
		case c >= 100 && c <= 107:
			t.curStyle = t.curStyle.Background(ansiBrightColor(c - 100))
		}
		i++
	}
}

func (t *Terminal) eraseDisplay(mode int) {
	clearStyle := tcell.StyleDefault
	switch mode {
	case 0: // Below
		// Clear from cursor to end of line
		for j := t.curCol; j < t.cols; j++ {
			t.cells[t.curRow][j] = Cell{Ch: ' ', Style: clearStyle}
		}
		if t.curRow < len(t.wrapped) {
			t.wrapped[t.curRow] = false
		}
		// Clear lines below
		for i := t.curRow + 1; i < t.rows; i++ {
			for j := 0; j < t.cols; j++ {
				t.cells[i][j] = Cell{Ch: ' ', Style: clearStyle}
			}
			if i < len(t.wrapped) {
				t.wrapped[i] = false
			}
		}
	case 1: // Above
		for j := 0; j <= t.curCol; j++ {
			t.cells[t.curRow][j] = Cell{Ch: ' ', Style: clearStyle}
		}
		for i := 0; i < t.curRow; i++ {
			for j := 0; j < t.cols; j++ {
				t.cells[i][j] = Cell{Ch: ' ', Style: clearStyle}
			}
			if i < len(t.wrapped) {
				t.wrapped[i] = false
			}
		}
	case 2, 3: // All
		for i := 0; i < t.rows; i++ {
			for j := 0; j < t.cols; j++ {
				t.cells[i][j] = Cell{Ch: ' ', Style: clearStyle}
			}
			if i < len(t.wrapped) {
				t.wrapped[i] = false
			}
		}
	}
}

func (t *Terminal) eraseLine(mode int) {
	clearStyle := tcell.StyleDefault
	switch mode {
	case 0: // Right
		for j := t.curCol; j < t.cols; j++ {
			t.cells[t.curRow][j] = Cell{Ch: ' ', Style: clearStyle}
		}
		// Erasing to end of line breaks the wrap
		if t.curRow < len(t.wrapped) {
			t.wrapped[t.curRow] = false
		}
	case 1: // Left
		for j := 0; j <= t.curCol; j++ {
			t.cells[t.curRow][j] = Cell{Ch: ' ', Style: clearStyle}
		}
	case 2: // All
		for j := 0; j < t.cols; j++ {
			t.cells[t.curRow][j] = Cell{Ch: ' ', Style: clearStyle}
		}
		if t.curRow < len(t.wrapped) {
			t.wrapped[t.curRow] = false
		}
	}
}

func (t *Terminal) Render(screen tcell.Screen, x, y, width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.x = x
	t.y = y
	t.w = width
	t.h = height

	theme := t.Theme
	if theme == nil {
		theme = config.Themes["monokai"]
	}

	// Draw separator line at top with theme
	sepStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.TreeBorder)
	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, '─', nil, sepStyle)
	}

	renderRows := t.rows
	if renderRows > height-1 {
		renderRows = height - 1
	}

	if t.viewOffset > 0 && !t.altActive {
		// Scrollback mode: render mix of scrollback + live cells
		sbLen := len(t.scrollback)
		// scrollStart is the index into scrollback where we begin rendering
		scrollStart := sbLen - t.viewOffset

		bgStyle := tcell.StyleDefault.Background(theme.Background)

		for row := 0; row < renderRows; row++ {
			srcIdx := scrollStart + row
			if srcIdx < 0 {
				// Before scrollback — blank line with theme background
				for col := 0; col < width; col++ {
					screen.SetContent(x+col, y+1+row, ' ', nil, bgStyle)
				}
			} else if srcIdx < sbLen {
				// From scrollback
				sbRow := t.scrollback[srcIdx].cells
				for col := 0; col < width; col++ {
					if col < len(sbRow) {
						// Apply theme background to scrollback cells
						cellStyle := sbRow[col].Style.Background(theme.Background)
						cellStyle = t.selectionStyle(srcIdx, col, cellStyle)
						screen.SetContent(x+col, y+1+row, sbRow[col].Ch, nil, cellStyle)
					} else {
						screen.SetContent(x+col, y+1+row, ' ', nil, bgStyle)
					}
				}
			} else {
				// From live cells
				liveRow := srcIdx - sbLen
				if liveRow >= 0 && liveRow < t.rows {
					for col := 0; col < width; col++ {
						if col < t.cols {
							cell := t.cells[liveRow][col]
							// Apply theme background to live cells in scrollback mode
							cellStyle := cell.Style.Background(theme.Background)
							cellStyle = t.selectionStyle(srcIdx, col, cellStyle)
							screen.SetContent(x+col, y+1+row, cell.Ch, nil, cellStyle)
						} else {
							screen.SetContent(x+col, y+1+row, ' ', nil, bgStyle)
						}
					}
				} else {
					// Past end of live cells
					for col := 0; col < width; col++ {
						screen.SetContent(x+col, y+1+row, ' ', nil, bgStyle)
					}
				}
			}
		}

		// Show scrollback indicator on separator line
		indicator := fmt.Sprintf(" ↑ %d lines ", t.viewOffset)
		indStyle := tcell.StyleDefault.Background(theme.StatusBarModeBg).Foreground(tcell.ColorWhite).Bold(true)
		indX := x + width - len(indicator)
		for i, ch := range indicator {
			if indX+i >= x && indX+i < x+width {
				screen.SetContent(indX+i, y, ch, nil, indStyle)
			}
		}

		// Hide cursor when viewing scrollback
		screen.HideCursor()
	} else {
		// Live mode: render current cells
		bgStyle := tcell.StyleDefault.Background(theme.Background)
		for row := 0; row < renderRows; row++ {
			for col := 0; col < t.cols && col < width; col++ {
				cell := t.cells[row][col]
				// Apply theme background to terminal cells
				cellStyle := cell.Style.Background(theme.Background)
				cellStyle = t.selectionStyle(len(t.scrollback)+row, col, cellStyle)
				screen.SetContent(x+col, y+1+row, cell.Ch, nil, cellStyle)
			}
			// Clear rest of line with theme background
			for col := t.cols; col < width; col++ {
				screen.SetContent(x+col, y+1+row, ' ', nil, bgStyle)
			}
		}

		// Clear any extra rows below terminal content (if height > renderRows)
		for row := renderRows; row < height-1; row++ {
			for col := 0; col < width; col++ {
				screen.SetContent(x+col, y+1+row, ' ', nil, bgStyle)
			}
		}

		// Show cursor if focused and not hidden
		// Only show if cursor is within the visible render area
		if t.focused && !t.cursorHidden && t.curRow < renderRows && t.curCol < width {
			screen.ShowCursor(x+t.curCol, y+1+t.curRow)
		} else {
			screen.HideCursor()
		}
	}
}

func (t *Terminal) Write(data []byte) {
	if t.ptyFile != nil {
		t.ptyFile.Write(data)
	}
}

func (t *Terminal) HandleKey(ev *tcell.EventKey) bool {
	if !t.focused {
		return false
	}

	// Shift+PgUp/PgDn for scrollback navigation
	if ev.Modifiers()&tcell.ModShift != 0 {
		switch ev.Key() {
		case tcell.KeyPgUp:
			t.mu.Lock()
			t.viewOffset += t.rows
			if t.viewOffset > len(t.scrollback)+t.rows {
				t.viewOffset = len(t.scrollback) + t.rows
			}
			t.mu.Unlock()
			return true
		case tcell.KeyPgDn:
			t.mu.Lock()
			t.viewOffset -= t.rows
			if t.viewOffset < 0 {
				t.viewOffset = 0
			}
			t.mu.Unlock()
			return true
		}
	}

	var data []byte

	switch ev.Key() {
	case tcell.KeyEnter:
		data = []byte{'\r'}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		data = []byte{0x7f}
	case tcell.KeyTab:
		data = []byte{'\t'}
	case tcell.KeyEscape:
		data = []byte{0x1b}
	case tcell.KeyUp:
		if t.appCursorKeys {
			data = []byte{0x1b, 'O', 'A'}
		} else {
			data = []byte{0x1b, '[', 'A'}
		}
	case tcell.KeyDown:
		if t.appCursorKeys {
			data = []byte{0x1b, 'O', 'B'}
		} else {
			data = []byte{0x1b, '[', 'B'}
		}
	case tcell.KeyRight:
		if t.appCursorKeys {
			data = []byte{0x1b, 'O', 'C'}
		} else {
			data = []byte{0x1b, '[', 'C'}
		}
	case tcell.KeyLeft:
		if t.appCursorKeys {
			data = []byte{0x1b, 'O', 'D'}
		} else {
			data = []byte{0x1b, '[', 'D'}
		}
	case tcell.KeyHome:
		data = []byte{0x1b, '[', 'H'}
	case tcell.KeyEnd:
		data = []byte{0x1b, '[', 'F'}
	case tcell.KeyPgUp:
		data = []byte{0x1b, '[', '5', '~'}
	case tcell.KeyPgDn:
		data = []byte{0x1b, '[', '6', '~'}
	case tcell.KeyDelete:
		data = []byte{0x1b, '[', '3', '~'}
	case tcell.KeyCtrlC:
		data = []byte{0x03}
	case tcell.KeyCtrlD:
		data = []byte{0x04}
	case tcell.KeyCtrlZ:
		data = []byte{0x1a}
	case tcell.KeyCtrlL:
		data = []byte{0x0c}
	case tcell.KeyCtrlA:
		data = []byte{0x01}
	case tcell.KeyCtrlE:
		data = []byte{0x05}
	case tcell.KeyCtrlK:
		data = []byte{0x0b}
	case tcell.KeyCtrlU:
		data = []byte{0x15}
	case tcell.KeyCtrlW:
		data = []byte{0x17}
	case tcell.KeyCtrlR:
		data = []byte{0x12}
	case tcell.KeyF1:
		data = []byte{0x1b, 'O', 'P'}
	case tcell.KeyF2:
		data = []byte{0x1b, 'O', 'Q'}
	case tcell.KeyF3:
		data = []byte{0x1b, 'O', 'R'}
	case tcell.KeyF4:
		data = []byte{0x1b, 'O', 'S'}
	case tcell.KeyF5:
		data = []byte{0x1b, '[', '1', '5', '~'}
	case tcell.KeyF6:
		data = []byte{0x1b, '[', '1', '7', '~'}
	case tcell.KeyF7:
		data = []byte{0x1b, '[', '1', '8', '~'}
	case tcell.KeyF8:
		data = []byte{0x1b, '[', '1', '9', '~'}
	case tcell.KeyF9:
		data = []byte{0x1b, '[', '2', '0', '~'}
	case tcell.KeyF10:
		data = []byte{0x1b, '[', '2', '1', '~'}
	case tcell.KeyF11:
		data = []byte{0x1b, '[', '2', '3', '~'}
	case tcell.KeyF12:
		data = []byte{0x1b, '[', '2', '4', '~'}
	case tcell.KeyInsert:
		data = []byte{0x1b, '[', '2', '~'}
	default:
		if ev.Key() == tcell.KeyRune {
			s := string(ev.Rune())
			data = []byte(s)
		}
	}

	if data != nil {
		t.Write(data)
	}
	return true
}

// WritePaste wraps text in bracketed paste sequences if the mode is active.
func (t *Terminal) WritePaste(text string) {
	if t.bracketedPaste {
		t.Write([]byte("\x1b[200~"))
		t.Write([]byte(text))
		t.Write([]byte("\x1b[201~"))
	} else {
		t.Write([]byte(text))
	}
}

func (t *Terminal) hasSelection() bool {
	return t.selStartRow != t.selEndRow || t.selStartCol != t.selEndCol
}

func (t *Terminal) normalizedSelection() (int, int, int, int, bool) {
	if !t.hasSelection() {
		return 0, 0, 0, 0, false
	}
	sr, sc := t.selStartRow, t.selStartCol
	er, ec := t.selEndRow, t.selEndCol
	if er < sr || (er == sr && ec < sc) {
		sr, sc, er, ec = er, ec, sr, sc
	}
	return sr, sc, er, ec, true
}

func (t *Terminal) selectionStyle(row, col int, base tcell.Style) tcell.Style {
	sr, sc, er, ec, ok := t.normalizedSelection()
	if !ok {
		return base
	}
	if row < sr || row > er {
		return base
	}
	if row == sr && col < sc {
		return base
	}
	if row == er && col > ec {
		return base
	}
	return base.Reverse(true)
}

func (t *Terminal) mouseToContent(mx, my int) (int, int, bool) {
	if t.cols <= 0 {
		return 0, 0, false
	}
	renderRows := t.rows
	if renderRows > t.h-1 {
		renderRows = t.h - 1
	}
	row := my - (t.y + 1)
	if row < 0 || row >= renderRows {
		return 0, 0, false
	}

	col := mx - t.x
	if col < 0 {
		col = 0
	}
	if col >= t.cols {
		col = t.cols - 1
	}

	if t.viewOffset > 0 && !t.altActive {
		contentRow := len(t.scrollback) - t.viewOffset + row
		if contentRow < 0 {
			return 0, 0, false
		}
		return contentRow, col, true
	}
	return len(t.scrollback) + row, col, true
}

func (t *Terminal) contentRowCells(row int) ([]Cell, bool) {
	sbLen := len(t.scrollback)
	if row < 0 {
		return nil, false
	}
	if row < sbLen {
		return t.scrollback[row].cells, true
	}
	liveRow := row - sbLen
	if liveRow >= 0 && liveRow < t.rows {
		return t.cells[liveRow], true
	}
	return nil, false
}

func (t *Terminal) CopySelection() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	sr, sc, er, ec, ok := t.normalizedSelection()
	if !ok {
		return false
	}

	var out strings.Builder
	for row := sr; row <= er; row++ {
		cells, ok := t.contentRowCells(row)
		if !ok {
			continue
		}
		start := 0
		end := len(cells)
		if row == sr {
			start = sc
		}
		if row == er {
			end = ec + 1
		}
		if start < 0 {
			start = 0
		}
		if end > len(cells) {
			end = len(cells)
		}
		if start > end {
			start = end
		}

		line := make([]rune, 0, end-start)
		for _, cell := range cells[start:end] {
			line = append(line, cell.Ch)
		}
		out.WriteString(strings.TrimRight(string(line), " "))
		if row < er {
			out.WriteByte('\n')
		}
	}

	text := out.String()
	if text == "" {
		return false
	}
	terminalClipboard = text
	clipboard.WriteAll(text)
	return true
}

func (t *Terminal) HandleMouse(ev *tcell.EventMouse) bool {
	mx, my := ev.Position()
	if my < t.y || my >= t.y+t.h {
		return false
	}

	btn := ev.Buttons()
	switch {
	case btn == tcell.WheelUp:
		t.mu.Lock()
		if !t.altActive {
			t.viewOffset += 3
			maxOffset := len(t.scrollback) + t.rows
			if t.viewOffset > maxOffset {
				t.viewOffset = maxOffset
			}
		}
		t.mu.Unlock()
		return true
	case btn == tcell.WheelDown:
		t.mu.Lock()
		t.viewOffset -= 3
		if t.viewOffset < 0 {
			t.viewOffset = 0
		}
		t.mu.Unlock()
		return true
	case btn == tcell.Button1:
		t.mu.Lock()
		row, col, ok := t.mouseToContent(mx, my)
		if ok {
			if !t.selecting {
				t.selStartRow, t.selStartCol = row, col
				t.selEndRow, t.selEndCol = row, col
				t.selecting = true
			} else {
				t.selEndRow, t.selEndCol = row, col
			}
		}
		t.mu.Unlock()
		return true
	case btn == tcell.ButtonNone:
		t.mu.Lock()
		t.selecting = false
		t.mu.Unlock()
		return true
	}
	return true
}

func (t *Terminal) Close() {
	if t.ptyFile != nil {
		t.ptyFile.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}
}

func (t *Terminal) IsFocused() bool   { return t.focused }
func (t *Terminal) SetFocused(f bool) { t.focused = f }

// Helper functions for parsing ANSI params

func parseParam(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	if n == 0 {
		return def
	}
	return n
}

func parseParamPair(s string, def1, def2 int) (int, int) {
	parts := splitString(s, ';')
	a, b := def1, def2
	if len(parts) >= 1 && parts[0] != "" {
		a = parseParam(parts[0], def1)
	}
	if len(parts) >= 2 && parts[1] != "" {
		b = parseParam(parts[1], def2)
	}
	return a, b
}

func splitParams(s string) []int {
	parts := splitString(s, ';')
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		result = append(result, parseParam(p, 0))
	}
	return result
}

func splitString(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func ansiColor(n int) tcell.Color {
	colors := []tcell.Color{
		tcell.ColorBlack, tcell.ColorMaroon, tcell.ColorGreen, tcell.ColorOlive,
		tcell.ColorNavy, tcell.ColorPurple, tcell.ColorTeal, tcell.ColorSilver,
	}
	if n >= 0 && n < len(colors) {
		return colors[n]
	}
	return tcell.ColorWhite
}

func ansiBrightColor(n int) tcell.Color {
	colors := []tcell.Color{
		tcell.ColorGray, tcell.ColorRed, tcell.ColorLime, tcell.ColorYellow,
		tcell.ColorBlue, tcell.ColorFuchsia, tcell.ColorAqua, tcell.ColorWhite,
	}
	if n >= 0 && n < len(colors) {
		return colors[n]
	}
	return tcell.ColorWhite
}
