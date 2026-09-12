package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// panicText renders a recovered panic value the way a person reads it,
// accepting either shape (string or error/errs.Fatal).
//
// The panics in this package used to carry strings and now carry errs.Fatal
// values, so a caller can errors.As the operation and the broken rule back out.
// The conversion was built to be MESSAGE-PRESERVING — errs.Fatal renders
// "Op: Rule", and each site's Op and Rule were split out of its existing
// sentence at a ": " boundary — so every assertion below reads the same text it
// read before.
func panicText(rec any) string {
	if err, ok := rec.(error); ok {
		return err.Error()
	}
	s, _ := rec.(string)
	return s
}

// TestRunPanicRepanic: a panicking handler leaves the terminal restored
// (backend stopped) before the ORIGINAL panic value propagates (PanicRepanic default).
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
	// backend must already be stopped.
	if err := h.tb.Inject(keyEv('y')); err == nil {
		t.Fatal("backend still accepts events — Stop did not run before the repanic")
	}
}

// TestRunPanicReturn: under PanicReturn the same panic surfaces as
// errors.Is(err, ErrPanic) with no propagation.
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

// TestRunPanicReturn_PreservesAFatalPayload: a panic carrying a value must
// reach the returned error with that value still recoverable.
//
// The convention tells authors to panic with errs.Fatal rather than a string,
// precisely so a recovering caller can errors.As the Op and Rule back out. If
// Run flattens the recovered value into text on the way out, that instruction
// is a trap: errors.Is(err, ErrPanic) still answers true, so the error looks
// entirely healthy while the payload it was carrying is gone.
func TestRunPanicReturn_PreservesAFatalPayload(t *testing.T) {
	t.Parallel()
	want := errs.Fatal{
		Op:     "tui: Mount",
		Rule:   "tree mutation inside Layout or Render",
		Detail: "node 7",
	}

	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(_ *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'x' {
			panic(want)
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
	// The identity check that passes either way — stated first so the test
	// records that it is NOT what distinguishes a healthy error here.
	if !errors.Is(res.err, ErrPanic) {
		t.Fatalf("err = %v, want errors.Is(err, ErrPanic)", res.err)
	}

	var got errs.Fatal
	if !errors.As(res.err, &got) {
		t.Fatalf("errors.As could not recover the panicked errs.Fatal from %v\n"+
			"the payload was flattened into text on the way out, and ErrPanic "+
			"still answering true is what makes that invisible", res.err)
	}
	if got != want {
		t.Errorf("recovered %+v, want %+v", got, want)
	}
}

// TestTaskPanicIsolation: a panicking task produces errors.Is(res.Err, ErrTaskPanic),
// the app keeps processing events, and the recovered stack appears via WithLogger.
func TestTaskPanicIsolation(t *testing.T) {
	t.Parallel()
	var lc logCapture
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2, WithLogger(lc.logger()))

	h.app.Go(root.nodeID(), func(context.Context) (any, error) {
		panic("task exploded")
	})
	waitFor(t, "panicked task result", func() bool { return len(taskResults(root)) == 1 })
	r := taskResults(root)[0]
	if !errors.Is(r.Err, ErrTaskPanic) {
		t.Fatalf("result Err = %v, want errors.Is(_, ErrTaskPanic)", r.Err)
	}
	if !lc.has("task panic") {
		t.Fatal("recovered task panic (with stack) was not logged via WithLogger")
	}
	// The app keeps processing events.
	before := root.eventCount()
	h.inject(keyEv('k'))
	waitFor(t, "post-panic event processing", func() bool { return root.eventCount() > before })
}

// TestTaskPanic_PreservesAFatalPayload: a panicking task preserves its
// errs.Fatal payload so the owner learns what broke, not just that something did.
func TestTaskPanic_PreservesAFatalPayload(t *testing.T) {
	t.Parallel()
	want := errs.Fatal{Op: "app: refresh", Rule: "store closed under the task"}

	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)

	h.app.Go(root.nodeID(), func(context.Context) (any, error) {
		panic(want)
	})
	waitFor(t, "panicked task result", func() bool { return len(taskResults(root)) == 1 })
	r := taskResults(root)[0]

	if !errors.Is(r.Err, ErrTaskPanic) {
		t.Fatalf("result Err = %v, want errors.Is(_, ErrTaskPanic)", r.Err)
	}
	var got errs.Fatal
	if !errors.As(r.Err, &got) {
		t.Fatalf("errors.As could not recover the panicked errs.Fatal from %v", r.Err)
	}
	if got != want {
		t.Errorf("recovered %+v, want %+v", got, want)
	}
}

// TestInvariantPanicsCarryTheirContract verifies that invariant panics carry
// an errs.Fatal error value that preserves the operation and broken contract.
func TestInvariantPanicsCarryTheirContract(t *testing.T) {
	cases := map[string]struct {
		run     func()
		wantOp  string
		wantSub string
	}{
		"tree mutation inside Layout": {
			run:     func() { (&App{}).mount(nil, nil) },
			wantOp:  "tui: Mount",
			wantSub: "nil component",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				rec := recover()
				if rec == nil {
					t.Fatal("expected a panic")
				}
				err, ok := rec.(error)
				if !ok {
					t.Fatalf("the panic must carry an error value, got %T", rec)
				}
				var f errs.Fatal
				if !errors.As(err, &f) {
					t.Fatalf("errors.As must recover the Fatal, got %v", err)
				}
				if f.Op != tc.wantOp {
					t.Errorf("Op = %q, want %q", f.Op, tc.wantOp)
				}
				if !strings.Contains(f.Rule, tc.wantSub) {
					t.Errorf("Rule = %q, want it to contain %q", f.Rule, tc.wantSub)
				}
				// And the identity: every Fatal answers the general question.
				if !errors.Is(err, errs.ErrFatal) {
					t.Error("a Fatal must satisfy errs.ErrFatal")
				}
			}()
			tc.run()
		})
	}
}

// TestConvertedPanicsKeepTheirMessage ensures that Op: Rule splitting
// exactly reproduces the original sentence without string drift.
func TestConvertedPanicsKeepTheirMessage(t *testing.T) {
	f := errs.Fatal{Op: "tui: Mount", Rule: "nil component"}
	if got, want := f.Error(), "tui: Mount: nil component"; got != want {
		t.Errorf("Error() = %q, want %q — the split must reproduce the original "+
			"sentence, or every existing assertion on these messages is now "+
			"asserting something different", got, want)
	}
}

// TestNewAppConstructionPanics: verify constructor validation panics.
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
