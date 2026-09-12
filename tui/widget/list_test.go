package widget_test

// List + ListSource seam.

import (
	"fmt"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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

// TestListViewportOnlyFetch asserts that Item(i) is called only for
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

// TestListWithItemsSugar asserts that WithItems/SetItems behave as
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

func TestListHonorsVimMotions(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b", "c", "d"}, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 30, 6)
	h.inject(tab())
	h.barrier(sh)

	at := func() int {
		var i int
		h.onLoop(func() { i, _ = l.Selected() })
		return i
	}
	h.inject(key('j'))
	h.barrier(sh)
	if got := at(); got != 1 {
		t.Fatalf("j: cursor = %d, want 1", got)
	}
	h.inject(key('j'), key('k'))
	h.barrier(sh)
	if got := at(); got != 1 {
		t.Fatalf("jk: cursor = %d, want 1", got)
	}
	h.inject(key('G'))
	h.barrier(sh)
	if got := at(); got != 3 {
		t.Fatalf("G: cursor = %d, want 3", got)
	}
	h.inject(key('g'))
	h.barrier(sh)
	if got := at(); got != 0 {
		t.Fatalf("g: cursor = %d, want 0", got)
	}
}

func TestListIgnoresApplicationChords(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b", "c"}, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 30, 6)
	h.inject(tab())
	h.barrier(sh)

	h.inject(keyMod('j', tui.ModCtrl))
	h.barrier(sh)
	var idx int
	h.onLoop(func() { idx, _ = l.Selected() })
	if idx != 0 {
		t.Fatalf("Ctrl-j must bubble, not move the cursor: got %d", idx)
	}
}

func TestListHostControls(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b", "c"}, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 30, 6)
	h.inject(tab())
	h.barrier(sh)

	h.onLoop(func() {
		l.SetCursor(2)
		l.SetStyles(widget.ListStyles{CursorRow: style.New().Background(style.ANSI(8))})
	})
	h.barrier(sh)
	var idx, n int
	h.onLoop(func() {
		idx, _ = l.Selected()
		n = l.Len()
	})
	if idx != 2 || n != 3 {
		t.Fatalf("SetCursor/Len: cursor=%d len=%d, want 2/3", idx, n)
	}
}

// Finding 2 — THE REGRESSION CONTROL. List's own detection compared logical
// rows and refused this pair; the first conversion to the boundary count
// compared only the cell and activated row 2 after a click on row 1.
func TestListDoubleClickAcrossAScrollDoesNotActivate(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d", "e", "f"})
	h, l, sh := focusedList(t, src, 10, 3)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1)) // logical row 1
	h.barrier(sh)
	var first int
	h.onLoop(func() { first, _ = l.Selected() })
	if first != 1 {
		t.Fatalf("precondition: selected %d, want 1", first)
	}

	// Scroll one row: the SAME cell now addresses logical row 2.
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
	h.barrier(sh)

	h.inject(click(2, 1))
	h.barrier(sh)

	if n := acts.count(); n != 0 {
		ev, _ := acts.last()
		t.Errorf("activations = %d (%+v), want 0 — the two presses landed on "+
			"different logical rows, so this is not a double-click", n, ev)
	}
}

// The same pair WITHOUT the scroll still activates, so the control above is
// about identity and not about the wheel resetting something.
func TestListDoubleClickWithoutScrollStillActivates(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d", "e", "f"})
	h, l, sh := focusedList(t, src, 10, 3)
	acts := record[widget.ActivateEvent](h)
	_ = l

	h.inject(click(2, 1))
	h.barrier(sh)
	h.inject(click(2, 1))
	h.barrier(sh)

	if ev, ok := acts.last(); !ok || ev.Index != 1 {
		t.Errorf("ActivateEvent = %+v (ok=%v), want index 1", ev, ok)
	}
}

// Finding 3 — a triple-click is ONE activation, not two. With `>= 2` it fired
// on counts 2 and 3.
func TestListTripleClickActivatesOnce(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d"})
	h, _, sh := focusedList(t, src, 10, 4)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1), click(2, 1), click(2, 1))
	h.barrier(sh)

	if n := acts.count(); n != 1 {
		t.Errorf("activations from a triple-click = %d, want exactly 1", n)
	}
}

// THE BLOCKING case — an index is logical identity only WITHIN ONE SOURCE
// EPOCH. Clicking old row 1, replacing the source, then clicking the same cell
// used to emit ActivateEvent{Index:1} for the NEW row 1, which the user had
// clicked exactly once.
func TestListDoubleClickAcrossASourceReplacementDoesNotActivate(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d"})
	h, l, sh := focusedList(t, src, 10, 4)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1)) // old logical row 1
	h.barrier(sh)

	h.onLoop(func() { l.SetItems([]string{"x", "y", "z"}) })
	h.barrier(sh)

	h.inject(click(2, 1)) // same cell, entirely different row
	h.barrier(sh)

	if n := acts.count(); n != 0 {
		ev, _ := acts.last()
		t.Errorf("activations = %d (%+v), want 0 — the source was replaced between "+
			"the presses, so the index no longer names the row that was pressed", n, ev)
	}
}

// The same shape through RefreshSource, which mutates in place under one source
// and can reorder rows without SetSource ever being called.
func TestListDoubleClickAcrossARefreshDoesNotActivate(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	h, l, sh := focusedList(t, widget.SliceSource(items), 10, 4)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1))
	h.barrier(sh)
	h.onLoop(func() { l.RefreshSource() })
	h.barrier(sh)
	h.inject(click(2, 1))
	h.barrier(sh)

	if n := acts.count(); n != 0 {
		t.Errorf("activations = %d, want 0 — a refresh ends the source epoch", n)
	}
}

// The POSITIVE that keeps the two controls above honest: with the source left
// alone, the identical gesture still activates. Without this, clearing the index
// unconditionally would pass both controls and break double-click entirely.
func TestListDoubleClickWithUnchangedSourceStillActivates(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d"})
	h, _, sh := focusedList(t, src, 10, 4)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1), click(2, 1))
	h.barrier(sh)

	if ev, ok := acts.last(); !ok || ev.Index != 1 {
		t.Errorf("ActivateEvent = %+v (ok=%v), want index 1 — an untouched source "+
			"must still pair two presses", ev, ok)
	}
}
