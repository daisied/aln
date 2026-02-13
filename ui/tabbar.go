package ui

import (
	"path/filepath"

	"editor/config"
	"github.com/gdamore/tcell/v2"
)

type Tab struct {
	Title              string
	Path               string
	Modified           bool
	ExternallyModified bool // File changed externally while buffer has unsaved changes
	Preview            bool // Preview tab (italic title, replaced on next file open from tree)
}

type TabBar struct {
	Tabs           []Tab
	Active         int
	scrollOff      int
	focused        bool
	x, y, w        int // layout coords set on render
	mouseX, mouseY int // current mouse position for hover effects

	// Mouse press tracking for proper click handling
	mousePressX, mousePressY int
	mousePressed             bool

	Theme *config.ColorScheme

	// Callbacks
	OnSwitch func(index int)
	OnClose  func(index int)
}

func NewTabBar() *TabBar {
	return &TabBar{mouseX: -1, mouseY: -1}
}

func (tb *TabBar) tabTitle(tab Tab) string {
	title := tab.Title
	if tab.ExternallyModified {
		title = "!" + title
	} else if tab.Modified {
		title = "*" + title
	}
	return title
}

func (tb *TabBar) tabWidthAt(index int) int {
	if index < 0 || index >= len(tb.Tabs) {
		return 0
	}
	// space + title + space + x + space
	w := 1 + len([]rune(tb.tabTitle(tb.Tabs[index]))) + 1 + 1 + 1
	if index < len(tb.Tabs)-1 {
		w++ // separator
	}
	return w
}

func (tb *TabBar) clampScroll() {
	if len(tb.Tabs) == 0 {
		tb.scrollOff = 0
		return
	}
	if tb.scrollOff < 0 {
		tb.scrollOff = 0
	}
	maxOff := len(tb.Tabs) - 1
	if tb.scrollOff > maxOff {
		tb.scrollOff = maxOff
	}
}

func (tb *TabBar) visibleLast(width int) int {
	if width <= 0 || len(tb.Tabs) == 0 {
		return tb.scrollOff - 1
	}
	remaining := width
	last := tb.scrollOff - 1
	for i := tb.scrollOff; i < len(tb.Tabs); i++ {
		w := tb.tabWidthAt(i)
		if w > remaining {
			break
		}
		remaining -= w
		last = i
	}
	return last
}

func (tb *TabBar) ensureActiveVisible(width int) {
	tb.clampScroll()
	if len(tb.Tabs) == 0 || width <= 0 {
		return
	}
	if tb.Active < 0 {
		tb.Active = 0
	}
	if tb.Active >= len(tb.Tabs) {
		tb.Active = len(tb.Tabs) - 1
	}
	if tb.Active < tb.scrollOff {
		tb.scrollOff = tb.Active
	}
	for {
		last := tb.visibleLast(width)
		if tb.Active <= last || tb.scrollOff >= tb.Active {
			break
		}
		tb.scrollOff++
	}
	tb.clampScroll()
}

func (tb *TabBar) scrollBy(delta int) {
	tb.scrollOff += delta
	tb.clampScroll()
}

func (tb *TabBar) AddTab(path string, modified bool) {
	title := filepath.Base(path)
	if title == "." || title == "" {
		title = "untitled"
	}
	// Check if tab already exists
	for i, tab := range tb.Tabs {
		if tab.Path == path {
			tb.Active = i
			return
		}
	}
	tb.Tabs = append(tb.Tabs, Tab{Title: title, Path: path, Modified: modified})
	tb.Active = len(tb.Tabs) - 1
	tb.ensureActiveVisible(tb.w)
}

func (tb *TabBar) RemoveTab(index int) {
	if index < 0 || index >= len(tb.Tabs) {
		return
	}
	tb.Tabs = append(tb.Tabs[:index], tb.Tabs[index+1:]...)
	if index < tb.scrollOff {
		tb.scrollOff--
	}
	if tb.Active >= len(tb.Tabs) {
		tb.Active = len(tb.Tabs) - 1
	}
	if tb.Active < 0 {
		tb.Active = 0
	}
	tb.clampScroll()
}

func (tb *TabBar) SetModified(index int, modified bool) {
	if index >= 0 && index < len(tb.Tabs) {
		tb.Tabs[index].Modified = modified
	}
}

func (tb *TabBar) SetExternallyModified(index int, externallyModified bool) {
	if index >= 0 && index < len(tb.Tabs) {
		tb.Tabs[index].ExternallyModified = externallyModified
	}
}

