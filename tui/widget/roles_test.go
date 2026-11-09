package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/files"
)

func TestWidgetRoles(t *testing.T) {
	cases := []struct {
		c    tui.Component
		want tui.AccessibleRole
	}{
		{core.NewButton("OK"), tui.RolePushButton},
		{core.NewTextInput(), tui.RoleEditableText},
		{core.NewTextArea(), tui.RoleTextArea},
		{core.NewProgressBar(), tui.RoleProgressBar},
		{core.NewTabs(core.WithTab("a", core.NewText(""))), tui.RolePageTabList},
		{core.NewStatusBar(), tui.RoleStatusBar},
		{core.NewBox(nil), tui.RoleGrouping},
		{core.NewText(""), tui.RoleStaticText},
		{files.NewTree(), tui.RoleTree},
		{core.NewFloat(core.NewText("")), tui.RoleWindow},
	}
	for _, c := range cases {
		if got := tui.RoleOf(c.c); got != c.want {
			t.Errorf("%T: role %v; want %v", c.c, got, c.want)
		}
	}
}
