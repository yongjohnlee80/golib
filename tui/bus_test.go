package tui

// Bus tests: typed dispatch, subscription order, loop-goroutine delivery,
// enqueue-only publish, scoped auto-unsubscribe, idempotent cancel.

import (
	"sync/atomic"
	"testing"
	"time"
)

type fooMsg struct{ n int }
type barMsg struct{ s string }

// TestBusTypedDispatch: Subscribe[Foo] receives only Foo publishes, on the
// loop goroutine, in subscription order; Publish from a non-loop goroutine
// returns before the handler runs.
func TestBusTypedDispatch(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	loopGid := h.loopGid()

	log := &callLog{}
	var handlerGid atomic.Int64

	h.onLoop(func() {
		Subscribe(h.app.Bus(), func(m fooMsg) {
			handlerGid.Store(goid())
			log.add("foo1")
		})
		Subscribe(h.app.Bus(), func(fooMsg) { log.add("foo2") })
		Subscribe(h.app.Bus(), func(barMsg) { log.add("bar") })
	})

	h.app.Bus().Publish(fooMsg{n: 1}) // non-loop goroutine
	h.sync()

	got := log.get()
	if len(got) != 2 || got[0] != "foo1" || got[1] != "foo2" {
		t.Fatalf("deliveries = %v, want [foo1 foo2] (typed, in subscription order)", got)
	}
	if handlerGid.Load() != loopGid {
		t.Fatalf("handler ran on goroutine %d, want loop %d", handlerGid.Load(), loopGid)
	}
}

// TestBusPublishEnqueueOnly: Publish never invokes handlers on the caller's
// goroutine — even when the loop is stalled, Publish returns immediately.
func TestBusPublishEnqueueOnly(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2)
	var delivered atomic.Bool
	h.onLoop(func() {
		Subscribe(h.app.Bus(), func(fooMsg) { delivered.Store(true) })
	})
	stall(t, h, entered)

	done := make(chan struct{})
	go func() {
		h.app.Bus().Publish(fooMsg{n: 7})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked while the loop was stalled")
	}
	if delivered.Load() {
		t.Fatal("handler ran on the publisher's goroutine (must be enqueue-only)")
	}
	close(release)
	waitFor(t, "delivery after drain", func() bool { return delivered.Load() })
}

// TestSubscribeScopedAutoUnsubscribe: SubscribeScoped stops
// after owner unmount; cancel is idempotent.
func TestSubscribeScopedAutoUnsubscribe(t *testing.T) {
	t.Parallel()
	var count atomic.Int64
	var cancel func()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	child.onInit = func(_ *probe, ctx *Context) {
		cancel = SubscribeScoped(ctx, func(fooMsg) { count.Add(1) })
	}
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2)

	h.app.Bus().Publish(fooMsg{n: 1})
	h.sync()
	if count.Load() != 1 {
		t.Fatalf("count = %d, want 1 before unmount", count.Load())
	}

	h.onLoop(func() { root.Remove(child) }) // unmount → auto-unsubscribe
	h.app.Bus().Publish(fooMsg{n: 2})
	h.sync()
	if count.Load() != 1 {
		t.Fatalf("count = %d, want 1 after unmount (subscription must die with the node)", count.Load())
	}

	cancel() // idempotent: already cancelled by the unmount hook
	cancel()
}

// TestBusCancelDuringDeliveryTombstones: a handler cancelled during delivery
// of the same batch is skipped.
func TestBusCancelDuringDeliveryTombstones(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)

	log := &callLog{}
	var cancel2 func()
	h.onLoop(func() {
		Subscribe(h.app.Bus(), func(fooMsg) {
			log.add("first")
			cancel2()
		})
		cancel2 = Subscribe(h.app.Bus(), func(fooMsg) { log.add("second") })
	})
	h.app.Bus().Publish(fooMsg{n: 1})
	h.sync()
	if got := log.get(); len(got) != 1 || got[0] != "first" {
		t.Fatalf("deliveries = %v, want [first] (second tombstoned mid-batch)", got)
	}
}

// TestBusPublishFromHandler: publish-during-drain delivers in a later drain
// — no livelock, no re-entrancy.
func TestBusPublishFromHandler(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	var chained atomic.Bool
	h.onLoop(func() {
		Subscribe(h.app.Bus(), func(m fooMsg) {
			if m.n == 1 {
				h.app.Bus().Publish(fooMsg{n: 2})
			}
			if m.n == 2 {
				chained.Store(true)
			}
		})
	})
	h.app.Bus().Publish(fooMsg{n: 1})
	waitFor(t, "chained publish delivery", func() bool { return chained.Load() })
}
