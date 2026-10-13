package tui

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// tipProbe is a focusable control with a tooltip, set on the loop.
type tipProbe struct {
	focusProbe
	tip string
}

func newTipProbe(name, tip string) *tipProbe {
	p := &tipProbe{focusProbe: *newFocusProbe(name, Size{W: 6, H: 1}), tip: tip}
	p.accepts.Store(true)
	return p
}

func (p *tipProbe) ToolTip() string { return p.tip }

// tipShown is the node the card is for, read on the loop (0: none).
func tipShown(h *harness) (id NodeID) {
	h.onLoop(func() { id = h.app.tip.node })
	return id
}

// screenRow is row y of the screen.
func screenRow(h *harness, y int) string {
	rows := strings.Split(h.tb.String(), "\n")
	if y < len(rows) {
		return rows[y]
	}
	return ""
}

// The pointer resting on a control shows its card under it after the delay, not before; leaving
// closes it. The card is no node: the pointer over it is over what lies beneath.
func TestToolTip_HoverShowsAfterTheDelayAndTheCardPassesThePointer(t *testing.T) {
	a := newTipProbe("a", "the prose font: the Rendered view and the HTML preview")
	under := newTipProbe("under", "")
	root := &box{rects: []Rect{{X: 1, Y: 1, W: 6, H: 1}, {X: 1, Y: 2, W: 20, H: 1}}}
	root.Add(a, under)
	h := startApp(t, root, 70, 6)
	start := time.Now()
	h.inject(motion(2, 1))
	h.sync()
	if id := tipShown(h); id != 0 && time.Since(start) < toolTipDelay {
		t.Fatal("the card showed before the pointer had rested")
	}
	waitFor(t, "the card", func() bool { return tipShown(h) == a.nodeID() })
	if time.Since(start) < toolTipDelay {
		t.Fatalf("the card showed after %v, before the delay", time.Since(start))
	}
	h.sync()
	if row := screenRow(h, 2); !strings.Contains(row, "the prose font: the Rendered view") {
		t.Fatalf("no card under the control:\n%s", h.tb.String())
	}
	// the pointer moves onto the card: it is over the control beneath, and the card closes
	h.inject(motion(4, 2))
	waitFor(t, "the control under the card hovered", func() bool {
		var id NodeID
		h.onLoop(func() { id = h.app.hovered })
		return id == under.nodeID()
	})
	waitFor(t, "the card closed", func() bool { return tipShown(h) == 0 })
}

// Focus the keyboard brings shows the card at once; focus a click gives does not; any key closes it.
func TestToolTip_KeyboardFocusShowsItAndAKeyClosesIt(t *testing.T) {
	a, b := newTipProbe("a", "first"), newTipProbe("b", "second, in full")
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 1}, {X: 0, Y: 2, W: 6, H: 1}}}
	root.Add(a, b)
	h := startApp(t, root, 40, 6)
	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	if tipShown(h) != 0 {
		t.Fatal("focus given by the program showed a card")
	}
	h.inject(KeyEvent{Kind: KeyPress, Code: KeyTab})
	waitFor(t, "b's card, at once", func() bool { return tipShown(h) == b.nodeID() })
	h.sync()
	if !strings.Contains(screenRow(h, 3), "second, in full") {
		t.Fatalf("no card under b:\n%s", h.tb.String())
	}
	h.inject(KeyEvent{Kind: KeyPress, Code: 'x', Text: "x"})
	waitFor(t, "a key closed it", func() bool { return tipShown(h) == 0 })

	// a click focuses a: no card for the focus, and the press holds the hover card back
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 1, Y: 0}, MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 1, Y: 0})
	waitFor(t, "a focused by the click", func() bool { return focusedID(h) == a.nodeID() })
	time.Sleep(toolTipDelay + 200*time.Millisecond)
	if id := tipShown(h); id != 0 {
		t.Fatalf("a card showed after a click (for node %d)", id)
	}
	// once the pointer has left and come back, it shows again (two injections: motions in one batch
	// may coalesce into the last)
	h.inject(motion(20, 5))
	waitFor(t, "the pointer off a", func() bool {
		var id NodeID
		h.onLoop(func() { id = h.app.hovered })
		return id != a.nodeID()
	})
	h.inject(motion(1, 0))
	waitFor(t, "the card again after leaving", func() bool { return tipShown(h) == a.nodeID() })
}

