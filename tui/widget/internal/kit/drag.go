package kit

import "github.com/yongjohnlee80/golib/tui"

// PointerDrag is one gesture in progress: start is what the gesture began from (its origin, and what
// a cancel restores), ctx the node holding the pointer, nil before capture.
type PointerDrag[S any] struct {
	On    bool
	Start S
	ctx   *tui.Context
}

// Begin starts a gesture from start. The pointer is captured separately (capture), by whichever
// node's handler is running when the gesture is accepted.
func (d *PointerDrag[S]) Begin(start S) { *d = PointerDrag[S]{On: true, Start: start} }

// Capture takes the pointer through ctx for the gesture, so motion and the release keep arriving
// once the pointer has left the widget. A nil ctx (no mount) captures nothing.
func (d *PointerDrag[S]) Capture(ctx *tui.Context) {
	if d.On && ctx != nil {
		ctx.CapturePointer()
		d.ctx = ctx
	}
}

// End finishes the gesture and lets the pointer go, reporting what it began from; false when none
// was in progress. A cancel is end, then the caller restores from start.
func (d *PointerDrag[S]) End() (start S, ok bool) {
	if !d.On {
		return start, false
	}
	start = d.Start
	if d.ctx != nil {
		d.ctx.ReleasePointer()
	}
	*d = PointerDrag[S]{}
	return start, true
}

// Lost forgets the gesture after the runtime revoked its capture: nothing is released (the runtime
// already took the pointer) and nothing is restored.
func (d *PointerDrag[S]) Lost() { *d = PointerDrag[S]{} }
