package widget_test

// Split zoom & SetRatio contract.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// --- Zoom ---

func TestSplitZoomHidesPaneAndTransfersFocus(t *testing.T) {
	a := widget.NewTextInput()
	b := widget.NewTextInput()
	sp := widget.NewSplit(widget.Horizontal, a, b)
	sh := newShell(sp)
	h := startApp(t, sh, 41, 5)

	// Focus pane B (two tabs: a then b).
	h.inject(tab(), tab())
	h.inject(typeString("bbb")...)
	h.barrier(sh)

	zooms := record[widget.SplitZoomEvent](h)
	h.onLoop(func() { sp.Zoom(widget.PaneA) })
	h.barrier(sh)

	// Pane B's content is gone from the grid; the divider too.
	h.wantNotContains("bbb")
	if zooms.count() != 1 {
		t.Fatalf("zoom events = %d, want 1", zooms.count())
	}

	// Focus transferred INTO the retained pane: typing lands in A.
	h.inject(typeString("aaa")...)
	h.barrier(sh)
	var av, bv string
	h.onLoop(func() { av, bv = a.Value(), b.Value() })
	if av != "aaa" || bv != "bbb" {
		t.Fatalf("values = (%q, %q), want typing in A only", av, bv)
	}

	// Restore: prior ratio, pane B visible again, focus stays where it is.
	h.onLoop(func() { sp.Zoom(widget.PaneNone) })
	h.barrier(sh)
	h.wantContains("bbb")
	h.inject(typeString("!")...)
	h.barrier(sh)
	h.onLoop(func() { av = a.Value() })
	if av != "aaa!" {
		t.Fatalf("focus moved on restore: a = %q", av)
	}
}

func TestSplitZoomWithUnfocusableRetainedPaneClearsFocus(t *testing.T) {
	a := widget.NewText("static") // not focusable
	b := widget.NewTextInput()
	sp := widget.NewSplit(widget.Horizontal, a, b)
	sh := newShell(sp)
	h := startApp(t, sh, 41, 5)
	h.inject(tab()) // focus the input in pane B
	h.inject(typeString("x")...)
	h.barrier(sh)

	h.onLoop(func() { sp.Zoom(widget.PaneA) })
	h.barrier(sh)

	// The hidden input must not receive keys (focus cleared, key bubbles).
	before := len(sh.bubbledKeys())
	h.inject(typeString("y")...)
	h.barrier(sh)
	var bv string
	h.onLoop(func() { bv = b.Value() })
	if bv != "x" {
		t.Fatalf("hidden pane received input: %q", bv)
	}
	if len(sh.bubbledKeys()) <= before {
		t.Fatal("key did not bubble after focus clear")
	}
}

func TestSplitSetRatio(t *testing.T) {
	a := widget.NewText("A")
	b := widget.NewText("B")
	sp := widget.NewSplit(widget.Horizontal, a, b)
	sh := newShell(sp)
	h := startApp(t, sh, 41, 5)
	h.barrier(sh)

	resizes := record[widget.SplitResizedEvent](h)
	h.onLoop(func() { sp.SetRatio(0.25) })
	h.barrier(sh)
	var r float64
	h.onLoop(func() { r = sp.Ratio() })
	if r != 0.25 {
		t.Fatalf("Ratio = %v, want 0.25", r)
	}
	if resizes.count() != 1 {
		t.Fatalf("resize events = %d, want 1", resizes.count())
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("SetRatio(1.5) did not panic")
			}
		}()
		sp.SetRatio(1.5)
	}()
}
