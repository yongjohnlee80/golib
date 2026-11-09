package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/terminal"
)

// stripOf is tabs' strip: the TabStrip among its children.
func stripOf(t *testing.T, tabs *terminal.TerminalTabs) *core.TabStrip {
	t.Helper()
	for _, c := range tabs.ListChildren() {
		if s, ok := c.(*core.TabStrip); ok {
			return s
		}
	}
	t.Fatal("TerminalTabs lists no TabStrip among its children")
	return nil
}

// A TerminalTabs's strip, its child on the first row, is the page tab list to assistive tools (the
// container is not one too, so a native painter paints the strip once), and its ActiveTabIndex is
// the active tab's place on it: the first tab added, until another is chosen.
func TestTerminalTabsRoleAndActiveIndex(t *testing.T) {
	tabs := terminal.NewTerminalTabs()
	strip := stripOf(t, tabs)
	if strip.AccessibleRole() != tui.RolePageTabList {
		t.Errorf("the strip's role %v", strip.AccessibleRole())
	}
	if r, ok := any(tabs).(interface{ AccessibleRole() tui.AccessibleRole }); ok && r.AccessibleRole() == tui.RolePageTabList {
		t.Error("the container reports a page tab list too: a native painter would paint the strip twice")
	}
	h := startApp(t, newShell(tabs), 60, 8)
	defer h.stop()
	var i int
	h.onLoop(func() {
		if _, err := tabs.Add("one"); err != nil {
			t.Error(err)
		}
		if _, err := tabs.Add("two"); err != nil {
			t.Error(err)
		}
	})
	h.settle()
	h.onLoop(func() { i = strip.ActiveTabIndex() })
	if i != 0 {
		t.Errorf("ActiveTabIndex %d, want 0: the first tab added is the active one", i)
	}
}
