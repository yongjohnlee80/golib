package parse_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// TestSourceSigilParses — ADR-decl-0001c rows 6 and 7.
func TestSourceSigilParses(t *testing.T) {
	cases := []struct {
		src  string
		kind parse.SpecValueKind
		raw  string
		args int
		arg0 parse.SpecValueKind
	}{
		{`N { a: $greeting }`, parse.SpecValueSource, "greeting", 0, 0},
		{`N { a: horizontal }`, parse.SpecValueRef, "horizontal", 0, 0},
		{`N { a: @surface }`, parse.SpecValueToken, "surface", 0, 0},
		{`N { a: f($g, bare, "s") }`, parse.SpecValueCall, "f", 3, parse.SpecValueSource},
		{`N { a: f(g($x)) }`, parse.SpecValueCall, "f", 1, parse.SpecValueCall},
	}
	for _, c := range cases {
		tree, err := parse.QML{}.Parse([]byte(c.src))
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		v := tree.Root.Props[0].Value
		if v.Kind != c.kind || v.Raw != c.raw || len(v.Args) != c.args {
			t.Errorf("%s -> kind=%v raw=%q args=%d, want %v/%q/%d",
				c.src, v.Kind, v.Raw, len(v.Args), c.kind, c.raw, c.args)
		}
		if c.args > 0 && v.Args[0].Kind != c.arg0 {
			t.Errorf("%s -> arg0 kind=%v, want %v", c.src, v.Args[0].Kind, c.arg0)
		}
	}
}

// TestSourceSigilMirrorsTheTokenSigil — ADR-decl-0001c row 7.
//
// A schema arrives from a watcher mid-write, so `$` at end of input must be
// INCOMPLETE — which holds the last good tree (ADR-decl-0001 D7) — while `$`
// followed by something that is not a name is an ordinary syntax error. The
// shapes must match @, or one sigil would blank the screen where the other
// waits.
func TestSourceSigilMirrorsTheTokenSigil(t *testing.T) {
	for _, c := range []struct {
		src        string
		incomplete bool
		want       string
	}{
		{`N { a: $`, true, "a source name after $"},
		{`N { a: @`, true, "a token name after @"},
		{`N { a: $ }`, false, "a source name after $"},
		{`N { a: @ }`, false, "a token name after @"},
	} {
		_, err := parse.QML{}.Parse([]byte(c.src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: err = %v, want a SyntaxError", c.src, err)
			continue
		}
		if se.Incomplete != c.incomplete || se.Want != c.want {
			t.Errorf("%q -> incomplete=%v want=%q, want incomplete=%v want=%q",
				c.src, se.Incomplete, se.Want, c.incomplete, c.want)
		}
	}
}

// TestAddingSourceDidNotRenumberTheOtherKinds.
//
// SpecValueSource was first declared beside @token, which shifted Ref from 5 to
// 6 and Call from 6 to 7. That is invisible in Go and not invisible to anything
// that has ever written one of those numbers down, so the numbers are pinned
// here rather than left to the next person's sense of tidiness.
func TestAddingSourceDidNotRenumberTheOtherKinds(t *testing.T) {
	for _, c := range []struct {
		kind parse.SpecValueKind
		n    uint8
	}{
		{parse.SpecValueInvalid, 0},
		{parse.SpecValueString, 1},
		{parse.SpecValueNumber, 2},
		{parse.SpecValueBool, 3},
		{parse.SpecValueToken, 4},
		{parse.SpecValueRef, 5},
		{parse.SpecValueCall, 6},
		{parse.SpecValueSource, 7},
	} {
		if uint8(c.kind) != c.n {
			t.Errorf("%v = %d, want %d", c.kind, uint8(c.kind), c.n)
		}
	}
}
