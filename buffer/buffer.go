package buffer

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Buffer struct {
	Lines              []string
	Path               string
	Cursor             Cursor
	Selection          *Selection
	Dirty              bool
	ExternallyModified bool // File was modified externally while buffer has unsaved changes
	Undo               *UndoStack
	Language           string
	ReadOnly           bool
	TabSize            int
	LastSaveTime       time.Time   // Track when file was last saved
	FileSize           int64       // File size in bytes at load time
	LineEnding         string      // "LF" or "CRLF" — detected from file, preserved on save
	UseTabs            bool        // Use real tabs instead of spaces
	AutoCloseEnabled   bool        // Enable automatic closing pairs
	Encoding           string      // Detected encoding (UTF-8, Latin-1, etc.)
	ExtraCursors       []Cursor    // additional cursors for multi-cursor editing
	FoldedLines        map[int]int // maps fold start line -> fold end line (exclusive)

	// Auto-close bracket swallowing state.
	// Tracks an ordered list of auto-inserted closers that are still pending
	// consumption at autoClosePos.
	autoClosePending []rune
	autoClosePos     Cursor

	savedSnapshot string
}

func NewBuffer(tabSize int) *Buffer {
	return &Buffer{
		Lines:            []string{""},
		Undo:             NewUndoStack(),
		TabSize:          tabSize,
		LineEnding:       "LF",
		AutoCloseEnabled: true,
		FoldedLines:      make(map[int]int),
		savedSnapshot:    "",
	}
}

func NewBufferFromFile(path string, tabSize int) (*Buffer, error) {
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - create a new buffer with this path
			// Detect language from extension even though file doesn't exist
			return &Buffer{
				Lines:            []string{""},
				Path:             path,
				Undo:             NewUndoStack(),
				TabSize:          tabSize,
				Dirty:            false, // New file starts clean
				LineEnding:       "LF",
				AutoCloseEnabled: true,
				Encoding:         "UTF-8",
				FoldedLines:      make(map[int]int),
				savedSnapshot:    "",
			}, nil
		}
		return nil, err
	}

	// File exists - check size before reading
	if info.Size() > 100*1024*1024 { // 100MB
		return nil, fmt.Errorf("file too large (%d MB), max supported is 100 MB", info.Size()/(1024*1024))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Binary file detection: check first 8KB for null bytes
	checkLen := len(data)
	if checkLen > 8192 {
		checkLen = 8192
	}
	isBinary := false
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			isBinary = true
			break
		}
	}

	// Encoding detection
	encoding := detectEncoding(data)

	// Line ending detection: check for CRLF before normalizing
	lineEnding := "LF"
	if strings.Contains(string(data), "\r\n") {
		lineEnding = "CRLF"
	}

	content := string(data)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimRight(content, "\n")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}

	// Auto-detect indentation from file content
	detectedTabSize, detectedUseTabs := DetectIndentation(lines)

	return &Buffer{
		Lines:            lines,
		Path:             path,
		Undo:             NewUndoStack(),
		TabSize:          detectedTabSize,
		UseTabs:          detectedUseTabs,
		ReadOnly:         isBinary,
		FileSize:         info.Size(),
		LineEnding:       lineEnding,
		AutoCloseEnabled: true,
		Encoding:         encoding,
		FoldedLines:      make(map[int]int),
		savedSnapshot:    strings.Join(lines, "\n"),
	}, nil
}

// detectEncoding checks BOM and validates UTF-8 to determine file encoding.
func detectEncoding(data []byte) string {
	// Check BOM
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return "UTF-8 BOM"
	}
	if len(data) >= 2 {
		if data[0] == 0xFF && data[1] == 0xFE {
			return "UTF-16 LE"
		}
		if data[0] == 0xFE && data[1] == 0xFF {
			return "UTF-16 BE"
		}
	}
	// Check if valid UTF-8
	if isValidUTF8(data) {
		return "UTF-8"
	}
	return "Latin-1"
}

func isValidUTF8(data []byte) bool {
	i := 0
	for i < len(data) {
		if data[i] < 0x80 {
			i++
			continue
		}
		var size int
		switch {
		case data[i]&0xE0 == 0xC0:
			size = 2
		case data[i]&0xF0 == 0xE0:
			size = 3
		case data[i]&0xF8 == 0xF0:
			size = 4
		default:
			return false
		}
		if i+size > len(data) {
			return false
		}
		for j := 1; j < size; j++ {
			if data[i+j]&0xC0 != 0x80 {
				return false
			}
		}
		i += size
	}
	return true
}

// DetectIndentation analyzes the file content to detect indentation style.
// Returns (tabSize, useTabs) based on the most common indentation pattern.
func DetectIndentation(lines []string) (int, bool) {
	if len(lines) == 0 {
		return 4, false // default
	}

	tabCount := 0
	spaceIndents := make(map[int]int) // count of each indent size

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		// Count leading whitespace
		spaces := 0
		tabs := 0
		for _, ch := range line {
			if ch == '\t' {
				tabs++
			} else if ch == ' ' {
				spaces++
			} else {
				break
			}
		}

		// If line has tabs, count it
		if tabs > 0 {
			tabCount++
		}

		// If line has spaces, record the indent level
		if spaces > 0 && tabs == 0 {
			// Common indentation levels: 2, 4, 8
			if spaces%2 == 0 {
				spaceIndents[2]++
			}
			if spaces%4 == 0 {
				spaceIndents[4]++
			}
			if spaces%8 == 0 {
				spaceIndents[8]++
			}
		}
	}

	// If we found tabs, use tabs
	if tabCount > 10 {
		return 4, true // use tabs, with 4-space visual width
	}

	// Otherwise, find most common space indentation
	maxCount := 0
	detectedSize := 4
	for size, count := range spaceIndents {
		if count > maxCount {
			maxCount = count
			detectedSize = size
		}
	}

	// If we have evidence of space indentation, use it
	if maxCount > 5 {
		return detectedSize, false
	}

	// Default to 4 spaces
	return 4, false
}

func (b *Buffer) Save() error {
	return b.SaveWithOptions(true, true)
}

// BuildSaveContent serializes the buffer content for writing to disk.
// When insertFinalNewline is enabled, output is normalized to exactly one
// trailing newline on disk.
func (b *Buffer) BuildSaveContent(trimTrailing, insertFinalNewline bool) string {
	lines := make([]string, len(b.Lines))
	copy(lines, b.Lines)

	if trimTrailing {
		for i, line := range lines {
			lines[i] = strings.TrimRight(line, " \t")
		}
	}

	if insertFinalNewline {
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "")
	}

	eol := "\n"
	if b.LineEnding == "CRLF" {
		eol = "\r\n"
	}

	content := strings.Join(lines, eol)
	if insertFinalNewline && len(lines) == 1 && lines[0] == "" {
		content = eol
	}

	b.Lines = lines
	return content
}

// SaveWithOptions saves with configurable trim and final newline behavior.
func (b *Buffer) SaveWithOptions(trimTrailing, insertFinalNewline bool) error {
	if b.Path == "" || b.ReadOnly {
		return nil
	}

	content := b.BuildSaveContent(trimTrailing, insertFinalNewline)

	err := os.WriteFile(b.Path, []byte(content), 0644)
	if err == nil {
		b.MarkSaved()
		b.LastSaveTime = time.Now()
	}
	return err
}

