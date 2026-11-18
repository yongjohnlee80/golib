package terminal

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// THE TABS' × (SetTabCloseButtons, SetOnTabClose, SetTabClosable): a × at the start of every tab,
// left of its name, as the editor's tabs have it. A press on it asks the host to close the tab and
// does nothing else; the tabs never close one by themselves.

// closeTabs is a TerminalTabs of two tabs, one and two, one active, its × on, recording what each
// press asks of the host: a close (by id), the +, the menu (by id).
type closeTabs struct {
	tabs     *TerminalTabs
	one, two TerminalTabID
	closes   []TerminalTabID
	added    int
	menus    []TerminalTabID
}

func newCloseTabs(t *testing.T) *closeTabs {
	t.Helper()
	c := &closeTabs{tabs: NewTerminalTabs()}
	c.tabs.SetActions(func() { c.added++ }, func(id TerminalTabID) { c.menus = append(c.menus, id) })
	c.tabs.SetOnTabClose(func(id TerminalTabID) { c.closes = append(c.closes, id) })
	c.one, _ = c.tabs.Add("one")
	c.two, _ = c.tabs.Add("two")
	return c
}

// stripRow is the tabs' first row as drawn.
func stripRow(h *widgettest.Harness) string { return strings.Split(h.TB.String(), "\n")[0] }

// spanOf is tab id's span on the strip.
func (c *closeTabs) spanOf(t *testing.T, h *widgettest.Harness, id TerminalTabID) core.TabSpan {
	t.Helper()
	var span core.TabSpan
	found := false
	h.OnLoop(func() {
		for i, tb := range c.tabs.strip.Tabs() {
			if tb.ID == string(id) {
				span, found = c.tabs.strip.TabSpans()[i], true
			}
		}
	})
	if !found {
		t.Fatalf("no tab %q on the strip", id)
	}
	return span
}

// press is a press of button at x on the strip's row, handled before it returns.
func (c *closeTabs) press(h *widgettest.Harness, button tui.MouseButton, x int) {
	h.OnLoop(func() { c.tabs.strip.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: button, X: x}) })
}

// The × is off by default; on, every tab starts with it, left of its name. A left press on a tab's
// × asks the host to close that tab (its id) and neither chooses it nor removes it; a left press on
// the rest of the tab chooses it, asking nothing; a right press opens its menu, the + adds, and
// neither asks a close. Off again, the × goes. The mutants, named here: the × press choosing the
// tab too, and the tabs removing the tab themselves.
func TestTerminalTabsCloseMarkAsksTheHostAndNothingElse(t *testing.T) {
	c := newCloseTabs(t)
	h := widgettest.StartApp(t, c.tabs, 40, 6)
	defer h.Stop()
	h.Sync()
	if row := stripRow(h); strings.Contains(row, "×") {
		t.Fatalf("a × with SetTabCloseButtons unset: %q", row)
	}
	h.OnLoop(func() { c.tabs.SetTabCloseButtons(true) })
	h.Sync()
	if row := stripRow(h); !strings.Contains(row, " × one ") || !strings.Contains(row, " × two ") {
		t.Fatalf("the strip with the × on: %q; want each tab starting with its ×", row)
	}
	two := c.spanOf(t, h, c.two)
	c.press(h, tui.MouseLeft, two.X+1) // two's ×
	h.OnLoop(func() {
		if len(c.closes) != 1 || c.closes[0] != c.two {
			t.Errorf("a press on two's × asked to close %v; want [%s]", c.closes, c.two)
		}
		if c.tabs.Active() != c.one || len(c.tabs.Tabs()) != 2 {
			t.Errorf("a press on two's × chose %q, %d tabs left; want one still active and both tabs there", c.tabs.Active(), len(c.tabs.Tabs()))
		}
	})
	c.press(h, tui.MouseLeft, two.X+4) // two's name
	c.press(h, tui.MouseRight, two.X+1)
	h.OnLoop(func() {
		if c.tabs.Active() != c.two || len(c.closes) != 1 {
			t.Errorf("a press on two's name: active %q, closes %v; want two chosen and no new close", c.tabs.Active(), c.closes)
		}
		if len(c.menus) != 1 || c.menus[0] != c.two || len(c.closes) != 1 {
			t.Errorf("a right press on two's ×: menus %v, closes %v; want its menu, no close", c.menus, c.closes)
		}
	})
	h.OnLoop(func() { c.tabs.SetTabCloseButtons(false) })
	h.Sync()
	if row := stripRow(h); strings.Contains(row, "×") {
		t.Fatalf("SetTabCloseButtons(false) left a ×: %q", row)
	}
}

// A tab the host makes not closable (SetTabClosable false) shows no ×: a press where it would be
// chooses the tab, asking nothing; closable again, its × is back. The other tab keeps its ×
// throughout. The mutant, named here: NoClose ignored by the strip's label (the × drawn and
// pressed on a tab that may not close).
func TestTerminalTabsTabNotClosableShowsNoCloseMark(t *testing.T) {
	c := newCloseTabs(t)
	c.tabs.SetTabCloseButtons(true)
	h := widgettest.StartApp(t, c.tabs, 40, 6)
	defer h.Stop()
	h.OnLoop(func() {
		if err := c.tabs.SetTabClosable(c.two, false); err != nil {
			t.Error(err)
		}
	})
	h.Sync()
	if row := stripRow(h); !strings.Contains(row, " × one ") || strings.Contains(row, "× two") || !strings.Contains(row, " two ") {
		t.Fatalf("two not closable: %q; want one's × and two without one", row)
	}
	two := c.spanOf(t, h, c.two)
	c.press(h, tui.MouseLeft, two.X+1)
	h.OnLoop(func() {
		if len(c.closes) != 0 || c.tabs.Active() != c.two {
			t.Errorf("a press at the start of a tab with no ×: closes %v, active %q; want no close, two chosen", c.closes, c.tabs.Active())
		}
	})
	h.OnLoop(func() { _ = c.tabs.SetTabClosable(c.two, true) })
	h.Sync()
	if row := stripRow(h); !strings.Contains(row, " × two ") {
		t.Fatalf("two closable again: %q; want its ×", row)
	}
	if err := c.tabs.SetTabClosable("no-such-tab", false); err == nil {
		t.Error("SetTabClosable on a tab that isn't there: no error")
	}
}

// The keyboard is unchanged by the ×: the strip's keys move between tabs (Ctrl+PgDn, Ctrl+PgUp)
// and close nothing; there is no key for the ×.
func TestTerminalTabsKeysCloseNothing(t *testing.T) {
	c := newCloseTabs(t)
	c.tabs.SetTabCloseButtons(true)
	h := widgettest.StartApp(t, c.tabs, 40, 6)
	defer h.Stop()
	h.OnLoop(func() {
		c.tabs.Context().RequestFocus()
	})
	h.OnLoop(func() {
		c.tabs.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageDown, Mods: tui.ModCtrl})
		c.tabs.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDelete})
		if c.tabs.Active() != c.two || len(c.closes) != 0 || len(c.tabs.Tabs()) != 2 {
			t.Errorf("Ctrl+PgDn and Delete: active %q, closes %v, %d tabs; want two chosen, nothing closed", c.tabs.Active(), c.closes, len(c.tabs.Tabs()))
		}
	})
}
