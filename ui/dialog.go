package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"editor/config"
	"github.com/gdamore/tcell/v2"
)

type DialogType int

const (
	DialogNone DialogType = iota
	DialogFind
	DialogGotoLine
	DialogSaveConfirm
	DialogSaveAs
	DialogHelp
	DialogSettings
	DialogInput // Generic text input dialog
	DialogReloadConfirm
)

type Dialog struct {
	Type    DialogType
	Input   string
	Cursor  int
	focused bool

	// Find state
	Matches    []Match
	MatchIndex int
	UseRegex   bool

	// Replace state
	ReplaceInput  string
	ReplaceCursor int
	ReplaceActive bool // true when cursor is in replace field
	ReplaceMode   bool // true when find+replace dialog is open

	// Settings state
	SettingsOptions  []string
	SettingsValues   []string
	SettingsIndex    int
	SettingsSections []SettingsSection
	SettingsScroll   int
	SettingsMaxVis   int

	// Theme support
	Theme *config.ColorScheme

	// Callbacks
	OnSubmit               func(value string)
	OnCancel               func()
	OnNavigate             func(line, col int) // for F3/Shift+F3 find navigation
	OnConfirm              func(answer rune)   // for save confirm: 'y', 'n', 'c'
	OnSettingChange        func(index int, value string)
	OnSettingChangeReverse func(index int, value string)          // for left arrow
	OnReplace              func(matchIdx int, replacement string) // replace single match
	OnReplaceAll           func(find, replacement string) int     // replace all, returns count

	// Generic input dialog prompt
	Prompt    string
	MaskInput bool
}

type SettingsSection struct {
	Name    string
	Options []string
	Indices []int
}

type Match struct {
	Line, Col int
	Length    int
}

func NewFindDialog() *Dialog {
	return &Dialog{
		Type:    DialogFind,
		focused: true,
	}
}

func NewFindReplaceDialog() *Dialog {
	return &Dialog{
		Type:        DialogFind,
		ReplaceMode: true,
		focused:     true,
	}
}

func NewGotoLineDialog() *Dialog {
	return &Dialog{
		Type:    DialogGotoLine,
		focused: true,
	}
}

func NewSaveAsDialog() *Dialog {
	return &Dialog{
		Type:    DialogSaveAs,
		focused: true,
	}
}

func NewSaveConfirmDialog(filename string) *Dialog {
	return &Dialog{
		Type:    DialogSaveConfirm,
		Input:   filename,
		focused: true,
	}
}

func NewHelpDialog() *Dialog {
	return &Dialog{
		Type:    DialogHelp,
		focused: true,
	}
}

func NewSettingsDialog(options, values []string) *Dialog {
	return &Dialog{
		Type:            DialogSettings,
		SettingsOptions: options,
		SettingsValues:  values,
		SettingsIndex:   0,
		SettingsScroll:  0,
		focused:         true,
	}
}

func NewInputDialog(prompt string) *Dialog {
	return &Dialog{
		Type:    DialogInput,
		Prompt:  prompt,
		focused: true,
	}
}

func NewReloadConfirmDialog(filename string) *Dialog {
	return &Dialog{
		Type:    DialogReloadConfirm,
		Input:   filename,
		focused: true,
	}
}

func NewDeleteConfirmDialog(filename string) *Dialog {
	return &Dialog{
		Type:    DialogSaveConfirm,
		Input:   filename,
		Prompt:  "delete",
		focused: true,
	}
}

func (d *Dialog) Render(screen tcell.Screen, x, y, width, height int) {
	switch d.Type {
	case DialogFind:
		d.renderInputBar(screen, x, y, width, "Find: ")
		if d.ReplaceMode {
			d.renderInputBar2(screen, x, y+1, width, "Replace: ")
		}
	case DialogInput:
		d.renderInputBar(screen, x, y, width, d.Prompt)
	case DialogGotoLine:
		d.renderInputBar(screen, x, y, width, "Go to line: ")
	case DialogSaveAs:
		d.renderInputBar(screen, x, y, width, "Save as: ")
	case DialogSaveConfirm:
		d.renderSaveConfirm(screen, x, y, width)
	case DialogReloadConfirm:
		d.renderReloadConfirm(screen, x, y, width)
	case DialogHelp:
		d.renderHelp(screen, x, y, width, height)
	case DialogSettings:
		d.renderSettings(screen, x, y, width, height)
	}
}