func (b *Buffer) currentSnapshot() string {
	return strings.Join(b.Lines, "\n")
}

func (b *Buffer) MarkSaved() {
	b.savedSnapshot = b.currentSnapshot()
	b.Dirty = false
}

func (b *Buffer) RecomputeDirty() {
	b.Dirty = b.currentSnapshot() != b.savedSnapshot
}

func (b *Buffer) clampCursor() {
	if len(b.Lines) == 0 {
		b.Lines = []string{""}
	}
	if b.Cursor.Line < 0 {
		b.Cursor.Line = 0
	}
	if b.Cursor.Line >= len(b.Lines) {
		b.Cursor.Line = len(b.Lines) - 1
	}
	lineLen := len(b.Lines[b.Cursor.Line])
	if b.Cursor.Col < 0 {
		b.Cursor.Col = 0
	}
	if b.Cursor.Col > lineLen {
		b.Cursor.Col = lineLen
	}
}

// ClearAutoClose clears the auto-close swallowing state.
// Call this when the cursor moves (arrow keys, mouse click, etc.)
func (b *Buffer) ClearAutoClose() {
	b.autoClosePending = nil
}

func (b *Buffer) InsertChar(ch rune) {
	b.deleteSelectionIfAny()
	b.clampCursor()
	line := b.Lines[b.Cursor.Line]
	before := b.Cursor
	inAutoCloseContext := len(b.autoClosePending) > 0 &&
		b.Cursor.Line == b.autoClosePos.Line && b.Cursor.Col == b.autoClosePos.Col

	// Check for auto-close swallowing: if user types the expected pending closer
	// and cursor hasn't moved, skip over it.
	if inAutoCloseContext && len(b.autoClosePending) > 0 && ch == b.autoClosePending[0] {
		if b.Cursor.Col < len(line) && rune(line[b.Cursor.Col]) == ch {
			b.Cursor.Col++
			b.autoClosePending = b.autoClosePending[1:]
			b.autoClosePos = b.Cursor
			return
		}
	}
	if !inAutoCloseContext {
		b.autoClosePending = nil
	}

	// Auto-close brackets and quotes for recognized languages
	pairs := map[rune]rune{'(': ')', '[': ']', '{': '}'}
	quotePairs := map[rune]bool{'"': true, '\'': true, '`': true}

	if b.AutoCloseEnabled && b.Language != "" && b.Language != "Text" {
		if closeCh, ok := pairs[ch]; ok {
			text := string(ch) + string(closeCh)
			b.Lines[b.Cursor.Line] = line[:b.Cursor.Col] + text + line[b.Cursor.Col:]
			b.Cursor.Col++
			b.Dirty = true
			b.Undo.Push(Operation{Type: OpInsert, Pos: before, Text: text, Before: before})
			// Track auto-close state for swallowing; if we're already in an
			// auto-close context, keep outer pending closers after this one.
			if inAutoCloseContext && len(b.autoClosePending) > 0 {
				b.autoClosePending = append([]rune{closeCh}, b.autoClosePending...)
			} else {
				b.autoClosePending = []rune{closeCh}
			}
			b.autoClosePos = b.Cursor
			return
		}
		if quotePairs[ch] {
			// Don't auto-close if the character to the right is a word character
			if b.Cursor.Col < len(line) {
				next := rune(line[b.Cursor.Col])
				if unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' {
					goto noAutoClose
				}
			}
			text := string(ch) + string(ch)
			b.Lines[b.Cursor.Line] = line[:b.Cursor.Col] + text + line[b.Cursor.Col:]
			b.Cursor.Col++
			b.Dirty = true
			b.Undo.Push(Operation{Type: OpInsert, Pos: before, Text: text, Before: before})
			// Track auto-close state for swallowing; if we're already in an
			// auto-close context, keep outer pending closers after this one.
			if inAutoCloseContext && len(b.autoClosePending) > 0 {
				b.autoClosePending = append([]rune{ch}, b.autoClosePending...)
			} else {
				b.autoClosePending = []rune{ch}
			}
			b.autoClosePos = b.Cursor
			return
		}
	}

noAutoClose:
	text := string(ch)
	b.Lines[b.Cursor.Line] = line[:b.Cursor.Col] + text + line[b.Cursor.Col:]
	b.Cursor.Col++
	if inAutoCloseContext && len(b.autoClosePending) > 0 {
		b.autoClosePos = b.Cursor
	}
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: before, Text: text, Before: before})
}

func (b *Buffer) InsertTab() {
	if b.Selection != nil {
		b.IndentSelection()
		return
	}

	var tabString string
	if b.UseTabs {
		tabString = "\t"
	} else {
		tabString = strings.Repeat(" ", b.TabSize)
	}

	b.deleteSelectionIfAny()
	b.clampCursor()
	inAutoCloseContext := len(b.autoClosePending) > 0 &&
		b.Cursor.Line == b.autoClosePos.Line && b.Cursor.Col == b.autoClosePos.Col
	line := b.Lines[b.Cursor.Line]
	before := b.Cursor
	b.Lines[b.Cursor.Line] = line[:b.Cursor.Col] + tabString + line[b.Cursor.Col:]
	if b.UseTabs {
		b.Cursor.Col += 1
	} else {
		b.Cursor.Col += b.TabSize
	}
	if inAutoCloseContext && len(b.autoClosePending) > 0 {
		b.autoClosePos = b.Cursor
	}
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: before, Text: tabString, Before: before})
}

func (b *Buffer) InsertNewline() {
	b.deleteSelectionIfAny()
	b.autoClosePending = nil
	b.clampCursor()
	line := b.Lines[b.Cursor.Line]
	before := b.Cursor

	// Auto-indent: copy leading whitespace from current line
	indent := ""
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			indent += string(ch)
		} else {
			break
		}
	}

	// Smart indent: add extra indent if line ends with ':'
	// (for Python, JavaScript, C, etc.)
	extraIndent := ""
	trimmedLine := strings.TrimSpace(line[:b.Cursor.Col])
	if strings.HasSuffix(trimmedLine, ":") {
		// Add extra indentation (use tab size or 4 spaces)
		if b.TabSize > 0 {
			extraIndent = strings.Repeat(" ", b.TabSize)
		} else {
			extraIndent = "    " // default to 4 spaces
		}
	}

	rest := line[b.Cursor.Col:]
	b.Lines[b.Cursor.Line] = line[:b.Cursor.Col]
	newLine := indent + extraIndent + rest
	// Insert new line after current
	b.Lines = append(b.Lines, "")
	copy(b.Lines[b.Cursor.Line+2:], b.Lines[b.Cursor.Line+1:])
	b.Lines[b.Cursor.Line+1] = newLine
	b.Cursor.Line++
	b.Cursor.Col = len(indent) + len(extraIndent)
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: before, Text: "\n" + indent + extraIndent, Before: before})
}

