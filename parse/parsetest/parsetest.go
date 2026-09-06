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

	// ErrNeedMore at EndOfInput asks for bytes that cannot exist. Honouring it
	// loops forever; ignoring it silently picks an answer only the form knows.
	if _, err := f.End([]byte(rest), opener, parse.EndOfInput); errors.Is(err, parse.ErrNeedMore) {
		t.Errorf("End(%q, EndOfInput) = ErrNeedMore: there is no more input to give", rest)
	}

	// WHERE A CONSTRUCT ENDS DOES NOT DEPEND ON HOW MUCH WAS READ PAST IT.
	//
	// Stated as "every window that ANSWERS must agree", not "every window from
	// the terminator onwards must answer". Deferring is always legitimate on a
	// window that may still grow — a doubling quote whose closer is the last
	// byte cannot yet tell a terminator from the first half of an escaped pair,
	// and demanding an answer there would be demanding the coin flip this
	// design exists to avoid. What is NOT legitimate is two different answers.
	answered := false
	var want, wantAt int
	for i := 0; i <= len(rest); i++ {
		n, err := f.End([]byte(rest[:i]), opener, parse.MoreInput)
		if err != nil {
			if !errors.Is(err, parse.ErrNeedMore) {
				var unterm *parse.UnterminatedError
				if !errors.As(err, &unterm) {
					t.Errorf("End(%q) = %v: a Form may only report ErrNeedMore or an "+
						"unterminated construct", rest[:i], err)
				}
			}
			continue
		}
		if n < 0 || n > i {
			t.Errorf("End(%q) = %d, outside [0, %d] — a form may not claim bytes it was "+
				"not shown", rest[:i], n, i)
			continue
		}
		if !answered {
			answered, want, wantAt = true, n, i
			continue
		}
		if n != want {
			t.Errorf("End(%q) = %d but End(%q) = %d — where a construct ends cannot depend "+
				"on how much was read past its terminator", rest[:i], n, rest[:wantAt], want)
		}
	}

	// And the boundary must not move a terminator that is present.
	if full, err := f.End([]byte(rest), opener, parse.MoreInput); err == nil {
		if n, err := f.End([]byte(rest), opener, parse.EndOfInput); err != nil || n != full {
			t.Errorf("End(%q) = %d at MoreInput but (%d, %v) at EndOfInput — a terminator "+
				"that is present decides the answer, not the boundary", rest, full, n, err)
		}
	}
}