func (d *Dialog) renderInputBar(screen tcell.Screen, x, y, width int, prompt string) {
	style := tcell.StyleDefault.Background(tcell.ColorDarkGray).Foreground(tcell.ColorWhite)
	promptStyle := tcell.StyleDefault.Background(tcell.ColorDarkGray).Foreground(tcell.ColorYellow).Bold(true)
	// Dim the find bar when replace field is active
	if d.ReplaceMode && d.ReplaceActive {
		promptStyle = tcell.StyleDefault.Background(tcell.ColorDarkGray).Foreground(tcell.ColorOlive)
	}

	// Clear line
	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, ' ', nil, style)
	}

	col := x
	// Prompt
	for _, ch := range prompt {
		if col < x+width {
			screen.SetContent(col, y, ch, nil, promptStyle)
			col++
		}
	}

	// Input text
	displayInput := d.Input
	if d.MaskInput {
		displayInput = strings.Repeat("*", len([]rune(d.Input)))
	}
	for i, ch := range displayInput {
		if col >= x+width {
			break
		}
		if i == d.Cursor && !d.ReplaceActive {
			screen.SetContent(col, y, ch, nil, style.Reverse(true))
		} else {
			screen.SetContent(col, y, ch, nil, style)
		}
		col++
	}

	// Cursor at end
	if !d.ReplaceActive && d.Cursor >= len([]rune(d.Input)) && col < x+width {
		screen.SetContent(col, y, ' ', nil, style.Reverse(true))
		col++
	}

	// Match count for find dialog
	if d.Type == DialogFind {
		var info string
		if d.UseRegex {
			info = " [.*]"
		}
		if len(d.Matches) > 0 {
			info += " (" + strconv.Itoa(d.MatchIndex+1) + "/" + strconv.Itoa(len(d.Matches)) + ")"
		} else if d.Input != "" {
			info += " (0)"
		}
		if info != "" {
			infoStart := x + width - len(info)
			if infoStart > col {
				regexStyle := style.Foreground(tcell.ColorGray)
				for i, ch := range info {
					screen.SetContent(infoStart+i, y, ch, nil, regexStyle)
				}
			}
		}
	}
}

// renderInputBar2 renders the replace input bar below the find bar
func (d *Dialog) renderInputBar2(screen tcell.Screen, x, y, width int, prompt string) {
	style := tcell.StyleDefault.Background(tcell.ColorDarkGray).Foreground(tcell.ColorWhite)
	promptStyle := tcell.StyleDefault.Background(tcell.ColorDarkGray).Foreground(tcell.ColorYellow).Bold(true)
	if !d.ReplaceActive {
		promptStyle = tcell.StyleDefault.Background(tcell.ColorDarkGray).Foreground(tcell.ColorOlive)
	}

	// Clear line
	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, ' ', nil, style)
	}

	col := x
	// Prompt
	for _, ch := range prompt {
		if col < x+width {
			screen.SetContent(col, y, ch, nil, promptStyle)
			col++
		}
	}

	// Input text
	for i, ch := range d.ReplaceInput {
		if col >= x+width {
			break
		}
		if i == d.ReplaceCursor && d.ReplaceActive {
			screen.SetContent(col, y, ch, nil, style.Reverse(true))
		} else {
			screen.SetContent(col, y, ch, nil, style)
		}
		col++
	}

	// Cursor at end
	if d.ReplaceActive && d.ReplaceCursor >= len([]rune(d.ReplaceInput)) && col < x+width {
		screen.SetContent(col, y, ' ', nil, style.Reverse(true))
		col++
	}

	// Hint text
	hint := " Enter=Replace  Ctrl+A=All"
	hintStart := x + width - len(hint)
	if hintStart > col {
		for i, ch := range hint {
			screen.SetContent(hintStart+i, y, ch, nil, style.Foreground(tcell.ColorGray))
		}
	}
}

func (d *Dialog) renderSaveConfirm(screen tcell.Screen, x, y, width int) {
	style := tcell.StyleDefault.Background(tcell.ColorDarkRed).Foreground(tcell.ColorWhite)
	var msg string
	if d.Prompt == "delete" {
		msg = " Delete " + d.Input + "? [Y]es [N]o "
	} else {
		msg = " Save changes to " + d.Input + "? [Y]es [N]o [C]ancel "
	}

	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, ' ', nil, style)
	}

	col := x
	for _, ch := range msg {
		if col < x+width {
			screen.SetContent(col, y, ch, nil, style)
			col++
		}
	}
}

