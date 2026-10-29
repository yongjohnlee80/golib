package tui

// Latest-wins lane-B tests: App.UpdateLatest and Bus.PublishLatest keep one slot per pending key,
// in the first call's place, with the last call's fn or value; a call after the drain, a handler's
// in the batch included, queues anew for the next batch.

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// runLog records, in order, what ran on the loop.
type runLog struct {
	mu  sync.Mutex
	ran []string
}

func (l *runLog) add(s string) { l.mu.Lock(); l.ran = append(l.ran, s); l.mu.Unlock() }

func (l *runLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ran...)
}

// TestUpdateLatestKeepsOneSlotPerKey: against a stalled loop, ten thousand calls on each of two
// keys add two closures to the lane, where Update adds one a call; released, each key runs once,
// with its last fn, as an Update, in the order the keys were first called.
func TestUpdateLatestKeepsOneSlotPerKey(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2)
	stall(t, h, entered)

	var log runLog
	before := h.app.queue.pending()
	const calls = 10000
	for i := range calls {
		for _, k := range []string{"a", "b"} {
			h.app.UpdateLatest(k, func() {
				log.add(fmt.Sprintf("%s%d update=%v", k, i, h.app.phase.updateDepth > 0))
			})
		}
	}
	if got := h.app.queue.pending() - before; got != 2 {
		t.Fatalf("%d calls on two keys added %d closures, want 2", 2*calls, got)
	}
	for range calls {
		h.app.Update(func() {})
	}
	if got := h.app.queue.pending() - before; got != 2+calls {
		t.Fatalf("Update added %d closures for %d calls, want %d (the comparison)", got-2, calls, calls)
	}
	close(release)
	h.sync()
	want := []string{fmt.Sprintf("a%d update=true", calls-1), fmt.Sprintf("b%d update=true", calls-1)}
	if got := log.get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ran %v, want %v", got, want)
	}
}

// TestUpdateLatestRunsInTheFirstCallsPlace: the ordering it gives up, documented. A key's fn runs
// where its first pending call was queued, so an Update queued between two calls for the key runs
// after the key's last fn.
func TestUpdateLatestRunsInTheFirstCallsPlace(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2)
	stall(t, h, entered)

	var log runLog
	h.app.UpdateLatest("k", func() { log.add("k first") })
	h.app.Update(func() { log.add("between") })
	h.app.UpdateLatest("k", func() { log.add("k last") })
	close(release)
	h.sync()
	if got, want := log.get(), []string{"k last", "between"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ran %v, want %v", got, want)
	}
}

// TestUpdateLatestFromItsOwnBatchQueuesForTheNext: a key's fn that calls UpdateLatest for its own
// key while its batch runs queues a new closure, which runs in the next drain: after the rest of
// the batch, and not lost into the slot already taken.
func TestUpdateLatestFromItsOwnBatchQueuesForTheNext(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2)
	stall(t, h, entered)

	var log runLog
	h.app.UpdateLatest("k", func() {
		log.add("k")
		h.app.UpdateLatest("k", func() { log.add("k again") })
	})
	h.app.Update(func() { log.add("rest of the batch") })
	close(release)
	h.sync()
	h.sync() // the next drain
	if got, want := log.get(), []string{"k", "rest of the batch", "k again"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ran %v, want %v", got, want)
	}
}

// TestLatestCallsAreNotCountedTowardTheLimit: a replacing call adds no closure, so with the lane
// at WithEventQueueLimit, more calls for a pending key do not panic; a new key's first call, a
// closure past the limit, still does. PublishLatest's "k" is a new key: its keys are apart.
func TestLatestCallsAreNotCountedTowardTheLimit(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2, WithEventQueueLimit(20))
	defer close(release)
	stall(t, h, entered)
	panics := func(fn func()) (p bool) {
		defer func() { p = recover() != nil }()
		fn()
		return false
	}

	for i := h.app.queue.pending(); i < 19; i++ {
		h.app.Update(func() {})
	}
	if panics(func() { h.app.UpdateLatest("k", func() {}) }) {
		t.Fatal("the 20th closure, at the limit, panicked")
	}
	if panics(func() {
		for range 1000 {
			h.app.UpdateLatest("k", func() {})
		}
	}) {
		t.Fatal("a replacing call at the limit panicked: it was counted")
	}
	if !panics(func() { h.app.Bus().PublishLatest("k", fooMsg{}) }) {
		t.Fatal("a new key's first call past the limit did not panic")
	}
}

// TestPublishLatestDeliversTheLastValue: against a stalled loop, a thousand PublishLatest calls
// under one key add one delivery, and subscribers get the last value once. Its keys are apart from
// UpdateLatest's, and a key's last value is delivered to that value's type's subscribers alone.
func TestPublishLatestDeliversTheLastValue(t *testing.T) {
	t.Parallel()
	root, entered, release := stallableRoot()
	h := startApp(t, root, 8, 2)
	var log runLog
	h.onLoop(func() {
		Subscribe(h.app.Bus(), func(m fooMsg) { log.add(fmt.Sprintf("foo %d", m.n)) })
		Subscribe(h.app.Bus(), func(barMsg) { log.add("bar") })
	})
	stall(t, h, entered)

	before := h.app.queue.pending()
	for i := range 1000 {
		h.app.Bus().PublishLatest("p", fooMsg{n: i})
	}
	h.app.UpdateLatest("p", func() { log.add("update p") }) // UpdateLatest's "p" is another key
	h.app.Bus().PublishLatest("q", fooMsg{n: 1})
	h.app.Bus().PublishLatest("q", barMsg{}) // the last value under q is a barMsg
	if got := h.app.queue.pending() - before; got != 3 {
		t.Fatalf("closures added: %d, want 3 (p published, p updated, q)", got)
	}
	close(release)
	h.sync()
	if got, want := log.get(), []string{"foo 999", "update p", "bar"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
}

// TestLatestRefusesKeysItCannotHold: a nil key, an uncomparable one (a slice, or a struct holding
// one), a nil fn and a nil value panic at the call, with errs.Fatal, before anything is queued.
func TestLatestRefusesKeysItCannotHold(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	type holder struct{ s []int }
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"update, nil key", func() { h.app.UpdateLatest(nil, func() {}) }},
		{"update, slice key", func() { h.app.UpdateLatest([]int{1}, func() {}) }},
		{"update, struct key holding a slice", func() { h.app.UpdateLatest(holder{}, func() {}) }},
		{"update, nil fn", func() { h.app.UpdateLatest("k", nil) }},
		{"publish, nil key", func() { h.app.Bus().PublishLatest(nil, fooMsg{}) }},
		{"publish, map key", func() { h.app.Bus().PublishLatest(map[int]int{}, fooMsg{}) }},
		{"publish, nil value", func() { h.app.Bus().PublishLatest("k", nil) }},
	} {
		before := h.app.queue.pending()
		func() {
			defer func() {
				if _, ok := recover().(errs.Fatal); !ok {
					t.Errorf("%s: did not panic with errs.Fatal", tc.name)
				}
			}()
			tc.call()
		}()
		if got := h.app.queue.pending(); got != before {
			t.Errorf("%s: queued %d closures before refusing", tc.name, got-before)
		}
	}
}
