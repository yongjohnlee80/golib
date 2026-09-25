package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// focus_design_test.go: which widgets are focusable BY DESIGN — what
// tui.App.HoldsFocusable reads, and forceActiveFocus refuses on.

// A pane Box — not built WithFocusable — holding a Text holds nothing
// focusable by design; a Box built WithFocusable(true) is focusable itself.
func TestAPaneBoxIsNotFocusableByDesign(t *testing.T) {
	pane := widget.NewBox(widget.NewText("label"))
	stop := widget.NewBox(widget.NewText("stop"), widget.WithFocusable(true))
	root := tui.NewFlex(tui.Vertical)
	root.Add(pane, stop)
	h := startApp(t, root, 20, 8)
	defer h.stop()
	var paneHolds, stopHolds bool
	h.onLoop(func() {
		paneHolds, _ = h.app.HoldsFocusable(pane)
		stopHolds, _ = h.app.HoldsFocusable(stop)
	})
	if paneHolds {
		t.Error("a pane Box around a Text was judged focusable by design")
	}
	if !stopHolds {
		t.Error("a Box built WithFocusable(true) was judged not focusable by design")
	}
}

// A pane Box around a List holds a focusable: its list — so focus can be
// moved INTO the pane, though the pane itself is never a stop.
func TestAPaneBoxAroundAListHoldsItsList(t *testing.T) {
	list := widget.NewList(widget.WithItems([]string{"a", "b"}, func(s string) string { return s }))
	pane := widget.NewBox(list)
	root := tui.NewFlex(tui.Vertical)
	root.Add(pane)
	h := startApp(t, root, 20, 8)
	defer h.stop()
	var holds, moved bool
	h.onLoop(func() {
		holds, _ = h.app.HoldsFocusable(pane)
		moved = h.app.FocusInto(pane)
	})
	if !holds || !moved {
		t.Errorf("a pane Box around a List: holds=%v, focus moved in=%v; want both", holds, moved)
	}
}
