package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestWidgetRoles(t *testing.T) {
	cases := []struct {
		c    tui.Component
		want tui.AccessibleRole
	}{
		{widget.NewButton("OK"), tui.RolePushButton},
		{widget.NewTextInput(), tui.RoleEditableText},
		{widget.NewTextArea(), tui.RoleTextArea},
		{widget.NewProgressBar(), tui.RoleProgressBar},
		{widget.NewTabs(widget.WithTab("a", widget.NewText(""))), tui.RolePageTabList},
		{widget.NewStatusBar(), tui.RoleStatusBar},
		{widget.NewBox(nil), tui.RoleGrouping},
		{widget.NewText(""), tui.RoleStaticText},
		{widget.NewTree(), tui.RoleTree},
	}
	for _, c := range cases {
		if got := tui.RoleOf(c.c); got != c.want {
			t.Errorf("%T: role %v; want %v", c.c, got, c.want)
		}
	}
}
