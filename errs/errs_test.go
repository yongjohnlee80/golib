package errs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// The layered pattern the convention settles on, written here as two packages
// would write it: a specific condition DEFINED BY WRAPPING a shared base, with
// the base in brackets so a reader can see where context ends and identity
// begins.
var (
	errBackendClosed = fmt.Errorf("(%w: backend is stopped)", errs.ErrClosed)
	errViewClosed    = fmt.Errorf("(%w: buffer view closed)", errs.ErrClosed)
)

// A caller may ask the specific question or the general one, and both are
// exact. This is the property that lets four packages each keep their own
// "closed" identity without either over-matching or under-matching.
func TestLayered_BothQuestionsAnswerExactly(t *testing.T) {
	// As a call site would produce it: wrapped once more with context.
	err := fmt.Errorf("term: write to /dev/tty: %w", errBackendClosed)

	if !errors.Is(err, errBackendClosed) {
		t.Error("the SPECIFIC question must answer true")
	}
	if !errors.Is(err, errs.ErrClosed) {
		t.Error("the GENERAL question must answer true")
	}
	if errors.Is(err, errViewClosed) {
		t.Error("a DIFFERENT condition sharing the same base must answer false; " +
			"layering must not make siblings interchangeable")
	}

	// Depth must not change any of it.
	deep := fmt.Errorf("app: render: %w", fmt.Errorf("session: %w", err))
	if !errors.Is(deep, errBackendClosed) || !errors.Is(deep, errs.ErrClosed) {
		t.Error("the chain must survive further wrapping")
	}
	if errors.Is(deep, errViewClosed) {
		t.Error("depth must not blur siblings together")
	}
}

// The negative control for the test above: siblings never answer for each
// other even compared directly, with no call-site wrapping involved.
func TestLayered_SiblingsAreNotInterchangeable(t *testing.T) {
	if !errors.Is(errViewClosed, errs.ErrClosed) {
		t.Error("a sibling must still satisfy the base")
	}
	if errors.Is(errViewClosed, errBackendClosed) {
		t.Error("two siblings must not satisfy each other")
	}
}

// Fatal carries its identity whatever its message says — the property the
// whole convention exists for.
func TestFatal_IdentitySurvivesAnyMessage(t *testing.T) {
	a := errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout or Render"}
	b := errs.Fatal{Op: "dao: Insert", Rule: "something else entirely", Detail: "id=7"}

	for _, f := range []errs.Fatal{a, b} {
		if !errors.Is(f, errs.ErrFatal) {
			t.Errorf("%q must satisfy ErrFatal regardless of its message", f.Error())
		}
	}
	if a.Error() == b.Error() {
		t.Fatal("the two fixtures have the same message, so this proves nothing about message independence")
	}
}

// errors.As recovers the VALUE through several wraps, which is the whole
// reason Fatal is a type rather than a sentinel.
func TestFatal_AsRecoversTheFieldsThroughWrapping(t *testing.T) {
	orig := errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout or Render", Detail: "node 7"}
	wrapped := fmt.Errorf("app: render: %w", fmt.Errorf("tree: %w", fmt.Errorf("mount: %w", orig)))

	var got errs.Fatal
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As must recover the Fatal through three wraps")
	}
	if got.Op != orig.Op || got.Rule != orig.Rule || got.Detail != orig.Detail {
		t.Errorf("recovered %+v, want %+v", got, orig)
	}
}

// THE NEGATIVE CONTROL, and the most important test in this file: the same
// Fatal flattened with %v is NOT recoverable — while a sentinel wrapped
// alongside it still answers errors.Is true.
//
// That combination is the trap the convention's %w rule exists to prevent: the
// identity check passes, so the caller concludes nothing was lost, and every
// field the panic was carrying has become text.
func TestFatal_FlatteningWithVerbDestroysTheValue(t *testing.T) {
	orig := errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout or Render"}
	sentinel := errors.New("tui: task panicked")

	flattened := fmt.Errorf("%w: %v", sentinel, orig) // the mistake
	wrapped := fmt.Errorf("%w: %w", sentinel, orig)   // the fix

	var f errs.Fatal
	if errors.As(flattened, &f) {
		t.Error("a flattened error must NOT be recoverable by errors.As; if this " +
			"passes, the negative control is broken and the wrapping rule is unproven")
	}
	if errors.Is(flattened, errs.ErrFatal) {
		t.Error("flattening must lose the Fatal identity too")
	}
	if !errors.Is(flattened, sentinel) {
		t.Error("the sentinel SURVIVES flattening — that is precisely why the mistake looks like success")
	}

	var g errs.Fatal
	if !errors.As(wrapped, &g) || g.Op != orig.Op {
		t.Error("a properly wrapped error must be recoverable with its fields intact")
	}
	if !errors.Is(wrapped, errs.ErrFatal) || !errors.Is(wrapped, sentinel) {
		t.Error("wrapping must preserve both identities")
	}
}

