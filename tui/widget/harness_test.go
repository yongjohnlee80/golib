package widget_test

// Shared TestBackend harness for the widget contract suites (ADR-0007 §5;
// ADR-0001 §5.3): drives a real App over tui.TestBackend — no PTY — with
// deterministic waits keyed on observable state (flush counts, grid
// contents, recorded Bus events).

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// harness runs an App on a TestBackend in a background goroutine.
type harness struct {
	t   *testing.T
	app *tui.App
	tb  *tui.TestBackend

	cancel   context.CancelFunc
	resc     chan error
	stopOnce sync.Once
}

// startApp builds and runs an App over a fresh TestBackend. The frame cap
// is disabled (WithMinFrameInterval(0)) for deterministic
// one-flush-per-change assertions.
func startApp(t *testing.T, root tui.Component, w, h int) *harness {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	h2 := &harness{t: t, app: app, tb: tb, cancel: cancel, resc: make(chan error, 1)}
	go func() { h2.resc <- app.Run(ctx) }()
	h2.sync()
	t.Cleanup(func() {
		tui.FailOnViolations(t, tb)
		h2.stop()
	})
	return h2
}

// stop cancels Run and waits for it.
func (h *harness) stop() {
	h.stopOnce.Do(func() {
		h.cancel()
		select {
		case <-h.resc:
		case <-time.After(5 * time.Second):
			h.t.Errorf("app did not shut down within 5s")
		}
	})
}

// sync round-trips an Update through the loop: every previously enqueued
// program-lane item has drained when it returns.
func (h *harness) sync() {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() { close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("loop did not respond to Update within 3s")
	}
}

// onLoop runs fn on the loop goroutine and waits — the sanctioned way for
// tests to touch loop-owned widget state (ADR-0005 §2.3).
func (h *harness) onLoop(fn func()) {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("loop did not run Update within 3s")
	}
}

// inject scripts backend events (raw — no waiting; pair with a barrier,
// waitFor, or settle).
func (h *harness) inject(evs ...tui.Event) {
	h.t.Helper()
	if err := h.tb.Inject(evs...); err != nil {
		h.t.Fatalf("inject: %v", err)
	}
}

// settle waits for flush quiescence: no new Flush arrives across a
// double program-lane round-trip plus a small grace window. Use it before
// taking flush-count baselines.
func (h *harness) settle() {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.sync()
		h.sync()
		f := h.tb.Flushes()
		time.Sleep(3 * time.Millisecond)
		h.sync()
		if h.tb.Flushes() == f {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("frame activity did not settle within 3s")
		}
	}
}

// waitFor polls cond until it holds or the deadline expires.
func (h *harness) waitFor(desc string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", desc)
}

// grid returns the current grid text.
func (h *harness) grid() string { return h.tb.String() }

// row returns row y of the grid.
func (h *harness) row(y int) string {
	rows := strings.Split(h.grid(), "\n")
	if y < 0 || y >= len(rows) {
		h.t.Fatalf("row %d outside the %d-row grid", y, len(rows))
	}
	return rows[y]
}

// wantContains asserts the grid contains sub.
func (h *harness) wantContains(sub string) {
	h.t.Helper()
	if !strings.Contains(h.grid(), sub) {
		h.t.Fatalf("grid does not contain %q:\n%s", sub, h.grid())
	}
}

// wantNotContains asserts the grid does not contain sub.
func (h *harness) wantNotContains(sub string) {
	h.t.Helper()
	if strings.Contains(h.grid(), sub) {
		h.t.Fatalf("grid unexpectedly contains %q:\n%s", sub, h.grid())
	}
}

// --- event builders ---

// key builds a plain key press; printable codes carry their text.
func key(code rune) tui.KeyEvent {
	text := ""
	if code >= 0x20 && code < 0xE000 {
		text = string(code)
	}
	return tui.KeyEvent{Kind: tui.KeyPress, Code: code, Text: text}
}

// keyMod builds a modified key press (no text — modifier chords).
func keyMod(code rune, mods tui.Mods) tui.KeyEvent {
	return tui.KeyEvent{Kind: tui.KeyPress, Code: code, Mods: mods}
}

