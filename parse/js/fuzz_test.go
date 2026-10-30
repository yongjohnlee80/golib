package js_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/js"
)

// FuzzParse: any source parses as an expression and as statements, or is refused, never panics
// or runs away, and a refusal points inside the source.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "a", "a + b * (c - d)", "f(x)(y)[z].w", "a ? b : c", "x => x * 2", "(a, b) => { return a }",
		"`t${a}t`", "'unterminated", "[1, 2, ...xs]", "{a: 1, [b]: 2, c}", "a?.b ?? c",
		"let x = 1; if (x) { x++ } else x--", "for (let i = 0; i < n; i++) {}", "((((((((((a))))))))))",
		"/re/g.test(s)", "a = b = c", "new F(1).g",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		_, err := js.NewExpression().Parse(src)
		refusedInSource(t, "the expression", err, len(src))
		_, err = js.NewStatements().Parse(src)
		refusedInSource(t, "the statements", err, len(src))
	})
}

// refusedInSource fails when err is a syntax error pointing outside a source of n bytes.
func refusedInSource(t *testing.T, what string, err error, n int) {
	t.Helper()
	var se parse.SyntaxError
	if errors.As(err, &se) && (se.Pos.Offset < 0 || se.Pos.Offset > n) {
		t.Fatalf("%s refused at offset %d, outside a %d-byte source", what, se.Pos.Offset, n)
	}
}

// TestARefusalIsASyntaxErrorTheFuzzerReads: the fuzz target's position check reads the refusal as
// a parse.SyntaxError; this keeps that read from passing by never matching.
func TestARefusalIsASyntaxErrorTheFuzzerReads(t *testing.T) {
	var se parse.SyntaxError
	if _, err := js.NewExpression().Parse([]byte("a +")); !errors.As(err, &se) {
		t.Fatalf("a truncated expression's refusal %v (%T) is not a parse.SyntaxError", err, err)
	}
	if _, err := js.NewStatements().Parse([]byte("if (")); !errors.As(err, &se) {
		t.Fatalf("a truncated statement's refusal %v (%T) is not a parse.SyntaxError", err, err)
	}
}
