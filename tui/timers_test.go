package tui

// Timer tests: demand-armed heap, After/Every, unmount cancellation, and
// the idle criterion — no goroutines/timers when idle (ADR-0005 §5.9).

import (
	"testing"
	"time"
)

func tickEvents(p *probe) []TickEvent {
	var out []TickEvent
	for _, ev := range p.recorded() {
		if e, ok := ev.(TickEvent); ok {
			out = append(out, e)
		}
	}
	return out
}

// TestAfterDeliversAddressedTick: Context.After posts one TickEvent
// addressed to the registrant, then the heap empties (demand-armed).
func TestAfterDeliversAddressedTick(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	child.onInit = func(_ *probe, ctx *Context) { ctx.After(10 * time.Millisecond) }
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2)

	waitFor(t, "one tick", func() bool { return len(tickEvents(child)) == 1 })
	tk := tickEvents(child)[0]
	if tk.Owner != child.nodeID() {
		t.Fatalf("tick Owner = %d, want %d", tk.Owner, child.nodeID())
	}
	var heapLen int
	var armed bool
	h.onLoop(func() { heapLen = len(h.app.timers); armed = h.app.timerC != nil })
	if heapLen != 0 || armed {
		t.Fatalf("after a one-shot fired: heap len %d, armed %v — want empty and disarmed (G5)", heapLen, armed)
	}
}

// TestEveryRepeatsAndCancelDisarms: Every re-arms fixed-delay after
// delivery; cancel empties the heap and disarms the timer.
func TestEveryRepeatsAndCancelDisarms(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	cancelc := make(chan func(), 1)
	child.onInit = func(_ *probe, ctx *Context) { cancelc <- ctx.Every(5 * time.Millisecond) }
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2)
	cancel := <-cancelc

	waitFor(t, "three ticks", func() bool { return len(tickEvents(child)) >= 3 })
	h.onLoop(cancel) // cancel is loop-goroutine-only
	n := len(tickEvents(child))
	time.Sleep(40 * time.Millisecond)
	if got := len(tickEvents(child)); got != n {
		t.Fatalf("ticks after cancel: %d → %d, want no growth", n, got)
	}
	var heapLen int
	var armed bool
	h.onLoop(func() { heapLen = len(h.app.timers); armed = h.app.timerC != nil })
	if heapLen != 0 || armed {
		t.Fatalf("after cancel: heap len %d, armed %v — want empty and disarmed", heapLen, armed)
	}
}

// TestTimerCancelledOnUnmount: unmount auto-cancels via OnUnmount
// (ADR-0005 §2.6; ADR-0004 §2.4 step 2).
func TestTimerCancelledOnUnmount(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	child.onInit = func(_ *probe, ctx *Context) { ctx.Every(5 * time.Millisecond) }
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2)

	waitFor(t, "first tick", func() bool { return len(tickEvents(child)) >= 1 })
	h.onLoop(func() { root.Remove(child) })
	var heapLen int
	h.onLoop(func() { heapLen = len(h.app.timers) })
	if heapLen != 0 {
		t.Fatalf("heap len after unmount = %d, want 0 (timer auto-cancelled)", heapLen)
	}
}

// TestIdleSchedulesNothing: ADR-0005 §5.9 — an app with no timers, no
// tasks, and no dirt performs zero timer arms and writes zero bytes over an
// observed idle window (TestBackend flush count + instrumented timer arm
// count + timer-heap introspection).
func TestIdleSchedulesNothing(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	// Default 16ms frame cap — idle must hold with the production config.
	tb := NewTestBackend(4, 2)
	h := runApp(t, NewApp(root, WithBackend(tb)), tb)

	h.sync()
	var arms int
	var armed bool
	h.onLoop(func() { arms = h.app.timerArms; armed = h.app.timerC != nil })
	flushes := h.tb.Flushes()

	time.Sleep(100 * time.Millisecond) // the observed idle window

	var arms2 int
	var armed2 bool
	var heapLen int
	h.onLoop(func() { arms2 = h.app.timerArms; armed2 = h.app.timerC != nil; heapLen = len(h.app.timers) })
	if arms2 != arms {
		t.Errorf("timer arms grew while idle: %d → %d", arms, arms2)
	}
	if armed || armed2 || heapLen != 0 {
		t.Errorf("idle app has an armed timer (before %v, after %v, heap %d) — want fully disarmed", armed, armed2, heapLen)
	}
	if got := h.tb.Flushes(); got != flushes {
		t.Errorf("flushes grew while idle: %d → %d (idle must write zero bytes)", flushes, got)
	}
}

// TestMinFrameIntervalCoalesces: dirty marks faster than the cap coalesce
// into one deferred frame via a heap deadline (ADR-0003 / ADR-0005 §2.6).
func TestMinFrameIntervalCoalesces(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	tb := NewTestBackend(4, 2)
	h := runApp(t, NewApp(root, WithBackend(tb), WithMinFrameInterval(50*time.Millisecond)), tb)

	base := h.tb.Flushes()
	for i := 0; i < 5; i++ {
		h.onLoop(func() { root.ctx.MarkDirty() })
	}
	waitFor(t, "the single coalesced frame", func() bool { return h.tb.Flushes() > base })
	time.Sleep(80 * time.Millisecond) // past another interval: no further dirt, no frame
	if got := h.tb.Flushes(); got > base+2 {
		t.Fatalf("flushes = %d after 5 rapid dirty marks (base %d) — want them coalesced (≤ base+2)", got, base)
	}
}
