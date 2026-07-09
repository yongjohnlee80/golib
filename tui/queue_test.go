package tui

// Lane-B (program queue) tests: never-block Post, always-enqueue Update,
// high-water logging, and the opt-in WithEventQueueLimit ceiling
// (ADR-0005 §5.2, §5.11).

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stallableRoot builds a probe whose handler blocks on the returned release
// channel when it sees the 's' key, signalling entry on entered.
func stallableRoot() (root *probe, entered chan struct{}, release chan struct{}) {
	entered = make(chan struct{}, 1)
	release = make(chan struct{})
	root = &probe{name: "root", pref: Size{W: 8, H: 2}}
	root.onEvent = func(_ *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 's' {
			entered <- struct{}{}
			<-release
			return true
		}
		return false
	}
	return root, entered, release
}

// stall injects the blocking key and waits for the handler to be inside it.
func stall(t *testing.T, h *harness, entered chan struct{}) {
	t.Helper()
	h.inject(keyEv('s'))
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("loop never entered the stalling handler")
	}
}

// TestPostConcurrentNeverBlocks: ADR-0005 §5.2 — 100 goroutines calling Post
// while the loop is blocked inside a slow handler: no call blocks, none
// panics, all events deliver after the handler returns, in enqueue order
// per producer.
func TestPostConcurrentNeverBlocks(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2)
	owner := root.nodeID()

	stall(t, h, entered)

	const producers, perProducer = 100, 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			<-start
			for s := 0; s < perProducer; s++ {
				h.app.Post(TaskProgress{Owner: owner, ID: TaskID(p), Value: s})
			}
		}(p)
	}
	close(start)
	posted := make(chan struct{})
	go func() { wg.Wait(); close(posted) }()
	select {
	case <-posted: // while the loop is still stalled — Post never blocked
	case <-time.After(3 * time.Second):
		t.Fatal("Post calls blocked while the loop was stalled")
	}

	close(release)
	waitFor(t, "all posts delivered", func() bool {
		return root.eventCount() >= 1+producers*perProducer
	})

	// Enqueue order per producer: each producer's sequence values ascend.
	last := make(map[TaskID]int)
	for _, ev := range root.recorded() {
		tp, ok := ev.(TaskProgress)
		if !ok {
			continue
		}
		seq := tp.Value.(int)
		if prev, seen := last[tp.ID]; seen && seq != prev+1 {
			t.Fatalf("producer %d out of order: %d after %d", tp.ID, seq, prev)
		}
		last[tp.ID] = seq
	}
	if len(last) != producers {
		t.Fatalf("saw %d producers, want %d", len(last), producers)
	}
}

// TestUpdateFromHandlerDeferred: ADR-0005 §5.2 rev 1 — Update called from
// inside a handler returns immediately without deadlock; its fn runs on the
// loop goroutine in a later drain, BEFORE the next frame.
func TestUpdateFromHandlerDeferred(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 8, H: 2}}
	h := startApp(t, root, 8, 2)
	loopGid := h.loopGid()
	flushesBefore := h.tb.Flushes()

	var handlerDone, fnAfterHandler atomic.Bool
	var fnFlushes, fnGid atomic.Int64
	done := make(chan struct{})

	root.onEvent = func(p *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'u' {
			h.app.Update(func() { // from inside the loop — must not deadlock
				fnAfterHandler.Store(handlerDone.Load())
				fnFlushes.Store(int64(h.tb.Flushes()))
				fnGid.Store(goid())
				close(done)
			})
			p.ctx.MarkDirty()
			handlerDone.Store(true)
			return true
		}
		return false
	}
	h.inject(keyEv('u'))
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Update fn never ran")
	}
	waitFor(t, "the frame after the drain", func() bool {
		return h.tb.Flushes() == flushesBefore+1
	})

	if !fnAfterHandler.Load() {
		t.Error("Update fn ran before the enqueuing handler returned (must be deferred — rev 1)")
	}
	if got := fnFlushes.Load(); got != int64(flushesBefore) {
		t.Errorf("Update fn saw %d flushes, want %d (fn must run before the next frame)", got, flushesBefore)
	}
	if fnGid.Load() != loopGid {
		t.Errorf("Update fn ran on goroutine %d, want loop %d", fnGid.Load(), loopGid)
	}
}

// TestEventQueueLimitPanics: ADR-0005 §5.11 — with WithEventQueueLimit(100)
// the 101st pending program event panics with the runaway-producer message;
// the identical load without the option only grows memory and logs.
func TestEventQueueLimitPanics(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2, WithEventQueueLimit(100))
	defer close(release)
	owner := root.nodeID()
	stall(t, h, entered)

	pending := h.app.queue.pending()
	for i := pending; i < 100; i++ {
		h.app.Post(TaskProgress{Owner: owner, Value: i}) // up to the ceiling: fine
	}
	func() {
		defer func() {
			rec := recover()
			if rec == nil {
				t.Fatal("101st pending program event did not panic")
			}
			want := "tui: program event queue exceeded 100 — runaway producer"
			if msg, _ := rec.(string); msg != want {
				t.Fatalf("panic %q, want %q", rec, want)
			}
		}()
		h.app.Post(TaskProgress{Owner: owner, Value: 100})
	}()
}

// TestQueueHighWaterLogging: ADR-0005 §5.11 — driving lane B to a high
// pending count while the loop is stalled emits high-water-mark log entries
// via WithLogger.
func TestQueueHighWaterLogging(t *testing.T) {
	t.Parallel()
	var lc logCapture
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2, WithLogger(lc.logger()))
	owner := root.nodeID()
	stall(t, h, entered)

	for i := 0; i < programQueueHighWaterStart+10; i++ {
		h.app.Post(TaskProgress{Owner: owner, Value: i}) // no limit: grows + logs only
	}
	close(release)
	h.sync()
	if !lc.has("high water") {
		t.Fatalf("no high-water log entry observed; entries: %+v", lc.entries)
	}
}

// TestPostNilPanics pins the nil-argument contract.
func TestPostNilPanics(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	for _, tc := range []struct {
		name string
		fn   func()
	}{
		{"nil event", func() { h.app.Post(nil) }},
		{"nil update", func() { h.app.Update(nil) }},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", tc.name)
				}
			}()
			tc.fn()
		}()
	}
}
