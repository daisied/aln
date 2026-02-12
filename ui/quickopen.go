package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"editor/config"

	"github.com/gdamore/tcell/v2"
)

type scoredFile struct {
	Path       string
	Score      int
	MatchIdxs  []int // indices of matched chars in Path for highlighting
}

type QuickOpen struct {
	Input     string
	CursorPos int
	Files     []string
	Filtered  []scoredFile
	Selected  int
	OnSelect  func(path string)
	OnClose   func()
	focused   bool
	Theme     *config.ColorScheme
	scrollOff int // scroll offset for the filtered list
}

func NewQuickOpen(files []string, theme *config.ColorScheme) *QuickOpen {
	qo := &QuickOpen{
		Files:   files,
		focused: true,
		Theme:   theme,
	}
	qo.updateFilter()
	return qo
}

// CollectFiles walks the directory tree and returns relative paths, skipping ignored dirs.
func CollectFiles(root string) []string {
	var files []string
	ignore := map[string]bool{
		".git": true, "node_modules": true, ".next": true,
		"__pycache__": true, "vendor": true, "dist": true, "build": true,
		".idea": true, ".vscode": true, "target": true,
	}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if ignore[base] || (strings.HasPrefix(base, ".") && base != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip hidden files
		if strings.HasPrefix(base, ".") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files
}

func (qo *QuickOpen) updateFilter() {
	if qo.Input == "" {
		// Show all files (up to a reasonable limit), no scoring needed
		qo.Filtered = make([]scoredFile, 0, len(qo.Files))
		for _, f := range qo.Files {
			qo.Filtered = append(qo.Filtered, scoredFile{Path: f})
		}
		qo.Selected = 0
		qo.scrollOff = 0
		return
	}

	qo.Filtered = qo.Filtered[:0]
	query := strings.ToLower(qo.Input)

	for _, f := range qo.Files {
		score, idxs := fuzzyScore(f, query)
		if score > 0 {
			qo.Filtered = append(qo.Filtered, scoredFile{
				Path:      f,
				Score:     score,
				MatchIdxs: idxs,
			})
		}
	}

	// Sort by score descending (simple insertion sort — fast enough for interactive use)
	for i := 1; i < len(qo.Filtered); i++ {
		for j := i; j > 0 && qo.Filtered[j].Score > qo.Filtered[j-1].Score; j-- {
			qo.Filtered[j], qo.Filtered[j-1] = qo.Filtered[j-1], qo.Filtered[j]
		}
	}

	qo.Selected = 0
	qo.scrollOff = 0
}

// fuzzyScore computes a fuzzy match score of path against query.
// Returns 0 if no match. Higher is better.
func fuzzyScore(path, query string) (int, []int) {
	lowerPath := strings.ToLower(path)
	queryRunes := []rune(query)
	pathRunes := []rune(lowerPath)
	origRunes := []rune(path)

	if len(queryRunes) == 0 {
		return 0, nil
	}
	if len(queryRunes) > len(pathRunes) {
		return 0, nil
	}

	// Find filename start
	filenameStart := 0
	for i := len(origRunes) - 1; i >= 0; i-- {
		if origRunes[i] == '/' || origRunes[i] == filepath.Separator {
			filenameStart = i + 1
			break
		}
	}

	// Try to match all query chars in order
	idxs := make([]int, 0, len(queryRunes))
	pi := 0
	for _, qr := range queryRunes {
		found := false
		for pi < len(pathRunes) {
			if pathRunes[pi] == qr {
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

	// Base score for matching
	score := 10

	// Bonus for consecutive matches
	for i := 1; i < len(idxs); i++ {
		if idxs[i] == idxs[i-1]+1 {
			score += 5
		}
	}

	// Bonus for matches at segment boundaries (after /, _, -, .)
	for _, idx := range idxs {
		if idx == 0 || idx == filenameStart {
			score += 10
		} else {
			prev := origRunes[idx-1]
			if prev == '/' || prev == '_' || prev == '-' || prev == '.' || prev == filepath.Separator {
				score += 8
			}
			// Bonus for camelCase boundary
			if idx > 0 && unicode.IsLower(rune(origRunes[idx-1])) && unicode.IsUpper(rune(origRunes[idx])) {
				score += 6
			}
		}
	}

	// Bonus for matches in filename portion (not directory path)
	filenameMatches := 0
	for _, idx := range idxs {
		if idx >= filenameStart {
			filenameMatches++
		}
	}
	score += filenameMatches * 3

	// Bonus for shorter paths (prefer less nested files)
	depth := strings.Count(path, string(filepath.Separator))
	if depth == 0 {
		depth = strings.Count(path, "/")
	}
	score -= depth

	// Bonus for exact filename prefix match
	filename := lowerPath[filenameStart:]
	if strings.HasPrefix(filename, query) {
		score += 20
	}

	return score, idxs
}

func (qo *QuickOpen) Render(screen tcell.Screen, x, y, width, height int) {
	theme := qo.Theme
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

	listCount := len(qo.Filtered)
	if listCount > maxVisible {
		listCount = maxVisible
	}
	dialogH := listCount + 4 // border top + input + border separator + items + border bottom
	if dialogH < 5 {
		dialogH = 5
	}

	dialogX := x + (width-dialogW)/2
	dialogY := y + 2 // near top of screen

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
	title := " Open File "
	titleX := dialogX + (dialogW-len(title))/2
	for i, ch := range title {
		screen.SetContent(titleX+i, dialogY, ch, nil, titleStyle)
	}

	// Input line
	inputY := dialogY + 1
	inputX := dialogX + 2
	inputW := dialogW - 4

	// Clear input line
	for dx := 0; dx < inputW; dx++ {
		screen.SetContent(inputX+dx, inputY, ' ', nil, inputStyle)
	}

	// Draw input text
	inputRunes := []rune(qo.Input)
	for i, ch := range inputRunes {
		if i >= inputW {
			break
		}
		screen.SetContent(inputX+i, inputY, ch, nil, inputStyle)
	}

	// Show cursor
	cursorX := inputX + qo.CursorPos
	if cursorX < inputX+inputW {
		if qo.CursorPos < len(inputRunes) {
			screen.SetContent(cursorX, inputY, inputRunes[qo.CursorPos], nil, inputStyle.Reverse(true))
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

	// File count on separator
	countStr := fmt.Sprintf(" %d files ", len(qo.Filtered))
	countX := dialogX + dialogW - 1 - len(countStr)
	if countX > dialogX+1 {
		for i, ch := range countStr {
			screen.SetContent(countX+i, sepY, ch, nil, countStyle)
		}
	}

	// Ensure selected is visible
	if qo.Selected < qo.scrollOff {
		qo.scrollOff = qo.Selected
	}
	if qo.Selected >= qo.scrollOff+maxVisible {
		qo.scrollOff = qo.Selected - maxVisible + 1
	}

	// Render filtered list
	listY := sepY + 1
	for i := 0; i < maxVisible && i+qo.scrollOff < len(qo.Filtered); i++ {
		idx := i + qo.scrollOff
		entry := qo.Filtered[idx]
		isSelected := idx == qo.Selected

		baseStyle := itemStyle
		highlightStyle := matchCharStyle
		if isSelected {
			baseStyle = selectedStyle
			highlightStyle = matchCharSelStyle
		}

		// Clear row
		rowY := listY + i
		for dx := 1; dx < dialogW-1; dx++ {
			screen.SetContent(dialogX+dx, rowY, ' ', nil, baseStyle)
		}

		// Build match index set for fast lookup
		matchSet := make(map[int]bool, len(entry.MatchIdxs))
		for _, mi := range entry.MatchIdxs {
			matchSet[mi] = true
		}

		// Draw path with highlighted match chars
		pathRunes := []rune(entry.Path)
		col := dialogX + 2
		maxCol := dialogX + dialogW - 2
		for ci, ch := range pathRunes {
			if col >= maxCol {
				break
			}
			style := baseStyle
			if matchSet[ci] {
				style = highlightStyle
			}
			screen.SetContent(col, rowY, ch, nil, style)
			col++
		}
	}
}

func (qo *QuickOpen) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		if qo.OnClose != nil {
			qo.OnClose()
		}
		return true
	case tcell.KeyEnter:
		if qo.Selected >= 0 && qo.Selected < len(qo.Filtered) {
			if qo.OnSelect != nil {
				qo.OnSelect(qo.Filtered[qo.Selected].Path)
			}
		}
		return true
	case tcell.KeyUp:
		if qo.Selected > 0 {
			qo.Selected--
		}
		return true
	case tcell.KeyDown:
		if qo.Selected < len(qo.Filtered)-1 {
			qo.Selected++
		}
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if qo.CursorPos > 0 {
			runes := []rune(qo.Input)
			qo.Input = string(runes[:qo.CursorPos-1]) + string(runes[qo.CursorPos:])
			qo.CursorPos--
			qo.updateFilter()
		}
		return true
	case tcell.KeyDelete:
		runes := []rune(qo.Input)
		if qo.CursorPos < len(runes) {
			qo.Input = string(runes[:qo.CursorPos]) + string(runes[qo.CursorPos+1:])
			qo.updateFilter()
		}
		return true
	case tcell.KeyLeft:
		if qo.CursorPos > 0 {
			qo.CursorPos--
		}
		return true
	case tcell.KeyRight:
		if qo.CursorPos < len([]rune(qo.Input)) {
			qo.CursorPos++
		}
		return true
	case tcell.KeyHome:
		qo.CursorPos = 0
		return true
	case tcell.KeyEnd:
		qo.CursorPos = len([]rune(qo.Input))
		return true
	case tcell.KeyRune:
		ch := ev.Rune()
		runes := []rune(qo.Input)
		qo.Input = string(runes[:qo.CursorPos]) + string(ch) + string(runes[qo.CursorPos:])
		qo.CursorPos++
		qo.updateFilter()
		return true
	}
	return true // absorb all keys while open
}

func (qo *QuickOpen) HandleMouse(ev *tcell.EventMouse) bool {
	return true // absorb mouse events
}

func (qo *QuickOpen) IsFocused() bool    { return qo.focused }
func (qo *QuickOpen) SetFocused(f bool)  { qo.focused = f }
