package widgettest

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// Harness runs an App for a test of tui/widget or a package under it. It is deliberately
// minimal — App.Update is both the way onto the loop and the way to wait for
// it, so nothing else is needed — rather than a copy of the external harness,
// whose input injection and trace barriers no test here uses.
type Harness struct {
	T      *testing.T
	App    *tui.App
	TB     *tui.TestBackend
	Done   chan error
	Cancel context.CancelFunc
}

// StartApp runs root in an App on a w×h TestBackend, on a goroutine of its own, and returns once
// the loop has run once (Sync).
func StartApp(t *testing.T, root tui.Component, w, h int) *Harness {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	ih := &Harness{T: t, App: app, TB: tb, Done: make(chan error, 1), Cancel: cancel}
	go func() { ih.Done <- app.Run(ctx) }()
	ih.Sync()
	return ih
}

// Stop cancels the App and waits up to 5s for Run to return, failing the test if it does not.
func (h *Harness) Stop() {
	h.Cancel()
	select {
	case <-h.Done:
	case <-time.After(5 * time.Second):
		h.T.Error("app did not shut down within 5s")
	}
}

// onLoopInternal runs fn on the loop goroutine and waits for it.
func (h *Harness) OnLoop(fn func()) {
	h.T.Helper()
	done := make(chan struct{})
	h.App.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.T.Fatal("loop did not run Update within 3s")
	}
}

// syncInternal drains one round-trip through the loop. Two calls drain a bus
// publish and the work its subscriber enqueues, because Publish is enqueue-only.
func (h *Harness) Sync() { h.OnLoop(func() {}) }

// waitInternal polls cond on the loop: injected events and the loop's Updates are unordered.
func Wait(h *Harness, cond func() bool) bool {
	var ok bool
	for range 300 {
		h.OnLoop(func() { ok = cond() })
		if ok {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Log collects the values a bus subscriber received, safe from any goroutine: a test subscribes
// with a func that adds what it wants of each event, and reads them back with Values.
type Log[T any] struct {
	mu  sync.Mutex
	got []T
}

// Add records v.
func (l *Log[T]) Add(v T) { l.mu.Lock(); l.got = append(l.got, v); l.mu.Unlock() }

// Values is what was added, in order.
func (l *Log[T]) Values() []T {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.got)
}

// Panics reports whether fn panics.
func Panics(fn func()) (did bool) {
	defer func() { did = recover() != nil }()
	fn()
	return false
}
