// Package parse_test drives the generic forms through the public conformance
// suite. It is an EXTERNAL test package because parsetest imports parse, and a
// package-internal test that imported parsetest would be importing its own
// package's dependency back into itself.
//
// parsetest checks the protocol, not the meaning: it drives every split under
// both boundaries and enforces the answer matrix and its monotonicity. A form
// that recognises the wrong thing consistently passes here — which is what the
// per-form tests in package parse are for.
package parse_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/parsetest"
)

func wordByte(i int, b byte) bool {
	letter := b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
	if i == 0 {
		return letter
	}
	return letter || (b >= '0' && b <= '9')
}

func spaceByte(_ int, b byte) bool { return b == ' ' || b == '\t' || b == '\n' }

func TestRunFormConformsAtEverySplit(t *testing.T) {
	for _, c := range []struct {
		name   string
		form   parse.Form
		corpus []string
	}{
		{
			"word", parse.RunForm(parse.Word, wordByte),
			[]string{"", "a", "abc", "a1b2", "x)", "hello world", "_9", "9x", "a b"},
		},
		{
			"space", parse.RunForm(parse.Space, spaceByte),
			[]string{"", " ", "  ", " \t\n", "  x", "x", "\n\n"},
		},
		{
			// The exact-one-byte fallback: a member true only at index 0.
			"exact one byte", parse.RunForm(parse.Operator, func(i int, _ byte) bool { return i == 0 }),
			[]string{"", "+", "+=", "===", "-x tail"},
		},
		{
			// A member bounded by index, which is what makes End's index base
			// observable rather than incidental.
			"bounded run", parse.RunForm(parse.Word, func(i int, _ byte) bool { return i < 3 }),
			[]string{"", "a", "ab", "abc", "abcd", "abcdef"},
		},
		{
			// Conforms to the PROTOCOL yet is unusable as a catch-all: with no
			// refusing byte it consumes the whole remainder. Included to show the
			// suite checks the contract, not whether a form means what you wanted.
			"always true (protocol-legal, semantically greedy)",
			parse.RunForm(parse.Operator, func(int, byte) bool { return true }),
			[]string{"", "x", "xyz"},
		},
	} {
		t.Run(c.name, func(t *testing.T) { parsetest.Form(t, c.form, c.corpus) })
	}
}

func TestSetFormConformsAtEverySplit(t *testing.T) {
	for _, c := range []struct {
		name   string
		form   parse.Form
		corpus []string
	}{
		{
			// The shared-prefix case: the short literal is IN the set, so Starts
			// fixes a stable width of one and End resolves the descendant. The
			// split after the first '-' is the row that matters.
			"dash and double dash", parse.SetForm(parse.Operator, "-", "--"),
			[]string{"", "-", "--", "---", "-x", "x", "- -"},
		},
		{
			"angle family", parse.SetForm(parse.Operator, "<", "<=", "<>"),
			[]string{"", "<", "<=", "<>", "<x", "<==", "<>>", "x"},
		},
		{
			// No short literal on the path: Starts waits, then answers the first
			// terminal it reaches, and refuses once the path leaves the trie.
			"long literal only", parse.SetForm(parse.Operator, "<="),
			[]string{"", "<", "<=", "<x", "<=x", "x"},
		},
		{
			"mixed widths", parse.SetForm(parse.Punct, ";", "::", ":", "::="),
			[]string{"", ";", ":", "::", "::=", ":x", "::x", ";;", "x"},
		},
	} {
		t.Run(c.name, func(t *testing.T) { parsetest.Form(t, c.form, c.corpus) })
	}
}
