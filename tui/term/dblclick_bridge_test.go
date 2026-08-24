package term

// Criterion 28 (ADR-0010 §2.5): the DOUBLE-CLICK contract proven against this
// real producer.
//
// Neither producer computes a click count — the ordinal is synthesised once in
// App.dispatch. That is exactly why it needs a behavioural bridge per producer:
// a test that injects events straight into a TestBackend would pass even if this
// package's decode path never reached dispatch at all.

import (
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// countRecorder records the Count of every press it is delivered.
type countRecorder struct {
	widget.Base
	mu     sync.Mutex
	counts []int
}

func (c *countRecorder) Layout(cs tui.Constraints) tui.Size {
	return cs.Constrain(tui.Size{W: 10, H: 4})
}
func (c *countRecorder) Render(tui.Surface) {}
func (c *countRecorder) AcceptsFocus() bool { return true }

func (c *countRecorder) HandleEvent(ev tui.Event) bool {
	if m, ok := ev.(tui.MouseEvent); ok && m.Kind == tui.MousePress {
		c.mu.Lock()
		c.counts = append(c.counts, m.Count)
		c.mu.Unlock()
	}
	return true
}

func (c *countRecorder) snapshot() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.counts...)
}

// Two real SGR presses on the same cell arrive as Count 1 then 2.
func TestSGRDoubleClickCarriesTheOrdinal(t *testing.T) {
	rec := &countRecorder{}
	send, _ := runSGR(t, rec, 10, 4, "\x1b[<0;2;3M")
	send("\x1b[<0;2;3M")

	waitLoop(t, "two presses delivered", func() bool { return len(rec.snapshot()) >= 2 })
	got := rec.snapshot()
	if len(got) < 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("press ordinals from real SGR bytes = %v, want [1 2]", got)
	}
}

// A second press on a DIFFERENT cell, through the same real decode path, does
// not continue the run.
func TestSGRPressesOnDifferentCellsDoNotDouble(t *testing.T) {
	rec := &countRecorder{}
	send, _ := runSGR(t, rec, 10, 4, "\x1b[<0;2;3M")
	send("\x1b[<0;2;4M") // one row down

	waitLoop(t, "two presses delivered", func() bool { return len(rec.snapshot()) >= 2 })
	got := rec.snapshot()
	if len(got) < 2 || got[0] != 1 || got[1] != 1 {
		t.Errorf("press ordinals = %v, want [1 1]", got)
	}
}
