package parsetest

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// recorder stands in for *testing.T so the suite can be watched failing.
type recorder struct{ msgs []string }

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

// says reports whether any recorded message contains want. Used where the
// DIAGNOSIS is the thing under test: two checks may both notice a violation
// while only one of them names it correctly, and a bare "something failed"
// cannot tell them apart.
func (r *recorder) says(want string) bool {
	for _, m := range r.msgs {
		if strings.Contains(m, want) {
			return true
		}
	}
	return false
}

var corpus = []string{
	"-- a line comment\nnext",
	"/* block */ tail",
	"'a literal' tail",
	"'with ''doubled'' inside' tail",
	"$tag$ body $tag$ tail",
	"not a construct at all",
	"",
	"-",
	"/",
	"'",
}

// THE SUITE PASSES THE REAL FORMS. Establish that first: a suite that fails
// everything detects nothing, it just fails.
func TestSuiteAcceptsTheGenericForms(t *testing.T) {
	t.Parallel()
	for name, f := range map[string]parse.Form{
		"line comment":  parse.LineComment("--"),
		"block comment": parse.BlockComment("/*", "*/", false),
		"nesting block": parse.BlockComment("/*", "*/", true),
		"quote":         parse.QuoteForm("'", "'", parse.QuoteOpts{Doubling: true}),
		"quote escape":  parse.QuoteForm("'", "'", parse.QuoteOpts{Escape: '\\'}),
		"delimited":     parse.DelimitedForm('$', '$', parse.DelimitedOpts{AllowEmpty: true}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			Form(t, f, corpus)
		})
	}
}

// statefulForm answers correctly the first time and differently afterwards —
// the defect the suite exists to catch, and the one no hand-written table finds
// because a table calls each input once.
type statefulForm struct{ calls int }

func (f *statefulForm) Kind() parse.Kind { return parse.Comment }

func (f *statefulForm) Starts(src []byte) (int, parse.Match) {
	f.calls++
	if f.calls > 3 {
		return 0, parse.NoMatch
	}
	if len(src) >= 2 && string(src[:2]) == "--" {
		return 2, parse.Matched
	}
	if len(src) < 2 && strings.HasPrefix("--", string(src)) {
		return 0, parse.Incomplete
	}
	return 0, parse.NoMatch
}

func (f *statefulForm) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	return len(src), nil
}

// CRITERION 22: the suite detects a stateful form. Shown failing, not asserted
// to be capable of it.
func TestSuiteDetectsAStatefulForm(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, &statefulForm{}, []string{"-- comment"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted a form that changes its answer on later calls; it " +
			"cannot be relied on to catch the defect it exists for")
	}
}

// contractBreaker returns Matched with n == 0: the scan would return to this
// offset with this input forever.
type contractBreaker struct{}

func (contractBreaker) Kind() parse.Kind { return parse.Word }
func (contractBreaker) Starts(src []byte) (int, parse.Match) {
	if len(src) == 0 {
		return 0, parse.Incomplete
	}
	return 0, parse.Matched
}
func (contractBreaker) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	return 0, nil
}

func TestSuiteDetectsAZeroWidthMatch(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, contractBreaker{}, []string{"x"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted Matched with n == 0, which is an infinite loop at one offset")
	}
}

// eofBegger asks for input at EndOfInput, where none can arrive.
type eofBegger struct{}

func (eofBegger) Kind() parse.Kind { return parse.String }
func (eofBegger) Starts(src []byte) (int, parse.Match) {
	if len(src) == 0 {
		return 0, parse.Incomplete
	}
	if src[0] == '\'' {
		return 1, parse.Matched
	}
	return 0, parse.NoMatch
}
func (eofBegger) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	return 0, parse.ErrNeedMore
}

func TestSuiteDetectsErrNeedMoreAtEndOfInput(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, eofBegger{}, []string{"'unterminated"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted ErrNeedMore at EndOfInput, which asks for bytes that " +
			"cannot exist")
	}
	// AND IT MUST SAY SO. Without the dedicated branch this violation still
	// fails, through the untyped-error check, with a message telling the author
	// to return *UnterminatedError — advice that is wrong for this defect. A
	// mutation control cannot see the difference; asserting the diagnosis can.
	if !rec.says("no more input to give") {
		t.Fatalf("detected, but diagnosed as something else: %q", rec.msgs)
	}
}

// driftingEnd moves its terminator when it is shown more input past it.
type driftingEnd struct{}

func (driftingEnd) Kind() parse.Kind { return parse.String }
func (driftingEnd) Starts(src []byte) (int, parse.Match) {
	if len(src) == 0 {
		return 0, parse.Incomplete
	}
	if src[0] == '\'' {
		return 1, parse.Matched
	}
	return 0, parse.NoMatch
}
func (driftingEnd) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	return len(src), nil // "ends wherever the window ends"
}

func TestSuiteDetectsATerminatorThatMovesWithTheWindow(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, driftingEnd{}, []string{"'abc' and more"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted an End whose answer depends on how much was read past " +
			"the terminator")
	}
}

// ---------------------------------------------------------------------------
// Decoys for the End matrix. Each breaks ONE rule the suite documents.
// ---------------------------------------------------------------------------

