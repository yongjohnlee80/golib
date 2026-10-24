package tui

import (
	"sync"
	"sync/atomic"
	"testing"
)

// preciseProbe is a scroller: it takes precise scrolls while it wants them, and records every
// mouse event it is sent.
type preciseProbe struct {
	counter
	wants  atomic.Bool
	mu     sync.Mutex
	events []MouseEvent
	// stalls: a key 's' holds the loop until release (the intake cells)
	entered, release chan struct{}
}

func (s *preciseProbe) PreciseScroll() bool { return s.wants.Load() }

func (s *preciseProbe) HandleEvent(ev Event) bool {
	switch e := ev.(type) {
	case KeyEvent:
		if e.Code == 's' && s.entered != nil {
			s.entered <- struct{}{}
			<-s.release
			return true
		}
	case MouseEvent:
		s.mu.Lock()
		s.events = append(s.events, e)
		s.mu.Unlock()
		return e.Kind == MouseWheel
	}
	return false
}

func (s *preciseProbe) got() []MouseEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MouseEvent(nil), s.events...)
}

// A precise scroll that made no step goes to the nearest PreciseScroller under the pointer, in
// its own frame, and to no other component: not the one under the pointer inside it, not its
// parent, not one beside it, and not a scroller that doesn't want it now. A wheel step still goes
// as it always has, its precise scroll on it.
func TestAPreciseScrollGoesToItsScrollerAlone(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	other := &counter{size: Size{W: 10, H: 4}}
	sc := &preciseProbe{counter: counter{size: Size{W: 10, H: 4}}}
	inner := &counter{size: Size{W: 4, H: 4}} // inside the scroller, under the pointer
	sc.Add(inner)
	root.Add(other, sc) // the scroller at x 10..20, inner at 10..14
	sc.wants.Store(true)
	h := startApp(t, root, 20, 4)
	h.sync()
	markers := int64(0)
	marker := func() { // a step over other, behind what came before: once it is in, they were dispatched
		markers++
		h.inject(MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 5, Y: 1})
		waitFor(t, "the marker arrived", func() bool { _, _, n := other.totals(); return n >= markers }) // the counts are checked below
	}
	precise := func(x int, dy float32) MouseEvent { return MouseEvent{Kind: MouseWheel, X: x, Y: 1, ScrollY: dy} }

	h.inject(precise(12, 1.5))
	marker()
	if got := sc.got(); len(got) != 1 || !got[0].precise() || got[0].ScrollY != 1.5 || got[0].X != 2 || got[0].Y != 1 {
		t.Fatalf("the scroller got %+v, want the precise scroll at its own (2, 1)", got)
	}
	h.inject(precise(5, 1)) // over other: no scroller on its path
	marker()
	sc.wants.Store(false)
	h.inject(precise(12, 1))
	marker()
	if _, _, n := inner.totals(); n != 0 {
		t.Errorf("the component inside the scroller was sent %d mouse events", n)
	}
	if _, _, n := root.totals(); n != markers {
		t.Errorf("the root was sent %d mouse events, want the %d markers alone", n, markers)
	}
	if _, _, n := other.totals(); n != markers {
		t.Errorf("the component beside the scroller was sent %d, want the %d markers alone", n, markers)
	}
	if got := sc.got(); len(got) != 1 {
		t.Errorf("a scroller that doesn't want it now was sent %+v", got[1:])
	}

	h.inject(MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 12, Y: 1, ScrollY: 9})
	waitFor(t, "the step reached the scroller", func() bool { return len(sc.got()) == 2 })
	if got := sc.got()[1]; got.Button != WheelDown || got.ScrollY != 9 {
		t.Errorf("the step arrived as %+v, want WheelDown with its 9", got)
	}
	if _, _, n := inner.totals(); n != 1 {
		t.Errorf("the step reached the component under the pointer %d times, want 1", n)
	}
}

// Consecutive precise scrolls at the same place sum in the input queue, none of the scroll lost;
// another place, or a wheel step between them, ends the run.
func TestConsecutivePreciseScrollsSum(t *testing.T) {
	sc := &preciseProbe{counter: counter{size: Size{W: 8, H: 2}}, entered: make(chan struct{}, 1), release: make(chan struct{})}
	sc.wants.Store(true)
	h := startApp(t, sc, 8, 2)
	stall(t, h, sc.entered)
	p := func(x int, dy float32) MouseEvent { return MouseEvent{Kind: MouseWheel, X: x, Y: 0, ScrollY: dy} }
	step := MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 2, Y: 0, ScrollY: 0.5}
	h.inject(p(1, 1), p(1, 2), p(1, 3), p(2, 4), step, p(2, 5), p(2, 6))
	// the whole script in the queue before the loop resumes, or the runs are racy
	waitFor(t, "intake absorbed the script", func() bool { return len(h.tb.events) == 0 })
	close(sc.release)
	waitFor(t, "the summed sequence", func() bool { return len(sc.got()) >= 4 })
	want := []struct {
		x  int
		b  MouseButton
		dy float32
	}{{1, MouseNone, 6}, {2, MouseNone, 4}, {2, WheelDown, 0.5}, {2, MouseNone, 11}}
	h.sync()
	got := sc.got()
	if len(got) != len(want) {
		t.Errorf("%d events, want %d: the runs did not sum", len(got), len(want))
	}
	for i, w := range want {
		if got[i].X != w.x || got[i].Button != w.b || got[i].ScrollY != w.dy {
			t.Errorf("event %d: %+v, want x %d, button %v, scroll %v", i, got[i], w.x, w.b, w.dy)
		}
	}
}
