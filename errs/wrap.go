package errs

import "fmt"

// Wrap returns an error that says what happened and carries base's identity,
// rendered in this repository's conventional shape:
//
//	where it happened, specifically (what it is)
//
// It replaces the fmt.Errorf spelling of the same thing:
//
//	errs.Wrap(errs.ErrInvalidArgument, "web: no bind address")
//	fmt.Errorf("web: no bind address (%w)", errs.ErrInvalidArgument)
//
// Both produce "web: no bind address (invalid argument)". The helper exists so
// the bracket cannot be forgotten, misplaced, or spelled differently in each
// package — a format that is retyped at every call site is a format that drifts.
//
// # When NOT to use it
//
// Wrapping is not a requirement. If your message would say nothing the base
// does not already say, RETURN THE BASE:
//
//	return ErrNoIdentity                              // yes
//	return errs.Wrap(ErrNoIdentity, "no identity")    // no — says it twice
//
// A wrapper whose text restates its base adds a layer, a line of output and a
// name, and tells the reader nothing. The point of a message is the part the
// identity cannot carry: which address, which file, which id.
//
// A nil base is a misuse, and it does NOT panic: building an error is not a
// path that should be able to take a process down, and nothing is corrupted by
// getting here. Instead the returned error carries [ErrInvalidArgument] and
// says what was wrong, so the mistake is unmissable in a log and catchable —
// while the message the caller wanted is still delivered.
//
// The identity is ErrInvalidArgument and not [ErrFatal] because nothing is
// corrupted, and not [ErrPrecondition] because the state was fine: the caller
// passed something this operation cannot accept, and the fault is at the call
// site. That is ErrInvalidArgument's contract exactly.
func Wrap(base error, format string, args ...any) error {
	if base == nil {
		return WrapCause(ErrInvalidArgument, fmt.Errorf(format, args...),
			"errs.Wrap: base must not be nil, so this error carries no real identity")
	}
	all := make([]any, 0, len(args)+1)
	all = append(all, args...)
	all = append(all, base)
	return fmt.Errorf(format+" (%w)", all...)
}

// Sentinel declares a package's own version of a shared condition, in the
// conventional layered shape:
//
//	var ErrBackendClosed = errs.Sentinel(errs.ErrClosed, "backend is stopped")
//
// which renders as "closed: backend is stopped" — general first, specific
// second, and NO brackets of its own. The bracket belongs to [Wrap], which puts
// it around whatever identity a call site is reporting, so there is exactly one
// pair however deep the layering goes:
//
//	term: write to /dev/tty (closed: backend is stopped)
//
// A sentinel that carried its own brackets would be wrapped into a second pair
// the moment a call site described it.
//
// Use it for a package-level var, not at a call site: it names a CONDITION,
// where Wrap describes an OCCURRENCE. A package whose condition is simply the
// shared one needs no sentinel of its own and should return the base directly.
//
// A nil base is a misuse and is reported the way [Wrap] reports it.
func Sentinel(base error, detail string) error {
	if base == nil {
		return fmt.Errorf("%w: errs.Sentinel: base must not be nil: %s", ErrInvalidArgument, detail)
	}
	return fmt.Errorf("%w: %s", base, detail)
}

// WrapCause is [Wrap] for a failure that has BOTH a first-party identity and an
// underlying cause worth keeping:
//
//	errs.WrapCause(ErrVerifierUnavailable, ctx.Err(), "verification cancelled by the caller")
//
// renders as
//
//	verification cancelled by the caller (sshkey: verifier unavailable): context canceled
//
// and both answer errors.Is — the caller can ask "was this the verifier?" and
// "was this MY cancellation?" and get an exact answer to each.
//
// Use it instead of formatting the cause with %v. That is the mistake this
// helper exists to make hard: %v renders the cause into text and destroys
// everything errors.As would have returned, while the base still answers
// errors.Is, so the check passes and the payload is gone.
//
// A nil base is a misuse and is reported the way [Wrap] reports it. A nil CAUSE
// is allowed and degrades to [Wrap], because "there was no underlying error" is
// a real state and not a mistake.
func WrapCause(base, cause error, format string, args ...any) error {
	if base == nil {
		return fmt.Errorf("%w: errs.WrapCause: base must not be nil: %s", ErrInvalidArgument,
			fmt.Sprintf(format, args...))
	}
	if cause == nil {
		return Wrap(base, format, args...)
	}
	all := make([]any, 0, len(args)+2)
	all = append(all, args...)
	all = append(all, base, cause)
	return fmt.Errorf(format+" (%w): %w", all...)
}

// Recovered builds the error for a value from recover(), keeping that value
// recoverable when it is an error.
//
//	defer func() {
//		if rec := recover(); rec != nil {
//			err = errs.Recovered(ErrPanic, rec, "tui: task %d", id)
//		}
//	}()
//
// A panic value is usually the most informative thing an error will ever
// carry — the convention asks authors to panic with an [Fatal] precisely so a
// recovering caller can errors.As the fields back out — and it is the value
// most easily destroyed on the way out, because the obvious spelling renders it
// into text:
//
//	fmt.Errorf("%w: %v", ErrPanic, rec)   // the identity survives, the value does not
//
// That mistake is invisible: errors.Is(err, ErrPanic) still answers true, so
// the error looks entirely healthy while the payload it was carrying is gone.
// It was written three separate times in this repository before it was named,
// each time as the same hand-rolled type switch, which is why it is a function
// rather than a rule.
//
// A nil rec returns nil, so a deferred handler can call this unconditionally:
// "nothing was recovered" is a real state, not a mistake.
//
// A non-error panic value — a string, an int — has no identity to preserve and
// is formatted into the cause position, so the shape of the message is the same
// either way.
func Recovered(base error, rec any, format string, args ...any) error {
	if rec == nil {
		return nil
	}
	if e, ok := rec.(error); ok {
		return WrapCause(base, e, format, args...)
	}
	return WrapCause(base, fmt.Errorf("%v", rec), format, args...)
}
