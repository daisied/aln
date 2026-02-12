package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"editor/config"

	"github.com/gdamore/tcell/v2"
)

type Command struct {
	Name     string
	Shortcut string
	Action   func()
}

type SearchResult struct {
	Path    string
	Line    int
	Col     int
	Text    string
	Preview string
}

type scoredCommand struct {
	Command
	Score     int
	MatchIdxs []int
}

type CommandPalette struct {
	Input         string
	CursorPos     int
	Commands      []Command
	Filtered      []scoredCommand
	SearchResults []SearchResult
	Selected      int
	OnClose       func()
	OnNavigate    func(path string, line int) // Called when navigating search results
	focused       bool
	Theme         *config.ColorScheme
	scrollOff     int
	workDir       string // Working directory for search
	isSearchMode  bool   // True when input starts with %
}

func NewCommandPalette(commands []Command, theme *config.ColorScheme) *CommandPalette {
	cp := &CommandPalette{
		Commands: commands,
		focused:  true,
		Theme:    theme,
	}
	cp.updateFilter()
	return cp
}

func (cp *CommandPalette) SetWorkDir(dir string) {
	cp.workDir = dir
}

func (cp *CommandPalette) updateFilter() {
	// Check if we're in search mode
	if strings.HasPrefix(cp.Input, "%") {
		cp.isSearchMode = true
		searchQuery := strings.TrimPrefix(cp.Input, "%")
		if searchQuery == "" {
			cp.SearchResults = nil
		} else {
			cp.performSearch(searchQuery)
		}
		cp.Selected = 0
		cp.scrollOff = 0
		return
	}

	// Regular command palette mode
	cp.isSearchMode = false
	cp.SearchResults = nil

	if cp.Input == "" {
		cp.Filtered = make([]scoredCommand, 0, len(cp.Commands))
		for _, c := range cp.Commands {
			cp.Filtered = append(cp.Filtered, scoredCommand{Command: c})
		}
		cp.Selected = 0
		cp.scrollOff = 0
		return
	}

	cp.Filtered = cp.Filtered[:0]
	query := strings.ToLower(cp.Input)

	for _, c := range cp.Commands {
		score, idxs := commandFuzzyScore(c.Name, query)
		if score > 0 {
			cp.Filtered = append(cp.Filtered, scoredCommand{
				Command:   c,
				Score:     score,
				MatchIdxs: idxs,
			})
		}
	}

	// Sort by score descending
	for i := 1; i < len(cp.Filtered); i++ {
		for j := i; j > 0 && cp.Filtered[j].Score > cp.Filtered[j-1].Score; j-- {
			cp.Filtered[j], cp.Filtered[j-1] = cp.Filtered[j-1], cp.Filtered[j]
		}
	}

	cp.Selected = 0
	cp.scrollOff = 0
}

func (cp *CommandPalette) performSearch(query string) {
	cp.SearchResults = nil

	if cp.workDir == "" {
		return
	}

	// Try ripgrep first, fall back to grep
	cmd := exec.Command("rg", "--line-number", "--column", "--no-heading", "--color=never", "-i", query)
	cmd.Dir = cp.workDir

	output, err := cmd.Output()
	if err != nil {
		// Try fallback to grep if rg not found
		cmd = exec.Command("grep", "-rni", "--with-filename", "--line-number", query, ".")
		cmd.Dir = cp.workDir
		output, err = cmd.Output()
		if err != nil {
			// No matches or grep failed
			return
		}
		
		// Parse grep output (format: filepath:line:text)
		lines := strings.Split(string(output), "\n")
		results := make([]SearchResult, 0, len(lines))

		for _, line := range lines {
			if line == "" {
				continue
			}

			// Format: path:line:text
			parts := strings.SplitN(line, ":", 3)
			if len(parts) < 3 {
				continue
			}

			lineNum, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}

			text := parts[2]
			text = strings.TrimSpace(text)
			if len(text) > 100 {
				text = text[:100] + "..."
			}

			results = append(results, SearchResult{
				Path:    parts[0],
				Line:    lineNum,
				Col:     1,
				Text:    text,
				Preview: text,
			})

			if len(results) >= 1000 {
				break
			}
		}

		cp.SearchResults = results
		return
	}

	// Parse ripgrep output (format: path:line:col:text)
	lines := strings.Split(string(output), "\n")
	results := make([]SearchResult, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		// Format: path:line:col:text
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 4 {
			continue
		}

		lineNum, err1 := strconv.Atoi(parts[1])
		colNum, err2 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil {
			continue
		}

		text := parts[3]
		text = strings.TrimSpace(text)
		if len(text) > 100 {
			text = text[:100] + "..."
		}

		results = append(results, SearchResult{
			Path:    parts[0],
			Line:    lineNum,
			Col:     colNum,
			Text:    text,
			Preview: text,
		})

		if len(results) >= 1000 {
			break
		}
	}

	cp.SearchResults = results
}

