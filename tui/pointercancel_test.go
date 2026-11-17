package tui

import (
	"sync/atomic"
	"testing"
)

// focusHearer is a dragger that also counts the FocusEvents it is sent: the focused node is what a
// terminal focus loss tells, and a pointer cancel must tell it nothing.
type focusHearer struct {
	*dragger
	focusEvents atomic.Int64
}

func (f *focusHearer) HandleEvent(ev Event) bool {
	if _, ok := ev.(FocusEvent); ok {
		f.focusEvents.Add(1)
	}
	return f.dragger.HandleEvent(ev)
}

// TestAPointerCancelEndsTheGestureAndNothingElse: a backend's PointerCancelEvent ends the capture
// with CaptureLostCancelled, and is not a focus loss. The focused node (here the owner itself, as a
// press focuses it) hears no FocusEvent, none is published, focus stays, and the hover stays on the
// node under the pointer: a terminal focus loss would do all four, and a menu or an editor's undo
// group would end with them.
func TestAPointerCancelEndsTheGestureAndNothingElse(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &focusHearer{dragger: &dragger{size: Size{W: 10, H: 4}}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var published atomic.Int64
	unsub := Subscribe(h.app.Bus(), func(FocusEvent) { published.Add(1) })
	defer unsub()

	startDrag(t, h, owner.dragger, 2, 1)
	var focused, hovered NodeID
	h.onLoop(func() { focused, hovered = h.app.focus.focused, h.app.hover.hovered })
	if focused == 0 || hovered == 0 {
		t.Fatalf("precondition: focused %d, hovered %d: both must be held for their keeping to mean anything", focused, hovered)
	}
	heard, pub := owner.focusEvents.Load(), published.Load()

	h.inject(PointerCancelEvent{})
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if got := CaptureLostReason(owner.lastReason.Load()); got != CaptureLostCancelled {
		t.Errorf("loss reason = %v, want %v", got, CaptureLostCancelled)
	}
	var held, focusedAfter, hoveredAfter NodeID
	h.onLoop(func() {
		held, focusedAfter, hoveredAfter = h.app.capture.captureOwner, h.app.focus.focused, h.app.hover.hovered
	})
	if held != 0 {
		t.Errorf("capture still held by %d after a pointer cancel", held)
	}
	if focusedAfter != focused {
		t.Errorf("focus moved from %d to %d: a pointer cancel is not a focus change", focused, focusedAfter)
	}
	if hoveredAfter != hovered {
		t.Errorf("hover moved from %d to %d: the pointer is still where it was", hovered, hoveredAfter)
	}
	if n := owner.focusEvents.Load() - heard; n != 0 {
		t.Errorf("the focused node was sent %d FocusEvent(s): a pointer cancel is not a terminal focus loss", n)
	}
	if n := published.Load() - pub; n != 0 {
		t.Errorf("%d FocusEvent(s) published: a pointer cancel is not a terminal focus loss", n)
	}
	if got := owner.releases.Load(); got != 0 {
		t.Errorf("the owner was sent %d release(s): a cancel is no release", got)
	}

	// With no capture held, a second cancel does nothing at all.
	h.inject(PointerCancelEvent{})
	h.sync()
	if got := owner.losses.Load(); got != 1 {
		t.Errorf("a cancel with no capture held delivered a loss: %d in all, want 1", got)
	}
}
