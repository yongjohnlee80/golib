package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// The point of carrying a value: a recovering caller can read WHICH contract
// broke, instead of parsing a sentence.
//
// These are the invariant sites — the ones a recover() actually sits above,
// because they run inside App.Run. The construction sites (NewApp, the With*
// options) deliberately still panic with strings: nothing recovers them, so a
// value would reach no one.
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

// The message did not change. That is the claim the whole conversion rests on:
// each Op and Rule was split out of the site's existing sentence at a ": "
// boundary, and errs.Fatal renders "Op: Rule", so the text is reproduced
// exactly.
func TestConvertedPanicsKeepTheirMessage(t *testing.T) {
	f := errs.Fatal{Op: "tui: Mount", Rule: "nil component"}
	if got, want := f.Error(), "tui: Mount: nil component"; got != want {
		t.Errorf("Error() = %q, want %q — the split must reproduce the original "+
			"sentence, or every existing assertion on these messages is now "+
			"asserting something different", got, want)
	}
}
