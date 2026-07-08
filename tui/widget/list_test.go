package widget_test

// List + ListSource seam (ADR-0007 §2.4 rev 1, §5.11).

import (
	"fmt"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// countingSource records every Len/Item call (loop-goroutine calls; the
// mutex makes test-goroutine reads race-free).
type countingSource struct {
	mu    sync.Mutex
	n     int
	lens  int
	items []int
}

func (c *countingSource) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lens++
	return c.n
}

func (c *countingSource) Item(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append(c.items, i)
	return fmt.Sprintf("item-%03d", i)
}

func (c *countingSource) stats() (lens int, items []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lens, append([]int(nil), c.items...)
}

func (c *countingSource) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lens, c.items = 0, nil
}

func focusedList(t *testing.T, src widget.ListSource[string], w, hh int, opts ...widget.ListOption[string]) (*harness, *widget.List[string], *shell) {
	t.Helper()
	opts = append([]widget.ListOption[string]{widget.WithSource(src, func(s string) string { return s })}, opts...)
	l := widget.NewList(opts...)
	sh := newShell(l)
	h := startApp(t, sh, w, hh)
	h.inject(tab())
	h.barrier(sh)
	return h, l, sh
}

// TestListViewportOnlyFetch asserts §5.11: Item(i) only for
// viewport-intersecting rows, Len() once per render pass.
func TestListViewportOnlyFetch(t *testing.T) {
	src := &countingSource{n: 10_000}
	h, _, sh := focusedList(t, src, 20, 5)
	h.settle()
	src.reset()

	// One cursor move → one repaint frame.
	flushes := h.tb.Flushes()
	h.inject(key(tui.KeyDown))
	h.barrier(sh)
	h.waitFor("repaint", func() bool { return h.tb.Flushes() > flushes })
	frames := h.tb.Flushes() - flushes

	lens, items := src.stats()
	if lens != frames {
		t.Fatalf("Len calls = %d over %d render pass(es), want one per pass", lens, frames)
	}
	// The handler fetches the cursor row (for the event label); the render
	// fetches the viewport [0,5). Nothing outside the viewport.
	for _, i := range items {
		if i < 0 || i >= 5 {
			t.Fatalf("Item(%d) fetched outside the [0,5) viewport (fetches: %v)", i, items)
		}
	}
	if len(items) > 5*frames+1 {
		t.Fatalf("%d Item calls for %d frame(s) of a 5-row viewport: %v", len(items), frames, items)
	}
}

// TestListKeysAndEvents: cursor keys emit SelectionChangedEvent (Owner
// set); Enter emits ActivateEvent; End jumps and scrolls.
func TestListKeysAndEvents(t *testing.T) {
	src := widget.SliceSource([]string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"})
	h, l, sh := focusedList(t, src, 20, 3)
	sels := record[widget.SelectionChangedEvent](h)
	acts := record[widget.ActivateEvent](h)

	h.inject(key(tui.KeyDown), key(tui.KeyDown), key(tui.KeyEnter))
	h.barrier(sh)

	var id tui.NodeID
	h.onLoop(func() { id = l.NodeID() })
	if ev, ok := sels.last(); !ok || ev.Owner != id || ev.Index != 2 || ev.Label != "gamma" {
		t.Fatalf("SelectionChangedEvent = %+v, want owner %d index 2 gamma", ev, id)
	}
	if ev, ok := acts.last(); !ok || ev.Owner != id || ev.Index != 2 {
		t.Fatalf("ActivateEvent = %+v, want index 2", ev)
	}

	// End: cursor to the last row; the 3-row viewport scrolls.
	h.inject(key(tui.KeyEnd))
	h.barrier(sh)
	h.wantContains("zeta")
	h.wantNotContains("alpha")
	var idx int
	var ok bool
	h.onLoop(func() { idx, ok = l.Selected() })
	if !ok || idx != 5 {
		t.Fatalf("Selected = (%d,%v), want (5,true)", idx, ok)
	}
}

// TestListWithItemsSugar asserts §5.11: WithItems/SetItems behave as
// SliceSource sugar, and RefreshSource re-reads Len and clamps the cursor.
func TestListWithItemsSugar(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b", "c"}, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 10, 5)
	h.inject(tab())
	h.barrier(sh)
	h.wantContains("a")

	h.onLoop(func() { l.SetItems([]string{"x", "y"}) })
	h.settle()
	h.wantContains("x")
	h.wantNotContains("a")

	// RefreshSource with a shrinking source clamps the cursor.
	src := &countingSource{n: 6}
	h.onLoop(func() { l.SetSource(src) })
	h.inject(key(tui.KeyEnd)) // cursor 5
	h.barrier(sh)
	h.onLoop(func() {
		src.mu.Lock()
		src.n = 2
		src.mu.Unlock()
		l.RefreshSource()
	})
	var idx int
	h.onLoop(func() { idx, _ = l.Selected() })
	if idx != 1 {
		t.Fatalf("cursor after shrink+RefreshSource = %d, want clamped 1", idx)
	}
}

// TestListMultiSelect: Space toggles; SelectedAll reports ascending.
func TestListMultiSelect(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c"})
	h, l, sh := focusedList(t, src, 10, 5, widget.WithMultiSelect[string](true))
	sels := record[widget.SelectionChangedEvent](h)

	h.inject(key(' '), key(tui.KeyDown), key(tui.KeyDown), key(' '))
	h.barrier(sh)
	var all []int
	h.onLoop(func() { all = l.SelectedAll() })
	if len(all) != 2 || all[0] != 0 || all[1] != 2 {
		t.Fatalf("SelectedAll = %v, want [0 2]", all)
	}
	if sels.count() != 0 {
		t.Fatalf("multi-select cursor moves emitted SelectionChangedEvent")
	}
}

// TestListMouse: click selects; double-click activates; wheel scrolls.
func TestListMouse(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d", "e", "f"})
	h, l, sh := focusedList(t, src, 10, 3)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1))
	h.barrier(sh)
	var idx int
	h.onLoop(func() { idx, _ = l.Selected() })
	if idx != 1 {
		t.Fatalf("click selected %d, want 1", idx)
	}
	h.inject(click(2, 1)) // second click within the window: activate
	h.barrier(sh)
	if ev, ok := acts.last(); !ok || ev.Index != 1 {
		t.Fatalf("double-click ActivateEvent = %+v, want index 1", ev)
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
	h.barrier(sh)
	h.wantContains("d") // viewport scrolled by one row
}

// TestListUnknownKeysBubble: non-list keys pass through.
func TestListUnknownKeysBubble(t *testing.T) {
	src := widget.SliceSource([]string{"a"})
	h, _, sh := focusedList(t, src, 10, 3)
	h.inject(key('x'), keyMod('r', tui.ModCtrl))
	h.barrier(sh)
	if got := len(sh.bubbledKeys()); got < 2 {
		t.Fatalf("expected x and Ctrl+R to bubble, got %v", sh.bubbledKeys())
	}
}
