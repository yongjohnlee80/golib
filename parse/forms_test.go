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

// Starts answers Incomplete rather than guessing at a window
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

// The same unterminated bytes, the same call, one boundary value
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

// Input that already decides itself must not lex differently
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

// A MULTI-BYTE CLOSER RULES OUT A DOUBLING WITH ITS FIRST DISAGREEING BYTE.
//
// The one-byte case cannot expose this: with a single-byte closer, "is the next
// byte the same closer?" is answerable from one byte, so there is never a
// visible byte that disagrees while the question is still open. With "END",
// `xENDz` has a complete closer AND a `z` that already rules out a second one —
// deferring there stalls a live stream on input that has decided itself, and
// makes the answer depend on the boundary, which the contract forbids.
func TestQuoteForm_MultiByteDoublingDefersOnlyOnAProperPrefix(t *testing.T) {
	t.Parallel()
	f := QuoteForm("BEGIN", "END", QuoteOpts{Doubling: true})

	for _, tc := range []struct {
		name    string
		src     string
		wantN   int
		wantErr bool // ErrNeedMore under MoreInput
	}{
		{"a visible byte disagrees, so the closer is final", "xENDz", 4, false},
		{"nothing after the closer yet: a doubling is still possible", "xEND", 0, true},
		{"a proper prefix follows: still possible", "xENDE", 0, true},
		{"a longer proper prefix follows: still possible", "xENDEN", 0, true},
		{"an escaped closer, then a real one with a byte after it", "xENDENDyENDz", 11, false},
		{"an escaped closer with no terminator yet", "xENDENDy", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, err := f.End([]byte(tc.src), []byte("BEGIN"), MoreInput)
			if tc.wantErr {
				if !errors.Is(err, ErrNeedMore) || n != 0 {
					t.Fatalf("End(%q, MoreInput) = (%d, %v), want (0, ErrNeedMore)", tc.src, n, err)
				}
				return
			}
			if err != nil || n != tc.wantN {
				t.Fatalf("End(%q, MoreInput) = (%d, %v), want (%d, nil)", tc.src, n, err, tc.wantN)
			}
			// Input that decides itself gives the same answer
			// however it arrived.
			eofN, eofErr := f.End([]byte(tc.src), []byte("BEGIN"), EndOfInput)
			if eofErr != nil || eofN != tc.wantN {
				t.Fatalf("End(%q, EndOfInput) = (%d, %v), want (%d, nil) — the boundary must "+
					"not change an answer the bytes already give", tc.src, eofN, eofErr, tc.wantN)
			}
		})
	}
}

// pgTag is the rule a PostgreSQL leaf would supply. It lives in the TEST,
// because writing it into forms.go is exactly what the no-dialect rule forbids:
// the
// core would then know a dialect by name.
func pgTag(index int, b byte) bool {
	letter := b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
	if index == 0 {
		return letter
	}
	return letter || (b >= '0' && b <= '9')
}

// A TAG RULE MUST BE POSITION-AWARE. This is the case that a func(byte) bool
// cannot express, and the reason the signature carries an index: the same digit
// is illegal first and legal afterwards, so `$1$` must be refused while `$a1$`
// is accepted.
func TestDelimitedForm_TheTagRuleIsPositionAware(t *testing.T) {
	t.Parallel()
	f := DelimitedForm('$', '$', DelimitedOpts{TagByte: pgTag, AllowEmpty: true})

	for _, tc := range []struct {
		src   string
		wantN int
		want  Match
		why   string
	}{
		{"$a1$", 4, Matched, "a digit after a letter is an ordinary tag byte"},
		{"$1$", 0, NoMatch, "a leading digit is a parameter marker in this dialect, not a tag"},
		{"$$", 2, Matched, "the empty tag is allowed here"},
		{"$_x$", 4, Matched, "underscore leads legally"},
		{"$ $", 0, NoMatch, "a space is not a tag byte at any position"},
	} {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			n, m := f.Starts([]byte(tc.src))
			if m != tc.want || (m == Matched && n != tc.wantN) {
				t.Fatalf("Starts(%q) = (%d, %v), want (%d, %v) — %s",
					tc.src, n, m, tc.wantN, tc.want, tc.why)
			}
		})
	}
}