func (d *Dialog) renderReloadConfirm(screen tcell.Screen, x, y, width int) {
	style := tcell.StyleDefault.Background(tcell.ColorOrange).Foreground(tcell.ColorBlack)
	msg := " Reload " + d.Input + " from disk? [Y]es [C]ancel "

	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, ' ', nil, style)
	}

	col := x
	for _, ch := range msg {
		if col < x+width {
			screen.SetContent(col, y, ch, nil, style)
			col++
		}
	}
}

func (d *Dialog) renderHelp(screen tcell.Screen, x, y, width, height int) {
	// Color scheme
	overlayStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorBlack)
	borderStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
	bgStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
	titleStyle := tcell.StyleDefault.Background(tcell.ColorTeal).Foreground(tcell.ColorBlack).Bold(true)
	categoryStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorLightCyan).Bold(true)
	keyStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorYellow)
	descStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorSilver)
	footerStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorGray).Italic(true)

	keybindings := []struct {
		category string
		key      string
		desc     string
	}{
		{"FILE OPERATIONS", "", ""},
		{"", "Ctrl+S", "Save file"},
		{"", "Ctrl+N", "New file"},
		{"", "Ctrl+W", "Close tab"},
		{"", "Ctrl+Q", "Quit editor"},
		{"", "Ctrl+P", "Command palette"},
		{"", "Ctrl+Shift+P", "Command palette (alt)"},
		{"", "Alt+1-9, 0", "Switch to tab 1-9, 10"},
		{"", "Ctrl+Tab", "Next tab"},
		{"", "Ctrl+Shift+Tab", "Previous tab"},
		{"", "", ""},
		{"EDITING", "", ""},
		{"", "Ctrl+Z", "Undo"},
		{"", "Ctrl+Shift+Z", "Redo"},
		{"", "Ctrl+C", "Copy (line if no sel.)"},
		{"", "Ctrl+X", "Cut (line if no sel.)"},
		{"", "Ctrl+V", "Paste"},
		{"", "Ctrl+A", "Select all"},
		{"", "Ctrl+D", "Duplicate line"},
		{"", "Ctrl+/", "Toggle line comment"},
		{"", "Alt+Up/Down", "Move line up/down"},
		{"", "Ctrl+Backspace", "Delete word backward"},
		{"", "Ctrl+Delete", "Delete word forward"},
		{"", "Tab / Shift+Tab", "Indent / Dedent"},
		{"", "", ""},
		{"NAVIGATION", "", ""},
		{"", "Ctrl+F", "Find text"},
		{"", "Ctrl+R", "Find and replace"},
		{"", "F3 / Shift+F3", "Next / Previous match"},
		{"", "Ctrl+G", "Go to line"},
		{"", "F12", "Go to definition"},
		{"", "F2", "Rename symbol"},
		{"", "Ctrl+]", "Jump to matching bracket"},
		{"", "Ctrl/Alt+Left/Right", "Word skip"},
		{"", "Shift+Arrow", "Character selection"},
		{"", "Ctrl+Shift+Arrow", "Word selection"},
		{"", "", ""},
		{"SEARCH", "", ""},
		{"", "Ctrl+P / Ctrl+Shift+P", "Command palette"},
		{"", "%<text>", "Search in files (palette)"},
		{"", "", ""},
		{"UI & DISPLAY", "", ""},
		{"", "Ctrl+B", "Toggle file tree"},
		{"", "Ctrl+E", "Toggle tree focus"},
		{"", "Ctrl+T", "Toggle terminal"},
		{"", "Ctrl+.", "Toggle code fold"},
		{"", "Alt+Z", "Toggle word wrap"},
		{"", "Alt+,", "Settings dialog"},
		{"", "Shift+Wheel", "Horizontal scroll"},
		{"", "Middle/Right drag", "Multi-cursor (vertical)"},
		{"", "Shift+Click", "Extend selection"},
		{"", "Ctrl+H / F1", "Toggle help"},
		{"", "Esc", "Close dialog / Clear sel."},
	}

	// Calculate dialog dimensions
	dialogW := 66
	dialogH := len(keybindings) + 4
	if dialogW > width-4 {
		dialogW = width - 4
	}
	if dialogH > height-4 {
		dialogH = height - 4
	}

	dialogX := x + (width-dialogW)/2
	dialogY := y + (height-dialogH)/2

	// Draw semi-transparent overlay
	for dy := 0; dy < height; dy++ {
		for dx := 0; dx < width; dx++ {
			screen.SetContent(x+dx, y+dy, '░', nil, overlayStyle)
		}
	}

	// Draw dialog box background
	for dy := 0; dy < dialogH; dy++ {
		for dx := 0; dx < dialogW; dx++ {
			screen.SetContent(dialogX+dx, dialogY+dy, ' ', nil, bgStyle)
		}
	}

	// Draw border
	// Top border
	for dx := 0; dx < dialogW; dx++ {
		screen.SetContent(dialogX+dx, dialogY, '─', nil, borderStyle)
	}
	// Bottom border
	for dx := 0; dx < dialogW; dx++ {
		screen.SetContent(dialogX+dx, dialogY+dialogH-1, '─', nil, borderStyle)
	}
	// Left and right borders
	for dy := 0; dy < dialogH; dy++ {
		screen.SetContent(dialogX, dialogY+dy, '│', nil, borderStyle)
		screen.SetContent(dialogX+dialogW-1, dialogY+dy, '│', nil, borderStyle)
	}
	// Corners
	screen.SetContent(dialogX, dialogY, '┌', nil, borderStyle)
	screen.SetContent(dialogX+dialogW-1, dialogY, '┐', nil, borderStyle)
	screen.SetContent(dialogX, dialogY+dialogH-1, '└', nil, borderStyle)
	screen.SetContent(dialogX+dialogW-1, dialogY+dialogH-1, '┘', nil, borderStyle)

	// Draw title bar - fill entire top border with title background
	title := " ⌨  Keyboard Shortcuts "
	titleX := dialogX + (dialogW-len(title))/2

	// Fill the entire top border with title background color
	for dx := 1; dx < dialogW-1; dx++ {
		screen.SetContent(dialogX+dx, dialogY, '─', nil, titleStyle)
	}

	// Draw the title text
	for i, ch := range title {
		screen.SetContent(titleX+i, dialogY, ch, nil, titleStyle)
	}

	// Draw keybindings
	row := dialogY + 2
	for _, kb := range keybindings {
		if row >= dialogY+dialogH-2 {
			break
		}

		// Category header
		if kb.category != "" {
			col := dialogX + 3
			for _, ch := range kb.category {
				if col < dialogX+dialogW-3 {
					screen.SetContent(col, row, ch, nil, categoryStyle)
					col++
				}
			}
			row++
			continue
		}

		// Empty line
		if kb.key == "" {
			row++
			continue
		}

		// Draw key (left aligned with padding)
		col := dialogX + 5
		for _, ch := range kb.key {
			if col < dialogX+dialogW-3 {
				screen.SetContent(col, row, ch, nil, keyStyle)
				col++
			}
		}

		// Draw description (right side)
		col = dialogX + 28
		for _, ch := range kb.desc {
			if col < dialogX+dialogW-3 {
				screen.SetContent(col, row, ch, nil, descStyle)
				col++
			}
		}

		row++
	}

	// Draw footer
	footer := "Press ESC or F1 to close"
	footerY := dialogY + dialogH - 1
	footerX := dialogX + (dialogW-len(footer))/2
	for i, ch := range footer {
		screen.SetContent(footerX+i, footerY, ch, nil, footerStyle)
	}
}

