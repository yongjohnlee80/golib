package tui

// App construction and Run lifecycle tests (ADR-0005 §5.1, §5.10, plus the
// ADR-0001 §5.4 idle/one-flush end-to-end).

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewAppConstructionPanics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		fn   func()
		want string
	}{
		{
			name: "nil root",
			fn:   func() { NewApp(nil, WithBackend(NewTestBackend(4, 4))) },
			want: "nil root",
		},
		{
			name: "missing backend",
			fn:   func() { NewApp(&probe{name: "r"}) },
			want: "WithBackend is required",
		},
		{
			name: "bad input queue size",
			fn:   func() { WithInputQueueSize(0) },
			want: "WithInputQueueSize",
		},
		{
			name: "bad event queue limit",
			fn:   func() { WithEventQueueLimit(0) },
			want: "WithEventQueueLimit",
		},
		{
			name: "bad task pool size",
			fn:   func() { WithTaskPoolSize(0) },
			want: "WithTaskPoolSize",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				rec := recover()
				if rec == nil {
					t.Fatalf("expected panic")
				}
				if msg := panicText(rec); !strings.Contains(msg, tc.want) {
					t.Fatalf("panic %q does not mention %q", rec, tc.want)
				}
			}()
			tc.fn()
		})
	}
}

// TestRunPanicRepanic: ADR-0005 §5.1 — a panicking handler leaves the
// terminal restored (backend stopped) before the ORIGINAL panic value
// propagates (PanicRepanic default).
func TestRunPanicRepanic(t *testing.T) {
	t.Parallel()
	boom := "component exploded"
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(_ *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'x' {
			panic(boom)
		}
		return false
	}
	h := startApp(t, root, 4, 2)
	h.inject(keyEv('x'))

	var res runResult
	select {
	case res = <-h.resc:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after handler panic")
	}
	h.stopOnce.Do(func() { h.res = res }) // the run already ended

	if res.rec != boom {
		t.Fatalf("repanic value = %v, want the original %q", res.rec, boom)
	}
	// Restore-before-repanic: by the time the panic escaped Run the
	// backend must already be stopped (ADR-0005 §2.2).
	if err := h.tb.Inject(keyEv('y')); err == nil {
		t.Fatal("backend still accepts events — Stop did not run before the repanic")
	}
}

// TestRunPanicReturn: ADR-0005 §5.1 — under PanicReturn the same panic
// surfaces as errors.Is(err, ErrPanic) with no propagation.
func TestRunPanicReturn(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(_ *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'x' {
			panic("controlled")
		}
		return false
	}
	h := startApp(t, root, 4, 2, WithPanicPolicy(PanicReturn))
	h.inject(keyEv('x'))
	res := func() runResult {
		select {
		case r := <-h.resc:
			return r
		case <-time.After(3 * time.Second):
			t.Fatal("Run did not return after handler panic")
			return runResult{}
		}
	}()
	h.stopOnce.Do(func() { h.res = res })
	if res.rec != nil {
		t.Fatalf("panic escaped Run under PanicReturn: %v", res.rec)
	}
	if !errors.Is(res.err, ErrPanic) {
		t.Fatalf("err = %v, want errors.Is(err, ErrPanic)", res.err)
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
// closes (ADR-0005 §2.2 / ADR-0002 rev 1).
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

// TestTeardownAbandonedTasks: ADR-0005 §5.10 — cancelling Run's ctx with 3
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
// zero flushes while idle (ADR-0001 §5.4; ADR-0005 §5.9 write side).
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
// Surface.StringWidth (ADR-0005 §2.1 rev 1 / ADR-0003 §2.4).
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
