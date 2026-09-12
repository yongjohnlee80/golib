package widget_test

// Base embedding contract tests.
//
// This file pairs with base.go and asserts that every widget properly embeds
// Base and chains Init to receive Context and NodeID.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
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
		"TextInput":   widget.NewTextInput(),
		"TextArea":    widget.NewTextArea(),
		"Select":      widget.NewSelect[string](),
		"List":        widget.NewList(widget.WithItems([]string{"a"}, func(s string) string { return s })),
		"BufferView":  widget.NewBufferView(),
		"Tabs":        widget.NewTabs(widget.WithTab("t", widget.NewText("x"))),
		"Split":       widget.NewSplit(widget.Horizontal, widget.NewText("a"), widget.NewText("b")),
		"Float":       widget.NewFloat(widget.NewText("f")),
		"StatusBar":   widget.NewStatusBar(),
		"ProgressBar": widget.NewProgressBar(),
		"Text":        widget.NewText("hello"),
		"Box":         widget.NewBox(widget.NewText("c")),
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
