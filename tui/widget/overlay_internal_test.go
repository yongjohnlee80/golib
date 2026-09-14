package widget

// The overlay open/close protocol is an UNEXPORTED bus handshake, so these
// tests live inside the package: a consumer cannot publish these events, which
// is exactly why nothing outside can prove they are handled safely.

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// internalHarness runs an App from INSIDE the package. It is deliberately
// minimal — App.Update is both the way onto the loop and the way to wait for
// it, so nothing else is needed — rather than a copy of the external harness,
// whose input injection and trace barriers no test here uses.
type internalHarness struct {
	t   *testing.T
	app *tui.App
	res chan error
	cxl context.CancelFunc
}

func startAppInternal(t *testing.T, root tui.Component, w, h int) *internalHarness {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	ih := &internalHarness{t: t, app: app, res: make(chan error, 1), cxl: cancel}
	go func() { ih.res <- app.Run(ctx) }()
	ih.syncInternal()
	return ih
}

func (h *internalHarness) stopInternal() {
	h.cxl()
	select {
	case <-h.res:
	case <-time.After(5 * time.Second):
		h.t.Error("app did not shut down within 5s")
	}
}

// onLoopInternal runs fn on the loop goroutine and waits for it.
func (h *internalHarness) onLoopInternal(fn func()) {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatal("loop did not run Update within 3s")
	}
}

// syncInternal drains one round-trip through the loop. Two calls drain a bus
// publish and the work its subscriber enqueues, because Publish is enqueue-only.
func (h *internalHarness) syncInternal() { h.onLoopInternal(func() {}) }

// TestADuplicateOverlayOpenIsANoOpRatherThanACrash.
//
// A duplicate open is an ordinary consequence of double activation — two clicks,
// a key and a click, a re-entrant handler — and the runtime refuses to mount one
// component twice by PANICKING. Without idempotence here the application dies
// for something the caller could only have prevented by tracking mount state the
// runtime owns.
func TestADuplicateOverlayOpenIsANoOpRatherThanACrash(t *testing.T) {
	base := NewButton("base")
	host := NewOverlayHost(base)
	h := startAppInternal(t, host, 20, 6)
	defer h.stopInternal()

	layer := NewText("popup")
	h.onLoopInternal(func() {
		host.ctx.Bus().Publish(overlayOpenEvent{layer: layer})
		host.ctx.Bus().Publish(overlayOpenEvent{layer: layer})
	})
	h.syncInternal()
	h.syncInternal()

	if got := countLayers(host, layer); got != 1 {
		t.Errorf("the popup is mounted %d times, want exactly 1", got)
	}

	// The mirror: closing twice must also be a no-op.
	h.onLoopInternal(func() {
		host.ctx.Bus().Publish(overlayCloseEvent{layer: layer})
		host.ctx.Bus().Publish(overlayCloseEvent{layer: layer})
	})
	h.syncInternal()
	h.syncInternal()

	if got := countLayers(host, layer); got != 0 {
		t.Errorf("the popup is still mounted %d times after two closes", got)
	}

	// And a close for something that was never open changes nothing.
	before := host.Stack.Len()
	h.onLoopInternal(func() {
		host.ctx.Bus().Publish(overlayCloseEvent{layer: NewText("never opened")})
	})
	h.syncInternal()
	h.syncInternal()
	if got := host.Stack.Len(); got != before {
		t.Errorf("closing an unopened layer changed the stack: %d -> %d", before, got)
	}
}

// countLayers reports how many times c appears among the host's layers. It
// counts rather than reporting presence, because the defect being excluded is a
// SECOND copy, which a boolean cannot see.
func countLayers(h *OverlayHost, c tui.Component) int {
	n := 0
	for _, layer := range h.Stack.All() {
		if layer == c {
			n++
		}
	}
	return n
}