func (b *Buffer) Backspace() {
	if b.deleteSelectionIfAny() {
		b.autoClosePending = nil
		return
	}
	b.clampCursor()
	if b.Cursor.Col > 0 {
		line := b.Lines[b.Cursor.Line]
		before := b.Cursor
		inAutoCloseContext := len(b.autoClosePending) > 0 &&
			b.Cursor.Line == b.autoClosePos.Line && b.Cursor.Col == b.autoClosePos.Col
		if inAutoCloseContext && b.Cursor.Col < len(line) && len(b.autoClosePending) > 0 {
			closeCh := b.autoClosePending[0]
			openCh, ok := openingFor(closeCh)
			if ok && rune(line[b.Cursor.Col-1]) == openCh && rune(line[b.Cursor.Col]) == closeCh {
				deleted := string(openCh) + string(closeCh)
				b.Lines[b.Cursor.Line] = line[:b.Cursor.Col-1] + line[b.Cursor.Col+1:]
				b.Cursor.Col--
				b.autoClosePending = b.autoClosePending[1:]
				b.autoClosePos = b.Cursor
				b.Dirty = true
				b.Undo.Push(Operation{Type: OpDelete, Pos: b.Cursor, Text: deleted, Before: before})
				return
			}
		}
		deleted := string(line[b.Cursor.Col-1])
		b.Lines[b.Cursor.Line] = line[:b.Cursor.Col-1] + line[b.Cursor.Col:]
		b.Cursor.Col--
		if inAutoCloseContext && len(b.autoClosePending) > 0 {
			b.autoClosePos = b.Cursor
		}
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpDelete, Pos: b.Cursor, Text: deleted, Before: before})
	} else if b.Cursor.Line > 0 {
		before := b.Cursor
		prevLen := len(b.Lines[b.Cursor.Line-1])
		b.Lines[b.Cursor.Line-1] += b.Lines[b.Cursor.Line]
		b.Lines = append(b.Lines[:b.Cursor.Line], b.Lines[b.Cursor.Line+1:]...)
		b.Cursor.Line--
		b.Cursor.Col = prevLen
		b.autoClosePending = nil
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpDelete, Pos: b.Cursor, Text: "\n", Before: before})
	}
}

func (b *Buffer) Delete() {
	if b.deleteSelectionIfAny() {
		b.autoClosePending = nil
		return
	}
	b.clampCursor()
	line := b.Lines[b.Cursor.Line]
	if b.Cursor.Col < len(line) {
		before := b.Cursor
		deletedRune := rune(line[b.Cursor.Col])
		deleted := string(deletedRune)
		inAutoCloseContext := len(b.autoClosePending) > 0 &&
			b.Cursor.Line == b.autoClosePos.Line && b.Cursor.Col == b.autoClosePos.Col
		b.Lines[b.Cursor.Line] = line[:b.Cursor.Col] + line[b.Cursor.Col+1:]
		if inAutoCloseContext && len(b.autoClosePending) > 0 && deletedRune == b.autoClosePending[0] {
			b.autoClosePending = b.autoClosePending[1:]
		}
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpDelete, Pos: b.Cursor, Text: deleted, Before: before})
	} else if b.Cursor.Line < len(b.Lines)-1 {
		before := b.Cursor
		b.Lines[b.Cursor.Line] += b.Lines[b.Cursor.Line+1]
		b.Lines = append(b.Lines[:b.Cursor.Line+1], b.Lines[b.Cursor.Line+2:]...)
		b.autoClosePending = nil
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpDelete, Pos: b.Cursor, Text: "\n", Before: before})
	}
}

func (b *Buffer) deleteSelectionIfAny() bool {
	if b.Selection == nil || b.Selection.Empty() {
		b.Selection = nil
		return false
	}
	b.DeleteSelection()
	return true
}

func (b *Buffer) DeleteSelection() {
	if b.Selection == nil {
		return
	}
	sel := *b.Selection
	text := b.GetSelectedText()
	before := b.Cursor

	if sel.Start.Line == sel.End.Line {
		// Single-line selection - validate bounds
		if sel.Start.Line < 0 || sel.Start.Line >= len(b.Lines) {
			b.Selection = nil
			return
		}
		line := b.Lines[sel.Start.Line]

		// Clamp selection bounds to line length
		startCol := sel.Start.Col
		endCol := sel.End.Col
		if startCol > len(line) {
			startCol = len(line)
		}
		if endCol > len(line) {
			endCol = len(line)
		}
		if startCol < 0 {
			startCol = 0
		}
		if endCol < 0 {
			endCol = 0
		}

		b.Lines[sel.Start.Line] = line[:startCol] + line[endCol:]
	} else {
		// Multi-line selection - validate bounds
		if sel.Start.Line < 0 || sel.Start.Line >= len(b.Lines) ||
			sel.End.Line < 0 || sel.End.Line >= len(b.Lines) {
			b.Selection = nil
			return
		}

		firstLine := b.Lines[sel.Start.Line]
		lastLine := b.Lines[sel.End.Line]

		// Clamp to line lengths
		startCol := sel.Start.Col
		endCol := sel.End.Col
		if startCol > len(firstLine) {
			startCol = len(firstLine)
		}
		if startCol < 0 {
			startCol = 0
		}
		if endCol > len(lastLine) {
			endCol = len(lastLine)
		}
		if endCol < 0 {
			endCol = 0
		}

		b.Lines[sel.Start.Line] = firstLine[:startCol] + lastLine[endCol:]
		b.Lines = append(b.Lines[:sel.Start.Line+1], b.Lines[sel.End.Line+1:]...)
	}

	b.Cursor = sel.Start
	b.Selection = nil
	b.clampCursor()
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpDelete, Pos: sel.Start, Text: text, Before: before})
}

func (b *Buffer) GetSelectedText() string {
	if b.Selection == nil {
		return ""
	}
	sel := *b.Selection

	// Validate line bounds
	if sel.Start.Line < 0 || sel.Start.Line >= len(b.Lines) ||
		sel.End.Line < 0 || sel.End.Line >= len(b.Lines) {
		return ""
	}

	if sel.Start.Line == sel.End.Line {
		line := b.Lines[sel.Start.Line]

		// Clamp column bounds
		startCol := sel.Start.Col
		endCol := sel.End.Col
		if startCol > len(line) {
			startCol = len(line)
		}
		if startCol < 0 {
			startCol = 0
		}
		if endCol > len(line) {
			endCol = len(line)
		}
		if endCol < 0 {
			endCol = 0
		}

		return line[startCol:endCol]
	}

	var sb strings.Builder
	firstLine := b.Lines[sel.Start.Line]
	startCol := sel.Start.Col
	if startCol > len(firstLine) {
		startCol = len(firstLine)
	}
	if startCol < 0 {
		startCol = 0
	}
	sb.WriteString(firstLine[startCol:])

	for i := sel.Start.Line + 1; i < sel.End.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(b.Lines[i])
	}

	sb.WriteByte('\n')
	lastLine := b.Lines[sel.End.Line]
	endCol := sel.End.Col
	if endCol > len(lastLine) {
		endCol = len(lastLine)
	}
	if endCol < 0 {
		endCol = 0
	}
	sb.WriteString(lastLine[:endCol])

	return sb.String()
}

