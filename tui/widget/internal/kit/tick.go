package kit

import (
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// Ticker is a widget's repeating timer (an Image's spinner, a ProgressBar's animation, the Toasts'
// clock): started once while it is wanted, stopped when it is not, and forgotten across a remount,
// since a timer is its Context's and a new mount starts with none. Its ticks are told apart from
// another node's by TickOf.
type Ticker struct{ stop func() }

// Start runs the timer every every on ctx, unless it runs already or there is no mount to run it on.
func (t *Ticker) Start(ctx *tui.Context, every time.Duration) {
	if t.stop == nil && ctx != nil {
		t.stop = ctx.Every(every)
	}
}

// Halt stops the timer, if it runs.
func (t *Ticker) Halt() {
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
}

// Forget drops a timer of a previous mount without stopping it: its Context stopped it.
func (t *Ticker) Forget() { t.stop = nil }

// TickOf reports whether ev is a tick of node id's own timer.
func TickOf(ev tui.Event, id tui.NodeID) bool {
	te, ok := ev.(tui.TickEvent)
	return ok && te.Owner == id
}
