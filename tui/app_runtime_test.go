package tui

// Shared test harness for the runtime + component tree suites: probe
// components, an App runner over
// TestBackend (no PTY), a polling waiter, a capturing logger, and a
// goroutine-id helper for loop-goroutine assertions.

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui/style"
)

// goid parses the current goroutine id from runtime.Stack — TEST-ONLY
// tooling for asserting the loop-goroutine invariant (the runtime itself
// never does this — gid parsing was dropped from the API).
func goid() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	fields := strings.Fields(string(buf[:n]))
	id, _ := strconv.ParseInt(fields[1], 10, 64)
	return id
}

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

// callLog is a shared, ordered recorder for lifecycle-order assertions.
type callLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, s)
}

func (l *callLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// logCapture adapts logger.Adapt into a race-safe payload recorder.
type logCapture struct {
	mu      sync.Mutex
	entries []map[string]any
}

func (lc *logCapture) logger() logger.Logger {
	return logger.Adapt(func(_ logger.Severity, payload any) {
		lc.mu.Lock()
		defer lc.mu.Unlock()
		if e, ok := payload.(logger.Entry); ok {
			payload = e.Payload // unwrap the error-bearing helpers' pairing
		}
		if m, ok := payload.(map[string]any); ok {
			lc.entries = append(lc.entries, m)
		} else {
			lc.entries = append(lc.entries, map[string]any{"payload": payload})
		}
	})
}

// has reports whether any captured entry's "tui" field contains sub.
func (lc *logCapture) has(sub string) bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, e := range lc.entries {
		if s, ok := e["tui"].(string); ok && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// probe is the instrumented test component: records every routed event, the
// goroutine its handler ran on, and its lifecycle counters; behavior is
// injected per test via the on* hooks.
type probe struct {
	name string
	log  *callLog // optional shared lifecycle recorder

	mu     sync.Mutex
	events []Event

	ctx *Context // loop-goroutine-owned; tests read it via harness.onLoop
	id  atomic.Uint64

	pref     Size   // Layout answer: c.Constrain(pref)
	fill     string // Render fill cluster ("" paints nothing)
	onInit   func(p *probe, ctx *Context)
	onEvent  func(p *probe, ev Event) bool
	onLayout func(p *probe, c Constraints) Size

	inits    atomic.Int64
	layouts  atomic.Int64
	renders  atomic.Int64
	lastGid  atomic.Int64
	unmounts atomic.Int64
}

func (p *probe) Init(ctx *Context) {
	p.ctx = ctx
	p.id.Store(uint64(ctx.ID()))
	p.inits.Add(1)
	if p.log != nil {
		p.log.add(p.name + ".init")
	}
	ctx.OnUnmount(func() { p.unmounts.Add(1) })
	if p.onInit != nil {
		p.onInit(p, ctx)
	}
}

func (p *probe) Layout(c Constraints) Size {
	p.layouts.Add(1)
	if p.log != nil {
		p.log.add(p.name + ".layout")
	}
	if p.onLayout != nil {
		return p.onLayout(p, c)
	}
	return c.Constrain(p.pref)
}

func (p *probe) Render(s Surface) {
	p.renders.Add(1)
	if p.log != nil {
		p.log.add(p.name + ".render")
	}
	if p.fill != "" {
		sz := s.Size()
		s.Fill(Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, p.fill, style.New())
	}
}

func (p *probe) HandleEvent(ev Event) bool {
	p.lastGid.Store(goid())
	p.mu.Lock()
	p.events = append(p.events, ev)
	p.mu.Unlock()
	if p.log != nil {
		p.log.add(p.name + ".event")
	}
	if p.onEvent != nil {
		return p.onEvent(p, ev)
	}
	return false
}

// recorded returns a copy of every event routed to the probe so far.
func (p *probe) recorded() []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Event(nil), p.events...)
}

func (p *probe) eventCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

// nodeID returns the probe's NodeID as recorded at Init (race-safe).
func (p *probe) nodeID() NodeID { return NodeID(p.id.Load()) }