func (d *Dialog) HandleKey(ev *tcell.EventKey) bool {
	if d.Type == DialogSaveConfirm {
		return d.handleSaveConfirmKey(ev)
	}
	if d.Type == DialogReloadConfirm {
		return d.handleReloadConfirmKey(ev)
	}
	if d.Type == DialogHelp {
		return d.handleHelpKey(ev)
	}
	if d.Type == DialogSettings {
		return d.handleSettingsKey(ev)
	}
	return d.handleInputKey(ev)
}

func (d *Dialog) handleSaveConfirmKey(ev *tcell.EventKey) bool {
	ch := ev.Rune()
	switch {
	case ch == 'y' || ch == 'Y':
		if d.OnConfirm != nil {
			d.OnConfirm('y')
		}
	case ch == 'n' || ch == 'N':
		if d.OnConfirm != nil {
			d.OnConfirm('n')
		}
	case ch == 'c' || ch == 'C' || ev.Key() == tcell.KeyEscape:
		if d.OnConfirm != nil {
			d.OnConfirm('c')
		}
	}
	return true
}

func (d *Dialog) handleReloadConfirmKey(ev *tcell.EventKey) bool {
	ch := ev.Rune()
	switch {
	case ch == 'y' || ch == 'Y':
		if d.OnConfirm != nil {
			d.OnConfirm('y')
		}
	case ch == 'c' || ch == 'C' || ev.Key() == tcell.KeyEscape:
		if d.OnConfirm != nil {
			d.OnConfirm('c')
		}
	}
	return true
}

