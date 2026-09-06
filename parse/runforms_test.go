package parse

import (
	"errors"
	"testing"
)

func wordByte(i int, b byte) bool {
	letter := b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
	if i == 0 {
		return letter // a word may not start with a digit
	}
	return letter || (b >= '0' && b <= '9')
}

func TestRunForm_StartsClaimsAtMostOneByte(t *testing.T) {
	f := RunForm(Word, wordByte)
	if n, m := f.Starts([]byte("")); m != Incomplete || n != 0 {
		t.Errorf(`Starts("") = (%d, %v), want (0, Incomplete)`, n, m)
	}
	// Starts has no boundary, so it can only claim the one byte it can decide;
	// the run's length is End's job.
	for _, s := range []string{"a", "ab", "ab1c"} {
		if n, m := f.Starts([]byte(s)); m != Matched || n != 1 {
			t.Errorf(`Starts(%q) = (%d, %v), want (1, Matched)`, s, n, m)
		}
	}
	if n, m := f.Starts([]byte("9a")); m != NoMatch || n != 0 {
		t.Errorf(`Starts("9a") = (%d, %v), want (0, NoMatch)`, n, m)
	}
}

func TestRunForm_TheBoundaryDecidesWhereARunEnds(t *testing.T) {
	f := RunForm(Word, wordByte)
	opener := []byte("a")

	// A window of pure member bytes defers under MoreInput (the run may grow)
	// and completes at EndOfInput (the word ends there — never unterminated).
	if n, err := f.End([]byte("b1"), opener, MoreInput); !errors.Is(err, ErrNeedMore) || n != 0 {
		t.Errorf("End(b1, MoreInput) = (%d, %v), want (0, ErrNeedMore)", n, err)
	}
	if n, err := f.End([]byte("b1"), opener, EndOfInput); err != nil || n != 2 {
		t.Errorf("End(b1, EndOfInput) = (%d, %v), want (2, nil)", n, err)
	}

	// A refused byte ends the run before it, identically under either boundary:
	// input that already decides itself does not depend on how it arrived.
	for _, bnd := range []InputBoundary{MoreInput, EndOfInput} {
		if n, err := f.End([]byte("b1 c"), opener, bnd); err != nil || n != 2 {
			t.Errorf("End(b1 c, %v) = (%d, %v), want (2, nil) — the space is the next token's", bnd, n, err)
		}
	}
}

// TestRunForm_EndIndexContinuesAcrossTheOpener makes the index base observable.
// A member bounded BY INDEX admits at most three bytes, so End must resolve its
// own bytes at len(openedWith)+i: it sees indices 1 and 2 and refuses at 3. A
// base of zero would take one byte too many, so this test distinguishes them.
func TestRunForm_EndIndexContinuesAcrossTheOpener(t *testing.T) {
	atMost3 := RunForm(Word, func(i int, _ byte) bool { return i < 3 })

	if n, m := atMost3.Starts([]byte("abcdef")); m != Matched || n != 1 {
		t.Fatalf("Starts = (%d, %v), want (1, Matched)", n, m)
	}
	n, err := atMost3.End([]byte("bcdef"), []byte("a"), EndOfInput)
	if err != nil || n != 2 {
		t.Errorf("End = (%d, %v), want (2, nil) — a three-byte run is opener plus two, so the token is abc", n, err)
	}
}

// TestRunForm_ExactOneByteFallbackVsAlwaysTrue pins the difference between the
// honest total-coverage fallback and the one that looks like it. The always-true
// member has no refusing byte, so its maximal run consumes the whole remainder —
// which is why it is not a catch-all.
func TestRunForm_ExactOneByteFallbackVsAlwaysTrue(t *testing.T) {
	oneByte := RunForm(Operator, func(i int, _ byte) bool { return i == 0 })
	if n, m := oneByte.Starts([]byte("-x tail")); m != Matched || n != 1 {
		t.Fatalf(`one-byte Starts("-x tail") = (%d, %v), want (1, Matched)`, n, m)
	}
	for _, bnd := range []InputBoundary{MoreInput, EndOfInput} {
		if n, err := oneByte.End([]byte("x tail"), []byte("-"), bnd); err != nil || n != 0 {
			t.Errorf("one-byte End(%v) = (%d, %v), want (0, nil) — the x begins the next token", bnd, n, err)
		}
	}

	always := RunForm(Operator, func(int, byte) bool { return true })
	rest := []byte("x tail and more")
	if n, err := always.End(rest, []byte("-"), EndOfInput); err != nil || n != len(rest) {
		t.Errorf("always-true End = (%d, %v), want (%d, nil) — it consumes to end of input, "+
			"which is exactly why it cannot serve as the one-byte fallback", n, err, len(rest))
	}
}

func TestRunForm_NilMemberPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("RunForm(nil) did not panic")
		}
	}()
	RunForm(Word, nil)
}

