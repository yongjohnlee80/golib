package qml_test

import (
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"

	"errors"
	"testing"
)

// TestReferenceValuesParse pins how a name in value position is recorded.
func TestReferenceValuesParse(t *testing.T) {
	cases := []struct {
		src  string
		kind qml.SpecValueKind
		raw  string
		args int
		arg0 qml.SpecValueKind
	}{
		{`N { a: greeting }`, qml.SpecValueRef, "greeting", 0, 0},
		{`N { a: parent.width }`, qml.SpecValueRef, "parent.width", 0, 0},
		{`N { a: horizontal }`, qml.SpecValueRef, "horizontal", 0, 0},
		{`N { a: f(g, bare, "s") }`, qml.SpecValueCall, "f", 3, qml.SpecValueRef},
		{`N { a: f(g(x)) }`, qml.SpecValueCall, "f", 1, qml.SpecValueCall},
	}
	for _, c := range cases {
		tree, err := qml.QML{}.Parse([]byte(c.src))
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

// TestMemberChainsParse — a schema that reads like QML must TOKENIZE like QML.
//
// The engine cannot evaluate `parent.width` and says so with an error naming the
// line. That is a different and far better failure than a parse error about a
// stray dot, which tells a reader nothing about why their file is wrong.
func TestMemberChainsParse(t *testing.T) {
	for _, c := range []struct {
		src  string
		raw  string
		path []string
	}{
		{`N { a: greeting }`, "greeting", []string{"greeting"}},
		{`N { a: parent.width }`, "parent.width", []string{"parent", "width"}},
		{`N { a: a.b.c }`, "a.b.c", []string{"a", "b", "c"}},
	} {
		tree, err := qml.QML{}.Parse([]byte(c.src))
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		v := tree.Root.Props[0].Value
		if v.Raw != c.raw || len(v.Path) != len(c.path) {
			t.Errorf("%s -> raw=%q path=%v, want %q/%v", c.src, v.Raw, v.Path, c.raw, c.path)
			continue
		}
		for i := range c.path {
			if v.Path[i] != c.path[i] {
				t.Errorf("%s -> path=%v, want %v", c.src, v.Path, c.path)
				break
			}
		}
	}
}

// TestAnIncompleteChainHoldsTheTree — a chain cut off at end of input must be
// INCOMPLETE, so a watcher catching a half-written save holds the last good
// tree instead of flashing an error (ADR-decl-0001 D7).
func TestAnIncompleteChainHoldsTheTree(t *testing.T) {
	for _, c := range []struct {
		src        string
		incomplete bool
	}{
		{`N { a: parent.`, true},
		{`N { a: parent. }`, false},
	} {
		_, err := qml.QML{}.Parse([]byte(c.src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: err = %v, want a parse.SyntaxError", c.src, err)
			continue
		}
		if se.Incomplete != c.incomplete || se.Want != "a name after ." {
			t.Errorf("%q -> incomplete=%v want=%q", c.src, se.Incomplete, se.Want)
		}
	}
}

// TestKindNumbersArePinned.
//
// A short-lived SpecValueSource was once INSERTED mid-list and shifted Ref from
// 5 to 6 and Call from 6 to 7. That is invisible in Go, and this test is what
// makes it visible.
//
// The numbers below changed once, deliberately: retiring the @name token kind
// removed slot 4 and moved everything after it. That is what an accidental
// insertion looks like too, which is the point — a change here has to be made
// on purpose, with the reason written down, rather than drifting.
func TestKindNumbersArePinned(t *testing.T) {
	for _, c := range []struct {
		kind qml.SpecValueKind
		n    uint8
	}{
		{qml.SpecValueInvalid, 0},
		{qml.SpecValueString, 1},
		{qml.SpecValueNumber, 2},
		{qml.SpecValueBool, 3},
		{qml.SpecValueRef, 4},
		{qml.SpecValueCall, 5},
		{qml.SpecValueExpr, 6},
	} {
		if uint8(c.kind) != c.n {
			t.Errorf("%v = %d, want %d", c.kind, uint8(c.kind), c.n)
		}
	}
}
