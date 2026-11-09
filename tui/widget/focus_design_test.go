package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// focus_design_test.go: which widgets are focusable BY DESIGN — what
// tui.App.HoldsFocusable reads, and forceActiveFocus refuses on.

// A pane Box — not built WithFocusable — holding a Text holds nothing
// focusable by design; a Box built WithFocusable(true) is focusable itself.
func TestAPaneBoxIsNotFocusableByDesign(t *testing.T) {
	pane := core.NewBox(core.NewText("label"))
	stop := core.NewBox(core.NewText("stop"), core.WithFocusable(true))
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
	list := core.NewList(core.WithItems([]string{"a", "b"}, func(s string) string { return s }))
	pane := core.NewBox(list)
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

// A float's layer takes focus only as a MODAL float's fallback stop. So a
// shown, non-modal Float holding a Text holds nothing focusable by design —
// and FocusInto cannot focus it, agreeing — while a modal one, whose layer is
// that stop, does.
func TestAFloatsLayerIsFocusableByDesignOnlyWhenModal(t *testing.T) {
	for _, modal := range []bool{false, true} {
		f := core.NewFloat(core.NewText("hello"), core.WithModal(modal))
		host := core.NewOverlayHost(core.NewText("behind"))
		sh := newShell(host)
		h := startApp(t, sh, 40, 10)
		h.onLoop(func() {
			host.Attach(f)
			f.Show()
		})
		h.barrier(sh)
		h.settle()
		var holds, mounted, moved bool
		h.onLoop(func() {
			holds, mounted = h.app.HoldsFocusable(f)
			moved = h.app.FocusInto(f)
		})
		if !mounted {
			t.Fatalf("modal=%v: the shown Float is not mounted", modal)
		}
		if holds != modal || moved != modal {
			t.Errorf("modal=%v: HoldsFocusable=%v, FocusInto=%v; want both %v", modal, holds, moved, modal)
		}
		h.stop()
	}
}
