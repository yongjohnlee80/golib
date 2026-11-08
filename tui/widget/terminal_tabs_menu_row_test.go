package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// A TerminalTabs' menu opens on the row right under its strip, as every strip's does
// (TestEveryStripsMenuOpensRightUnderTheStrip): its ≡, its +, and a right press on its blank.
func TestTerminalTabsMenuOpensRightUnderTheStrip(t *testing.T) {
	rows := func(label string) []MenuItemModel {
		return []MenuItemModel{NewCommand("row", label, TabStripMenuAction{ID: "row"})}
	}
	s := NewTerminalTabs()
	s.SetMenu(func(TerminalTabID) []MenuItemModel { return rows("Menu") })
	s.SetAddMenu(func() []MenuItemModel { return rows("Add") })
	h := widgettest.StartApp(t, NewOverlayHost(s), 40, 8)
	defer h.Stop()
	m := widgettest.StripMenu{Cells: map[kit.StripKind]int{}, Tab: -1, Blank: true,
		Open: func() bool { return s.strip.MenuOpen != nil }}
	h.OnLoop(func() {
		for _, c := range s.strip.Row() {
			if _, ok := m.Cells[c.Kind]; !ok {
				m.Cells[c.Kind] = c.X
			}
			if c.Kind == kit.CellTab && m.Tab < 0 {
				m.Tab = c.X
			}
		}
	})
	widgettest.MenuOpensUnderTheStrip(t, h, m, "Menu", "Add")
}