func (b *Buffer) InsertText(text string) {
	b.deleteSelectionIfAny()
	b.clampCursor()
	inAutoCloseContext := len(b.autoClosePending) > 0 &&
		b.Cursor.Line == b.autoClosePos.Line && b.Cursor.Col == b.autoClosePos.Col
	before := b.Cursor

	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		line := b.Lines[b.Cursor.Line]
		// Extra safety: validate cursor column
		if b.Cursor.Col > len(line) {
			b.Cursor.Col = len(line)
		}
		if b.Cursor.Col < 0 {
			b.Cursor.Col = 0
		}
		b.Lines[b.Cursor.Line] = line[:b.Cursor.Col] + text + line[b.Cursor.Col:]
		b.Cursor.Col += len(text)
		if inAutoCloseContext && len(b.autoClosePending) > 0 {
			b.autoClosePos = b.Cursor
		}
	} else {
		line := b.Lines[b.Cursor.Line]
		// Extra safety: validate cursor column
		if b.Cursor.Col > len(line) {
			b.Cursor.Col = len(line)
		}
		if b.Cursor.Col < 0 {
			b.Cursor.Col = 0
		}
		rest := line[b.Cursor.Col:]
		b.Lines[b.Cursor.Line] = line[:b.Cursor.Col] + lines[0]

		newLines := make([]string, len(lines)-1)
		for i := 1; i < len(lines); i++ {
			newLines[i-1] = lines[i]
		}
		newLines[len(newLines)-1] += rest

		after := b.Lines[b.Cursor.Line+1:]
		b.Lines = append(b.Lines[:b.Cursor.Line+1], newLines...)
		b.Lines = append(b.Lines, after...)

		b.Cursor.Line += len(lines) - 1
		b.Cursor.Col = len(lines[len(lines)-1])
		b.autoClosePending = nil
	}

	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: before, Text: text, Before: before})
}

func (b *Buffer) IndentSelection() {
	before := b.Cursor

	var indentString string
	if b.UseTabs {
		indentString = "\t"
	} else {
		indentString = strings.Repeat(" ", b.TabSize)
	}

	if b.Selection == nil {
		// No selection - indent current line (VSCode behavior)
		b.clampCursor()
		b.Lines[b.Cursor.Line] = indentString + b.Lines[b.Cursor.Line]
		if b.UseTabs {
			b.Cursor.Col += 1
		} else {
			b.Cursor.Col += b.TabSize
		}
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpInsert, Pos: Cursor{Line: b.Cursor.Line}, Text: indentString, Before: before})
		return
	}

	// Has selection - indent all selected lines
	sel := *b.Selection
	for i := sel.Start.Line; i <= sel.End.Line; i++ {
		b.Lines[i] = indentString + b.Lines[i]
	}
	if b.UseTabs {
		b.Selection.Start.Col += 1
		b.Selection.End.Col += 1
		b.Cursor.Col += 1
	} else {
		b.Selection.Start.Col += b.TabSize
		b.Selection.End.Col += b.TabSize
		b.Cursor.Col += b.TabSize
	}
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: Cursor{Line: sel.Start.Line}, Text: indentString, Before: before})
}

func (b *Buffer) DedentSelection() {
	before := b.Cursor

	if b.Selection == nil {
		// No selection - dedent current line (VSCode behavior)
		b.clampCursor()
		line := b.Lines[b.Cursor.Line]
		removed := 0

		// Handle tab character
		if len(line) > 0 && line[0] == '\t' {
			b.Lines[b.Cursor.Line] = line[1:]
			if b.Cursor.Col > 0 {
				b.Cursor.Col--
			}
			b.Dirty = true
			b.Undo.Push(Operation{Type: OpDelete, Pos: Cursor{Line: b.Cursor.Line}, Text: "\t", Before: before})
			return
		}

		// Remove up to TabSize spaces from the beginning
		for j := 0; j < b.TabSize && j < len(line) && line[j] == ' '; j++ {
			removed++
		}
		if removed > 0 {
			b.Lines[b.Cursor.Line] = line[removed:]
			if b.Cursor.Col >= removed {
				b.Cursor.Col -= removed
			} else {
				b.Cursor.Col = 0
			}
			b.Dirty = true
			b.Undo.Push(Operation{Type: OpDelete, Pos: Cursor{Line: b.Cursor.Line}, Text: line[:removed], Before: before})
		}
		return
	}

	// Has selection - dedent all selected lines
	sel := *b.Selection
	for i := sel.Start.Line; i <= sel.End.Line; i++ {
		line := b.Lines[i]
		removed := 0

		// Handle tab character
		if len(line) > 0 && line[0] == '\t' {
			b.Lines[i] = line[1:]
			removed = 1
			if i == sel.Start.Line && b.Selection.Start.Col >= removed {
				b.Selection.Start.Col -= removed
			} else if i == sel.Start.Line {
				b.Selection.Start.Col = 0
			}
			if i == sel.End.Line && b.Selection.End.Col >= removed {
				b.Selection.End.Col -= removed
			} else if i == sel.End.Line {
				b.Selection.End.Col = 0
			}
			if i == b.Cursor.Line && b.Cursor.Col >= removed {
				b.Cursor.Col -= removed
			} else if i == b.Cursor.Line {
				b.Cursor.Col = 0
			}
			continue
		}

		// Remove up to TabSize spaces
		for j := 0; j < b.TabSize && j < len(line) && line[j] == ' '; j++ {
			removed++
		}
		if removed > 0 {
			b.Lines[i] = line[removed:]
			if i == sel.Start.Line && b.Selection.Start.Col >= removed {
				b.Selection.Start.Col -= removed
			} else if i == sel.Start.Line {
				b.Selection.Start.Col = 0
			}
			if i == sel.End.Line && b.Selection.End.Col >= removed {
				b.Selection.End.Col -= removed
			} else if i == sel.End.Line {
				b.Selection.End.Col = 0
			}
			if i == b.Cursor.Line && b.Cursor.Col >= removed {
				b.Cursor.Col -= removed
			} else if i == b.Cursor.Line {
				b.Cursor.Col = 0
			}
		}
	}
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpDelete, Pos: Cursor{Line: sel.Start.Line}, Text: "", Before: before})
}

func (b *Buffer) DuplicateLine() {
	b.clampCursor()
	before := b.Cursor
	line := b.Lines[b.Cursor.Line]
	newLines := make([]string, len(b.Lines)+1)
	copy(newLines, b.Lines[:b.Cursor.Line+1])
	newLines[b.Cursor.Line+1] = line
	copy(newLines[b.Cursor.Line+2:], b.Lines[b.Cursor.Line+1:])
	b.Lines = newLines
	b.Cursor.Line++
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: Cursor{Line: b.Cursor.Line, Col: 0}, Text: line + "\n", Before: before})
}

func (b *Buffer) MoveLineUp() {
	b.clampCursor()
	if b.Cursor.Line == 0 {
		return // Already at top
	}

	before := b.Cursor
	currentLine := b.Cursor.Line

	// Swap current line with previous line
	b.Lines[currentLine], b.Lines[currentLine-1] = b.Lines[currentLine-1], b.Lines[currentLine]

	// Move cursor up with the line
	b.Cursor.Line--

	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: b.Cursor, Text: "", Before: before})
}

