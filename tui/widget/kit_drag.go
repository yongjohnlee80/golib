package widget

import "github.com/yongjohnlee80/golib/tui"

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
