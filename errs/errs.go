// Package errs holds the error conditions that BELONG TO NO SINGLE PACKAGE.
//
// An error's identity is its contract; its message is prose. Callers compare
// with errors.Is or errors.As and never with text, so a message may be
// reworded at any time without breaking anyone.
//
// # What lives here, and what does not
//
// One question decides it: COULD A SECOND, UNRELATED PACKAGE PRODUCE THIS SAME
// CONDITION?
//
//   - Yes, so it lives here. A broken contract, an operation an implementation
//     will never support, something not written yet, a bad argument, an unmet
//     precondition, a closed thing, a timeout. Any package may produce these,
//     and a caller handling one should not have to know which package it came
//     from.
//   - No, so it lives in the package that owns the concept. A transaction
//     rolling back is a fact about dao and nothing outside it can mean that;
//     dao.ErrTxRolledBack stays in dao.
//
// This package stays SMALL on purpose. It is a handful of conditions, not a
// registry. The moment a name here would only make sense to one package, it
// belongs in that package.
//
// # Layering: keep the specific identity AND gain the shared one
//
// A package with its own version of a shared condition DEFINES IT BY WRAPPING
// the base, which lets a caller ask either question and get an exact answer:
//
//	// package tui
//	var ErrBackendClosed = fmt.Errorf("(%w: backend is stopped)", errs.ErrClosed)
//
//	// package widget — a DIFFERENT condition, same base
//	var ErrViewClosed = fmt.Errorf("(%w: buffer view closed)", errs.ErrClosed)
//
// Then errors.Is(err, tui.ErrBackendClosed) answers the specific question,
// errors.Is(err, errs.ErrClosed) answers the general one, and the two siblings
// never answer for each other. Both hold however deeply the error is wrapped
// afterwards.
//
// The base goes in BRACKETS, so a reader can see where the call-site context
// ends and the identity begins:
//
//	term: write to /dev/tty: (closed: backend is stopped)
//
// Everything before the bracket is WHERE; everything inside it is WHAT. The
// base messages here are deliberately terse for the same reason — each appears
// inside the bracket of every error layered on it, and the package prefix is
// already carried by the specific half.
//
// PREFER ONE OR TWO IDENTITY LAYERS; three is the ceiling.
//
// One layer is a real answer: if a package's condition is simply "closed",
// with nothing package-specific a caller would act on differently, return
// ErrClosed directly rather than wrapping it in a near-identical sentinel that
// adds a name to the API and a layer to every message for nothing. Two is the
// usual case — the shared condition plus the one distinction that matters.
// Three is ALLOWED where a caller genuinely must distinguish a sub-case, and
// is the hard maximum — do not contort a design to avoid a third layer the
// callers need. A fourth is not a judgement call: it means the hierarchy is
// doing work that a typed error's FIELDS should do instead.
//
// # The shape of a good error
//
//	return fmt.Errorf("dial %s after %s: %w", addr, elapsed, errs.ErrTimeout)
//
// The message says what happened, for a person reading a log. The wrapped
// sentinel says what it IS, for code that must act. A good error does both;
// neither substitutes for the other.
package errs

import "errors"

// The shared conditions. Compare with errors.Is; never with text.
//
// Each message is minimal because layered errors embed it. A package wraps one
// of these to define its own specific condition rather than declaring an
// unrelated sentinel — see the package documentation.
var (
	// ErrFatal marks a broken contract: a caller did what the API documents as
	// illegal, and continuing would corrupt state rather than merely fail. It
	// is the identity carried by [Fatal], which is what a panic should hold
	// instead of a bare string.
	ErrFatal = errors.New("fatal")

	// ErrUnsupported means this implementation cannot do it and never will —
	// a capability the engine, driver or backend does not have. It is not a
	// temporary failure and retrying cannot help.
	//
	// Distinct from [ErrNotImplemented]: unsupported is about the thing,
	// not-implemented is about the code.
	ErrUnsupported = errors.New("unsupported")

	// ErrNotImplemented means it is intended and not written yet. A caller
	// seeing this is looking at a gap in this repository, not a limit of the
	// system being talked to.
	ErrNotImplemented = errors.New("not implemented")

	// ErrInvalidArgument means the caller passed something this operation
	// cannot accept — malformed, out of range, or contradictory. The fault is
	// at the call site.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrPrecondition means the arguments were fine but the state was not:
	// something had to be true before this call and was not. Doing the missing
	// step first may make the same call succeed.
	ErrPrecondition = errors.New("precondition failed")

	// ErrClosed means the thing being used has been closed, stopped or
	// unmounted. Several packages have their own version — a stopped backend,
	// an unmounted view, a closed client, a finished transaction — and each
	// defines it by wrapping this one, so a caller cleaning up after any
	// closure can ask this single question.
	ErrClosed = errors.New("closed")

	// ErrTimeout means an operation did not finish inside the time allowed.
	// It is separate from context cancellation, which callers detect with
	// errors.Is(err, context.DeadlineExceeded) — wrap that instead when the
	// deadline came from a context.
	ErrTimeout = errors.New("timeout")
)

// Fatal is a broken contract, carried as a VALUE so a recovering caller or a
// test can read what happened instead of matching a sentence.
//
// A panic passes one of these rather than a string:
//
//	panic(errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout or Render"})
//
// and whatever recovers it uses errors.As to get the fields back. That only
// works if every wrap on the way out uses %w — flattening it with %v renders
// the value into text and destroys everything As would have returned, while
// leaving errors.Is(err, ErrFatal) answering true. The check passes and the
// payload is gone.
//
// # Why a value and not a pointer
//
// The methods below take a VALUE receiver, so errs.Fatal is itself an error
// and the natural way to write one — errs.Fatal{...} — has no nil state to
// reach. Error() cannot nil-dereference because there is no pointer to
// dereference.
//
// A pointer receiver would make *errs.Fatal the only spelling that implements
// error, which makes a typed nil the easy mistake: var f *Fatal; return f
// yields a NON-nil error interface holding a nil pointer, so `err != nil` is
// true, Error() panics, and Is() answers for a value that was never
// constructed. Guarding each method against nil hides that state behind a
// caveat rather than removing it. A value type removes it.
//
// The cost is a 3-word copy on each wrap, which no error path will notice.
type Fatal struct {
	// Op is the operation whose contract was broken, e.g. "tui: Mount".
	Op string
	// Rule is the rule that was broken, in plain language and without
	// shorthand: a reader of this string has nothing else to consult.
	Rule string
	// Detail is optional specifics — an id, an index, a name.
	Detail string
}

// Error implements error. The text is prose and may change; code compares
// identity with errors.Is or reads the fields with errors.As.
func (f Fatal) Error() string {
	s := f.Op
	if s == "" {
		s = "fatal"
	}
	if f.Rule != "" {
		s += ": " + f.Rule
	}
	if f.Detail != "" {
		s += " (" + f.Detail + ")"
	}
	return s
}

// Is reports whether target is [ErrFatal], so every Fatal answers the general
// question regardless of its Op, Rule or message.
func (f Fatal) Is(target error) bool {
	return target == ErrFatal
}
