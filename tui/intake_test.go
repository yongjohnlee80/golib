package tui

// Lane-A intake tests: promptness, drop-oldest, resize latest-wins, motion
// coalescing.

import (
	"testing"
	"time"
)

// TestIntakePromptness: TestBackend injection during
// a slow handler never stalls the backend side: the intake stage keeps
// Events() drained while the loop is busy. The backend buffer (4) is far
// smaller than the flood (30); only a promptly draining intake lets every
// Inject eventually succeed while the loop is still stalled.
func TestIntakePromptness(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	tb := NewTestBackend(8, 2, WithTestEventBuffer(4))
	app := NewApp(root,
		WithBackend(tb), WithMinFrameInterval(0), WithInputQueueSize(8))
	h := runApp(t, app, tb)
	stall(t, h, entered)

	for i := 0; i < 30; i++ {
		ev := KeyEvent{Kind: KeyPress, Code: rune(0x3000 + i)}
		deadline := time.Now().Add(3 * time.Second)
		for {
			if err := tb.Inject(ev); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("backend buffer stayed full — intake is not draining Events() during a slow handler")
			}
			time.Sleep(time.Millisecond)
		}
	}
	// Quiesce: intake has consumed the whole flood (the drop counter is
	// only reachable with the loop still stalled — promptness proven).
	waitFor(t, "intake drained the flood while stalled", func() bool {
		return h.app.inputDrops.Load() == 30-8
	})
	close(release)
	// The newest 8 keys (plus the stalling key) survive the 8-cap lane.
	waitFor(t, "flood tail delivered", func() bool { return root.eventCount() == 1+8 })
}

// TestIntakeDropOldest: lane-A overflow drops the oldest
// events, keeping the newest, with drops counted.
func TestIntakeDropOldest(t *testing.T) {
	t.Parallel()
	const capacity = 8
	const flood = 50
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2, WithInputQueueSize(capacity))
	stall(t, h, entered)

	// Flood while the loop is stalled; each key carries its sequence.
	for i := 0; i < flood; i++ {
		h.inject(KeyEvent{Kind: KeyPress, Code: rune(0x1000 + i)})
	}
	// Quiesce: wait for intake to consume the whole flood (observable via
	// the drop counter) BEFORE releasing the loop, so the drop pattern is
	// deterministic.
	waitFor(t, "intake consumed the flood", func() bool {
		return h.app.inputDrops.Load() == flood-capacity
	})
	close(release)
	// Delivered: the stalling key + the newest `capacity` flood keys; the
	// oldest flood keys were dropped.
	waitFor(t, "flood tail delivered", func() bool {
		return root.eventCount() == 1+capacity
	})
	got := root.recorded()[1:]
	for i, ev := range got {
		want := rune(0x1000 + flood - capacity + i)
		k, ok := ev.(KeyEvent)
		if !ok || k.Code != want {
			t.Fatalf("delivered[%d] = %#v, want key %#x (newest %d keys, in order)", i, ev, want, capacity)
		}
	}
	if drops := h.app.inputDrops.Load(); drops != flood-capacity {
		t.Fatalf("inputDrops = %d, want %d", drops, flood-capacity)
	}
}

// TestIntakeKeysNeverDroppedBelowCapacity: no KeyEvent is
// ever dropped below lane-A capacity.
func TestIntakeKeysNeverDroppedBelowCapacity(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2) // default capacity 256
	stall(t, h, entered)
	const n = 100
	for i := 0; i < n; i++ {
		h.inject(KeyEvent{Kind: KeyPress, Code: rune(0x2000 + i)})
	}
	close(release)
	waitFor(t, "all keys delivered", func() bool { return root.eventCount() == 1+n })
	for i, ev := range root.recorded()[1:] {
		if k, ok := ev.(KeyEvent); !ok || k.Code != rune(0x2000+i) {
			t.Fatalf("delivered[%d] = %#v, want key %#x", i, ev, 0x2000+i)
		}
	}
	if drops := h.app.inputDrops.Load(); drops != 0 {
		t.Fatalf("inputDrops = %d, want 0", drops)
	}
}

// TestResizeLatestWins: a burst of resizes yields exactly
// one delivery carrying the final size (the atomic slot, never a queue of
// sizes).
func TestResizeLatestWins(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	var resizes []ResizeEvent
	resizeSeen := make(chan struct{}, 64)
	root.onInit = func(_ *probe, ctx *Context) {
		SubscribeScoped(ctx, func(e ResizeEvent) {
			resizes = append(resizes, e) // loop goroutine
			resizeSeen <- struct{}{}
		})
	}
	h := startApp(t, root, 8, 2)
	stall(t, h, entered)

	for w := 10; w <= 40; w++ { // resize storm while the loop is stalled
		h.tb.InjectResize(w, 3)
	}
	// Quiesce: intake must have absorbed the whole storm into the slot
	// before the loop resumes (Events() drained → channel empty).
	waitFor(t, "intake absorbed the storm", func() bool { return len(h.tb.events) == 0 })
	close(release)
	waitFor(t, "one coalesced resize", func() bool { return len(resizeSeen) >= 1 })
	h.sync() // drain any (unexpected) stragglers
	h.sync()

	var got []ResizeEvent
	h.onLoop(func() { got = append([]ResizeEvent(nil), resizes...) })
	if len(got) != 1 {
		t.Fatalf("resize deliveries = %d (%+v), want exactly 1", len(got), got)
	}
	if got[0] != (ResizeEvent{W: 40, H: 3}) {
		t.Fatalf("delivered resize = %+v, want the final {40 3}", got[0])
	}
}

// TestMotionCoalescing: consecutive mouse motions collapse
// to the newest; non-consecutive runs are preserved around keys.
func TestMotionCoalescing(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2)
	stall(t, h, entered)

	motion := func(x int) MouseEvent { return MouseEvent{Kind: MouseMotion, X: x, Y: 0} }
	h.inject(motion(1), motion(2), motion(3), keyEv('k'), motion(4), motion(5))
	// Quiesce: intake must coalesce the whole script before the loop
	// resumes, or the collapse points become racy.
	waitFor(t, "intake absorbed the script", func() bool { return len(h.tb.events) == 0 })
	close(release)

	waitFor(t, "coalesced sequence", func() bool { return root.eventCount() == 1+3 })
	got := root.recorded()[1:]
	if m, ok := got[0].(MouseEvent); !ok || m.Kind != MouseMotion || m.X != 3 {
		t.Fatalf("got[0] = %#v, want motion x=3 (run collapsed to newest)", got[0])
	}
	if k, ok := got[1].(KeyEvent); !ok || k.Code != 'k' {
		t.Fatalf("got[1] = %#v, want key 'k'", got[1])
	}
	if m, ok := got[2].(MouseEvent); !ok || m.Kind != MouseMotion || m.X != 5 {
		t.Fatalf("got[2] = %#v, want motion x=5", got[2])
	}
}
