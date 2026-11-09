package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// Every tab strip's menu opens on the row right under the strip: its ≡, its + and a right press
// on it, for each of core's strip kinds (a TabStrip, a Tabs; TerminalTabs' is the terminal's test).
// The menu's panel is placed at row 1, its border drawn there and its first row under it, with no
// empty row between the panel and the strip.
func TestEveryStripsMenuOpensRightUnderTheStrip(t *testing.T) {
	rows := func(label string) []MenuItemModel {
		return []MenuItemModel{NewCommand("row", label, TabStripMenuAction{ID: "row"})}
	}
	type strip struct {
		root  tui.Component
		core  *StripCore
		blank bool // it is wider than its cells, so a right press on its blank opens the menu
	}
	kinds := map[string]func() strip{
		"TabStrip": func() strip {
			s := NewTabStrip()
			s.SetTabs([]StripTab{{"a", "alpha"}, {"b", "beta"}})
			s.SetActive("a")
			s.SetMenu(func(string) []MenuItemModel { return rows("Menu") })
			s.SetAddMenu(func() []MenuItemModel { return rows("Add") })
			return strip{s, &s.core, false} // as wide as its cells: no blank
		},
		"Tabs": func() strip {
			s := NewTabs(WithTab("alpha", NewText("one")), WithTab("beta", NewText("two")))
			s.SetMenu(func(int) []MenuItemModel { return rows("Menu") })
			return strip{s, &s.strip, true}
		},
	}
	for name, build := range kinds {
		t.Run(name, func(t *testing.T) {
			k := build()
			h := widgettest.StartApp(t, NewOverlayHost(k.root), 40, 8)
			defer h.Stop()
			m := widgettest.StripMenu{Cells: map[kit.StripKind]int{}, Tab: -1, Blank: k.blank,
				Open: func() bool { return k.core.MenuOpen != nil }}
			h.OnLoop(func() { m.Cells, m.Tab = stripCellsOf(k.core) })
			widgettest.MenuOpensUnderTheStrip(t, h, m, "Menu", "Add")
		})
	}
}

// stripCellsOf is where each kind of stretch first starts on s's row, and the first tab's start
// (-1 for none), as MenuOpensUnderTheStrip presses them.
func stripCellsOf(s *StripCore) (map[kit.StripKind]int, int) {
	cells, tab := map[kit.StripKind]int{}, -1
	for _, c := range s.Row() {
		if _, ok := cells[c.Kind]; !ok {
			cells[c.Kind] = c.X
		}
		if c.Kind == kit.CellTab && tab < 0 {
			tab = c.X
		}
	}
	return cells, tab
}

// A Tabs bar's menu row runs with the index of the tab it was opened for; a row with no Run does
// nothing; and with the menu unset (SetMenu(nil)) the ≡ goes and a right press is not taken.
func TestATabsMenuRowRunsWithItsTabsIndex(t *testing.T) {
	tabs := NewTabs(WithTab("alpha", NewText("one")), WithTab("beta", NewText("two")))
	ran := make(chan int, 2)
	tabs.SetMenu(func(int) []MenuItemModel {
		return []MenuItemModel{
			NewCommand("go", "Go", TabsMenuAction{ID: "go", Run: func(i int) { ran <- i }}),
			NewCommand("idle", "Idle", TabsMenuAction{ID: "idle"}),
		}
	})
	h := widgettest.StartApp(t, NewOverlayHost(tabs), 40, 8)
	defer h.Stop()
	beta := -1
	h.OnLoop(func() {
		for _, c := range tabs.strip.Row() {
			if c.Kind == kit.CellTab && c.id == "1" {
				beta = c.X
			}
		}
	})
	open := func() {
		t.Helper()
		if err := h.TB.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: beta + 1, Y: 0}); err != nil {
			t.Fatal(err)
		}
		if !widgettest.Wait(h, func() bool { return tabs.strip.MenuOpen != nil }) {
			t.Fatalf("a right press on beta opened no menu:\n%s", h.TB.String())
		}
	}
	open()
	if err := h.TB.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if !widgettest.Wait(h, func() bool { return len(ran) == 1 }) {
		t.Fatal("Go ran nothing")
	}
	if i := <-ran; i != 1 {
		t.Fatalf("Go ran with tab %d, want beta's index, 1", i)
	}

	open()
	if err := h.TB.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	h.Sync()
	h.Sync()
	if len(ran) != 0 {
		t.Fatalf("Idle, with no Run, ran %d", <-ran)
	}

	h.OnLoop(func() {
		tabs.strip.CloseMenu()
		tabs.SetMenu(nil)
	})
	var taken bool
	h.OnLoop(func() {
		taken = tabs.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: beta + 1, Y: 0})
	})
	if taken {
		t.Fatal("with no menu, a right press on a tab was taken")
	}
	if !widgettest.Wait(h, func() bool { return !strings.Contains(h.TB.String(), "≡") }) {
		t.Fatalf("with no menu, ≡ still shows:\n%s", h.TB.String())
	}
}