// keyShift builds a Shift-modified key press.
func keyShift(code rune) tui.KeyEvent { return keyMod(code, tui.ModShift) }

// tab and shiftTab drive framework focus traversal.
func tab() tui.KeyEvent      { return key(tui.KeyTab) }
func shiftTab() tui.KeyEvent { return keyMod(tui.KeyTab, tui.ModShift) }

// typeString yields one key press per rune.
func typeString(s string) []tui.Event {
	evs := make([]tui.Event, 0, len(s))
	for _, r := range s {
		evs = append(evs, key(r))
	}
	return evs
}

// click builds a left mouse press at absolute (x, y).
func click(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y}
}

// --- Bus event recorder ---

// recorder captures Bus events of type T in publish order.
type recorder[T any] struct {
	mu  sync.Mutex
	got []T
}

// record subscribes a recorder for T on the app's bus.
func record[T any](h *harness) *recorder[T] {
	r := &recorder[T]{}
	tui.Subscribe(h.app.Bus(), func(v T) {
		r.mu.Lock()
		r.got = append(r.got, v)
		r.mu.Unlock()
	})
	return r
}

// events returns a copy of the captured events.
func (r *recorder[T]) events() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]T(nil), r.got...)
}

func (r *recorder[T]) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *recorder[T]) last() (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) == 0 {
		var zero T
		return zero, false
	}
	return r.got[len(r.got)-1], true
}

// --- consumption probe ---

// shell wraps a child and records every event that bubbles up unconsumed —
// the "keys consumed vs bubbled" assertion hook (ADR-0007 §5.3a).
type shell struct {
	child tui.Component
	ctx   *tui.Context

	mu     sync.Mutex
	keys   []tui.KeyEvent
	pastes int
}

func newShell(child tui.Component) *shell { return &shell{child: child} }

func (s *shell) Init(ctx *tui.Context) {
	s.ctx = ctx
	ctx.Mount(s.child)
}

func (s *shell) Layout(c tui.Constraints) tui.Size {
	w := c.MaxW
	h := c.MaxH
	if s.child != nil {
		sz := s.ctx.LayoutChild(s.child, tui.Tight(tui.Size{W: w, H: h}))
		s.ctx.PlaceChild(s.child, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// unmountChild unmounts the wrapped child (loop goroutine).
func (s *shell) unmountChild() {
	if s.child != nil {
		s.ctx.Unmount(s.child)
		s.child = nil
	}
}

func (s *shell) Render(tui.Surface) {}

func (s *shell) HandleEvent(ev tui.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e := ev.(type) {
	case tui.KeyEvent:
		s.keys = append(s.keys, e)
	case tui.PasteEvent:
		s.pastes++
	}
	return false
}

// bubbledKeys returns the key events that reached the shell (unconsumed by
// the child), excluding barrier sentinels.
func (s *shell) bubbledKeys() []tui.KeyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tui.KeyEvent, 0, len(s.keys))
	for _, k := range s.keys {
		if k.Code == barrierKey {
			continue
		}
		out = append(out, k)
	}
	return out
}

// sawBarriers counts barrier sentinels received so far.
func (s *shell) sawBarriers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.keys {
		if k.Code == barrierKey {
			n++
		}
	}
	return n
}

// barrierKey is a sentinel no widget consumes: injected after the events
// under test, its arrival at the shell proves everything before it was
// dispatched (lane-A order is preserved).
const barrierKey = tui.KeyF12

// barrier injects the sentinel and waits for the shell to see it.
func (h *harness) barrier(s *shell) {
	h.t.Helper()
	want := s.sawBarriers() + 1
	h.inject(key(barrierKey))
	h.waitFor("input barrier", func() bool { return s.sawBarriers() >= want })
	h.settle()
}

// cellAttrs returns the resolved attrs of the grid cell at (x, y).
func cellAttrs(h *harness, x, y int) tui.CellAttrs {
	snap := h.tb.Snapshot()
	if y >= len(snap) || x >= len(snap[y]) {
		h.t.Fatalf("cell (%d,%d) outside grid", x, y)
	}
	return snap[y][x].Attrs
}
