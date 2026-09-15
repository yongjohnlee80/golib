package tui

// App construction and Run lifecycle tests, plus the idle/one-flush
// end-to-end.
//
// This file pairs with app.go and also houses the shared test harness and fixtures:
//   - probe / focusProbe / cursorProbe / scopeProbe: instrumented test components
//   - harness / startApp / runApp: background App runner over TestBackend
//   - waitFor: deterministic polling condition assertion
//   - logCapture: structured logger recorder
//   - callLog: ordered method invocation tracker
//   - goid: goroutine ID extractor for verifying loop-goroutine-only invariants

import (
	"context"
	"errors"
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

func TestSetThemeRejectsNilBeforeQueueMutation(t *testing.T) {
	a := NewApp(&probe{}, WithBackend(NewTestBackend(1, 1)))
	defer func() {
		if recover() == nil {
			t.Fatal("SetTheme(nil) did not panic")
		}
	}()
	a.SetTheme(nil)
}

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

// renderHook wraps a probe to observe its Render surface.
type renderHook struct {
	*probe
	hook func(Surface)
}

func (r *renderHook) Render(s Surface) {
	r.probe.Render(s)
	if r.hook != nil {
		r.hook(s)
	}
}

func TestRunTwiceErrors(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	if err := h.app.Run(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "more than once") {
		t.Fatalf("second Run: err = %v, want 'more than once'", err)
	}
}

// TestRunBackendEventsClosed: the loop collects backend.Err() when Events()
// closes.
func TestRunBackendEventsClosed(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("reader died")
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	h.tb.SetErr(sentinel)
	_ = h.tb.Stop() // closes Events(); intake closes a.input; loop returns
	var res runResult
	select {
	case res = <-h.resc: // without cancelling ctx: the closed-Events path
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after Events() closed")
	}
	h.stopOnce.Do(func() { h.res = res })
	if !errors.Is(res.err, sentinel) {
		t.Fatalf("Run err = %v, want errors.Is(err, sentinel)", res.err)
	}
}

// TestTeardownAbandonedTasks: cancelling Run's ctx with 3
// in-flight tasks (2 well-behaved, 1 ignoring its ctx) returns within the
// drain timeout +ε reporting "1 task(s) abandoned".
func TestTeardownAbandonedTasks(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	defer close(block)
	started := make(chan struct{}, 3)
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onInit = func(_ *probe, ctx *Context) {
		for i := 0; i < 2; i++ {
			ctx.Go(func(tctx context.Context) (any, error) {
				started <- struct{}{}
				<-tctx.Done()
				return nil, tctx.Err()
			})
		}
		ctx.Go(func(context.Context) (any, error) {
			started <- struct{}{} // ignores its ctx — the abandoned one
			<-block
			return nil, nil
		})
	}
	h := startApp(t, root, 4, 2, WithTaskDrainTimeout(100*time.Millisecond))
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("tasks did not start")
		}
	}
	begin := time.Now()
	res := h.wait()
	if elapsed := time.Since(begin); elapsed > 3*time.Second {
		t.Fatalf("teardown took %v, want ~drain timeout", elapsed)
	}
	if res.err == nil || !strings.Contains(res.err.Error(), "1 task(s) abandoned") {
		t.Fatalf("Run err = %v, want '1 task(s) abandoned'", res.err)
	}
}

// TestEndToEndTwoPane: mount a two-pane Flex on TestBackend, inject keys and
// a resize, and assert grid snapshots plus EXACTLY one Flush per change and
// zero flushes while idle.
func TestEndToEndTwoPane(t *testing.T) {
	t.Parallel()
	left := newFocusProbe("left", Size{})
	left.fill = "a"
	var flip atomic.Bool
	left.onEvent = func(p *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'x' {
			flip.Store(true)
			p.fill = "A"
			p.ctx.MarkDirty()
			return true
		}
		return false
	}
	left.onInit = func(p *probe, ctx *Context) { ctx.RequestFocus() }
	right := newFocusProbe("right", Size{})
	right.fill = "b"

	root := NewFlex(Horizontal)
	root.AddWeighted(left, 1)
	root.AddWeighted(right, 1)

	h := startApp(t, root, 10, 2)

	if got, want := h.tb.String(), "aaaaabbbbb\naaaaabbbbb"; got != want {
		t.Fatalf("initial frame:\n%q\nwant\n%q", got, want)
	}
	base := h.tb.Flushes()

	// One key → one state change → exactly one more flush.
	h.inject(keyEv('x'))
	waitFor(t, "left pane repaint", func() bool {
		return strings.HasPrefix(h.tb.String(), "AAAAA")
	})
	h.sync()
	if got := h.tb.Flushes(); got != base+1 {
		t.Fatalf("flushes after key = %d, want %d (exactly one per change)", got, base+1)
	}

	// Idle window: zero further writes (idle = zero bytes).
	time.Sleep(50 * time.Millisecond)
	if got := h.tb.Flushes(); got != base+1 {
		t.Fatalf("flushes grew while idle: %d, want %d", got, base+1)
	}

	// Resize → one layout pass + one full repaint + exactly one flush.
	h.tb.InjectResize(12, 2)
	waitFor(t, "resized frame", func() bool {
		return h.tb.String() == "AAAAAAbbbbbb\nAAAAAAbbbbbb"
	})
	h.sync()
	if got := h.tb.Flushes(); got != base+2 {
		t.Fatalf("flushes after resize = %d, want %d", got, base+2)
	}
	FailOnViolations(t, h.tb)
}

// TestWidthPolicyTravelsWithSurface: WithWidthPolicy reaches components via
// Surface.StringWidth.
func TestWidthPolicyTravelsWithSurface(t *testing.T) {
	t.Parallel()
	const ambiguous = "±" // East Asian Ambiguous: width 1 default, 2 wide
	widths := make(chan int, 1)
	root := &probe{name: "root", pref: Size{W: 8, H: 2}}
	rendered := false // loop-goroutine-owned
	wrapped := &renderHook{probe: root, hook: func(s Surface) {
		if !rendered {
			rendered = true
			widths <- s.StringWidth(ambiguous)
		}
	}}
	startApp(t, wrapped, 8, 2, WithWidthPolicy(WidthPolicyAmbiguousWide))
	select {
	case w := <-widths:
		if w != 2 {
			t.Fatalf("StringWidth(%q) = %d under WidthPolicyAmbiguousWide, want 2", ambiguous, w)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("root never rendered")
	}
}