// TestSetForm_StableFirstTerminalOpener: the opener is the SHORTEST literal on
// the observed path and never changes as the window grows. That stability is what
// makes a shared prefix chunk-invariant — Starts fixes a width, not a token.
func TestSetForm_StableFirstTerminalOpener(t *testing.T) {
	dash := SetForm(Operator, "-", "--")
	for _, s := range []string{"-", "--", "-x", "---"} {
		if n, m := dash.Starts([]byte(s)); m != Matched || n != 1 {
			t.Errorf(`Starts(%q) = (%d, %v), want (1, Matched)`, s, n, m)
		}
	}
	if n, m := dash.Starts([]byte("")); m != Incomplete || n != 0 {
		t.Errorf(`Starts("") = (%d, %v), want (0, Incomplete)`, n, m)
	}

	// With no short literal on the path, Starts waits until it reaches the first
	// terminal, and refuses once the path leaves every literal.
	le := SetForm(Operator, "<=")
	if n, m := le.Starts([]byte("<")); m != Incomplete || n != 0 {
		t.Errorf(`Starts("<") = (%d, %v), want (0, Incomplete)`, n, m)
	}
	if n, m := le.Starts([]byte("<=")); m != Matched || n != 2 {
		t.Errorf(`Starts("<=") = (%d, %v), want (2, Matched)`, n, m)
	}
	if n, m := le.Starts([]byte("<x")); m != NoMatch || n != 0 {
		t.Errorf(`Starts("<x") = (%d, %v), want (0, NoMatch)`, n, m)
	}
}

// TestSetForm_SharedPrefixSplitTable pins the agreed shared-prefix table for
// SetForm("-", "--") — the case a two-valued Starts cannot express. Starts is the
// same stable opener in every row; the boundary decides in End.
func TestSetForm_SharedPrefixSplitTable(t *testing.T) {
	f := SetForm(Operator, "-", "--")
	opener := []byte("-")

	for _, c := range []struct {
		name     string
		rest     string
		boundary InputBoundary
		wantN    int
		wantErr  error
		token    string
	}{
		{"lone dash, more may arrive", "", MoreInput, 0, ErrNeedMore, "(defer)"},
		{"lone dash at end of input", "", EndOfInput, 0, nil, `"-"`},
		{"double dash, mid-stream", "-", MoreInput, 1, nil, `"--"`},
		{"double dash at end of input", "-", EndOfInput, 1, nil, `"--"`},
		{"dash then other, mid-stream", "x", MoreInput, 0, nil, `"-" then x`},
		{"dash then other at end of input", "x", EndOfInput, 0, nil, `"-" then x`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if n, m := f.Starts([]byte("-" + c.rest)); m != Matched || n != 1 {
				t.Fatalf("Starts(%q) = (%d, %v), want (1, Matched) — one stable opener for every row",
					"-"+c.rest, n, m)
			}
			n, err := f.End([]byte(c.rest), opener, c.boundary)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) || n != c.wantN {
					t.Fatalf("End(%q, %v) = (%d, %v), want (%d, %v)", c.rest, c.boundary, n, err, c.wantN, c.wantErr)
				}
				return
			}
			if err != nil || n != c.wantN {
				t.Fatalf("End(%q, %v) = (%d, %v), want (%d, nil) — token %s",
					c.rest, c.boundary, n, err, c.wantN, c.token)
			}
		})
	}
}

// TestSetForm_LongestDescendantWins: the opener is the shortest literal, and End
// extends it to the longest completed descendant — so the token is "<=" even
// though Starts answered a width of one.
func TestSetForm_LongestDescendantWins(t *testing.T) {
	f := SetForm(Operator, "<", "<=", "<>")

	if n, m := f.Starts([]byte("<=x")); m != Matched || n != 1 {
		t.Fatalf(`Starts("<=x") = (%d, %v), want (1, Matched)`, n, m)
	}
	if n, err := f.End([]byte("=x"), []byte("<"), EndOfInput); err != nil || n != 1 {
		t.Errorf(`End("=x", "<") = (%d, %v), want (1, nil) — opener plus one makes the token "<="`, n, err)
	}
	// A byte matching no descendant leaves the opener as the whole token.
	if n, err := f.End([]byte("zz"), []byte("<"), EndOfInput); err != nil || n != 0 {
		t.Errorf(`End("zz", "<") = (%d, %v), want (0, nil)`, n, err)
	}
	// While a longer descendant is still consistent with what arrived, defer.
	if n, err := f.End([]byte(""), []byte("<"), MoreInput); !errors.Is(err, ErrNeedMore) || n != 0 {
		t.Errorf(`End("", "<", MoreInput) = (%d, %v), want (0, ErrNeedMore)`, n, err)
	}
}

func TestSetForm_NoMatchAndConstructionPanics(t *testing.T) {
	if n, m := SetForm(Operator, "<=").Starts([]byte("x")); m != NoMatch || n != 0 {
		t.Errorf(`Starts("x") = (%d, %v), want (0, NoMatch)`, n, m)
	}
	for _, bad := range [][]string{nil, {"a", ""}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("SetForm(%q) did not panic", bad)
				}
			}()
			SetForm(Operator, bad...)
		}()
	}
}
