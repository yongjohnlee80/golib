package parsetest

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// recorder stands in for *testing.T so the suite can be watched failing.
type recorder struct{ msgs []string }

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, format)
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