func commandFuzzyScore(name, query string) (int, []int) {
	lowerName := strings.ToLower(name)
	queryRunes := []rune(query)
	nameRunes := []rune(lowerName)
	origRunes := []rune(name)

	if len(queryRunes) == 0 {
		return 0, nil
	}
	if len(queryRunes) > len(nameRunes) {
		return 0, nil
	}

	// Match all query chars in order
	idxs := make([]int, 0, len(queryRunes))
	pi := 0
	for _, qr := range queryRunes {
		found := false
		for pi < len(nameRunes) {
			if nameRunes[pi] == qr {
				idxs = append(idxs, pi)
				pi++
				found = true
				break
			}
			pi++
		}
		if !found {
			return 0, nil
		}
	}

	score := 10

	// Consecutive match bonus
	for i := 1; i < len(idxs); i++ {
		if idxs[i] == idxs[i-1]+1 {
			score += 5
		}
	}

	// Word boundary bonus
	for _, idx := range idxs {
		if idx == 0 {
			score += 10
		} else {
			prev := origRunes[idx-1]
			if prev == ' ' || prev == '_' || prev == '-' {
				score += 8
			}
			if unicode.IsLower(rune(origRunes[idx-1])) && unicode.IsUpper(rune(origRunes[idx])) {
				score += 6
			}
		}
	}

	// Prefix match bonus
	if strings.HasPrefix(lowerName, query) {
		score += 20
	}

	return score, idxs
}

