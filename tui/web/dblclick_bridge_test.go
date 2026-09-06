package web

// The DOUBLE-CLICK contract, proven against this
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

// Two real client reports on the same cell arrive as Count 1 then 2.
func TestWebDoubleClickCarriesTheOrdinal(t *testing.T) {
	rec := &countRecorder{}
	send, _ := runDecoded(t, rec, 10, 4, MouseReport{Kind: "down", Button: 0, X: 1, Y: 2})
	send(MouseReport{Kind: "down", Button: 0, X: 1, Y: 2})

	waitFor(t, func() bool { return len(rec.snapshot()) >= 2 }, "two presses delivered")
	got := rec.snapshot()
	if len(got) < 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("press ordinals from real client reports = %v, want [1 2]", got)
	}
}

// A second report on a DIFFERENT cell does not continue the run.
func TestWebPressesOnDifferentCellsDoNotDouble(t *testing.T) {
	rec := &countRecorder{}
	send, _ := runDecoded(t, rec, 10, 4, MouseReport{Kind: "down", Button: 0, X: 1, Y: 2})
	send(MouseReport{Kind: "down", Button: 0, X: 1, Y: 3})

	waitFor(t, func() bool { return len(rec.snapshot()) >= 2 }, "two presses delivered")
	got := rec.snapshot()
	if len(got) < 2 || got[0] != 1 || got[1] != 1 {
		t.Errorf("press ordinals = %v, want [1 1]", got)
	}
}