// focusProbe opts into the focus system.
type focusProbe struct {
	probe
	accepts atomic.Bool
}

func newFocusProbe(name string, pref Size) *focusProbe {
	f := &focusProbe{probe: probe{name: name, pref: pref}}
	f.accepts.Store(true)
	return f
}

func (f *focusProbe) AcceptsFocus() bool { return f.accepts.Load() }

// cursorProbe is a focusable CursorReporter.
type cursorProbe struct {
	focusProbe
	cx, cy int
	report atomic.Bool
}

func (c *cursorProbe) Cursor() (int, int, bool) {
	if !c.report.Load() {
		return 0, 0, false
	}
	return c.cx, c.cy, true
}

// scopeProbe is a focus trap built over a vertical Flex.
type scopeProbe struct {
	Flex
	traps bool
}

func newScopeProbe(traps bool) *scopeProbe {
	return &scopeProbe{Flex: *NewFlex(Vertical), traps: traps}
}

func (s *scopeProbe) TrapsFocus() bool { return s.traps }

// runResult is what the Run goroutine reports.
type runResult struct {
	err error
	rec any // recovered panic, if Run repanicked
}

// harness runs an App on a TestBackend in a background goroutine.
type harness struct {
	t      *testing.T
	app    *App
	tb     *TestBackend
	cancel context.CancelFunc
	resc   chan runResult

	stopOnce sync.Once
	res      runResult
}

// startApp builds and runs an App over a fresh TestBackend. The frame cap
// is disabled by default (WithMinFrameInterval(0)) for deterministic
// one-flush-per-change assertions; opts may override.
func startApp(t *testing.T, root Component, w, h int, opts ...AppOption) *harness {
	t.Helper()
	tb := NewTestBackend(w, h)
	all := append([]AppOption{WithBackend(tb), WithMinFrameInterval(0)}, opts...)
	return runApp(t, NewApp(root, all...), tb)
}

// runApp starts app.Run on a background goroutine and waits for the loop to
// come alive.
func runApp(t *testing.T, app *App, tb *TestBackend) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, app: app, tb: tb, cancel: cancel, resc: make(chan runResult, 1)}
	go func() {
		var res runResult
		defer func() {
			if r := recover(); r != nil {
				res.rec = r
			}
			h.resc <- res
		}()
		res.err = app.Run(ctx)
	}()
	h.sync()
	t.Cleanup(func() { h.wait() })
	return h
}

// sync round-trips an Update through the loop, proving it is alive and that
// every previously enqueued lane-B item has drained.
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

// onLoop runs fn on the loop goroutine and waits for it — the sanctioned
// way for tests to read loop-owned state.
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

// loopGid reports the loop goroutine's id.
func (h *harness) loopGid() int64 {
	var gid int64
	h.onLoop(func() { gid = goid() })
	return gid
}

// wait cancels the run context (idempotent) and returns Run's outcome.
func (h *harness) wait() runResult {
	h.stopOnce.Do(func() {
		h.cancel()
		select {
		case h.res = <-h.resc:
		case <-time.After(5 * time.Second):
			h.t.Errorf("app did not shut down within 5s")
		}
	})
	return h.res
}

// inject scripts backend events, failing the test on buffer overflow.
func (h *harness) inject(evs ...Event) {
	h.t.Helper()
	if err := h.tb.Inject(evs...); err != nil {
		h.t.Fatalf("inject: %v", err)
	}
}

// keyEv builds a plain key press.
func keyEv(code rune) KeyEvent {
	text := ""
	if code < 0xE000 && code >= 0x20 {
		text = string(code)
	}
	return KeyEvent{Kind: KeyPress, Code: code, Text: text}
}

// tabEv builds Tab / Shift-Tab presses.
func tabEv(shift bool) KeyEvent {
	var mods Mods
	if shift {
		mods = ModShift
	}
	return KeyEvent{Kind: KeyPress, Code: KeyTab, Mods: mods}
}