func (cp *CommandPalette) Render(screen tcell.Screen, x, y, width, height int) {
	theme := cp.Theme
	if theme == nil {
		theme = config.Themes["dark"]
	}

	maxVisible := 15
	if maxVisible > height-6 {
		maxVisible = height - 6
	}
	if maxVisible < 3 {
		maxVisible = 3
	}

	dialogW := width * 60 / 100
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > width-4 {
		dialogW = width - 4
	}

	listCount := len(cp.Filtered)
	if listCount > maxVisible {
		listCount = maxVisible
	}
	dialogH := listCount + 4
	if dialogH < 5 {
		dialogH = 5
	}

	dialogX := x + (width-dialogW)/2
	dialogY := y + 2

	// Styles
	borderStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(theme.DialogFg)
	bgStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(theme.DialogFg)
	titleStyle := tcell.StyleDefault.Background(theme.StatusBarModeBg).Foreground(tcell.ColorWhite).Bold(true)
	inputStyle := tcell.StyleDefault.Background(theme.DialogInputBg).Foreground(theme.Foreground)
	itemStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(theme.DialogFg)
	selectedStyle := tcell.StyleDefault.Background(theme.Selection).Foreground(theme.Foreground)
	matchCharStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(tcell.ColorYellow).Bold(true)
	matchCharSelStyle := tcell.StyleDefault.Background(theme.Selection).Foreground(tcell.ColorYellow).Bold(true)
	countStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(theme.LineNumber)
	shortcutStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(theme.LineNumber)
	shortcutSelStyle := tcell.StyleDefault.Background(theme.Selection).Foreground(theme.LineNumber)

	// Draw background
	for dy := 0; dy < dialogH; dy++ {
		for dx := 0; dx < dialogW; dx++ {
			screen.SetContent(dialogX+dx, dialogY+dy, ' ', nil, bgStyle)
		}
	}

	// Draw border
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

	// Title
	title := " Command Palette "
	if cp.isSearchMode {
		title = " Search in Files "
	}
	titleX := dialogX + (dialogW-len(title))/2
	for i, ch := range title {
		screen.SetContent(titleX+i, dialogY, ch, nil, titleStyle)
	}

	// Input line with "> " prefix
	inputY := dialogY + 1
	inputX := dialogX + 2
	inputW := dialogW - 4

	for dx := 0; dx < inputW; dx++ {
		screen.SetContent(inputX+dx, inputY, ' ', nil, inputStyle)
	}

	// Draw "> " prefix
	screen.SetContent(inputX, inputY, '>', nil, inputStyle)
	screen.SetContent(inputX+1, inputY, ' ', nil, inputStyle)

	// Draw input text after prefix
	inputRunes := []rune(cp.Input)
	for i, ch := range inputRunes {
		if i+2 >= inputW {
			break
		}
		screen.SetContent(inputX+2+i, inputY, ch, nil, inputStyle)
	}

	// Show cursor
	cursorX := inputX + 2 + cp.CursorPos
	if cursorX < inputX+inputW {
		if cp.CursorPos < len(inputRunes) {
			screen.SetContent(cursorX, inputY, inputRunes[cp.CursorPos], nil, inputStyle.Reverse(true))
		} else {
			screen.SetContent(cursorX, inputY, ' ', nil, inputStyle.Reverse(true))
		}
	}

	// Separator line
	sepY := dialogY + 2
	for dx := 1; dx < dialogW-1; dx++ {
		screen.SetContent(dialogX+dx, sepY, '─', nil, borderStyle)
	}
	screen.SetContent(dialogX, sepY, '├', nil, borderStyle)
	screen.SetContent(dialogX+dialogW-1, sepY, '┤', nil, borderStyle)

	// Command/result count on separator
	countStr := ""
	if cp.isSearchMode {
		countStr = fmt.Sprintf(" %d results ", len(cp.SearchResults))
	} else {
		countStr = fmt.Sprintf(" %d commands ", len(cp.Filtered))
	}
	countX := dialogX + dialogW - 1 - len(countStr)
	if countX > dialogX+1 {
		for i, ch := range countStr {
			screen.SetContent(countX+i, sepY, ch, nil, countStyle)
		}
	}

	// Ensure selected is visible
	if cp.Selected < cp.scrollOff {
		cp.scrollOff = cp.Selected
	}
	if cp.Selected >= cp.scrollOff+maxVisible {
		cp.scrollOff = cp.Selected - maxVisible + 1
	}

	listY := sepY + 1

	if cp.isSearchMode {
		// Render search results
		for i := 0; i < maxVisible && i+cp.scrollOff < len(cp.SearchResults); i++ {
			idx := i + cp.scrollOff
			result := cp.SearchResults[idx]
			isSelected := idx == cp.Selected

			baseStyle := itemStyle
			if isSelected {
				baseStyle = selectedStyle
			}

			rowY := listY + i
			for dx := 1; dx < dialogW-1; dx++ {
				screen.SetContent(dialogX+dx, rowY, ' ', nil, baseStyle)
			}

			// Format: "filename:line - preview text"
			filename := filepath.Base(result.Path)
			dir := filepath.Dir(result.Path)
			if dir == "." {
				dir = ""
			}

			// Build display string
			lineStr := fmt.Sprintf(":%d", result.Line)
			displayStr := filename + lineStr

			// Add directory if space allows
			if dir != "" && len(displayStr)+len(dir)+4 < dialogW-4 {
				displayStr = filepath.Join(dir, filename) + lineStr
			}

			// Add preview
			preview := " - " + result.Preview
			maxLen := dialogW - 4
			if len(displayStr)+len(preview) > maxLen {
				// Truncate preview
				availPreview := maxLen - len(displayStr) - 4
				if availPreview > 10 {
					preview = " - " + result.Preview[:availPreview] + "..."
				} else {
					preview = ""
				}
			}
			displayStr += preview

			// Draw the result
			col := dialogX + 2
			for _, ch := range displayStr {
				if col >= dialogX+dialogW-2 {
					break
				}
				screen.SetContent(col, rowY, ch, nil, baseStyle)
				col++
			}
		}
	} else {
		// Render filtered command list
		for i := 0; i < maxVisible && i+cp.scrollOff < len(cp.Filtered); i++ {
			idx := i + cp.scrollOff
			entry := cp.Filtered[idx]
			isSelected := idx == cp.Selected

			baseStyle := itemStyle
			highlightStyle := matchCharStyle
			scStyle := shortcutStyle
			if isSelected {
				baseStyle = selectedStyle
				highlightStyle = matchCharSelStyle
				scStyle = shortcutSelStyle
			}

			rowY := listY + i
			for dx := 1; dx < dialogW-1; dx++ {
				screen.SetContent(dialogX+dx, rowY, ' ', nil, baseStyle)
			}

			// Build match index set
			matchSet := make(map[int]bool, len(entry.MatchIdxs))
			for _, mi := range entry.MatchIdxs {
				matchSet[mi] = true
			}

			// Draw command name with highlighted match chars
			nameRunes := []rune(entry.Name)
			col := dialogX + 2
			maxCol := dialogX + dialogW - 2
			// Reserve space for shortcut
			shortcutLen := 0
			if entry.Shortcut != "" {
				shortcutLen = len([]rune(entry.Shortcut)) + 2 // 2 for padding
			}
			nameMaxCol := maxCol - shortcutLen
			for ci, ch := range nameRunes {
				if col >= nameMaxCol {
					break
				}
				style := baseStyle
				if matchSet[ci] {
					style = highlightStyle
				}
				screen.SetContent(col, rowY, ch, nil, style)
				col++
			}

			// Draw shortcut right-aligned
			if entry.Shortcut != "" {
				scRunes := []rune(entry.Shortcut)
				scX := maxCol - len(scRunes)
				if scX > col {
					for i, ch := range scRunes {
						screen.SetContent(scX+i, rowY, ch, nil, scStyle)
					}
				}
			}
		}
	}
}

