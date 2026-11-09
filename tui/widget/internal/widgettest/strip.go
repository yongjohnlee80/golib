package widgettest

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// StripMenu is a tab strip under test, as MenuOpensUnderTheStrip presses it: where each kind of
// stretch first starts on its row (Cells), the first tab's start (-1 for none), whether it is wider
// than its cells so a right press on its blank opens the menu (Blank), and whether its menu is open
// now (Open, read on the loop).
type StripMenu struct {
	Cells map[kit.StripKind]int
	Tab   int
	Blank bool
	Open  func() bool
}

// MenuOpensUnderTheStrip presses each way m's menu opens, in h's App, and fails t unless each menu
// is drawn right under the strip: its panel's border on row 1, labelled (menu, or add for the +) on
// row 2, with no empty row between. Escape closes each before the next. The strip is on row 0 of
// an App 40 cells wide (a right press on the blank lands at x 38).
func MenuOpensUnderTheStrip(t TB, h *Harness, m StripMenu, menu, add string) {
	t.Helper()
	opens := func(how string, at tui.Point, button tui.MouseButton, label string) {
		t.Helper()
		if err := h.TB.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: button, X: at.X, Y: at.Y}); err != nil {
			t.Fatal(err)
		}
		top := -1
		drawn := Wait(h, func() bool {
			lines := strings.Split(h.TB.String(), "\n")
			top = -1
			for i, l := range lines {
				if strings.Contains(l, "┌") {
					top = i
					break
				}
			}
			return m.Open() && top >= 0 && top+1 < len(lines) && strings.Contains(lines[top+1], label)
		})
		if !drawn || top != 1 {
			t.Fatalf("%s: the menu's panel is at row %d, want 1, right under the strip (drawn there: %v):\n%s",
				how, top, drawn, h.TB.String())
		}
		if err := h.TB.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}); err != nil {
			t.Fatal(err)
		}
		if !Wait(h, func() bool { return !m.Open() }) {
			t.Fatalf("%s: Escape left the menu open", how)
		}
	}
	at, ok := m.Cells[kit.CellMenu]
	if !ok {
		t.Fatalf("no ≡ on the strip: %v", m.Cells)
	}
	opens("≡", tui.Point{X: at + 1}, tui.MouseLeft, menu)
	if x, ok := m.Cells[kit.CellAdd]; ok {
		opens("+", tui.Point{X: x + 1}, tui.MouseLeft, add)
	}
	if m.Blank {
		opens("a right press on the blank", tui.Point{X: 38}, tui.MouseRight, menu)
	}
	if m.Tab >= 0 {
		opens("a right press on a tab", tui.Point{X: m.Tab + 1}, tui.MouseRight, menu)
	}
}
