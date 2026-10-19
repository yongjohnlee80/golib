package tui

import (
	"sync/atomic"
	"testing"
)

// scroller is a Revealer 10 rows high whose one child sits 15 rows down its content, offset rows
// up: a child at the bottom of any view needs it to move.
type scroller struct {
	MultiChild
	off   int
	moves atomic.Int64
}

const scrollerView, scrollerChildY = 10, 15

func (s *scroller) Layout(c Constraints) Size {
	for _, ch := range s.All() {
		h := scrollerView
		if _, leaf := ch.(*focusProbe); leaf {
			h = 1
		}
		s.ctx.LayoutChild(ch, Tight(Size{W: c.MaxW, H: h}))
		s.ctx.PlaceChild(ch, Rect{Y: scrollerChildY - s.off, W: c.MaxW, H: h})
	}
	return Size{W: c.MaxW, H: scrollerView}
}

func (s *scroller) Render(Surface)         {}
func (s *scroller) HandleEvent(Event) bool { return false }
func (s *scroller) Reveal(r Rect) (moved bool) {
	if r.Y+r.H <= scrollerView && r.Y >= 0 {
		return false
	}
	s.off += r.Y + r.H - scrollerView
	s.moves.Add(1)
	s.ctx.RequestLayout()
	return true
}

// nested is depth scrollers, each inside the next, the focusable leaf in the innermost.
func nested(depth int) (outer *scroller, all []*scroller, leaf *focusProbe) {
	leaf = newFocusProbe("leaf", Size{W: 4, H: 1})
	var c Component = leaf
	for range depth {
		s := &scroller{}
		s.Add(c)
		all = append(all, s)
		c = s
	}
	return c.(*scroller), all, leaf
}

func allMoved(h *harness, all []*scroller) bool {
	for _, s := range all {
		if s.moves.Load() == 0 {
			return false
		}
	}
	var pending bool
	h.onLoop(func() { pending = h.app.reveal })
	return !pending
}

// A frame serves at most maxRevealSteps; the rest of a deep nesting is served by the frames after
// it, without more input.
func TestARevealServesItsStepsAFrameAtATime(t *testing.T) {
	outer, all, leaf := nested(maxRevealSteps + 3)
	root := &box{rects: []Rect{{W: 20, H: scrollerView}}}
	root.Add(outer)
	h := startApp(t, root, 20, scrollerView)
	h.onLoop(func() {
		leaf.ctx.RequestFocus()
		h.app.layoutAndCommit() // one frame's layout, before the loop draws its own
		moved := 0
		for _, s := range all {
			if s.moves.Load() > 0 {
				moved++
			}
		}
		if moved != maxRevealSteps || !h.app.reveal {
			t.Errorf("one frame moved %d views, the request pending %v; want %d, pending", moved, h.app.reveal, maxRevealSteps)
		}
	})
	waitFor(t, "every view moved, the request cleared", func() bool { return allMoved(h, all) })
}

// A sibling whose commits dirty layout five times and settle on the sixth, every frame, legally
// within the eight-pass guard, never uses up the reveal's turn: the leaf is still brought into
// view, and the request clears.
func TestOrdinaryCommitsNeverStarveAReveal(t *testing.T) {
	outer, all, leaf := nested(2)
	var commits atomic.Int64
	busy := &probe{name: "busy", onLayout: func(p *probe, c Constraints) Size {
		p.ctx.AfterLayout("busy", func() {
			if commits.Add(1)%6 != 0 {
				p.ctx.RequestLayout()
			}
		})
		return c.Constrain(Size{W: 1, H: 1})
	}}
	root := &box{rects: []Rect{{W: 20, H: scrollerView}, {X: 20, W: 1, H: 1}}}
	root.Add(outer, busy)
	h := startApp(t, root, 21, scrollerView)
	h.onLoop(func() { leaf.ctx.RequestFocus() })
	waitFor(t, "the leaf revealed", func() bool { return allMoved(h, all) })
	if n := commits.Load(); n < 12 {
		t.Fatalf("the busy sibling committed %d times; want it to have spent six passes a frame", n)
	}
}
