package qml_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// FuzzParse: any source parses or is refused, never panics or runs away (a depth limit bounds
// nesting), and every position the result or the refusal names lies inside the source.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "{", "}", "A {", "A { }", "import tui 1.0\nWindow { Text { text: \"hi\" } }",
		"A { id: a; x: 1 + 2 * (3 - b.c); onClicked: go(); property int n: 3 }",
		"A { B { C { D { } } } }", `A { s: "unterminated }`, "A { x: [1, 2, {y: 3}] }",
		"A { function f(a, b) { return a ? b : -a } }", "/* c */ A { // c\n }", "A { x: `t${1}` }",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		spec, err := qml.New(qml.MaxDepth(32)).Parse(src)
		if err != nil {
			var se parse.SyntaxError
			if errors.As(err, &se) {
				inSource(t, "the refusal", se.Pos, len(src))
			}
			return
		}
		stack := []*qml.SpecNode{spec.Root}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n == nil {
				continue
			}
			inSource(t, "node "+n.Type, n.Pos, len(src))
			stack = append(stack, n.Children...)
		}
	})
}

// inSource fails unless p is a position in a source of n bytes (the end included).
func inSource(t *testing.T, what string, p parse.Position, n int) {
	t.Helper()
	if p.Offset < 0 || p.Offset > n || p.Line < 1 || p.Column < 1 {
		t.Fatalf("%s at %+v, outside a %d-byte source", what, p, n)
	}
}

// TestARefusalIsASyntaxErrorTheFuzzerReads: the fuzz target's position check reads the refusal as
// a parse.SyntaxError; this keeps that read from passing by never matching.
func TestARefusalIsASyntaxErrorTheFuzzerReads(t *testing.T) {
	var se parse.SyntaxError
	if _, err := qml.New().Parse([]byte("A {")); !errors.As(err, &se) {
		t.Fatalf("a truncated document's refusal %v (%T) is not a parse.SyntaxError", err, err)
	}
}