func (d *Dialog) handleHelpKey(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyF1 || ev.Key() == tcell.KeyCtrlH {
		if d.OnCancel != nil {
			d.OnCancel()
		}
		return true
	}
	return true
}

func (d *Dialog) handleInputKey(ev *tcell.EventKey) bool {
	// F3/Shift+F3 for find navigation
	if d.Type == DialogFind {
		switch ev.Key() {
		case tcell.KeyF3:
			if ev.Modifiers()&tcell.ModShift != 0 {
				d.PrevMatch()
			} else {
				d.NextMatch()
			}
			if d.OnNavigate != nil && len(d.Matches) > 0 {
				m := d.Matches[d.MatchIndex]
				d.OnNavigate(m.Line, m.Col)
			}
			return true
		case tcell.KeyTab, tcell.KeyBacktab:
			// Toggle between find and replace fields
			if d.ReplaceMode {
				d.ReplaceActive = !d.ReplaceActive
			}
			return true
		case tcell.KeyRune:
			if ev.Modifiers()&tcell.ModAlt != 0 && (ev.Rune() == 'r' || ev.Rune() == 'R') {
				d.UseRegex = !d.UseRegex
				return true
			}
		}
	}

	switch ev.Key() {
	case tcell.KeyEscape:
		if d.OnCancel != nil {
			d.OnCancel()
		}
		return true
	case tcell.KeyEnter:
		if d.ReplaceMode && d.ReplaceActive {
			// Replace current match
			if d.OnReplace != nil && len(d.Matches) > 0 {
				d.OnReplace(d.MatchIndex, d.ReplaceInput)
			}
			return true
		}
		if d.OnSubmit != nil {
			d.OnSubmit(d.Input)
		}
		return true
	case tcell.KeyCtrlA:
		if d.ReplaceMode && d.ReplaceActive {
			// Replace all
			if d.OnReplaceAll != nil {
				d.OnReplaceAll(d.Input, d.ReplaceInput)
			}
			return true
		}
		return false // let default Ctrl+A (select all) work in find field
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if d.ReplaceMode && d.ReplaceActive {
			if d.ReplaceCursor > 0 {
				runes := []rune(d.ReplaceInput)
				d.ReplaceInput = string(runes[:d.ReplaceCursor-1]) + string(runes[d.ReplaceCursor:])
				d.ReplaceCursor--
			}
			return true
		}
		if d.Cursor > 0 {
			runes := []rune(d.Input)
			d.Input = string(runes[:d.Cursor-1]) + string(runes[d.Cursor:])
			d.Cursor--
		}
		return true
	case tcell.KeyDelete:
		if d.ReplaceMode && d.ReplaceActive {
			runes := []rune(d.ReplaceInput)
			if d.ReplaceCursor < len(runes) {
				d.ReplaceInput = string(runes[:d.ReplaceCursor]) + string(runes[d.ReplaceCursor+1:])
			}
			return true
		}
		runes := []rune(d.Input)
		if d.Cursor < len(runes) {
			d.Input = string(runes[:d.Cursor]) + string(runes[d.Cursor+1:])
		}
		return true
	case tcell.KeyLeft:
		if d.ReplaceMode && d.ReplaceActive {
			if d.ReplaceCursor > 0 {
				d.ReplaceCursor--
			}
			return true
		}
		if d.Cursor > 0 {
			d.Cursor--
		}
		return true
	case tcell.KeyRight:
		if d.ReplaceMode && d.ReplaceActive {
			if d.ReplaceCursor < len([]rune(d.ReplaceInput)) {
				d.ReplaceCursor++
			}
			return true
		}
		if d.Cursor < len([]rune(d.Input)) {
			d.Cursor++
		}
		return true
	case tcell.KeyHome:
		if d.ReplaceMode && d.ReplaceActive {
			d.ReplaceCursor = 0
		} else {
			d.Cursor = 0
		}
		return true
	case tcell.KeyEnd:
		if d.ReplaceMode && d.ReplaceActive {
			d.ReplaceCursor = len([]rune(d.ReplaceInput))
		} else {
			d.Cursor = len([]rune(d.Input))
		}
		return true
	default:
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if d.Type == DialogGotoLine && (ch < '0' || ch > '9') {
				return true
			}
			if d.ReplaceMode && d.ReplaceActive {
				runes := []rune(d.ReplaceInput)
				d.ReplaceInput = string(runes[:d.ReplaceCursor]) + string(ch) + string(runes[d.ReplaceCursor:])
				d.ReplaceCursor++
				return true
			}
			runes := []rune(d.Input)
			d.Input = string(runes[:d.Cursor]) + string(ch) + string(runes[d.Cursor:])
			d.Cursor++
			return true
		}
	}
	return false
}

