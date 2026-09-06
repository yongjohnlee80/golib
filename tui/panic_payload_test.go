package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// A panic carrying a value must reach the returned error with that value still
// recoverable.
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

// The same guarantee for a panicking TASK: the owner learns what broke, not
// just that something did.
//
// A task panic reaches its owner as a result error rather than through Run, so
// it is a second, independent path with the same hazard — and it had the same
// defect.
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
