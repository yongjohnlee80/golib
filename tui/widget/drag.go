package widget

import "github.com/yongjohnlee80/golib/tui"

// drag.go: one pointer gesture's lifecycle, shared by every widget a drag reshapes or scrolls (a
// Resizable's grip, a Split's divider, an Image's pan): begun with what a cancel restores, the
// pointer captured through the node that took it, released when the gesture ends or is cancelled,
// and only forgotten when the runtime revokes the capture. A revoked capture is not a cancellation:
// the state reached so far stands, since silently reverting the drag would be a change the user
// never made.

// pointerDrag is one gesture in progress: start is what the gesture began from (its origin, and what
// a cancel restores), ctx the node holding the pointer, nil before capture.
type pointerDrag[S any] struct {
	on    bool
	start S
	ctx   *tui.Context
}

// begin starts a gesture from start. The pointer is captured separately (capture), by whichever
// node's handler is running when the gesture is accepted.
func (d *pointerDrag[S]) begin(start S) { *d = pointerDrag[S]{on: true, start: start} }

// capture takes the pointer through ctx for the gesture, so motion and the release keep arriving
// once the pointer has left the widget. A nil ctx (no mount) captures nothing.
func (d *pointerDrag[S]) capture(ctx *tui.Context) {
	if d.on && ctx != nil {
		ctx.CapturePointer()
		d.ctx = ctx
	}
}

// end finishes the gesture and lets the pointer go, reporting what it began from; false when none
// was in progress. A cancel is end, then the caller restores from start.
func (d *pointerDrag[S]) end() (start S, ok bool) {
	if !d.on {
		return start, false
	}
	start = d.start
	if d.ctx != nil {
		d.ctx.ReleasePointer()
	}
	*d = pointerDrag[S]{}
	return start, true
}

// lost forgets the gesture after the runtime revoked its capture: nothing is released (the runtime
// already took the pointer) and nothing is restored.
func (d *pointerDrag[S]) lost() { *d = pointerDrag[S]{} }

// resizeGesture maps a primary-button pointer event onto the resize vocabulary a grip or a divider
// shares: a press on what grab accepts begins (at at, in the owner's frame), and while dragging,
// motion updates and the release ends. A non-primary release does not end a primary drag.
func resizeGesture(e tui.MouseEvent, at tui.Point, dragging bool, grab func() (Handle, bool)) (tui.Action, bool) {
	if e.Button != tui.MouseLeft && e.Kind != tui.MouseMotion {
		return nil, false
	}
	switch e.Kind {
	case tui.MousePress:
		if h, ok := grab(); ok {
			return ResizeBeginAction{Handle: h, At: at}, true
		}
	case tui.MouseMotion:
		if dragging {
			return ResizeUpdateAction{At: at}, true
		}
	case tui.MouseRelease:
		if dragging {
			return ResizeEndAction{}, true
		}
	}
	return nil, false
}

// stepCellsOf is how many cells one keyboard step of step moves along an axis of length cells: step
// itself in cells, or that percentage of the axis, never under one cell (a percentage of a narrow
// axis rounds to zero, and a keypress that provably cannot move anything is worse than a slow one).
func stepCellsOf(step int, unit StepUnit, cells int) int {
	if unit == StepPercent {
		return max(cells*step/100, 1)
	}
	return step
}
