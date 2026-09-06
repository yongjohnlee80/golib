package parse_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// The scan-with-lookahead loop must terminate.
//
// This is the shape every consumer of this scanner will write, and it did NOT
// terminate before: Next at end of input returned false without moving the undo
// target, so Unread rewound onto the last real rune, Done reported false again,
// and the same rune was read forever with no end-of-input signal.
//
// The loop is bounded and FAILS rather than hanging, because a test that proves
// termination by running forever is not a test.
func TestScanner_LookaheadLoopTerminates(t *testing.T) {
	const limit = 1000
	sc := parse.NewScanner([]byte("ab"))

	reads := 0
	for !sc.Done() {
		if _, ok := sc.Next(); !ok {
			break
		}
		reads++
		if reads > limit {
			t.Fatalf("read %d runes from a 2-rune source: the loop does not "+
				"terminate, and a caller would hang here rather than see an error",
				reads)
		}
		// Scan a run and put back the character that ended it. This is the
		// idiom that spins, and it is written the way a caller writes it: the
		// terminator is unread WITHOUT first asking whether one was read,
		// because at end of input there is nothing to put back and Unread is
		// expected to know that.
		for {
			r, ok := sc.Next()
			if !ok || r == ' ' {
				sc.Unread()
				break
			}
			reads++
			if reads > limit {
				t.Fatalf("read %d runes from a 2-rune source: the inner run scan "+
					"does not terminate", reads)
			}
		}
	}
	if reads != 2 {
		t.Errorf("read %d runes, want 2", reads)
	}
}

// Unread after a Next that consumed nothing must consume nothing in turn.
// Stated on its own because it is the exact mechanism behind the loop above,
// and a future change could fix one without the other.
func TestScanner_UnreadAfterFailedNextIsANoOp(t *testing.T) {
	sc := parse.NewScanner([]byte("x"))
	if _, ok := sc.Next(); !ok {
		t.Fatal("the fixture is wrong: the first Next must succeed")
	}
	at := sc.Pos()
	if _, ok := sc.Next(); ok {
		t.Fatal("the fixture is wrong: the second Next must report end of input")
	}

	sc.Unread()

	if sc.Pos() != at {
		t.Errorf("Unread moved the cursor from %+v to %+v after a Next that read "+
			"nothing", at, sc.Pos())
	}
	if !sc.Done() {
		t.Error("the scanner reports more input after the source was exhausted")
	}
	if r, ok := sc.Next(); ok {
		t.Errorf("the last rune %q is readable a second time", r)
	}
}

// Unread after a SUCCESSFUL Next must still step back — the fix must not have
// disabled the feature it was guarding.
func TestScanner_UnreadAfterSuccessfulNextStillStepsBack(t *testing.T) {
	sc := parse.NewScanner([]byte("ab"))
	first, _ := sc.Next()
	sc.Unread()
	again, ok := sc.Next()
	if !ok || again != first {
		t.Errorf("re-read %q ok=%v after Unread, want %q true", again, ok, first)
	}
}

// Position must track lines and columns in runes, since the numbers are shown
// to a person and must match what an editor reports.
func TestScanner_PositionTracksLinesAndRunes(t *testing.T) {
	sc := parse.NewScanner([]byte("aé\nb"))
	want := []parse.Position{
		{Offset: 0, Line: 1, Column: 1},
		{Offset: 1, Line: 1, Column: 2},
		{Offset: 3, Line: 1, Column: 3},
		{Offset: 4, Line: 2, Column: 1},
	}
	for i, w := range want {
		if got := sc.Pos(); got != w {
			t.Errorf("before rune %d: pos = %+v, want %+v", i, got, w)
		}
		sc.Next()
	}
}