func (b *Buffer) MoveLineDown() {
	b.clampCursor()
	if b.Cursor.Line >= len(b.Lines)-1 {
		return // Already at bottom
	}

	before := b.Cursor
	currentLine := b.Cursor.Line

	// Swap current line with next line
	b.Lines[currentLine], b.Lines[currentLine+1] = b.Lines[currentLine+1], b.Lines[currentLine]

	// Move cursor down with the line
	b.Cursor.Line++

	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: b.Cursor, Text: "", Before: before})
}

func (b *Buffer) ToggleLineComment(commentStr string) {
	b.clampCursor()
	startLine := b.Cursor.Line
	endLine := b.Cursor.Line
	if b.Selection != nil {
		startLine = b.Selection.Start.Line
		endLine = b.Selection.End.Line
	}

	// Check if all lines are commented
	allCommented := true
	prefix := commentStr + " "
	for i := startLine; i <= endLine; i++ {
		trimmed := strings.TrimLeft(b.Lines[i], " \t")
		if trimmed != "" && !strings.HasPrefix(trimmed, commentStr) {
			allCommented = false
			break
		}
	}

	before := b.Cursor
	if allCommented {
		// Uncomment
		for i := startLine; i <= endLine; i++ {
			idx := strings.Index(b.Lines[i], commentStr)
			if idx >= 0 {
				end := idx + len(commentStr)
				if end < len(b.Lines[i]) && b.Lines[i][end] == ' ' {
					end++
				}
				b.Lines[i] = b.Lines[i][:idx] + b.Lines[i][end:]
			}
		}
	} else {
		// Comment
		for i := startLine; i <= endLine; i++ {
			if strings.TrimSpace(b.Lines[i]) != "" {
				b.Lines[i] = prefix + b.Lines[i]
			}
		}
	}
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpInsert, Pos: Cursor{Line: startLine}, Text: "", Before: before})
}

func (b *Buffer) SelectAll() {
	if len(b.Lines) == 0 {
		return
	}
	lastLine := len(b.Lines) - 1
	sel := NewSelection(
		Cursor{Line: 0, Col: 0},
		Cursor{Line: lastLine, Col: len(b.Lines[lastLine])},
	)
	b.Selection = &sel
	b.Cursor = sel.End
}

func (b *Buffer) WordAt(line, col int) (start, end int) {
	if line < 0 || line >= len(b.Lines) {
		return col, col
	}
	l := b.Lines[line]
	if col >= len(l) {
		return len(l), len(l)
	}

	r := rune(l[col])
	isWord := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'

	start = col
	end = col
	if isWord {
		for start > 0 {
			r := rune(l[start-1])
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				start--
			} else {
				break
			}
		}
		for end < len(l) {
			r := rune(l[end])
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				end++
			} else {
				break
			}
		}
	} else {
		end = col + 1
	}
	return
}

// charClass returns 0 for whitespace, 1 for word chars (letter/digit/_), 2 for symbols.
func charClass(r rune) int {
	if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
		return 0
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
		return 1
	}
	return 2
}

func openingFor(closeCh rune) (rune, bool) {
	switch closeCh {
	case ')':
		return '(', true
	case ']':
		return '[', true
	case '}':
		return '{', true
	case '"':
		return '"', true
	case '\'':
		return '\'', true
	case '`':
		return '`', true
	default:
		return 0, false
	}
}

func (b *Buffer) MoveWordLeft() {
	b.clampCursor()
	if b.Cursor.Col == 0 {
		if b.Cursor.Line > 0 {
			b.Cursor.Line--
			b.Cursor.Col = len(b.Lines[b.Cursor.Line])
		}
		return
	}
	line := b.Lines[b.Cursor.Line]
	col := b.Cursor.Col - 1
	// Skip whitespace
	for col > 0 && charClass(rune(line[col])) == 0 {
		col--
	}
	// Skip contiguous chars of the same class
	if col >= 0 && col < len(line) {
		cls := charClass(rune(line[col]))
		for col > 0 && charClass(rune(line[col-1])) == cls {
			col--
		}
	}
	b.Cursor.Col = col
}

func (b *Buffer) MoveWordRight() {
	b.clampCursor()
	line := b.Lines[b.Cursor.Line]
	if b.Cursor.Col >= len(line) {
		if b.Cursor.Line < len(b.Lines)-1 {
			b.Cursor.Line++
			b.Cursor.Col = 0
		}
		return
	}
	col := b.Cursor.Col

	// Get class of current char
	cls := charClass(rune(line[col]))

	if cls == 0 {
		// On whitespace: skip whitespace, then skip next chunk
		for col < len(line) && charClass(rune(line[col])) == 0 {
			col++
		}
		if col < len(line) {
			nextCls := charClass(rune(line[col]))
			for col < len(line) && charClass(rune(line[col])) == nextCls {
				col++
			}
		}
	} else {
		// On word or symbol: skip contiguous same-class chars
		for col < len(line) && charClass(rune(line[col])) == cls {
			col++
		}
		// Then skip trailing whitespace
		for col < len(line) && charClass(rune(line[col])) == 0 {
			col++
		}
	}
	b.Cursor.Col = col
}

// DeleteWordBackward deletes from the cursor backward to the start of the current word
func (b *Buffer) DeleteWordBackward() {
	if b.deleteSelectionIfAny() {
		return
	}
	b.clampCursor()

	// If at start of line, join with previous line
	if b.Cursor.Col == 0 {
		if b.Cursor.Line > 0 {
			b.Backspace()
		}
		return
	}

	line := b.Lines[b.Cursor.Line]
	startCol := b.Cursor.Col
	col := b.Cursor.Col - 1

	// Skip whitespace backward
	for col > 0 && charClass(rune(line[col])) == 0 {
		col--
	}

	// Skip contiguous chars of the same class backward
	if col >= 0 && col < len(line) {
		cls := charClass(rune(line[col]))
		if cls != 0 {
			for col > 0 && charClass(rune(line[col-1])) == cls {
				col--
			}
		}
	}

	// Delete the text
	if col < startCol {
		before := b.Cursor
		deleted := line[col:startCol]
		b.Lines[b.Cursor.Line] = line[:col] + line[startCol:]
		b.Cursor.Col = col
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpDelete, Pos: b.Cursor, Text: deleted, Before: before})
	}
}

// DeleteWordForward deletes from the cursor forward to the end of the current word
func (b *Buffer) DeleteWordForward() {
	if b.deleteSelectionIfAny() {
		return
	}
	b.clampCursor()

	line := b.Lines[b.Cursor.Line]

	// If at end of line, join with next line (like Delete key)
	if b.Cursor.Col >= len(line) {
		b.Delete()
		return
	}

	startCol := b.Cursor.Col
	col := b.Cursor.Col

	// Get class of current char
	cls := charClass(rune(line[col]))

	if cls == 0 {
		// On whitespace: skip whitespace
		for col < len(line) && charClass(rune(line[col])) == 0 {
			col++
		}
	} else {
		// On word or symbol: skip contiguous same-class chars
		for col < len(line) && charClass(rune(line[col])) == cls {
			col++
		}
		// Then skip trailing whitespace
		for col < len(line) && charClass(rune(line[col])) == 0 {
			col++
		}
	}

	// Delete the text
	if col > startCol {
		before := b.Cursor
		deleted := line[startCol:col]
		b.Lines[b.Cursor.Line] = line[:startCol] + line[col:]
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpDelete, Pos: b.Cursor, Text: deleted, Before: before})
	}
}