func (d *Dialog) FindMatches(lines []string) {
	d.Matches = nil
	d.MatchIndex = 0
	if d.Input == "" {
		return
	}
	if d.UseRegex {
		re, err := regexp.Compile("(?i)" + d.Input)
		if err != nil {
			return
		}
		for i, line := range lines {
			locs := re.FindAllStringIndex(line, -1)
			for _, loc := range locs {
				// Convert byte offsets to rune indices
				runeCol := utf8.RuneCountInString(line[:loc[0]])
				runeLen := utf8.RuneCountInString(line[loc[0]:loc[1]])
				d.Matches = append(d.Matches, Match{
					Line:   i,
					Col:    runeCol,
					Length: runeLen,
				})
			}
		}
		return
	}
	query := strings.ToLower(d.Input)
	queryRuneLen := utf8.RuneCountInString(d.Input)
	for i, line := range lines {
		lower := strings.ToLower(line)
		idx := 0
		for {
			pos := strings.Index(lower[idx:], query)
			if pos < 0 {
				break
			}
			bytePos := idx + pos
			// Convert byte offset to rune index
			runeCol := utf8.RuneCountInString(line[:bytePos])
			d.Matches = append(d.Matches, Match{
				Line:   i,
				Col:    runeCol,
				Length: queryRuneLen,
			})
			idx = bytePos + len(query)
		}
	}
}

func (d *Dialog) NextMatch() {
	if len(d.Matches) == 0 {
		return
	}
	d.MatchIndex = (d.MatchIndex + 1) % len(d.Matches)
}

func (d *Dialog) PrevMatch() {
	if len(d.Matches) == 0 {
		return
	}
	d.MatchIndex--
	if d.MatchIndex < 0 {
		d.MatchIndex = len(d.Matches) - 1
	}
}

