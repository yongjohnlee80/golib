package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// Visual's operators over the selection: c (s), r{c}, ~ u U, > and < (with a count), and o.
func TestVimVisualOperators(t *testing.T) {
	runEdits(t, []editCase{
		{what: "v c", text: "abcd", seq: "vlcX<esc>", want: "Xcd"},
		{what: "v s", text: "abcd", seq: "vlsX<esc>", want: "Xcd"},
		{what: "V c keeps the indent", text: "  ab\ncd", seq: "VcX<esc>", want: "  X\ncd"},
		{what: "v r", text: "abcd", seq: "vlr-", want: "--cd", cursor: true},
		{what: "v r across lines keeps the breaks", text: "ab\ncd", col: 1, seq: "vjr-", want: "a-\n--"},
		{what: "V r", text: "ab\ncd", seq: "Vr-", want: "--\ncd"},
		{what: "v ~", text: "aBc", seq: "vl~", want: "Abc"},
		{what: "v u", text: "ABC", seq: "vlu", want: "abC"},
		{what: "v U", text: "abc", seq: "vlU", want: "ABc"},
		{what: "V >", text: "a\nb", seq: "Vj>", want: "    a\n    b"},
		{what: "V 3>", text: "a", seq: "V3>", want: "            a"},
		{what: "V <", text: "    a\n    b", seq: "Vj<", want: "a\nb"},
		{what: "v o: the other end", text: "abcdef", col: 2, seq: "vllohd", want: "af"},
		{what: "v r then Esc cancels r", text: "abcd", seq: "vlr<esc>", want: "abcd"},
		{what: "u after v U", text: "abc", seq: "vlUu", want: "abc"},
	})
}

// . repeats a Visual operation over a region of the same size from the cursor, a c's text with it.
func TestVimVisualRepeat(t *testing.T) {
	runEdits(t, []editCase{
		{what: "v d then .", text: "abcdef", seq: "vld.", want: "ef"},
		{what: "V d then .", text: "a\nb\nc\nd", seq: "Vjd.", want: ""},
		{what: "v c then .", text: "abcd", seq: "vlcX<esc>l.", want: "XX"},
		{what: "v r then .", text: "abcd", seq: "vlr-ll.", want: "----"},
		{what: "V > then .", text: "a\nb", seq: "V>j.", want: "    a\n    b"},
		{what: "v across lines, then .: the lines, and the last line's end column (Vim's visual-repeat)", text: "ab\ncd\nef\ngh", col: 1, seq: "vjdj.", want: "a\n"},
	})
}

// On a viewer the Visual operators bubble, the text unchanged; o still moves.
func TestVimVisualOperatorsOnAViewer(t *testing.T) {
	c, _ := coreWith(t, "abc", widget.CoreReadOnly(true))
	vimSeq(c, "vl")
	for _, k := range []rune{'~', 'u', 'U', 'c', '>', 'r'} {
		if c.HandleKey(key(k)) {
			t.Errorf("a viewer consumed Visual %q", k)
		}
	}
	if c.Value() != "abc" {
		t.Fatalf("a viewer's Visual operators changed it: %q", c.Value())
	}
}

// Insert's Ctrl-w and Ctrl-u, as Vim's ins_bs: blanks then one class; to the indent, then the
// line's start; a join at a line's start; a stop once where this Insert began.
func TestVimInsertDeleteBack(t *testing.T) {
	runEdits(t, []editCase{
		{what: "Ctrl-w a word", text: "foo bar", seq: "A<ctw><esc>", want: "foo "},
		{what: "Ctrl-w blanks, then a word", text: "foo bar  ", seq: "A<ctw><esc>", want: "foo "},
		{what: "Ctrl-w one class", text: "foo.bar", seq: "A<ctw><esc>", want: "foo."},
		{what: "Ctrl-w twice", text: "foo bar", seq: "A<ctw><ctw><esc>", want: ""},
		{what: "Ctrl-w stops where Insert began", text: "foo bar", seq: "A baz<ctw><ctw><esc>", want: "foo bar"},
		{what: "Ctrl-w goes on past it", text: "foo bar", seq: "A baz<ctw><ctw><ctw><esc>", want: "foo "},
		{what: "Ctrl-w at a line's start joins", text: "ab\ncd", ln: 1, seq: "i<ctw><esc>", want: "abcd"},
		{what: "Ctrl-u keeps the indent", text: "    foo bar", seq: "A<ctu><esc>", want: "    "},
		{what: "Ctrl-u then the indent", text: "    foo bar", seq: "A<ctu><ctu><esc>", want: ""},
		{what: "Ctrl-u stops where Insert began", text: "foo", seq: "A bar<ctu><esc>", want: "foo"},
		{what: "Ctrl-u at a line's start joins", text: "ab\ncd", ln: 1, seq: "i<ctu><esc>", want: "abcd"},
		{what: ". replays Ctrl-w", text: "x\ny", seq: "Afoo bar<ctw><esc>j.", want: "xfoo \nyfoo "},
		{what: "u undoes the Insert with its Ctrl-w", text: "foo bar", seq: "Abaz<ctw><esc>u", want: "foo bar"},
	})
}
