package ui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestTabBarRenderKeepsActiveTabVisible(t *testing.T) {
	tb := NewTabBar()
	for i := 0; i < 14; i++ {
		tb.AddTab(fmt.Sprintf("file-%d.txt", i), false)
	}
	tb.Active = len(tb.Tabs) - 1

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen init failed: %v", err)
	}
	defer screen.Fini()

	tb.Render(screen, 0, 0, 32, 1)

	if tb.scrollOff <= 0 {
		t.Fatalf("expected tab bar to scroll for active off-screen tab, got scrollOff=%d", tb.scrollOff)
	}
	if tb.Active < tb.scrollOff {
		t.Fatalf("active tab should stay visible: active=%d scrollOff=%d", tb.Active, tb.scrollOff)
	}
}

func TestTabBarWheelScrollsHiddenTabs(t *testing.T) {
	tb := NewTabBar()
	for i := 0; i < 10; i++ {
		tb.AddTab(fmt.Sprintf("tab-%d.txt", i), false)
	}
	tb.Active = 0

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen init failed: %v", err)
	}
	defer screen.Fini()

	tb.Render(screen, 0, 0, 28, 1)
	if tb.scrollOff != 0 {
		t.Fatalf("expected initial scrollOff=0, got %d", tb.scrollOff)
	}

	tb.HandleMouse(tcell.NewEventMouse(5, 0, tcell.WheelDown, tcell.ModNone))
	if tb.scrollOff == 0 {
		t.Fatalf("expected wheel down to increase scrollOff")
	}

	tb.HandleMouse(tcell.NewEventMouse(5, 0, tcell.WheelUp, tcell.ModNone))
	if tb.scrollOff != 0 {
		t.Fatalf("expected wheel up to restore scrollOff=0, got %d", tb.scrollOff)
	}
}
