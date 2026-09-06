package parse

import (
	"errors"
	"fmt"
)

// A Form recognises one construct. New forms are new implementations; the
// lexer is never edited to accept them.
//
// # Two answers are not enough
//
// Both methods can be called with a window that does not contain the whole
// construct — the ordinary case for a streaming lexer, where a delimiter, a
// long comment or a large literal routinely outruns the bytes read so far. A
// signature that admits only yes and no forces a guess at every chunk
// boundary, and a guess is a bug that appears only when the boundary lands in
// the wrong place.
//
// # Purity is a contract this interface cannot enforce
//
// A Form MUST be a pure function of its arguments. It will be called again
// from the same offset with more input, so anything remembered between calls
// is a wrong answer waiting for an unlucky split. The type system will not
// hold this; the conformance suite drives every input at every split and will.
//
// # The byte slices are READ-ONLY and live only for the call
//
// src and openedWith are windows the scan owns, not copies made for the form. A
// window may be a reslice of the caller's own input, or a shared buffer the next
// widening appends to. So a Form must not write to either slice and must not
// retain either past the call that received it: doing the first corrupts the
// caller's bytes, and the second leaves a form reading a buffer that has since
// moved on.
type Form interface {
	// Starts reports whether src opens this form. THREE ANSWERS, not two.
	Starts(src []byte) (n int, r Match)

	// End reports where the construct closes, given the bytes AFTER the opener
	// and the opener itself — which a form carrying a tag needs in order to
	// know what it is looking for.
	//
	// It is told whether more input may arrive because whether end-of-input
	// COMPLETES a construct is a property of the form: a line comment ends at
	// EOF, an unterminated quote is an error there. No single rule is right for
	// both, so the core cannot own this one.
	End(src []byte, openedWith []byte, boundary InputBoundary) (n int, err error)

	// Kind is the token kind this form produces.
	Kind() Kind
}

// Match is Starts' answer. The third value is the point of it.
type Match uint8

const (
	// NoMatch: not this form, whatever arrives later. n must be 0.
	NoMatch Match = iota
	// Matched: opens this form, consuming n bytes. 0 < n <= len(src).
	Matched
	// Incomplete: CANNOT DECIDE with these bytes. n must be 0.
	//
	// A form that could say how many more bytes it needs would be a form that
	// had already decided, so there is deliberately no count here.
	Incomplete
)

func (m Match) String() string {
	switch m {
	case NoMatch:
		return "NoMatch"
	case Matched:
		return "Matched"
	case Incomplete:
		return "Incomplete"
	}
	return "Match(" + itoa(uint8(m)) + ")"
}

// InputBoundary says whether src is all there will ever be.
type InputBoundary uint8

const (
	// MoreInput: src may grow. A decision can be deferred with ErrNeedMore.
	MoreInput InputBoundary = iota
	// EndOfInput: src is the whole remainder. Decide now — asking for more
	// input here asks for bytes that cannot exist.
	EndOfInput
)

func (b InputBoundary) String() string {
	switch b {
	case MoreInput:
		return "MoreInput"
	case EndOfInput:
		return "EndOfInput"
	}
	return "InputBoundary(" + itoa(uint8(b)) + ")"
}

// ErrNeedMore reports that a decision cannot be made with the bytes supplied.
// The lexer widens the window and calls again from the same offset.
//
// It is legal only at MoreInput. Returning it at EndOfInput asks for input that
// cannot arrive: honouring it loops forever, and ignoring it silently picks one
// of the two answers only the form knows. The core reports it as a contract
// violation instead of choosing.
var ErrNeedMore = errors.New("parse: need more input")

// ErrFormContract reports a Form that broke the rules of the interface. It is
// never the input's fault and never recoverable by reading further; the fix is
// in the Form.
var ErrFormContract = errors.New("parse: form contract violation")

// UnterminatedError reports a construct that reached end of input without its
// terminator. Forms return it WITHOUT a position: a Form sees a window, not the
// stream, so the offset is the core's to attach.
type UnterminatedError struct {
	Kind Kind
	Open string // the opener, verbatim, so the message can quote it
}

func (e *UnterminatedError) Error() string {
	return fmt.Sprintf("parse: unterminated %s opened by %q", e.Kind, e.Open)
}

// contractf builds a violation that names what was expected of the form.
func contractf(f Form, format string, args ...any) error {
	return fmt.Errorf("%w: %T: %s", ErrFormContract, f, fmt.Sprintf(format, args...))
}