func (d *Dialog) renderSettings(screen tcell.Screen, x, y, width, height int) {
	theme := d.Theme
	if theme == nil {
		theme = config.Themes["monokai"]
	}

	titleStyle := tcell.StyleDefault.Background(theme.StatusBarModeBg).Foreground(tcell.ColorWhite).Bold(true)
	borderStyle := tcell.StyleDefault.Foreground(theme.TreeBorder).Background(theme.StatusBarBg)
	bgStyle := tcell.StyleDefault.Background(theme.StatusBarBg).Foreground(theme.StatusBarFg)
	selectedStyle := tcell.StyleDefault.Background(theme.Selection).Foreground(theme.Foreground).Bold(true)
	labelStyle := tcell.StyleDefault.Background(theme.StatusBarBg).Foreground(theme.TreeHeaderFg)
	valueStyle := tcell.StyleDefault.Background(theme.StatusBarBg).Foreground(theme.Foreground).Bold(true)
	footerStyle := tcell.StyleDefault.Background(theme.StatusBarBg).Foreground(theme.LineNumber)
	sectionStyle := tcell.StyleDefault.Background(theme.StatusBarBg).Foreground(theme.StatusBarFg).Bold(true)

	dialogW := width / 3
	if dialogW < 38 {
		dialogW = 38
	}
	if dialogW > 56 {
		dialogW = 56
	}
	if dialogW > width-1 {
		dialogW = width - 1
	}
	if dialogW < 3 {
		return
	}
	dialogY := y
	dialogH := height
	if height > 2 {
		// Right sidebar sits between tab bar and status bar.
		dialogY = y + 1
		dialogH = height - 2
	}
	if dialogH < 3 {
		return
	}
	dialogX := x + width - dialogW

	for dy := 0; dy < dialogH; dy++ {
		for dx := 0; dx < dialogW; dx++ {
			screen.SetContent(dialogX+dx, dialogY+dy, ' ', nil, bgStyle)
		}
	}

	for dx := 0; dx < dialogW; dx++ {
		screen.SetContent(dialogX+dx, dialogY, '─', nil, borderStyle)
		screen.SetContent(dialogX+dx, dialogY+dialogH-1, '─', nil, borderStyle)
	}
	for dy := 0; dy < dialogH; dy++ {
		screen.SetContent(dialogX, dialogY+dy, '│', nil, borderStyle)
		screen.SetContent(dialogX+dialogW-1, dialogY+dy, '│', nil, borderStyle)
	}
	screen.SetContent(dialogX, dialogY, '┌', nil, borderStyle)
	screen.SetContent(dialogX+dialogW-1, dialogY, '┐', nil, borderStyle)
	screen.SetContent(dialogX, dialogY+dialogH-1, '└', nil, borderStyle)
	screen.SetContent(dialogX+dialogW-1, dialogY+dialogH-1, '┘', nil, borderStyle)

	title := " Settings "
	titleX := dialogX + (dialogW-len(title))/2
	for i, ch := range title {
		screen.SetContent(titleX+i, dialogY, ch, nil, titleStyle)
	}

	trimToWidth := func(s string, max int) string {
		if max <= 0 {
			return ""
		}
		r := []rune(s)
		if len(r) <= max {
			return s
		}
		if max <= 3 {
			return string(r[:max])
		}
		return string(r[:max-3]) + "..."
	}

	totalLines := 0
	for _, sec := range d.SettingsSections {
		totalLines += 2 + len(sec.Indices)
	}
	maxVis := dialogH - 4
	if maxVis < 1 {
		maxVis = 1
	}
	d.SettingsMaxVis = maxVis

	if d.SettingsScroll < 0 {
		d.SettingsScroll = 0
	}
	if totalLines > maxVis {
		if d.SettingsScroll > totalLines-maxVis {
			d.SettingsScroll = totalLines - maxVis
		}
	} else {
		d.SettingsScroll = 0
	}

	arrowStyle := tcell.StyleDefault.Background(theme.StatusBarBg).Foreground(theme.LineNumber)
	topArrowLine := -1
	botArrowLine := -1
	if totalLines > maxVis {
		if d.SettingsScroll > 0 {
			topArrowLine = d.SettingsScroll
		}
		if d.SettingsScroll+maxVis < totalLines {
			botArrowLine = d.SettingsScroll + maxVis - 1
		}
	}

	row := dialogY + 2
	lineNum := 0

	for _, sec := range d.SettingsSections {
		if row >= dialogY+dialogH-2 {
			break
		}

		if lineNum == topArrowLine {
			for cx := dialogX + 2; cx < dialogX+dialogW-2; cx++ {
				screen.SetContent(cx, row, ' ', nil, bgStyle)
			}
			screen.SetContent(dialogX+dialogW/2, row, '▲', nil, arrowStyle)
			row++
			lineNum++
		}

		if lineNum >= d.SettingsScroll && lineNum < d.SettingsScroll+maxVis {
			row++
		}
		lineNum++

		if lineNum >= d.SettingsScroll && lineNum < d.SettingsScroll+maxVis {
			col := dialogX + 2
			upperName := trimToWidth("["+strings.ToUpper(sec.Name)+"]", dialogW-4)
			for _, ch := range upperName {
				if col < dialogX+dialogW-3 {
					screen.SetContent(col, row, ch, nil, sectionStyle)
					col++
				}
			}
			row++
		}
		lineNum++

		for idx, optIdx := range sec.Indices {
			if row >= dialogY+dialogH-2 {
				break
			}
			if lineNum == botArrowLine {
				for cx := dialogX + 2; cx < dialogX+dialogW-2; cx++ {
					screen.SetContent(cx, row, ' ', nil, bgStyle)
				}
				screen.SetContent(dialogX+dialogW/2, row, '▼', nil, arrowStyle)
				row++
				lineNum++
				continue
			}
			if lineNum >= d.SettingsScroll && lineNum < d.SettingsScroll+maxVis {
				option := sec.Options[idx]
				value := d.SettingsValues[optIdx]

				style := bgStyle
				if optIdx == d.SettingsIndex {
					style = selectedStyle
				}

				for cx := dialogX + 2; cx < dialogX+dialogW-2; cx++ {
					screen.SetContent(cx, row, ' ', nil, style)
				}

				col := dialogX + 4
				if optIdx == d.SettingsIndex {
					screen.SetContent(dialogX+2, row, '>', nil, selectedStyle)
				}
				optStyle := labelStyle
				if optIdx == d.SettingsIndex {
					optStyle = selectedStyle
				}
				valueX := dialogX + dialogW - len(value) - 2
				if valueX <= col {
					valueX = col + 1
				}
				labelMax := valueX - col - 1
				label := trimToWidth(option, labelMax)
				for _, ch := range label {
					if col >= valueX {
						break
					}
					screen.SetContent(col, row, ch, nil, optStyle)
					col++
				}

				valStyle := valueStyle
				if optIdx == d.SettingsIndex {
					valStyle = selectedStyle.Bold(true)
				}
				for j, ch := range value {
					screen.SetContent(valueX+j, row, ch, nil, valStyle)
				}
				row++
			}
			lineNum++
		}
	}

	footerY := dialogY + dialogH - 1

	if totalLines > maxVis {
		posStr := fmt.Sprintf("%d/%d", d.SettingsScroll+1, totalLines)
		for i, ch := range posStr {
			if i < len(posStr) {
				screen.SetContent(dialogX+2+i, footerY, ch, nil, footerStyle)
			}
		}
		helpText := "< > change | ESC close"
		helpX := dialogX + (dialogW-len(helpText))/2
		for i, ch := range helpText {
			screen.SetContent(helpX+i, footerY, ch, nil, footerStyle)
		}
	} else {
		helpText := "< > change | ESC close"
		helpX := dialogX + (dialogW-len(helpText))/2
		for i, ch := range helpText {
			screen.SetContent(helpX+i, footerY, ch, nil, footerStyle)
		}
	}
}

