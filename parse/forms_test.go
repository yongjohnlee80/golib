package parse

import (
	"errors"
	"strings"
	"testing"
)

// everySplit calls Starts with each prefix of src, from empty to whole, and
// returns the answers in order. A form is pure, so the sequence must be
// monotone in information: once it says NoMatch or Matched it may not change
// its mind, and Incomplete may only appear before a decision.
func everySplit(f Form, src string) []Match {
	out := make([]Match, 0, len(src)+1)
	for i := 0; i <= len(src); i++ {
		_, m := f.Starts([]byte(src[:i]))
		out = append(out, m)
	}
	return out
}

// CRITERION 17: Starts answers Incomplete rather than guessing at a window
// edge, driven over EVERY split of a shared-prefix opener pair.
//
// `/` against `/*` is the case that cannot be decided by declaration order: with
// one byte visible, "is this a block comment?" has no answer yet, and both of
// the two-valued answers are wrong.
func TestStarts_IncompleteAtEveryWindowEdge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		form Form
		src  string
		want []Match
	}{
		{"block comment opener arrives one byte at a time", BlockComment("/*", "*/", false), "/*",
			[]Match{Incomplete, Incomplete, Matched}},
		{"the same first byte, then a byte that rules it out", BlockComment("/*", "*/", false), "/x",
			[]Match{Incomplete, Incomplete, NoMatch}},
		{"line comment shares its first byte with an operator", LineComment("--"), "--",
			[]Match{Incomplete, Incomplete, Matched}},
		{"a single dash is a decision nobody can make yet", LineComment("--"), "-",
			[]Match{Incomplete, Incomplete}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := everySplit(tc.form, tc.src)
			if len(got) != len(tc.want) {
				t.Fatalf("%d answers for %q, want %d", len(got), tc.src, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("Starts(%q) = %v, want %v", tc.src[:i], got[i], tc.want[i])
				}
			}
		})
	}
}

// SPECIFICITY: a shorter opener that is NOT a prefix must be refused at once,
// not deferred. Incomplete everywhere would satisfy the test above; it must not
// satisfy this one.
func TestStarts_UnrelatedFirstByteIsRefusedImmediately(t *testing.T) {
	t.Parallel()
	f := BlockComment("/*", "*/", false)
	if _, m := f.Starts([]byte("x")); m != NoMatch {
		t.Fatalf("Starts(%q) = %v, want NoMatch — a byte that cannot begin the opener "+
			"is decidable with one byte", "x", m)
	}
}

// CRITERION 20: the same unterminated bytes, the same call, one boundary value
// apart. This is the whole reason End takes a boundary.
func TestEnd_TheBoundaryDecidesWhatEOFMeans(t *testing.T) {
	t.Parallel()
	comment := LineComment("--")
	quote := QuoteForm("'", "'", QuoteOpts{Doubling: true})
	const rest = " no terminator here"

	// MORE MAY ARRIVE: both defer. Neither is entitled to an answer yet.
	if n, err := comment.End([]byte(rest), []byte("--"), MoreInput); !errors.Is(err, ErrNeedMore) || n != 0 {
		t.Errorf("line comment at MoreInput = (%d, %v), want (0, ErrNeedMore)", n, err)
	}
	if n, err := quote.End([]byte(rest), []byte("'"), MoreInput); !errors.Is(err, ErrNeedMore) || n != 0 {
		t.Errorf("quote at MoreInput = (%d, %v), want (0, ErrNeedMore)", n, err)
	}

	// END OF INPUT: they part company, and the bytes are identical.
	if n, err := comment.End([]byte(rest), []byte("--"), EndOfInput); err != nil || n != len(rest) {
		t.Errorf("line comment at EndOfInput = (%d, %v), want (%d, nil) — a comment may "+
			"end at EOF", n, err, len(rest))
	}
	n, err := quote.End([]byte(rest), []byte("'"), EndOfInput)
	var unterm *UnterminatedError
	if !errors.As(err, &unterm) || n != 0 {
		t.Fatalf("quote at EndOfInput = (%d, %v), want (0, *UnterminatedError) — an "+
			"unterminated literal is an error wherever the input stops", n, err)
	}
	if unterm.Kind != String || unterm.Open != "'" {
		t.Errorf("UnterminatedError{Kind:%v, Open:%q}, want {String, \"'\"}", unterm.Kind, unterm.Open)
	}
}

// CRITERION 21: input that already decides itself must not lex differently
// because of how it arrived.
func TestEnd_APresentTerminatorIsUnaffectedByTheBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		form   Form
		src    string
		opener string
		want   int
	}{
		{"line comment", LineComment("--"), " text\nmore", "--", 5},
		{"block comment", BlockComment("/*", "*/", false), " a */ tail", "/*", 5},
		{"nested block comment", BlockComment("/*", "*/", true), " a /* b */ c */ tail", "/*", 15},
		{"quote", QuoteForm("'", "'", QuoteOpts{Doubling: true}), "abc' tail", "'", 4},
		{"quote with a doubled delimiter inside", QuoteForm("'", "'", QuoteOpts{Doubling: true}), "a''b' tail", "'", 5},
		{"quote with a backslash escape", QuoteForm("'", "'", QuoteOpts{Escape: '\\'}), `a\'b' tail`, "'", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			more, errMore := tc.form.End([]byte(tc.src), []byte(tc.opener), MoreInput)
			eof, errEOF := tc.form.End([]byte(tc.src), []byte(tc.opener), EndOfInput)
			if errMore != nil || errEOF != nil {
				t.Fatalf("errors: MoreInput %v, EndOfInput %v", errMore, errEOF)
			}
			if more != tc.want || eof != tc.want {
				t.Fatalf("n = %d at MoreInput, %d at EndOfInput; want %d for both",
					more, eof, tc.want)
			}
		})
	}
}