func (b *Buffer) ApplyUndo() {
	op, ok := b.Undo.PopUndo()
	if !ok {
		return
	}

	// PopUndo already moved all grouped ops to redo stack.
	// We need to apply inversions for all ops in the group.
	// The first popped op is the most recent; grouped ops are in redo stack
	// in reverse order (most recent first).
	if op.Group != 0 {
		// Collect all grouped ops that were moved to redo (they're at the end)
		groupOps := []Operation{op}
		for i := len(b.Undo.redos) - 1; i >= 0; i-- {
			if b.Undo.redos[i].Group == op.Group {
				groupOps = append(groupOps, b.Undo.redos[i])
			} else {
				break
			}
		}
		// Apply all in reverse-chronological order (already in that order)
		for _, gop := range groupOps {
			b.applyInverseNoState(gop)
		}
		// Restore cursor to earliest op's before position
		b.Cursor = groupOps[len(groupOps)-1].Before
	} else {
		b.applyInverse(op)
		return
	}
	b.Selection = nil
	b.Dirty = true
}

func (b *Buffer) ApplyRedo() {
	op, ok := b.Undo.PopRedo()
	if !ok {
		return
	}

	if op.Group != 0 {
		// Collect all grouped ops moved to undo stack
		groupOps := []Operation{op}
		for i := len(b.Undo.undos) - 1; i >= 0; i-- {
			if b.Undo.undos[i].Group == op.Group {
				groupOps = append(groupOps, b.Undo.undos[i])
			} else {
				break
			}
		}
		// Apply in chronological order (reverse of how they were collected)
		for i := len(groupOps) - 1; i >= 0; i-- {
			b.applyForwardNoState(groupOps[i])
		}
		b.Cursor = b.posAfterInsert(groupOps[0].Pos, groupOps[0].Text)
	} else {
		b.applyForward(op)
		return
	}
	b.Selection = nil
	b.Dirty = true
}

func (b *Buffer) applyInverseNoState(op Operation) {
	switch op.Type {
	case OpInsert:
		b.removeText(op.Pos, op.Text)
	case OpDelete:
		b.insertTextAt(op.Pos, op.Text)
	}
}

func (b *Buffer) applyForwardNoState(op Operation) {
	switch op.Type {
	case OpInsert:
		b.insertTextAt(op.Pos, op.Text)
	case OpDelete:
		b.removeText(op.Pos, op.Text)
	}
}

func (b *Buffer) applyInverse(op Operation) {
	switch op.Type {
	case OpInsert:
		// Inverse of insert is delete
		b.removeText(op.Pos, op.Text)
	case OpDelete:
		// Inverse of delete is insert
		b.insertTextAt(op.Pos, op.Text)
	}
	b.Cursor = op.Before
	b.Selection = nil
	b.Dirty = true
}

func (b *Buffer) applyForward(op Operation) {
	switch op.Type {
	case OpInsert:
		b.insertTextAt(op.Pos, op.Text)
		b.Cursor = b.posAfterInsert(op.Pos, op.Text)
	case OpDelete:
		b.removeText(op.Pos, op.Text)
		b.Cursor = op.Pos
	}
	b.Selection = nil
	b.Dirty = true
}

func (b *Buffer) insertTextAt(pos Cursor, text string) {
	if len(text) == 0 {
		return
	}
	// Validate position
	if pos.Line >= len(b.Lines) {
		return
	}
	line := b.Lines[pos.Line]
	if pos.Col > len(line) {
		pos.Col = len(line)
	}

	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		b.Lines[pos.Line] = line[:pos.Col] + text + line[pos.Col:]
	} else {
		rest := line[pos.Col:]
		b.Lines[pos.Line] = line[:pos.Col] + lines[0]

		newLines := make([]string, len(lines)-1)
		for i := 1; i < len(lines); i++ {
			newLines[i-1] = lines[i]
		}
		newLines[len(newLines)-1] += rest

		after := make([]string, len(b.Lines)-pos.Line-1)
		copy(after, b.Lines[pos.Line+1:])
		b.Lines = append(b.Lines[:pos.Line+1], newLines...)
		b.Lines = append(b.Lines, after...)
	}
}

func (b *Buffer) removeText(pos Cursor, text string) {
	if len(text) == 0 {
		return
	}
	if pos.Line >= len(b.Lines) {
		return
	}
	line := b.Lines[pos.Line]
	if pos.Col > len(line) {
		pos.Col = len(line)
	}

	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		end := pos.Col + len(text)
		if end > len(line) {
			end = len(line)
		}
		b.Lines[pos.Line] = line[:pos.Col] + line[end:]
	} else {
		firstPart := line[:pos.Col]
		lastLineIdx := pos.Line + len(lines) - 1
		if lastLineIdx >= len(b.Lines) {
			lastLineIdx = len(b.Lines) - 1
		}
		lastLineLen := len(lines[len(lines)-1])
		lastLine := b.Lines[lastLineIdx]
		lastPart := ""
		if lastLineLen < len(lastLine) {
			lastPart = lastLine[lastLineLen:]
		}
		b.Lines[pos.Line] = firstPart + lastPart
		b.Lines = append(b.Lines[:pos.Line+1], b.Lines[lastLineIdx+1:]...)
	}
}

func (b *Buffer) posAfterInsert(pos Cursor, text string) Cursor {
	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		return Cursor{Line: pos.Line, Col: pos.Col + len(text)}
	}
	return Cursor{
		Line: pos.Line + len(lines) - 1,
		Col:  len(lines[len(lines)-1]),
	}
}

// ReplaceAt replaces `length` characters at the given position with `replacement`.
func (b *Buffer) ReplaceAt(line, col, length int, replacement string) {
	if line < 0 || line >= len(b.Lines) {
		return
	}
	l := b.Lines[line]
	end := col + length
	if end > len(l) {
		end = len(l)
	}
	before := b.Cursor
	oldText := l[col:end]
	b.Lines[line] = l[:col] + replacement + l[end:]
	b.Dirty = true
	// Record as delete+insert for undo
	b.Undo.Push(Operation{Type: OpDelete, Pos: Cursor{Line: line, Col: col}, Text: oldText, Before: before})
	b.Undo.Push(Operation{Type: OpInsert, Pos: Cursor{Line: line, Col: col}, Text: replacement, Before: before})
}

func (b *Buffer) WrapSelectionWith(ch rune) bool {
	if b.Selection == nil || b.Selection.Empty() {
		return false
	}
	sel := *b.Selection
	if sel.Start.Line != sel.End.Line {
		return false
	}
	if sel.Start.Line < 0 || sel.Start.Line >= len(b.Lines) {
		return false
	}

	line := b.Lines[sel.Start.Line]
	startCol := sel.Start.Col
	endCol := sel.End.Col
	if startCol < 0 {
		startCol = 0
	}
	if endCol < 0 {
		endCol = 0
	}
	if startCol > len(line) {
		startCol = len(line)
	}
	if endCol > len(line) {
		endCol = len(line)
	}
	if endCol <= startCol {
		return false
	}

	before := b.Cursor
	original := line[startCol:endCol]
	wrapped := string(ch) + original + string(ch)
	b.Lines[sel.Start.Line] = line[:startCol] + wrapped + line[endCol:]
	b.Cursor = Cursor{Line: sel.Start.Line, Col: startCol + len(wrapped)}
	b.Selection = nil
	b.autoClosePending = nil
	b.Dirty = true
	b.Undo.Push(Operation{Type: OpDelete, Pos: Cursor{Line: sel.Start.Line, Col: startCol}, Text: original, Before: before})
	b.Undo.Push(Operation{Type: OpInsert, Pos: Cursor{Line: sel.Start.Line, Col: startCol}, Text: wrapped, Before: before})
	return true
}