// The card closes when its text changes and when its control unmounts.
func TestToolTip_ATextChangeOrAnUnmountClosesIt(t *testing.T) {
	a, b := newTipProbe("a", "one"), newTipProbe("b", "two")
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 1}, {X: 0, Y: 2, W: 6, H: 1}}}
	root.Add(a, b)
	h := startApp(t, root, 40, 6)
	h.inject(motion(1, 0))
	waitFor(t, "a's card", func() bool { return tipShown(h) == a.nodeID() })
	h.onLoop(func() { a.tip = "one, changed"; a.ctx.MarkDirty() })
	waitFor(t, "a text change closed it", func() bool { return tipShown(h) == 0 })

	h.inject(motion(1, 2))
	waitFor(t, "b's card", func() bool { return tipShown(h) == b.nodeID() })
	h.onLoop(func() { root.Remove(b); root.rects = root.rects[:1] })
	waitFor(t, "an unmount closed it", func() bool { return tipShown(h) == 0 })
}

// In a window, the card covers the native views beneath it: its cells are painted after them.
func TestToolTip_TheCardCoversANativeViewBeneath(t *testing.T) {
	a := newTipProbe("a", "over the graph")
	view := &box{view: "graph", report: true}
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 1}, {X: 0, Y: 1, W: 30, H: 4}}}
	root.Add(a, reporterBox{view})
	h, nb := startNative(t, root, 40, 6, nil)
	h.inject(motion(1, 0))
	waitFor(t, "a's card", func() bool { return tipShown(h) == a.nodeID() })
	var card Rect
	h.onLoop(func() { card = h.app.tip.rect })
	waitFor(t, "the graph covered by the card", func() bool {
		for _, p := range nb.last() {
			if p.View == "graph" && slices.Contains(p.Covered, card) {
				return true
			}
		}
		return false
	})
}

func TestToolTip_PlacementAndWrap(t *testing.T) {
	screen := Size{W: 40, H: 10}
	if got := placeToolTip(Rect{X: 2, Y: 3, W: 6, H: 1}, Size{W: 10, H: 2}, screen); got != (Rect{X: 2, Y: 4, W: 10, H: 2}) {
		t.Errorf("below: %+v", got)
	}
	if got := placeToolTip(Rect{X: 2, Y: 9, W: 6, H: 1}, Size{W: 10, H: 2}, screen); got != (Rect{X: 2, Y: 7, W: 10, H: 2}) {
		t.Errorf("above, at the bottom: %+v", got)
	}
	if got := placeToolTip(Rect{X: 35, Y: 0, W: 4, H: 1}, Size{W: 10, H: 1}, screen); got != (Rect{X: 30, Y: 1, W: 10, H: 1}) {
		t.Errorf("clamped at the right: %+v", got)
	}
	width := func(s string) int { return len([]rune(s)) }
	if got := wrapToolTip("the prose font: the Rendered view", 12, width); !slices.Equal(got, []string{"the prose", "font: the", "Rendered", "view"}) {
		t.Errorf("wrapped at spaces: %q", got)
	}
	if got := wrapToolTip("abcdefghij xy", 4, width); !slices.Equal(got, []string{"abcd", "efgh", "ij", "xy"}) {
		t.Errorf("a long word cut: %q", got)
	}
	if got := wrapToolTip("a\nb\nc\nd\ne\nf\ng\nh", 5, width); len(got) != toolTipRows || got[toolTipRows-1] != "f…" {
		t.Errorf("past the rows: %q", got)
	}
}

// tipBox is a container with a tooltip: a ComboBox, whose focused field is its child.
type tipBox struct {
	box
	tip string
}

func (b *tipBox) ToolTip() string { return b.tip }

// Focus the keyboard brings to a control inside a ToolTipper shows that ToolTipper's card.
func TestToolTip_KeyboardFocusInsideAToolTipperShowsItsCard(t *testing.T) {
	a, field := newTipProbe("a", ""), newTipProbe("field", "")
	combo := &tipBox{box: box{rects: []Rect{{W: 6, H: 1}}}, tip: "the font the page is drawn in"}
	combo.Add(field)
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 1}, {X: 0, Y: 2, W: 6, H: 1}}}
	root.Add(a, combo)
	h := startApp(t, root, 40, 6)
	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	h.inject(KeyEvent{Kind: KeyPress, Code: KeyTab})
	waitFor(t, "the field focused", func() bool { return focusedID(h) == field.nodeID() })
	waitFor(t, "the combo's card", func() bool {
		var id NodeID
		h.onLoop(func() { id = combo.ctx.ID() })
		return tipShown(h) == id
	})
}
