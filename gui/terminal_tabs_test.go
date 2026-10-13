package gui

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestNativeTerminalTabsPaintOnlyStripChromeAndKeepActionTargetsSeparate(t *testing.T) {
	tabs := widget.NewTerminalTabs()
	var added int
	var menu widget.TerminalTabID
	tabs.SetActions(func() { added++ }, func(id widget.TerminalTabID) { menu = id })
	a, _ := tabs.Add("one")
	b, _ := tabs.Add("two")
	app, sb := styledApp(t, tabs, 40, 8, NativeStyle())
	var id tui.NodeID
	onLoop(t, app, func() {
		id = tabs.NodeID()
		if err := tabs.Select(b); err != nil {
			t.Error(err)
		}
	})
	waitUntil(t, "terminal strip native placement", func() bool {
		for _, p := range sb.placements() {
			if p.Node == id {
				return true
			}
		}
		return false
	})
	p := placementOf(t, sb, id)
	if p.Scope != tui.ScopeChrome {
		t.Fatalf("native painter covers child content: %v", p.Scope)
	}
	rc := paintOn(t, app, sb, p)
	rounded, lines := 0, 0
	for _, call := range rc.Calls {
		if call.Op == "FillRRect" {
			rounded++
		}
		if call.Op == "PaintCells" {
			lines++
			if call.Cells.H != 1 {
				t.Error("strip painter repaints terminal content")
			}
		}
	}
	if rounded != 4 || lines != 1 {
		t.Fatalf("want two pages and two action targets; rounded=%d cell rows=%d", rounded, lines)
	}
	onLoop(t, app, func() {
		tabs.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 13})
		tabs.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 17})
		if added != 1 || menu != b || tabs.Active() != b {
			t.Error("native strip action hit targets overlap pages")
		}
		if _, err := tabs.Remove(a); err != nil {
			t.Error(err)
		}
		if _, err := tabs.Remove(b); err != nil {
			t.Error(err)
		}
		if tabs.ActiveTabIndex() != -1 {
			t.Error("empty strip retains an active presentation index")
		}
	})
}
