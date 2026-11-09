package widget_test

// Base embedding contract tests.
//
// This file pairs with base.go and asserts that every widget properly embeds
// Base and chains Init to receive Context and NodeID.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// contexter is the Base plumbing surface every widget promotes.
type contexter interface {
	Context() *tui.Context
	NodeID() tui.NodeID
}

// TestBaseInitPlumbing mounts every v1 widget and asserts the Base Init
// chain ran: non-nil Context, valid NodeID (which catches a missed
// Base.Init chaining).
func TestBaseInitPlumbing(t *testing.T) {
	widgets := map[string]tui.Component{
		"TextInput":   core.NewTextInput(),
		"TextArea":    core.NewTextArea(),
		"Select":      core.NewSelect[string](),
		"List":        core.NewList(core.WithItems([]string{"a"}, func(s string) string { return s })),
		"BufferView":  core.NewBufferView(),
		"Tabs":        core.NewTabs(core.WithTab("t", core.NewText("x"))),
		"Split":       core.NewSplit(core.Horizontal, core.NewText("a"), core.NewText("b")),
		"Float":       core.NewFloat(core.NewText("f")),
		"StatusBar":   core.NewStatusBar(),
		"ProgressBar": core.NewProgressBar(),
		"Text":        core.NewText("hello"),
		"Box":         core.NewBox(core.NewText("c")),
	}
	for name, w := range widgets {
		t.Run(name, func(t *testing.T) {
			h := startApp(t, w, 40, 12)
			h.onLoop(func() {
				c := w.(contexter)
				if c.Context() == nil {
					t.Errorf("%s: nil Context after mount — Base.Init not chained", name)
				}
				if c.NodeID() == 0 {
					t.Errorf("%s: zero NodeID after mount", name)
				}
			})
		})
	}
}