// opensOnQuote is the shared Starts for the decoys below: they differ only in
// how End misbehaves, so Starts must not be where they are caught.
type opensOnQuote struct{}

func (opensOnQuote) Kind() parse.Kind { return parse.String }
func (opensOnQuote) Starts(src []byte) (int, parse.Match) {
	if len(src) == 0 {
		return 0, parse.Incomplete
	}
	if src[0] == '\'' {
		return 1, parse.Matched
	}
	return 0, parse.NoMatch
}

// prematureGiveUp reports the construct unterminated while more input may still
// arrive. The bytes that would close it are simply not here YET.
type prematureGiveUp struct{ opensOnQuote }

func (prematureGiveUp) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	return 0, &parse.UnterminatedError{Kind: parse.String, Open: string(openedWith)}
}

// beggarWithACount asks for more input AND claims bytes at the same time. It
// breaks exactly ONE rule: everything else it does is legitimate, so the check
// for that rule is the only thing that can catch it. A decoy that breaks two
// lets each check hide behind the other, and neither gets proven.
type beggarWithACount struct{ opensOnQuote }

func (beggarWithACount) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	if i := bytes.IndexByte(src, '\''); i >= 0 {
		return i + 1, nil
	}
	if b == parse.EndOfInput {
		return 0, &parse.UnterminatedError{Kind: parse.String, Open: string(openedWith)}
	}
	return 1, parse.ErrNeedMore // the one violation: a count alongside a refusal
}

// untypedAtEOF reports a bare error where the contract requires a typed one, so
// a caller cannot name what was left open. Everything else is correct.
type untypedAtEOF struct{ opensOnQuote }

func (untypedAtEOF) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	if i := bytes.IndexByte(src, '\''); i >= 0 {
		return i + 1, nil
	}
	if b == parse.EndOfInput {
		return 0, errors.New("parsetest_test: unterminated, but untyped")
	}
	return 0, parse.ErrNeedMore
}

// countAlongsideAnError returns a byte count next to its unterminated report.
type countAlongsideAnError struct{ opensOnQuote }

func (countAlongsideAnError) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	if i := bytes.IndexByte(src, '\''); i >= 0 {
		return i + 1, nil
	}
	if b == parse.EndOfInput {
		return len(src), &parse.UnterminatedError{Kind: parse.String, Open: string(openedWith)}
	}
	return 0, parse.ErrNeedMore
}

// boundarySwinger closes at one place under MoreInput and another at EOF, on
// the SAME window — the bytes decide, and it lets the boundary decide instead.
type boundarySwinger struct{ opensOnQuote }

func (boundarySwinger) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	i := bytes.IndexByte(src, '\'')
	if i < 0 {
		if b == parse.EndOfInput {
			return 0, &parse.UnterminatedError{Kind: parse.String, Open: string(openedWith)}
		}
		return 0, parse.ErrNeedMore
	}
	if b == parse.EndOfInput {
		return len(src), nil // the same bytes, a different answer
	}
	return i + 1, nil
}

// eofBeggarOnASplit answers properly for the whole remainder but asks for more
// input at EOF on a SHORTER window — the case a suite that only calls EndOfInput
// once, on the full remainder, cannot see.
type eofBeggarOnASplit struct{ opensOnQuote }

func (eofBeggarOnASplit) End(src, openedWith []byte, b parse.InputBoundary) (int, error) {
	if i := bytes.IndexByte(src, '\''); i >= 0 {
		return i + 1, nil
	}
	return 0, parse.ErrNeedMore // legal at MoreInput, a violation at EndOfInput
}

func TestSuiteDetectsThePrematureTerminalError(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, prematureGiveUp{}, []string{"'abc' tail"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted a form that gives up while more input may arrive; " +
			"under MoreInput the only legitimate refusal is ErrNeedMore")
	}
}

func TestSuiteDetectsANonZeroCountAlongsideErrNeedMore(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, beggarWithACount{}, []string{"'abc' tail"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted (1, ErrNeedMore): a count next to a refusal claims " +
			"bytes the form just said it could not judge")
	}
}

func TestSuiteDetectsAnUntypedErrorAtEndOfInput(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, untypedAtEOF{}, []string{"'abc' tail"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted a bare error at EndOfInput; a caller cannot name what " +
			"was left open")
	}
}

func TestSuiteDetectsACountAlongsideAnError(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, countAlongsideAnError{}, []string{"'abc' tail"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted a non-zero n next to an unterminated report")
	}
}

func TestSuiteDetectsAnAnswerThatSwingsWithTheBoundary(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, boundarySwinger{}, []string{"'abc' tail"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite accepted two different answers for the SAME window under the " +
			"two boundaries; a terminator that is present decides, not the boundary")
	}
}

func TestSuiteDetectsEOFBeggingOnAnIntermediateSplit(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	run(rec, eofBeggarOnASplit{}, []string{"'abc' tail"})
	if len(rec.msgs) == 0 {
		t.Fatal("the suite called EndOfInput only for the whole remainder, so a form that " +
			"begs for input at EOF on a shorter window went unseen")
	}
	if !rec.says("no more input to give") {
		t.Fatalf("detected, but diagnosed as something else: %q", rec.msgs)
	}
}
