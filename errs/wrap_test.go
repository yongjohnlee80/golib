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

// A nil base is a misuse, and it must be unmissable WITHOUT taking the process
// down: building an error is not a path that should be able to panic, and
// nothing is corrupted by getting here.
//
// So the result carries ErrFatal — catchable by any handler already watching
// for a broken contract, and loud in a log — while still delivering the message
// the caller wanted, which is what makes the mistake diagnosable rather than
// merely fatal.
func TestHelpers_ANilBaseIsReportedNotPanicked(t *testing.T) {
	cases := map[string]func() error{
		"Wrap":      func() error { return errs.Wrap(nil, "dial %s", "10.0.0.1") },
		"Sentinel":  func() error { return errs.Sentinel(nil, "backend is stopped") },
		"WrapCause": func() error { return errs.WrapCause(nil, context.Canceled, "dial %s", "10.0.0.1") },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			var err error
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						t.Fatalf("building an error must not panic, got %v", rec)
					}
				}()
				err = call()
			}()

			if err == nil {
				t.Fatal("a nil base must still produce an error, not nil")
			}
			if !errors.Is(err, errs.ErrFatal) {
				t.Errorf("must carry ErrFatal so the misuse is catchable; got %v", err)
			}
			// The caller's own message survives, or the report says the mistake
			// happened without saying where.
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the report must name the helper that was misused: %q", err)
			}
		})
	}

	// The detail the caller passed is not thrown away either.
	if got := errs.Wrap(nil, "dial %s", "10.0.0.1").Error(); !strings.Contains(got, "10.0.0.1") {
		t.Errorf("the caller's message must survive the misuse report: %q", got)
	}
}
