package errs_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// The whole point of the helpers is that one shape comes out of every call
// site, so the shape is asserted literally rather than described.
func TestWrap_ProducesTheConventionalShape(t *testing.T) {
	err := errs.Wrap(errs.ErrInvalidArgument, "web: no bind address")
	if got, want := err.Error(), "web: no bind address (invalid argument)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Error("the identity must survive; that is what the message buys")
	}

	// Identical to the fmt spelling it replaces — so adopting the helper is not
	// a behaviour change anywhere it is applied.
	equiv := fmt.Errorf("web: no bind address (%w)", errs.ErrInvalidArgument)
	if err.Error() != equiv.Error() {
		t.Errorf("helper and fmt spelling disagree:\n %q\n %q", err, equiv)
	}

	// Formatting arguments land before the bracket, where the context belongs.
	f := errs.Wrap(errs.ErrTimeout, "dial %s after %s", "10.0.0.1:5432", "3s")
	if got, want := f.Error(), "dial 10.0.0.1:5432 after 3s (timeout)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// A layered sentinel is declared once and then appears inside the bracket of
// every error built on it, at any depth.
func TestSentinel_LayersAndStaysReadableAtDepth(t *testing.T) {
	errBackendClosed := errs.Sentinel(errs.ErrClosed, "backend is stopped")
	if got, want := errBackendClosed.Error(), "closed: backend is stopped"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	at := errs.Wrap(errBackendClosed, "term: write to %s", "/dev/tty")
	// Exactly one pair of brackets, however deep the layering goes — the
	// sentinel carries none of its own, so a call site cannot produce a nest.
	if n := strings.Count(at.Error(), "("); n != 1 {
		t.Errorf("want exactly 1 open bracket, got %d in %q", n, at)
	}
	if got, want := at.Error(), "term: write to /dev/tty (closed: backend is stopped)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	// Both questions answer exactly, which is the reason to layer at all.
	if !errors.Is(at, errBackendClosed) {
		t.Error("the specific question must answer")
	}
	if !errors.Is(at, errs.ErrClosed) {
		t.Error("the general question must answer")
	}
	// And a sibling on the same base stays a different condition.
	errViewClosed := errs.Sentinel(errs.ErrClosed, "buffer view closed")
	if errors.Is(at, errViewClosed) {
		t.Error("two siblings sharing a base must never answer for each other")
	}
}

// The helper that exists to make the %v mistake hard.
func TestWrapCause_KeepsBothIdentities(t *testing.T) {
	base := errs.Sentinel(errs.ErrPrecondition, "verifier unavailable")
	err := errs.WrapCause(base, context.Canceled, "verification cancelled by the caller")

	if !errors.Is(err, base) {
		t.Error("the first-party identity must answer")
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("the underlying cause must answer — this is the half a rendering verb destroys")
	}
	if !errors.Is(err, errs.ErrPrecondition) {
		t.Error("the shared condition under the base must answer too")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("the cause must still be readable by a person: %q", err)
	}

	// A nil cause is a real state, not a mistake, and degrades to Wrap.
	none := errs.WrapCause(base, nil, "verification refused")
	if got, want := none.Error(), errs.Wrap(base, "verification refused").Error(); got != want {
		t.Errorf("a nil cause must degrade to Wrap:\n got  %q\n want %q", got, want)
	}
}

// A nil base is the one thing these helpers must not accept quietly: an error
// with no identity is exactly what this package exists to prevent, and building
// one silently would hide the mistake at the only moment it is cheap to find.
func TestHelpers_RejectANilBaseLoudly(t *testing.T) {
	cases := map[string]func(){
		"Wrap":      func() { _ = errs.Wrap(nil, "something") },
		"Sentinel":  func() { _ = errs.Sentinel(nil, "something") },
		"WrapCause": func() { _ = errs.WrapCause(nil, context.Canceled, "something") },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				rec := recover()
				if rec == nil {
					t.Fatal("a nil base must panic, not produce an identity-less error")
				}
				// And it panics with a VALUE, per the convention, so whatever
				// recovers it can read the fields instead of matching text.
				var f errs.Fatal
				if err, ok := rec.(error); !ok || !errors.As(err, &f) {
					t.Fatalf("panicked with %#v; want an errs.Fatal value", rec)
				}
				if f.Op == "" || f.Rule == "" {
					t.Errorf("the Fatal must say which operation and which rule: %+v", f)
				}
			}()
			call()
		})
	}
}