// ReplaceAll replaces all occurrences of `find` with `replacement` (case-insensitive).
func (b *Buffer) ReplaceAll(find, replacement string) int {
	if find == "" {
		return 0
	}
	before := b.Cursor
	count := 0
	findLower := strings.ToLower(find)
	// Process lines from bottom to top to preserve positions
	for i := len(b.Lines) - 1; i >= 0; i-- {
		line := b.Lines[i]
		lower := strings.ToLower(line)
		// Find all occurrences in this line, from right to left
		idx := len(lower)
		for {
			pos := strings.LastIndex(lower[:idx], findLower)
			if pos < 0 {
				break
			}
			b.Lines[i] = b.Lines[i][:pos] + replacement + b.Lines[i][pos+len(find):]
			lower = strings.ToLower(b.Lines[i])
			idx = pos
			count++
		}
	}
	if count > 0 {
		b.Dirty = true
		b.Undo.Push(Operation{Type: OpInsert, Pos: Cursor{}, Text: "", Before: before})
	}
	return count
}

// FoldRegion folds lines from startLine to endLine (endLine is the last folded line)
func (b *Buffer) FoldRegion(startLine, endLine int) {
	b.FoldedLines[startLine] = endLine + 1
}

// UnfoldLine unfolds the region starting at line
func (b *Buffer) UnfoldLine(line int) {
	delete(b.FoldedLines, line)
}

// IsFolded returns true if this line is the start of a folded region
func (b *Buffer) IsFolded(line int) bool {
	_, ok := b.FoldedLines[line]
	return ok
}

// IsHiddenByFold returns true if this line is hidden inside a fold
func (b *Buffer) IsHiddenByFold(line int) bool {
	for start, end := range b.FoldedLines {
		if line > start && line < end {
			return true
		}
	}
	return false
}

// FindFoldRange finds the foldable range at the given line based on indentation.
func (b *Buffer) FindFoldRange(line int) (int, int) {
	if line < 0 || line >= len(b.Lines) {
		return -1, -1
	}

	baseIndent := b.lineIndent(line)
	if baseIndent < 0 {
		return -1, -1
	}

	endLine := line
	for i := line + 1; i < len(b.Lines); i++ {
		indent := b.lineIndent(i)
		if indent < 0 {
			continue // skip empty lines
		}
		if indent <= baseIndent {
			break
		}
		endLine = i
	}

	if endLine > line {
		return line, endLine
	}
	return -1, -1
}

func (b *Buffer) lineIndent(line int) int {
	if line < 0 || line >= len(b.Lines) || len(strings.TrimSpace(b.Lines[line])) == 0 {
		return -1
	}
	indent := 0
	for _, ch := range b.Lines[line] {
		if ch == ' ' {
			indent++
		} else if ch == '\t' {
			indent += b.TabSize
		} else {
			break
		}
	}
	return indent
}

// ToggleFold toggles fold at the given line
func (b *Buffer) ToggleFold(line int) {
	if b.IsFolded(line) {
		b.UnfoldLine(line)
	} else {
		start, end := b.FindFoldRange(line)
		if start >= 0 {
			b.FoldRegion(start, end)
		}
	}
}

// FoldedLineCount returns the number of hidden lines in a fold starting at line
func (b *Buffer) FoldedLineCount(line int) int {
	end, ok := b.FoldedLines[line]
	if !ok {
		return 0
	}
	return end - line - 1
}

// AddCursorAt adds an extra cursor at the given position
func (b *Buffer) AddCursorAt(line, col int) {
	// Validate bounds
	if line < 0 || line >= len(b.Lines) {
		return
	}

	// Check if cursor already exists at this line (prevent duplicates on same line)
	if b.Cursor.Line == line {
		return // Main cursor is already on this line
	}
	for _, c := range b.ExtraCursors {
		if c.Line == line {
			return // Extra cursor already on this line
		}
	}

	// Clamp column to line length
	if col > len(b.Lines[line]) {
		col = len(b.Lines[line])
	}
	if col < 0 {
		col = 0
	}

	b.ExtraCursors = append(b.ExtraCursors, Cursor{Line: line, Col: col})
}

// ClearExtraCursors removes all extra cursors
func (b *Buffer) ClearExtraCursors() {
	b.ExtraCursors = b.ExtraCursors[:0]
}

// HasExtraCursors returns true if there are extra cursors active
func (b *Buffer) HasExtraCursors() bool {
	return len(b.ExtraCursors) > 0
}

// allCursorsSorted returns pointers to all cursors sorted by position (top-left first)
func (b *Buffer) allCursorsSorted() []*Cursor {
	result := make([]*Cursor, 0, 1+len(b.ExtraCursors))
	result = append(result, &b.Cursor)
	for i := range b.ExtraCursors {
		result = append(result, &b.ExtraCursors[i])
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Line != result[j].Line {
			return result[i].Line < result[j].Line
		}
		return result[i].Col < result[j].Col
	})
	return result
}

// InsertCharMulti inserts a character at all cursor positions
func (b *Buffer) InsertCharMulti(ch rune) {
	allCursors := b.allCursorsSorted()
	text := string(ch)
	groupID := b.Undo.NewGroup()

	// Process from bottom-right to top-left to preserve positions
	for i := len(allCursors) - 1; i >= 0; i-- {
		pos := allCursors[i]
		if pos.Line < 0 || pos.Line >= len(b.Lines) {
			continue
		}
		line := b.Lines[pos.Line]
		col := pos.Col
		if col > len(line) {
			col = len(line)
		}
		before := *pos
		b.Lines[pos.Line] = line[:col] + text + line[col:]
		b.Undo.PushGrouped(Operation{Type: OpInsert, Pos: before, Text: text, Before: before}, groupID)
	}

	// Advance all cursors
	for i := len(allCursors) - 1; i >= 0; i-- {
		allCursors[i].Col++
	}
	b.Dirty = true
}

// DeleteCharMulti deletes the character before each cursor (backspace)
func (b *Buffer) DeleteCharMulti() {
	allCursors := b.allCursorsSorted()
	groupID := b.Undo.NewGroup()

	// Process from bottom-right to top-left
	for i := len(allCursors) - 1; i >= 0; i-- {
		pos := allCursors[i]
		if pos.Line < 0 || pos.Line >= len(b.Lines) {
			continue
		}
		if pos.Col > 0 {
			line := b.Lines[pos.Line]
			col := pos.Col
			if col > len(line) {
				col = len(line)
			}
			before := *pos
			deleted := string(line[col-1])
			b.Lines[pos.Line] = line[:col-1] + line[col:]
			b.Undo.PushGrouped(Operation{Type: OpDelete, Pos: Cursor{Line: pos.Line, Col: col - 1}, Text: deleted, Before: before}, groupID)
			pos.Col = col - 1
		}
	}
	b.Dirty = true
}