// A REJECTED TAG IS NoMatch, NOT AN UNTERMINATED CONSTRUCT. The distinction is
// what lets a later form lex the prefix: `$1` is a parameter, and a form that
// claimed it and then failed would take the token away from whatever can.
func TestDelimitedForm_ARejectedTagLeavesThePrefixForOtherForms(t *testing.T) {
	t.Parallel()
	f := DelimitedForm('$', '$', DelimitedOpts{TagByte: pgTag})

	if _, m := f.Starts([]byte("$1$ body $1$")); m != NoMatch {
		t.Fatalf("Starts on a rejected tag = %v, want NoMatch", m)
	}
	// The empty tag is refused by default too, and by the same route.
	if _, m := f.Starts([]byte("$$ body $$")); m != NoMatch {
		t.Fatalf("Starts(%q) = %v, want NoMatch — AllowEmpty is off", "$$ body $$", m)
	}
	// SPECIFICITY: turning AllowEmpty on changes exactly that answer.
	on := DelimitedForm('$', '$', DelimitedOpts{TagByte: pgTag, AllowEmpty: true})
	if n, m := on.Starts([]byte("$$ body $$")); m != Matched || n != 2 {
		t.Fatalf("with AllowEmpty: (%d, %v), want (2, Matched)", n, m)
	}
}

// The bound is decidable BEFORE the suffix arrives: once the tag is longer than
// it may ever be, no later byte can rescue it.
func TestDelimitedForm_AnOversizedTagIsRefusedWithoutWaiting(t *testing.T) {
	t.Parallel()
	f := DelimitedForm('$', '$', DelimitedOpts{TagByte: pgTag, MaxTagBytes: 3})

	if n, m := f.Starts([]byte("$abc$")); m != Matched || n != 5 {
		t.Fatalf("a tag at the limit: (%d, %v), want (5, Matched)", n, m)
	}
	if _, m := f.Starts([]byte("$abcd$")); m != NoMatch {
		t.Fatalf("a tag past the limit = %v, want NoMatch", m)
	}
	// AND WITHOUT THE SUFFIX IN VIEW: the answer does not wait for a terminator
	// that cannot change it.
	if _, m := f.Starts([]byte("$abcd")); m != NoMatch {
		t.Fatalf("an over-long tag with no suffix yet = %v, want NoMatch — the bound is "+
			"already spent, so no later byte can make this legal", m)
	}
	// SPECIFICITY: a tag still within the bound and still unterminated defers.
	if _, m := f.Starts([]byte("$ab")); m != Incomplete {
		t.Fatalf("a legal partial tag = %v, want Incomplete", m)
	}
}

// The terminator is the opener repeated, so a tagged form only ends on ITS tag.
func TestDelimitedForm_EndsOnlyOnItsOwnTag(t *testing.T) {
	t.Parallel()
	f := DelimitedForm('$', '$', DelimitedOpts{TagByte: pgTag})
	const body = " body $other$ still body $tag$ tail"

	n, err := f.End([]byte(body), []byte("$tag$"), MoreInput)
	if err != nil {
		t.Fatalf("End: %v", err)
	}
	if want := len(" body $other$ still body $tag$"); n != want {
		t.Fatalf("End = %d, want %d — a different tag is body text", n, want)
	}
	if _, err := f.End([]byte(" body $other$ "), []byte("$tag$"), MoreInput); !errors.Is(err, ErrNeedMore) {
		t.Fatalf("End with only a foreign tag in view = %v, want ErrNeedMore", err)
	}
	var unterm *UnterminatedError
	if _, err := f.End([]byte(" body $other$ "), []byte("$tag$"), EndOfInput); !errors.As(err, &unterm) {
		t.Fatalf("End at EndOfInput = %v, want *UnterminatedError", err)
	}
}

// A negative bound expresses nothing a caller could have meant — zero already
// means "bounded only by the scan limit" — so it is refused where it was
// written rather than silently becoming an unbounded scan.
func TestDelimitedForm_RejectsANegativeBound(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("DelimitedForm accepted a negative MaxTagBytes")
		}
	}()
	DelimitedForm('$', '$', DelimitedOpts{MaxTagBytes: -1})
}

// SPECIFICITY: zero is legal and means unbounded here.
func TestDelimitedForm_ZeroBoundIsUnbounded(t *testing.T) {
	t.Parallel()
	f := DelimitedForm('$', '$', DelimitedOpts{TagByte: pgTag, MaxTagBytes: 0})
	long := "$" + strings.Repeat("a", 500) + "$"
	if n, m := f.Starts([]byte(long)); m != Matched || n != len(long) {
		t.Fatalf("Starts(len %d tag) = (%d, %v), want (%d, Matched)", len(long), n, m, len(long))
	}
}
