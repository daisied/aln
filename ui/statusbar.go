package ui

import (
	"fmt"

	"editor/config"
	"github.com/gdamore/tcell/v2"
)

type StatusBar struct {
	Mode      string // "EDIT" or "TERM"
	Filename  string
	Line      int
	Col       int
	Language  string
	Encoding  string
	LineEnd   string
	TabInfo   string // "Tabs" or "Spaces: 4"
	Message   string // temporary status message
	Theme     *config.ColorScheme
	SelChars  int    // number of selected characters (0 = no selection)
	SelLines  int    // number of selected lines
	DiagErrors   int // number of LSP diagnostic errors
	DiagWarnings int // number of LSP diagnostic warnings
}

func NewStatusBar() *StatusBar {
	return &StatusBar{
		Mode:     "EDIT",
		Encoding: "UTF-8",
		LineEnd:  "LF",
	}
}

func (s *StatusBar) Render(screen tcell.Screen, x, y, width, height int) {
	theme := s.Theme
	if theme == nil {
		theme = config.Themes["monokai"]
	}
	
	style := tcell.StyleDefault.Background(theme.StatusBarBg).Foreground(theme.StatusBarFg)
	modeStyle := tcell.StyleDefault.Background(theme.StatusBarModeBg).Foreground(tcell.ColorWhite).Bold(true)

	// Clear the line
	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, ' ', nil, style)
	}

	col := x

	// Mode
	mode := " " + s.Mode + " "
	for _, ch := range mode {
		if col < x+width {
			screen.SetContent(col, y, ch, nil, modeStyle)
			col++
		}
	}

	// Separator
	if col < x+width {
		screen.SetContent(col, y, ' ', nil, style)
		col++
	}

	// If there's a temporary message, show that instead
	if s.Message != "" {
		for _, ch := range s.Message {
			if col < x+width {
				screen.SetContent(col, y, ch, nil, style)
				col++
			}
		}
		return
	}

	// Filename
	fname := s.Filename
	if fname == "" {
		fname = "untitled"
	}
	for _, ch := range fname {
		if col < x+width {
			screen.SetContent(col, y, ch, nil, style)
			col++
		}
	}

	// Right-aligned info
	var right string
	diagPart := ""
	if s.DiagErrors > 0 || s.DiagWarnings > 0 {
		diagPart = fmt.Sprintf("E:%d W:%d │ ", s.DiagErrors, s.DiagWarnings)
	}
	tabInfo := s.TabInfo
	if tabInfo == "" {
		tabInfo = "Spaces: 4"
	}
	if s.SelChars > 0 {
		right = fmt.Sprintf("%sSel: %d chars, %d lines │ Ln %d, Col %d │ %s │ %s │ %s │ %s ", diagPart, s.SelChars, s.SelLines, s.Line+1, s.Col+1, s.Language, s.Encoding, s.LineEnd, tabInfo)
	} else {
		right = fmt.Sprintf("%sLn %d, Col %d │ %s │ %s │ %s │ %s ", diagPart, s.Line+1, s.Col+1, s.Language, s.Encoding, s.LineEnd, tabInfo)
	}
	rightRunes := []rune(right)
	rightStart := x + width - len(rightRunes)
	if rightStart > col+2 {
		// Render diagnostic counts with colors if present
		diagRuneLen := len([]rune(diagPart))
		for i, ch := range rightRunes {
			st := style
			if i < diagRuneLen && s.DiagErrors > 0 && i < 2+len(fmt.Sprintf("%d", s.DiagErrors)) {
				st = style.Foreground(tcell.ColorRed)
			} else if i < diagRuneLen && i >= diagRuneLen-2-len(fmt.Sprintf("%d", s.DiagWarnings))-2 && i < diagRuneLen-2 {
				st = style.Foreground(tcell.ColorYellow)
			}
			screen.SetContent(rightStart+i, y, ch, nil, st)
		}
	}
}

func (s *StatusBar) HandleKey(ev *tcell.EventKey) bool   { return false }
func (s *StatusBar) HandleMouse(ev *tcell.EventMouse) bool { return false }
func (s *StatusBar) IsFocused() bool                      { return false }
func (s *StatusBar) SetFocused(f bool)                    {}