// DeleteForwardMulti deletes the character after each cursor (delete key)
func (b *Buffer) DeleteForwardMulti() {
	allCursors := b.allCursorsSorted()
	groupID := b.Undo.NewGroup()

	for i := len(allCursors) - 1; i >= 0; i-- {
		pos := allCursors[i]
		if pos.Line < 0 || pos.Line >= len(b.Lines) {
			continue
		}
		line := b.Lines[pos.Line]
		if pos.Col < len(line) {
			before := *pos
			deleted := string(line[pos.Col])
			b.Lines[pos.Line] = line[:pos.Col] + line[pos.Col+1:]
			b.Undo.PushGrouped(Operation{Type: OpDelete, Pos: *pos, Text: deleted, Before: before}, groupID)
		}
	}
	b.Dirty = true
}

// MoveCursorsLeft moves all cursors one position to the left
func (b *Buffer) MoveCursorsLeft() {
	for i := range b.ExtraCursors {
		if b.ExtraCursors[i].Line < 0 || b.ExtraCursors[i].Line >= len(b.Lines) {
			continue
		}
		if b.ExtraCursors[i].Col > 0 {
			b.ExtraCursors[i].Col--
		} else if b.ExtraCursors[i].Line > 0 {
			b.ExtraCursors[i].Line--
			if b.ExtraCursors[i].Line >= 0 && b.ExtraCursors[i].Line < len(b.Lines) {
				b.ExtraCursors[i].Col = len(b.Lines[b.ExtraCursors[i].Line])
			}
		}
	}
}

// MoveCursorsRight moves all cursors one position to the right
func (b *Buffer) MoveCursorsRight() {
	for i := range b.ExtraCursors {
		if b.ExtraCursors[i].Line < 0 || b.ExtraCursors[i].Line >= len(b.Lines) {
			continue
		}
		if b.ExtraCursors[i].Col < len(b.Lines[b.ExtraCursors[i].Line]) {
			b.ExtraCursors[i].Col++
		} else if b.ExtraCursors[i].Line < len(b.Lines)-1 {
			b.ExtraCursors[i].Line++
			b.ExtraCursors[i].Col = 0
		}
	}
}

// MoveCursorsUp moves all cursors one line up
func (b *Buffer) MoveCursorsUp() {
	for i := range b.ExtraCursors {
		if b.ExtraCursors[i].Line > 0 {
			b.ExtraCursors[i].Line--
			if b.ExtraCursors[i].Line >= 0 && b.ExtraCursors[i].Line < len(b.Lines) {
				lineLen := len(b.Lines[b.ExtraCursors[i].Line])
				if b.ExtraCursors[i].Col > lineLen {
					b.ExtraCursors[i].Col = lineLen
				}
			}
		}
	}
}

// MoveCursorsDown moves all cursors one line down
func (b *Buffer) MoveCursorsDown() {
	for i := range b.ExtraCursors {
		if b.ExtraCursors[i].Line < len(b.Lines)-1 {
			b.ExtraCursors[i].Line++
			if b.ExtraCursors[i].Line >= 0 && b.ExtraCursors[i].Line < len(b.Lines) {
				lineLen := len(b.Lines[b.ExtraCursors[i].Line])
				if b.ExtraCursors[i].Col > lineLen {
					b.ExtraCursors[i].Col = lineLen
				}
			}
		}
	}
}

// SelectNextOccurrence finds the next occurrence of the current word/selection and adds a cursor there
func (b *Buffer) SelectNextOccurrence() {
	var searchText string
	if b.Selection != nil && !b.Selection.Empty() {
		searchText = b.GetSelectedText()
	} else {
		searchText = b.WordAtCursor()
	}
	if searchText == "" {
		return
	}

	// Find the last cursor position to search from after it
	lastLine := b.Cursor.Line
	lastCol := b.Cursor.Col + len(searchText)
	for _, c := range b.ExtraCursors {
		if c.Line > lastLine || (c.Line == lastLine && c.Col+len(searchText) > lastCol) {
			lastLine = c.Line
			lastCol = c.Col + len(searchText)
		}
	}

	searchLower := strings.ToLower(searchText)

	// Search from after the last cursor
	for lineIdx := lastLine; lineIdx < len(b.Lines); lineIdx++ {
		startCol := 0
		if lineIdx == lastLine {
			startCol = lastCol
		}
		line := b.Lines[lineIdx]
		if startCol > len(line) {
			continue
		}
		idx := strings.Index(strings.ToLower(line[startCol:]), searchLower)
		if idx >= 0 {
			b.AddCursorAt(lineIdx, startCol+idx)
			return
		}
	}
	// Wrap around from beginning
	for lineIdx := 0; lineIdx <= lastLine; lineIdx++ {
		line := b.Lines[lineIdx]
		endCol := len(line)
		if lineIdx == lastLine {
			endCol = lastCol - len(searchText)
			if endCol < 0 {
				endCol = 0
			}
		}
		if endCol > len(line) {
			endCol = len(line)
		}
		idx := strings.Index(strings.ToLower(line[:endCol]), searchLower)
		if idx >= 0 {
			b.AddCursorAt(lineIdx, idx)
			return
		}
	}
}

// GetTextInRange returns the text between two cursor positions.
func (b *Buffer) GetTextInRange(start, end Cursor) string {
	if start.Line < 0 || start.Line >= len(b.Lines) || end.Line < 0 || end.Line >= len(b.Lines) {
		return ""
	}
	if start.Line == end.Line {
		line := b.Lines[start.Line]
		sc := start.Col
		ec := end.Col
		if sc > len(line) {
			sc = len(line)
		}
		if ec > len(line) {
			ec = len(line)
		}
		if sc >= ec {
			return ""
		}
		return line[sc:ec]
	}
	var sb strings.Builder
	firstLine := b.Lines[start.Line]
	sc := start.Col
	if sc > len(firstLine) {
		sc = len(firstLine)
	}
	sb.WriteString(firstLine[sc:])
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(b.Lines[i])
	}
	sb.WriteByte('\n')
	lastLine := b.Lines[end.Line]
	ec := end.Col
	if ec > len(lastLine) {
		ec = len(lastLine)
	}
	sb.WriteString(lastLine[:ec])
	return sb.String()
}

// RemoveTextAt removes the given text starting at pos (exported wrapper).
func (b *Buffer) RemoveTextAt(pos Cursor, text string) {
	b.removeText(pos, text)
}

// InsertTextAt inserts text at the given position without moving the cursor (exported wrapper).
func (b *Buffer) InsertTextAtPos(pos Cursor, text string) {
	b.insertTextAt(pos, text)
}

// WordAtCursor returns the word under the cursor
func (b *Buffer) WordAtCursor() string {
	if b.Cursor.Line < 0 || b.Cursor.Line >= len(b.Lines) {
		return ""
	}
	line := b.Lines[b.Cursor.Line]
	if b.Cursor.Col > len(line) {
		return ""
	}
	start, end := b.WordAt(b.Cursor.Line, b.Cursor.Col)
	if start == end {
		return ""
	}
	return line[start:end]
}