// Every exported sentinel is a distinct value: none accidentally satisfies
// another, which a copy-paste declaration would silently cause.
func TestSentinelsAreDistinct(t *testing.T) {
	all := map[string]error{
		"ErrFatal": errs.ErrFatal, "ErrUnsupported": errs.ErrUnsupported,
		"ErrNotImplemented": errs.ErrNotImplemented, "ErrInvalidArgument": errs.ErrInvalidArgument,
		"ErrPrecondition": errs.ErrPrecondition, "ErrClosed": errs.ErrClosed,
		"ErrTimeout": errs.ErrTimeout,
	}
	for na, a := range all {
		for nb, b := range all {
			if na == nb {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("%s satisfies %s; every sentinel must be its own condition", na, nb)
			}
		}
	}
	if len(all) < 7 {
		t.Fatalf("only %d sentinels checked; the table is stale and this passes vacuously", len(all))
	}
}

// Fatal is a VALUE type, which is what removes the typed-nil failure rather
// than guarding against it.
//
// An earlier revision used a pointer receiver, and review found that a nil
// *Fatal assigned to an error answered errors.Is(err, ErrFatal) TRUE and
// panicked in Error(). Both were patched with nil checks. Johno's ruling was
// that the design, not the symptom, was the defect: Error() must not be
// reachable on a nil reference at all.
//
// With a value receiver there is no pointer to be nil. This test pins the two
// properties that follow, so a change back to a pointer receiver fails here
// rather than in a consumer.
func TestFatal_IsAValueTypeSoNoNilStateExists(t *testing.T) {
	// 1. The VALUE implements error. If this stops compiling, someone moved
	//    to a pointer receiver and reintroduced the typed-nil state.
	var _ error = errs.Fatal{}

	// 2. The zero value is a usable error, not a crash and not "<nil>".
	//    Nothing has to be set for Error() to be safe to call.
	var zero errs.Fatal
	msg := zero.Error()
	if msg == "" || msg == "<nil>" {
		t.Errorf("the zero Fatal must render as real prose, got %q", msg)
	}
	if !errors.Is(zero, errs.ErrFatal) {
		t.Error("the zero Fatal must still answer the general question")
	}

	// 3. A constructed Fatal reads as a detailed, human-readable sentence —
	//    identity is for code, the message is for a person, and requiring
	//    identity comparison is not a licence to make the text useless.
	f := errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout", Detail: "node 7"}
	want := "tui: Mount: tree mutation inside Layout (node 7)"
	if got := f.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// The errors.As target must be spelled as a VALUE, and the pointer spelling
// fails SILENTLY — which is why this is pinned rather than only documented.
//
// Fatal is the first value-receiver error type in this repository, so a reader
// reaching for the universal *T idiom that every other typed error here uses
// gets false with no error and concludes the payload was not there. That is
// rule 2's failure shape — the check passes, and the value is gone — arriving
// through the spelling of the target rather than through a %v.
//
// Found by zen probing the value-receiver change (PR #38 r2).
func TestFatal_AsTargetMustBeSpelledAsAValue(t *testing.T) {
	err := fmt.Errorf("outer: %w", errs.Fatal{Op: "tui: Mount", Rule: "the rule"})

	// The working idiom.
	var val errs.Fatal
	if !errors.As(err, &val) {
		t.Fatal("a VALUE target must match a wrapped Fatal; the documented idiom is broken")
	}
	if val.Op != "tui: Mount" {
		t.Errorf("As matched but recovered the wrong value: %+v", val)
	}

	// The trap, pinned. If a later change to Fatal's receivers makes this
	// start passing, the doc comment in errs.go is stale and must be updated.
	var ptr *errs.Fatal
	if errors.As(err, &ptr) {
		t.Error("a POINTER target now matches; errs.go's doc comment says it cannot " +
			"and must be corrected")
	}
}
