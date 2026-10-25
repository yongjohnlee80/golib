package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// Every tab strip's menu opens on the row right under the strip: its ≡, its + and a right press
// on it, for each strip kind (a TabStrip, a TerminalTabs, a Tabs). The menu's panel is placed at
// row 1, its border drawn there and its first row under it, with no empty row between the panel
// and the strip.
func TestEveryStripsMenuOpensRightUnderTheStrip(t *testing.T) {
	rows := func(label string) []MenuItemModel {
		return []MenuItemModel{NewCommand("row", label, TabStripMenuAction{ID: "row"})}
	}
	type strip struct {
		root  tui.Component
		core  *tabStrip
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
		"TerminalTabs": func() strip {
			s := NewTerminalTabs()
			s.SetMenu(func(TerminalTabID) []MenuItemModel { return rows("Menu") })
			s.SetAddMenu(func() []MenuItemModel { return rows("Add") })
			return strip{s, &s.strip, true}
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
			h := startAppInternal(t, NewOverlayHost(k.root), 40, 8)
			defer h.stopInternal()
			cells := map[stripKind]int{}
			tab := -1
			h.onLoopInternal(func() {
				for _, c := range k.core.row() {
					if _, ok := cells[c.kind]; !ok {
						cells[c.kind] = c.x
					}
					if c.kind == cellTab && tab < 0 {
						tab = c.x
					}
				}
			})
			opens := func(how string, at tui.Point, button tui.MouseButton, label string) {
				t.Helper()
				if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: button, X: at.X, Y: at.Y}); err != nil {
					t.Fatal(err)
				}
				drawn := waitInternal(h, func() bool {
					lines := strings.Split(h.tb.String(), "\n")
					return k.core.menuOpen != nil && len(lines) > 2 && strings.Contains(lines[1], "┌") && strings.Contains(lines[2], label)
				})
				var top int
				h.onLoopInternal(func() {
					if k.core.menuOpen != nil {
						top = k.core.menuOpen.rect.Y
					}
				})
				if !drawn || top != 1 {
					t.Fatalf("%s: the menu's panel is at row %d, want 1, right under the strip (drawn there: %v):\n%s",
						how, top, drawn, h.tb.String())
				}
				if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}); err != nil {
					t.Fatal(err)
				}
				if !waitInternal(h, func() bool { return k.core.menuOpen == nil }) {
					t.Fatalf("%s: Escape left the menu open", how)
				}
			}
			menu, ok := cells[cellMenu]
			if !ok {
				t.Fatalf("no ≡ on the strip: %v", k.core.row())
			}
			opens("≡", tui.Point{X: menu + 1}, tui.MouseLeft, "Menu")
			if add, ok := cells[cellAdd]; ok {
				opens("+", tui.Point{X: add + 1}, tui.MouseLeft, "Add")
			}
			if k.blank {
				opens("a right press on the blank", tui.Point{X: 38}, tui.MouseRight, "Menu")
			}
			if tab >= 0 {
				opens("a right press on a tab", tui.Point{X: tab + 1}, tui.MouseRight, "Menu")
			}
		})
	}
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
	h := startAppInternal(t, NewOverlayHost(tabs), 40, 8)
	defer h.stopInternal()
	beta := -1
	h.onLoopInternal(func() {
		for _, c := range tabs.strip.row() {
			if c.kind == cellTab && c.id == "1" {
				beta = c.x
			}
		}
	})
	open := func() {
		t.Helper()
		if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: beta + 1, Y: 0}); err != nil {
			t.Fatal(err)
		}
		if !waitInternal(h, func() bool { return tabs.strip.menuOpen != nil }) {
			t.Fatalf("a right press on beta opened no menu:\n%s", h.tb.String())
		}
	}
	open()
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if !waitInternal(h, func() bool { return len(ran) == 1 }) {
		t.Fatal("Go ran nothing")
	}
	if i := <-ran; i != 1 {
		t.Fatalf("Go ran with tab %d, want beta's index, 1", i)
	}

	open()
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	h.syncInternal()
	h.syncInternal()
	if len(ran) != 0 {
		t.Fatalf("Idle, with no Run, ran %d", <-ran)
	}

	h.onLoopInternal(func() {
		tabs.strip.closeMenu()
		tabs.SetMenu(nil)
	})
	var taken bool
	h.onLoopInternal(func() {
		taken = tabs.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: beta + 1, Y: 0})
	})
	if taken {
		t.Fatal("with no menu, a right press on a tab was taken")
	}
	if !waitInternal(h, func() bool { return !strings.Contains(h.tb.String(), "≡") }) {
		t.Fatalf("with no menu, ≡ still shows:\n%s", h.tb.String())
	}
}
