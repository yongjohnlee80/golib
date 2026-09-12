package widget_test

// Split zoom, layout, keyboard resize, mouse drag, and SetRatio contracts.
//
// This file pairs with split.go and tests the dual-pane divider behavior.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// pane is a plain focusable filler for Split focus/resize tests.
type pane struct {
	widget.Base
	fill string
}

func (p *pane) AcceptsFocus() bool { return true }

func (p *pane) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}

func (p *pane) Render(s tui.Surface) {
	sz := s.Size()
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, p.fill, style.New())
}

func TestSplitLayoutAndKeyboardResize(t *testing.T) {
	left := &pane{fill: "a"}
	right := &pane{fill: "b"}
	split := widget.NewSplit(widget.Horizontal, left, right, widget.WithRatio(0.5), widget.WithMinSizes(4, 4))
	sh := newShell(split)
	h := startApp(t, sh, 21, 5)
	resized := record[widget.SplitResizedEvent](h)
	h.inject(tab()) // focus the left box
	h.barrier(sh)

	// 21 wide: avail 20, ratio .5 → divider at column 10.
	if got := strings.Index(h.row(2), "│"); got != 10 {
		t.Fatalf("divider at %d, want 10\n%s", got, h.grid())
	}

	h.inject(keyMod(tui.KeyRight, tui.ModAlt))
	h.barrier(sh)
	if got := strings.Index(h.row(2), "│"); got != 11 {
		t.Fatalf("divider after Alt+Right at %d, want 11\n%s", got, h.grid())
	}
	if ev, ok := resized.last(); !ok || ev.Ratio <= 0.5 {
		t.Fatalf("SplitResizedEvent = %+v, want ratio > 0.5", ev)
	}

	// Min sizes clamp: hammer Alt+Right; pane b never shrinks below 4.
	for i := 0; i < 20; i++ {
		h.inject(keyMod(tui.KeyRight, tui.ModAlt))
	}
	h.barrier(sh)
	if got := strings.Index(h.row(2), "│"); got != 16 {
		t.Fatalf("divider clamped at %d, want 16 (minB=4)\n%s", got, h.grid())
	}
}

func TestSplitMouseDrag(t *testing.T) {
	split := widget.NewSplit(widget.Horizontal, widget.NewText("L"), widget.NewText("R"))
	sh := newShell(split)
	h := startApp(t, sh, 21, 3)
	h.settle()
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 10, Y: 1},
		tui.MouseEvent{Kind: tui.MouseMotion, X: 6, Y: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 6, Y: 1},
	)
	h.waitFor("divider dragged", func() bool { return strings.Index(h.row(1), "│") == 6 })
}

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
}