// A close delimiter at the very edge of the window is ambiguous when doubling
// is in play: it may be the terminator, or the first half of an escaped
// occurrence. Answering either way is a coin flip, so it defers.
func TestQuoteForm_DoubledDelimiterAtTheWindowEdgeDefers(t *testing.T) {
	t.Parallel()
	f := QuoteForm("'", "'", QuoteOpts{Doubling: true})

	if n, err := f.End([]byte("abc'"), []byte("'"), MoreInput); !errors.Is(err, ErrNeedMore) || n != 0 {
		t.Fatalf("End(%q) at MoreInput = (%d, %v), want (0, ErrNeedMore) — the next byte "+
			"decides whether this quote ends the literal or escapes itself", "abc'", n, err)
	}
	// At EOF there is no next byte, so it closes.
	if n, err := f.End([]byte("abc'"), []byte("'"), EndOfInput); err != nil || n != 4 {
		t.Fatalf("End(%q) at EndOfInput = (%d, %v), want (4, nil)", "abc'", n, err)
	}
	// SPECIFICITY: without doubling there is no ambiguity to defer for.
	plain := QuoteForm("'", "'", QuoteOpts{})
	if n, err := plain.End([]byte("abc'"), []byte("'"), MoreInput); err != nil || n != 4 {
		t.Fatalf("plain quote End(%q) at MoreInput = (%d, %v), want (4, nil) — nothing is "+
			"ambiguous when a close cannot be escaped by doubling", "abc'", n, err)
	}
}

// A terminator split across the window edge is not an absent terminator.
func TestBlockComment_PartialTerminatorAtTheEdgeDefers(t *testing.T) {
	t.Parallel()
	f := BlockComment("/*", "*/", false)
	if n, err := f.End([]byte(" body *"), []byte("/*"), MoreInput); !errors.Is(err, ErrNeedMore) || n != 0 {
		t.Fatalf("End(%q) = (%d, %v), want (0, ErrNeedMore)", " body *", n, err)
	}
	if n, err := f.End([]byte(" body */"), []byte("/*"), MoreInput); err != nil || n != 8 {
		t.Fatalf("End(%q) = (%d, %v), want (8, nil)", " body */", n, err)
	}
}

// Nesting is not the same walk as non-nesting, and the difference shows on
// input that contains a nested opener.
func TestBlockComment_NestingChangesWhereItEnds(t *testing.T) {
	t.Parallel()
	const src = " a /* b */ c */ tail"
	nesting, err := BlockComment("/*", "*/", true).End([]byte(src), []byte("/*"), EndOfInput)
	if err != nil {
		t.Fatal(err)
	}
	flat, err := BlockComment("/*", "*/", false).End([]byte(src), []byte("/*"), EndOfInput)
	if err != nil {
		t.Fatal(err)
	}
	if flat != 10 || nesting != 15 {
		t.Fatalf("flat=%d nesting=%d, want 10 and 15 — a non-nesting comment ends at the "+
			"first close; a nesting one counts depth", flat, nesting)
	}
}

// An unterminated nested comment is still unterminated, and reports the kind
// and opener rather than a bare error.
func TestBlockComment_UnterminatedNamesTheConstruct(t *testing.T) {
	t.Parallel()
	_, err := BlockComment("/*", "*/", true).End([]byte(" a /* b "), []byte("/*"), EndOfInput)
	var unterm *UnterminatedError
	if !errors.As(err, &unterm) {
		t.Fatalf("err = %v, want *UnterminatedError", err)
	}
	if !strings.Contains(unterm.Error(), "/*") || !strings.Contains(unterm.Error(), "Comment") {
		t.Fatalf("message %q names neither the construct nor its opener", unterm.Error())
	}
}

// An empty delimiter matches everywhere and consumes nothing: an infinite loop
// at the first offset. It is refused where the mistake was made.
func TestForms_RejectEmptyDelimiters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"LineComment", func() { LineComment("") }},
		{"BlockComment open", func() { BlockComment("", "*/", false) }},
		{"BlockComment close", func() { BlockComment("/*", "", false) }},
		{"QuoteForm open", func() { QuoteForm("", "'", QuoteOpts{}) }},
		{"QuoteForm close", func() { QuoteForm("'", "", QuoteOpts{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Fatal("no panic; an empty delimiter is an infinite loop, not a form")
				}
			}()
			tc.call()
		})
	}
}

// SPECIFICITY for the above: ordinary delimiters must not panic.
func TestForms_AcceptOrdinaryDelimiters(t *testing.T) {
	t.Parallel()
	_ = LineComment("--")
	_ = BlockComment("/*", "*/", true)
	_ = QuoteForm("'", "'", QuoteOpts{Doubling: true})
}