func (cp *CommandPalette) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		if cp.OnClose != nil {
			cp.OnClose()
		}
		return true
	case tcell.KeyEnter:
		if cp.isSearchMode {
			// Navigate to search result and close
			if cp.Selected >= 0 && cp.Selected < len(cp.SearchResults) {
				result := cp.SearchResults[cp.Selected]
				if cp.OnClose != nil {
					cp.OnClose()
				}
				if cp.OnNavigate != nil {
					cp.OnNavigate(result.Path, result.Line)
				}
			}
		} else {
			// Execute command
			if cp.Selected >= 0 && cp.Selected < len(cp.Filtered) {
				action := cp.Filtered[cp.Selected].Action
				if cp.OnClose != nil {
					cp.OnClose()
				}
				if action != nil {
					action()
				}
			}
		}
		return true
	case tcell.KeyUp:
		if cp.Selected > 0 {
			cp.Selected--
			// Navigate to the result if in search mode
			if cp.isSearchMode && cp.OnNavigate != nil && cp.Selected < len(cp.SearchResults) {
				result := cp.SearchResults[cp.Selected]
				cp.OnNavigate(result.Path, result.Line)
			}
		}
		return true
	case tcell.KeyDown:
		maxLen := len(cp.Filtered)
		if cp.isSearchMode {
			maxLen = len(cp.SearchResults)
		}
		if cp.Selected < maxLen-1 {
			cp.Selected++
			// Navigate to the result if in search mode
			if cp.isSearchMode && cp.OnNavigate != nil && cp.Selected < len(cp.SearchResults) {
				result := cp.SearchResults[cp.Selected]
				cp.OnNavigate(result.Path, result.Line)
			}
		}
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if cp.CursorPos > 0 {
			runes := []rune(cp.Input)
			cp.Input = string(runes[:cp.CursorPos-1]) + string(runes[cp.CursorPos:])
			cp.CursorPos--
			cp.updateFilter()
		}
		return true
	case tcell.KeyDelete:
		runes := []rune(cp.Input)
		if cp.CursorPos < len(runes) {
			cp.Input = string(runes[:cp.CursorPos]) + string(runes[cp.CursorPos+1:])
			cp.updateFilter()
		}
		return true
	case tcell.KeyLeft:
		if cp.CursorPos > 0 {
			cp.CursorPos--
		}
		return true
	case tcell.KeyRight:
		if cp.CursorPos < len([]rune(cp.Input)) {
			cp.CursorPos++
		}
		return true
	case tcell.KeyHome:
		cp.CursorPos = 0
		return true
	case tcell.KeyEnd:
		cp.CursorPos = len([]rune(cp.Input))
		return true
	case tcell.KeyRune:
		ch := ev.Rune()
		runes := []rune(cp.Input)
		cp.Input = string(runes[:cp.CursorPos]) + string(ch) + string(runes[cp.CursorPos:])
		cp.CursorPos++
		cp.updateFilter()
		return true
	}
	return true // absorb all keys while open
}

func (cp *CommandPalette) HandleMouse(ev *tcell.EventMouse) bool {
	return true // absorb mouse events
}

func (cp *CommandPalette) IsFocused() bool   { return cp.focused }
func (cp *CommandPalette) SetFocused(f bool) { cp.focused = f }
