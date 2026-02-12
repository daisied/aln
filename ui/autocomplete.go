package ui

import (
	"editor/config"

	"github.com/gdamore/tcell/v2"
)

type CompletionItem struct {
	Label      string
	Detail     string
	InsertText string
	Kind       int
}

type Autocomplete struct {
	Items    []CompletionItem
	Selected int
	Visible  bool
	X, Y     int // screen position to render at
	OnSelect func(item CompletionItem)
	OnClose  func()
	Theme    *config.ColorScheme
}

func NewAutocomplete(items []CompletionItem, x, y int, theme *config.ColorScheme) *Autocomplete {
	return &Autocomplete{
		Items:   items,
		Visible: len(items) > 0,
		X:       x,
		Y:       y,
		Theme:   theme,
	}
}

func (a *Autocomplete) Render(screen tcell.Screen, x, y, width, height int) {
	if !a.Visible || len(a.Items) == 0 {
		return
	}

	// Calculate popup dimensions
	maxWidth := 40
	for _, item := range a.Items {
		w := len(item.Label) + 4
		if item.Detail != "" {
			w += len(item.Detail) + 1
		}
		if w > maxWidth {
			maxWidth = w
		}
	}
	if maxWidth > 60 {
		maxWidth = 60
	}

	maxVisible := 10
	if len(a.Items) < maxVisible {
		maxVisible = len(a.Items)
	}

	// Determine render position
	posX := a.X
	posY := a.Y + 1 // below cursor

	// If popup would go off screen bottom, show above cursor
	if posY+maxVisible > height {
		posY = a.Y - maxVisible
	}
	if posX+maxWidth > width {
		posX = width - maxWidth
	}
	if posX < 0 {
		posX = 0
	}

	theme := a.Theme
	if theme == nil {
		theme = config.Themes["monokai"]
	}

	bgStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(theme.DialogFg)
	selStyle := tcell.StyleDefault.Background(theme.Selection).Foreground(theme.Foreground)
	detailStyle := tcell.StyleDefault.Background(theme.DialogBg).Foreground(theme.LineNumber)

	// Scroll offset
	scrollOff := 0
	if a.Selected >= scrollOff+maxVisible {
		scrollOff = a.Selected - maxVisible + 1
	}
	if a.Selected < scrollOff {
		scrollOff = a.Selected
	}

	for i := 0; i < maxVisible; i++ {
		idx := scrollOff + i
		if idx >= len(a.Items) {
			break
		}
		item := a.Items[idx]
		style := bgStyle
		if idx == a.Selected {
			style = selStyle
		}

		// Clear the row
		for cx := posX; cx < posX+maxWidth && cx < width; cx++ {
			screen.SetContent(cx, posY+i, ' ', nil, style)
		}

		// Draw kind icon
		kindChar := kindIcon(item.Kind)
		screen.SetContent(posX, posY+i, kindChar, nil, style)

		// Draw label
		col := posX + 2
		for _, ch := range item.Label {
			if col < posX+maxWidth && col < width {
				screen.SetContent(col, posY+i, ch, nil, style)
				col++
			}
		}

		// Draw detail (dimmer)
		if item.Detail != "" {
			col++
			dStyle := detailStyle
			if idx == a.Selected {
				dStyle = selStyle
			}
			for _, ch := range item.Detail {
				if col < posX+maxWidth && col < width {
					screen.SetContent(col, posY+i, ch, nil, dStyle)
					col++
				}
			}
		}
	}
}

func kindIcon(kind int) rune {
	switch kind {
	case 1:
		return '◆' // Text
	case 2:
		return 'ƒ' // Method
	case 3:
		return 'ƒ' // Function
	case 4:
		return '⊕' // Constructor
	case 5:
		return '◇' // Field
	case 6:
		return '▸' // Variable
	case 7:
		return '◻' // Class
	case 8:
		return '◻' // Interface
	case 9:
		return '▪' // Module
	case 10:
		return '◇' // Property
	case 13:
		return 'E' // Enum
	case 14:
		return 'K' // Keyword
	case 15:
		return '⋯' // Snippet
	default:
		return '·'
	}
}

func (a *Autocomplete) HandleKey(ev *tcell.EventKey) bool {
	if !a.Visible {
		return false
	}

	switch ev.Key() {
	case tcell.KeyUp:
		if a.Selected > 0 {
			a.Selected--
		}
		return true
	case tcell.KeyDown:
		if a.Selected < len(a.Items)-1 {
			a.Selected++
		}
		return true
	case tcell.KeyEnter, tcell.KeyTab:
		if a.Selected >= 0 && a.Selected < len(a.Items) && a.OnSelect != nil {
			a.OnSelect(a.Items[a.Selected])
		}
		a.Visible = false
		return true
	case tcell.KeyEscape:
		a.Visible = false
		if a.OnClose != nil {
			a.OnClose()
		}
		return true
	}
	return false
}

func (a *Autocomplete) HandleMouse(ev *tcell.EventMouse) bool { return false }
func (a *Autocomplete) IsFocused() bool                      { return a.Visible }
func (a *Autocomplete) SetFocused(f bool)                    { a.Visible = f }
