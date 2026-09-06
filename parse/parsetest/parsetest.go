// Package parsetest is the conformance suite for a [parse.Form].
//
// It lives one package out from parse on purpose. Shipping a *testing.T helper
// inside the core would put `import "testing"` in every binary that links the
// parser and register testing's command-line flags there, for a symbol no
// production caller uses. The standard library keeps exactly this kind of
// helper next door — testing/fstest, testing/iotest, net/http/httptest — and
// the reason is the same.
package parsetest

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// Form drives f over every corpus entry at EVERY SPLIT and asserts the contract
// a Form cannot be made to keep by its signature.
//
// # Why every split
//
// A Form is called again from the same offset as the window widens, so it must
// be a pure function of what it was shown. Statefulness is invisible until an
// input straddles a boundary, which is the case a hand-written test is least
// likely to contain and a real stream is most likely to produce. Driving every
// split turns "hard to reach" into "reached every time".
//
// # What it checks
//
//   - Starts answers consistently: once it decides, more input does not change
//     its mind, and Incomplete only ever appears before a decision.
//   - n obeys the contract for each Match value.
//   - End never asks for input that cannot arrive: ErrNeedMore at EndOfInput.
//   - End agrees with itself: a construct that closes within the window closes
//     at the same place however the window was reached.
//
// It does NOT check what the form MEANS — only that it obeys the protocol. A
// form that recognises the wrong thing consistently will pass; that is what
// your own tests are for.
func Form(t testing.TB, f parse.Form, corpus []string) {
	t.Helper()
	run(t, f, corpus)
}

// reporter is the slice of testing.TB this suite uses. It exists so the suite
// can be pointed at a recorder and PROVEN to detect the forms it claims to —
// a conformance suite nobody has seen fail is a conformance suite nobody has
// checked.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

func run(rep reporter, f parse.Form, corpus []string) {
	rep.Helper()
	for _, src := range corpus {
		checkStarts(rep, f, src)
		checkEnd(rep, f, src)
	}
}

func checkStarts(t reporter, f parse.Form, src string) {
	t.Helper()
	decided := false
	var decidedAt int
	var decidedN int
	var decidedM parse.Match

	for i := 0; i <= len(src); i++ {
		window := []byte(src[:i])
		n, m := f.Starts(window)

		switch m {
		case parse.NoMatch, parse.Incomplete:
			if n != 0 {
				t.Errorf("Starts(%q) = (%d, %v): n must be 0 for %v", src[:i], n, m, m)
			}
		case parse.Matched:
			if n <= 0 || n > i {
				t.Errorf("Starts(%q) = (%d, Matched): want 0 < n <= %d — n <= 0 returns the "+
					"scan to this offset forever, n > len(src) claims bytes it was not shown",
					src[:i], n, i)
			}
		default:
			t.Errorf("Starts(%q) returned %v, which is not a Match value", src[:i], m)
		}

		if m == parse.Incomplete {
			if decided {
				t.Errorf("Starts(%q) = Incomplete after Starts(%q) already answered %v — a "+
					"decision may not be withdrawn once more input has only confirmed it",
					src[:i], src[:decidedAt], decidedM)
			}
			continue
		}
		if !decided {
			decided, decidedAt, decidedN, decidedM = true, i, n, m
			continue
		}
		// PURITY. Growing the window may only turn Incomplete into an answer,
		// never one answer into another.
		if m != decidedM || (m == parse.Matched && n != decidedN) {
			t.Errorf("Starts(%q) = (%d, %v) but Starts(%q) = (%d, %v) — the answer changed "+
				"with more input, so the form is not a pure function of what it was shown",
				src[:i], n, m, src[:decidedAt], decidedN, decidedM)
		}
	}
}

func checkEnd(t reporter, f parse.Form, src string) {
	t.Helper()
	openN, m := f.Starts([]byte(src))
	if m != parse.Matched {
		return // End is only reachable for input this form opens
	}
	opener := []byte(src[:openN])
	rest := src[openN:]

	// EVERY PREFIX, BOTH BOUNDARIES. Calling EndOfInput only for the whole
	// remainder leaves a form free to misbehave on every shorter window, which
	// is where a stream actually spends its time.
	answered := false
	var want, wantAt int

	for i := 0; i <= len(rest); i++ {
		window := []byte(rest[:i])

		// --- MoreInput -----------------------------------------------------
		// Two legitimate answers and no third: a terminator that is present,
		// or a request for the bytes that would settle it. A terminal error
		// here is a form giving up on input that has not arrived.
		n, err := f.End(window, opener, parse.MoreInput)
		switch {
		case err == nil:
			if n < 0 || n > i {
				t.Errorf("End(%q, MoreInput) = %d, outside [0, %d] — a form may not claim "+
					"bytes it was not shown", rest[:i], n, i)
			} else if !answered {
				answered, want, wantAt = true, n, i
			} else if n != want {
				t.Errorf("End(%q, MoreInput) = %d but End(%q, MoreInput) = %d — where a "+
					"construct ends cannot depend on how much was read past its terminator",
					rest[:i], n, rest[:wantAt], want)
			}
		case errors.Is(err, parse.ErrNeedMore):
			if n != 0 {
				t.Errorf("End(%q, MoreInput) = (%d, ErrNeedMore): n must be 0 when no "+
					"decision was reached — a count here claims bytes the form just said "+
					"it could not judge", rest[:i], n)
			}
		default:
			t.Errorf("End(%q, MoreInput) = %v: the only refusal available while more input "+
				"may arrive is ErrNeedMore. Reporting the construct terminal here decides "+
				"against bytes that have not been read yet", rest[:i], err)
		}

		// --- EndOfInput ----------------------------------------------------
		// Success, or a typed unterminated report. Never a request for input.
		//
		// The successful n is NOT compared across prefixes: a form that may end
		// at EOF completes every prefix, each at its own length, and that is
		// correct rather than drift.
		eofN, eofErr := f.End(window, opener, parse.EndOfInput)
		switch {
		case eofErr == nil:
			if eofN < 0 || eofN > i {
				t.Errorf("End(%q, EndOfInput) = %d, outside [0, %d]", rest[:i], eofN, i)
			}
		case errors.Is(eofErr, parse.ErrNeedMore):
			t.Errorf("End(%q, EndOfInput) = ErrNeedMore: there is no more input to give, so "+
				"honouring it loops forever and ignoring it picks an answer only the form "+
				"knows", rest[:i])
		default:
			var unterm *parse.UnterminatedError
			if !errors.As(eofErr, &unterm) {
				t.Errorf("End(%q, EndOfInput) = %v: a construct that cannot close at end of "+
					"input reports *parse.UnterminatedError, so a caller can name what was "+
					"left open", rest[:i], eofErr)
			} else if eofN != 0 {
				t.Errorf("End(%q, EndOfInput) = (%d, %v): n must be 0 alongside an error",
					rest[:i], eofN, eofErr)
			}
		}

		// --- the two agree where the bytes decide ---------------------------
		// SAME WINDOW, so this compares the boundary and nothing else.
		if err == nil && (eofErr != nil || eofN != n) {
			t.Errorf("End(%q) = %d at MoreInput but (%d, %v) at EndOfInput — a terminator "+
				"that is present decides the answer, not the boundary",
				rest[:i], n, eofN, eofErr)
		}
	}
}
