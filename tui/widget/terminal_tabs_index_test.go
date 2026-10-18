package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A TerminalTabs is a page tab list to assistive tools, and ActiveTabIndex is the active tab's place on
// its strip: the first tab added, until another is chosen.
func TestTerminalTabsRoleAndActiveIndex(t *testing.T) {
	tabs := widget.NewTerminalTabs()
	if tabs.AccessibleRole() != tui.RolePageTabList {
		t.Errorf("role %v", tabs.AccessibleRole())
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
	h.onLoop(func() { i = tabs.ActiveTabIndex() })
	if i != 0 {
		t.Errorf("ActiveTabIndex %d, want 0: the first tab added is the active one", i)
	}
}