func (d *Dialog) handleSettingsKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		if d.OnCancel != nil {
			d.OnCancel()
		}
		return true
	case tcell.KeyUp:
		if d.SettingsIndex > 0 {
			d.SettingsIndex--
			d.adjustScrollUp()
		}
		return true
	case tcell.KeyDown:
		if d.SettingsIndex < len(d.SettingsOptions)-1 {
			d.SettingsIndex++
			d.adjustScrollDown()
		}
		return true
	case tcell.KeyRight, tcell.KeyEnter:
		if d.OnSettingChange != nil {
			d.OnSettingChange(d.SettingsIndex, d.SettingsValues[d.SettingsIndex])
		}
		return true
	case tcell.KeyLeft:
		if d.OnSettingChangeReverse != nil {
			d.OnSettingChangeReverse(d.SettingsIndex, d.SettingsValues[d.SettingsIndex])
		}
		return true
	}
	return false
}

func (d *Dialog) getLineForIndex(index int) int {
	line := 0
	for _, sec := range d.SettingsSections {
		for i, idx := range sec.Indices {
			if idx == index {
				return line + 2 + i
			}
		}
		line += 2 + len(sec.Indices)
	}
	return line
}

func (d *Dialog) adjustScrollDown() {
	if d.SettingsMaxVis <= 0 {
		return
	}
	line := d.getLineForIndex(d.SettingsIndex)
	if line >= d.SettingsScroll+d.SettingsMaxVis {
		d.SettingsScroll = line - d.SettingsMaxVis + 1
	}
	totalLines := 0
	for _, sec := range d.SettingsSections {
		totalLines += 2 + len(sec.Indices)
	}
	if totalLines > d.SettingsMaxVis && d.SettingsScroll > totalLines-d.SettingsMaxVis {
		d.SettingsScroll = totalLines - d.SettingsMaxVis
	}
}

func (d *Dialog) adjustScrollUp() {
	if d.SettingsMaxVis <= 0 {
		return
	}
	line := d.getLineForIndex(d.SettingsIndex)
	if line < d.SettingsScroll {
		d.SettingsScroll = line
	} else if d.SettingsIndex == 0 && d.SettingsScroll > 0 {
		d.SettingsScroll = 0
	}
	if d.SettingsScroll < 0 {
		d.SettingsScroll = 0
	}
}

func (d *Dialog) HandleMouse(ev *tcell.EventMouse) bool { return false }
func (d *Dialog) IsFocused() bool                       { return d.focused }
func (d *Dialog) SetFocused(f bool)                     { d.focused = f }