func (tb *TabBar) Render(screen tcell.Screen, x, y, width, height int) {
	tb.x, tb.y, tb.w = x, y, width
	tb.ensureActiveVisible(width)

	theme := tb.Theme
	if theme == nil {
		theme = config.Themes["monokai"]
	}

	// Tab bar background uses status bar color for unified look, or specific TabBarBg if defined
	tabBgStyle := tcell.StyleDefault.Background(theme.TabBarBg).Foreground(theme.TabBarFg)

	// Active tab stands out with its own background
	activeBg := tcell.StyleDefault.Background(theme.TabBarActiveBg).Foreground(theme.TabBarActiveFg).Bold(true)

	// Inactive tabs use the bar background
	inactiveBg := tcell.StyleDefault.Background(theme.TabBarBg).Foreground(theme.TabBarFg)

	// Close button: two states only - normal (dim) and hover (bright)
	closeNormalStyle := tcell.StyleDefault.Background(theme.TabBarBg).Foreground(theme.TabBarFg)
	closeActiveNormalStyle := tcell.StyleDefault.Background(theme.TabBarActiveBg).Foreground(theme.TabBarActiveFg)

	// Fill background
	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, ' ', nil, tabBgStyle)
	}

	col := x
	for i := tb.scrollOff; i < len(tb.Tabs); i++ {
		tab := tb.Tabs[i]
		if col >= x+width {
			break
		}

		title := tb.tabTitle(tab)

		style := inactiveBg
		closeStyle := closeNormalStyle
		if i == tb.Active {
			style = activeBg
			closeStyle = closeActiveNormalStyle
		} else {
			// Check for hover on inactive tabs
			tabW := tb.tabWidthAt(i)

			if tb.mouseY == y && tb.mouseX >= col && tb.mouseX < col+tabW {
				// Use lighter background for hover
				hoverColor := theme.StatusBarBg.TrueColor().Hex() + 0x101010
				style = style.Background(tcell.NewHexColor(int32(hoverColor)))
				closeStyle = closeStyle.Background(tcell.NewHexColor(int32(hoverColor)))
			}
		}
		// Preview tabs shown in italic
		if tab.Preview {
			style = style.Italic(true)
		}

		// Leading space
		if col < x+width {
			screen.SetContent(col, y, ' ', nil, style)
			col++
		}

		// Title
		for _, ch := range title {
			if col >= x+width {
				break
			}
			screen.SetContent(col, y, ch, nil, style)
			col++
		}

		// Space before close button
		if col < x+width {
			screen.SetContent(col, y, ' ', nil, style)
			col++
		}

		// Close button - hover only shows bright red, no glow on active
		if col < x+width {
			isHovering := tb.mouseY == y && tb.mouseX == col
			if isHovering {
				// Use the current tab's background color (active, inactive, or inactive-hover)
				// combined with red foreground
				// Decompose returns (fg, bg, attrs)
				_, bg, _ := style.Decompose()
				closeStyle = tcell.StyleDefault.Background(bg).Foreground(tcell.ColorRed).Bold(true)
			}
			screen.SetContent(col, y, 'x', nil, closeStyle)
			col++
		}

		// Trailing space
		if col < x+width {
			screen.SetContent(col, y, ' ', nil, style)
			col++
		}

		// Separator
		if col < x+width && i < len(tb.Tabs)-1 {
			screen.SetContent(col, y, '│', nil, tabBgStyle)
			col++
		}
	}
}

func (tb *TabBar) HandleKey(ev *tcell.EventKey) bool {
	return false
}

func (tb *TabBar) HandleMouse(ev *tcell.EventMouse) bool {
	mx, my := ev.Position()
	btn := ev.Buttons()

	if my != tb.y || mx < tb.x || mx >= tb.x+tb.w {
		// Mouse outside tab bar - reset position to clear hover effects
		tb.mouseX, tb.mouseY = -1, -1
		tb.mousePressed = false
		return false
	}

	// Update mouse position for hover effects
	tb.mouseX, tb.mouseY = mx, my

	// Mouse wheel scrolls hidden tabs horizontally.
	switch btn {
	case tcell.WheelUp, tcell.WheelLeft:
		tb.scrollBy(-1)
		return true
	case tcell.WheelDown, tcell.WheelRight:
		tb.scrollBy(1)
		return true
	}

	// Handle mouse press (Button1 down)
	if btn == tcell.Button1 {
		if !tb.mousePressed {
			// Record where the mouse was pressed
			tb.mousePressX, tb.mousePressY = mx, my
			tb.mousePressed = true
		}
		return true
	}

	// Handle mouse release (ButtonNone after Button1)
	if btn == tcell.ButtonNone && tb.mousePressed {
		tb.mousePressed = false

		// Only trigger click if release happened at same position as press
		if mx == tb.mousePressX && my == tb.mousePressY {
			// Determine which tab was clicked
			col := tb.x
			for i := tb.scrollOff; i < len(tb.Tabs); i++ {
				title := tb.tabTitle(tb.Tabs[i])
				tabWidth := tb.tabWidthAt(i)
				if col >= tb.x+tb.w {
					break
				}

				if mx >= col && mx < col+tabWidth {
					// Check if close button was clicked
					closeX := col + 1 + len([]rune(title)) + 1
					if mx == closeX {
						if tb.OnClose != nil {
							tb.OnClose(i)
						}
					} else {
						if tb.OnSwitch != nil {
							tb.OnSwitch(i)
						}
					}
					return true
				}
				col += tabWidth
			}
		}
		return true
	}

	// Mouse is over tab bar but not clicking
	return true
}

func (tb *TabBar) IsFocused() bool   { return tb.focused }
func (tb *TabBar) SetFocused(f bool) { tb.focused = f }
