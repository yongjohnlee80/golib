package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Visual block's operators: the lines between the anchor and the cursor, and the display cells
// between theirs; a short line has nothing in it, $ runs to every line's end, and a cluster
// straddling an edge is split into spaces (Vim's block_prep).
func TestVimVisualBlockOperators(t *testing.T) {
	runEdits(t, []editCase{
		{what: "d", text: "abc\ndef\nghi", seq: "l<ctv>jd", want: "ac\ndf\nghi", cursor: true, wantCol: 1},
		{what: "x", text: "abc\ndef", seq: "l<ctv>jlx", want: "a\nd"},
		{what: "a short line has nothing in it", text: "abcd\na\nabcd", col: 2, seq: "<ctv>jjld", want: "ab\na\nab"},
		{what: "$ runs to every line's end", text: "ab\nabcd\nabc", seq: "l<ctv>jj$d", want: "a\na\na"},
		{what: "wide characters count two cells", text: "한글\nabcd", seq: "<ctv>jd", want: "글\ncd"},
		{what: "a straddling wide character leaves spaces", text: "abcd\nx한y\nabcd", col: 2, seq: "<ctv>jjd", want: "abd\nx y\nabd"},
		{what: "a straddling tab leaves spaces", text: "abcdef\n\tx\nabcdef", col: 2, seq: "<ctv>jjd", want: "abdef\n   x\nabdef"},
		{what: "r", text: "abc\ndef", seq: "l<ctv>jr-", want: "a-c\nd-f"},
		{what: "r with a wide character: a cell left over is a space", text: "abc\nabc", seq: "<ctv>jllr한", want: "한 \n한 "},
		{what: "~", text: "abc\ndef", seq: "<ctv>jl~", want: "ABc\nDEf"},
		{what: "U", text: "abc\ndef", seq: "<ctv>jU", want: "Abc\nDef"},
		{what: "u", text: "ABC\nDEF", seq: "l<ctv>ju", want: "AbC\nDeF"},
		{what: ">", text: "abc\ndef", seq: "l<ctv>j>", want: "a    bc\nd    ef"},
		{what: "<", text: "a    bc\nd    ef", seq: "l<ctv>j<", want: "abc\ndef"},
		{what: "O swaps the columns", text: "abcd\nefgh", seq: "l<ctv>jlOhd", want: "d\nh"},
		{what: "o swaps the corners", text: "abcd\nefgh", seq: "l<ctv>jlohd", want: "d\nh"},
		{what: "v then Ctrl-v", text: "abc\ndef", seq: "vj<ctv>d", want: "bc\nef"},
		{what: "O outside a block is o", text: "abcd", seq: "lvlOhd", want: "d"},
	})
}

// I, A and c type on the first line; on Esc the text goes in on the block's other lines: I and c
// where the left edge falls on a line that reaches it, A after the right edge, padding a short line.
// Each is one undo step, and . repeats it.
func TestVimVisualBlockInsert(t *testing.T) {
	runEdits(t, []editCase{
		{what: "I", text: "abc\ndef", seq: "l<ctv>jIX<esc>", want: "aXbc\ndXef"},
		{what: "I skips a line short of the edge", text: "abc\n\nabc", seq: "l<ctv>jjIX<esc>", want: "aXbc\n\naXbc"},
		{what: "A", text: "abc\ndef", seq: "<ctv>jAX<esc>", want: "aXbc\ndXef"},
		{what: "A pads a short line", text: "abc\na\nabc", seq: "l<ctv>jjAX<esc>", want: "abXc\na X\nabXc"},
		{what: "A with $", text: "ab\nabcd", seq: "<ctv>j$AX<esc>", want: "abX\nabcdX"},
		{what: "c", text: "abc\ndef", seq: "l<ctv>jcX<esc>", want: "aXc\ndXf"},
		{what: "c on a line reaching the edge", text: "abc\nd\nabc", seq: "l<ctv>jjcX<esc>", want: "aXc\ndX\naXc"},
		{what: "a line break typed: the first line only", text: "abc\ndef", seq: "l<ctv>jIX<cr>Y<esc>", want: "aX\nYbc\ndef"},
		{what: "Insert ended on another line: the first line only", text: "abc\ndef", seq: "l<ctv>jIX<down>Y<esc>", want: "aXbc\ndeYf"},
		{what: "u after I with arrows in it: still one step", text: "abc\ndef", seq: "l<ctv>jIX<left><right><esc>u", want: "abc\ndef"},
		{what: "Ctrl-o ends it: the first line only", text: "abc\ndef", seq: "l<ctv>jIX<cto>lY<esc>", want: "aXbYc\ndef"},
		{what: "u after I", text: "abc\ndef", seq: "l<ctv>jIX<esc>u", want: "abc\ndef"},
		{what: "u after c", text: "abc\ndef", seq: "l<ctv>jcX<esc>u", want: "abc\ndef"},
		{what: "u after a padding A", text: "abc\na", seq: "l<ctv>jAX<esc>u", want: "abc\na"},
		{what: "A padding the first line", text: "ab\nabcd", col: 1, seq: "<ctv>jlAX<esc>", want: "ab X\nabcXd"},
		{what: "u after A padding the first line", text: "ab\nabcd", col: 1, seq: "<ctv>jlAX<esc>u", want: "ab\nabcd"},
		{what: "I<Esc> changes nothing, and is no step", text: "abc\ndef", seq: "xl<ctv>jI<esc>u", want: "abc\ndef"},
		{what: "d then .", text: "abcd\nefgh", seq: "l<ctv>jd.", want: "ad\neh"},
		{what: "I then .", text: "abc\ndef\nghi\njkl", seq: "<ctv>jIX<esc>jj.", want: "Xabc\nXdef\nXghi\nXjkl"},
	})
}

// The block's modes and its view: Ctrl-v enters, and again leaves; v and V switch; the cells it
// covers are selected; y takes its pieces, blockwise.
func TestVimVisualBlockMode(t *testing.T) {
	c, _ := coreWith(t, "abcd\nefgh")
	vimSeq(c, "l<ctv>jl")
	if c.Mode() != widget.ModeVisualBlock || c.Mode().String() != "V-BLOCK" {
		t.Fatalf("Ctrl-v: mode %v", c.Mode())
	}
	for _, p := range []struct {
		ln, col int
		in      bool
	}{{0, 0, false}, {0, 1, true}, {0, 2, true}, {0, 3, false}, {1, 0, false}, {1, 2, true}, {1, 3, false}} {
		if got := c.Selected(p.ln, p.col); got != p.in {
			t.Errorf("Selected(%d, %d) = %v, want %v", p.ln, p.col, got, p.in)
		}
	}
	if got := c.SelectedText(); got != "bc\nfg" {
		t.Errorf("SelectedText = %q", got)
	}
	if r, col, er, ec, ok := c.SelectionRange(); !ok || r != 0 || col != 1 || er != 1 || ec != 3 {
		t.Errorf("SelectionRange = %d %d %d %d %v", r, col, er, ec, ok)
	}
	vimSeq(c, "v")
	if c.Mode() != widget.ModeVisual {
		t.Fatalf("v from a block: mode %v", c.Mode())
	}
	vimSeq(c, "<ctv>V")
	if c.Mode() != widget.ModeVisualLine {
		t.Fatalf("V from a block: mode %v", c.Mode())
	}
	vimSeq(c, "<ctv><ctv>")
	if c.Mode() != widget.ModeNormal {
		t.Fatalf("Ctrl-v twice: mode %v", c.Mode())
	}
	vimSeq(c, "<ctv><esc>")
	if c.Mode() != widget.ModeNormal {
		t.Fatalf("Esc from a block: mode %v", c.Mode())
	}
	vimSeq(c, "gg0l<ctv>jly")
	if text, linewise, ok := c.NamedRegister('"'); !ok || linewise || text != "bc\nfg" {
		t.Fatalf("y over a block: %q linewise %v", text, linewise)
	}
	if row, col := c.Line(); row != 0 || col != 1 {
		t.Fatalf("after y the cursor is at %d, %d, want the block's top left", row, col)
	}
}

// On a viewer a block selects and yanks; its edits bubble, the text unchanged.
func TestVimVisualBlockOnAViewer(t *testing.T) {
	c, _ := coreWith(t, "abc\ndef", widget.CoreReadOnly(true))
	vimSeq(c, "<ctv>j")
	for _, k := range []rune{'d', 'c', 'I', 'A', 'r', '~', '>'} {
		if c.HandleKey(key(k)) {
			t.Errorf("a viewer consumed block %q", k)
		}
	}
	if c.Value() != "abc\ndef" || c.Mode() != widget.ModeVisualBlock {
		t.Fatalf("a viewer's block: %q, mode %v", c.Value(), c.Mode())
	}
	if !c.HandleKey(tui.KeyEvent{Code: 'y', Text: "y"}) {
		t.Fatal("a viewer's block y bubbled")
	}
}

// Outside a block, I and A are linewise (Vim's v_visop): Insert at the first line's start, or at
// the last line's end.
func TestVimVisualInsertAppendOutsideABlock(t *testing.T) {
	runEdits(t, []editCase{
		{what: "v I", text: "abc\ndef", col: 1, seq: "vjIX<esc>", want: "Xabc\ndef"},
		{what: "v A", text: "abc\ndef", seq: "vjAX<esc>", want: "abc\ndefX"},
		{what: "V A", text: "abc\ndef", seq: "VAX<esc>", want: "abcX\ndef"},
	})
}

// A block put (Vim's do_put for a block): its pieces on successive lines at the cursor's display
// column, short lines padded, a tab there split, another character moved after the piece; copies
// padded to the width, but no trailing blanks after a short line's last copy.
func TestVimBlockPut(t *testing.T) {
	cases := []struct {
		what, text string
		ln, col    int
		pieces     []string
		seq, want  string
	}{
		{"P", "abc\ndef", 0, 1, []string{"XY", "Z"}, `"aP`, "aXYbc\ndZ ef"},
		{"p", "abc\ndef", 0, 1, []string{"XY", "Z"}, `"ap`, "abXYc\ndeZ f"},
		{"a short line padded, no trailing blanks", "abcd\nx\nabcd", 0, 2, []string{"XY", "Z"}, `"aP`, "abXYcd\nx Z\nabcd"},
		{"a copy padded before the text after it", "ab\nab", 0, 1, []string{"X", "YZ"}, `"a3P`, "aX X X b\naYZYZYZb"},
		{"a short line: the last copy unpadded", "a\na", 0, 0, []string{"X", "YZ"}, `"a2p`, "aX X\naYZYZ"},
		{"a tab at the column split", "abcd\n\tx", 0, 2, []string{"P", "Q"}, `"aP`, "abPcd\n  Q  x"},
		{"a wide character at the column moved after", "abcd\n한글", 0, 1, []string{"P", "Q"}, `"aP`, "aPbcd\n Q한글"},
		{"lines past the end added", "ab", 0, 0, []string{"P", "Q", "R"}, `"aP`, "Pab\nQ\nR"},
		{"one undo step", "ab", 0, 0, []string{"P", "Q", "R"}, `"aPu`, "ab"},
	}
	for _, e := range cases {
		c, _ := coreWith(t, e.text)
		c.SetNamedBlock('a', e.pieces, 0)
		c.SetLine(e.ln, e.col)
		vimSeq(c, e.seq)
		if got := c.Value(); got != e.want {
			t.Errorf("%s: %q by %q: %q, want %q", e.what, e.text, e.seq, got, e.want)
		}
	}
	c, _ := coreWith(t, "abcd\nx\nabcd")
	c.SetNamedBlock('a', []string{"XY", "Z"}, 0)
	c.SetLine(0, 2)
	vimSeq(c, `"aP`)
	if row, col := c.Line(); row != 0 || col != 2 {
		t.Errorf("after a block put the cursor is at %d, %d, want its top left", row, col)
	}
	y, _ := coreWith(t, "abc\ndef")
	vimSeq(y, "l<ctv>jly0P")
	if got := y.Value(); got != "bcabc\nefdef" {
		t.Errorf("y over a block, then P: %q", got)
	}
}

// The registers' kinds, and a block's pieces and width, through the API.
func TestVimBlockRegisterAPI(t *testing.T) {
	c, _ := coreWith(t, "abc\ndef")
	kind := func(name rune) widget.RegisterKind {
		k, ok := c.RegisterKind(name)
		if !ok {
			t.Fatalf("RegisterKind(%q): not ok", name)
		}
		return k
	}
	vimSeq(c, `"ayy"byw<ctv>jl"cy`)
	if kind('a') != widget.RegisterLines || kind('b') != widget.RegisterChars || kind('c') != widget.RegisterBlock {
		t.Fatalf("kinds: a %v b %v c %v", kind('a'), kind('b'), kind('c'))
	}
	if pieces, width, ok := c.NamedBlock('c'); !ok || width != 2 || len(pieces) != 2 || pieces[0] != "ab" || pieces[1] != "de" {
		t.Fatalf("NamedBlock('c') = %q %d %v", pieces, width, ok)
	}
	if _, _, ok := c.NamedBlock('a'); ok {
		t.Fatal("NamedBlock of a linewise register: ok")
	}
	for _, e := range []struct {
		pieces []string
		width  int
		want   int
	}{{[]string{"a", "한"}, 0, 2}, {[]string{"abc"}, 1, 3}, {[]string{"ab"}, 5, 5}, {[]string{"\tx"}, 0, 5}} {
		c.SetNamedBlock('d', e.pieces, e.width)
		if _, w, _ := c.NamedBlock('d'); w != e.want {
			t.Errorf("SetNamedBlock(%q, %d): width %d, want %d", e.pieces, e.width, w, e.want)
		}
	}
	c.SetNamedBlock('e', []string{"X"}, 0)
	c.SetNamedRegister('E', "YZ", false)
	if pieces, width, ok := c.NamedBlock('e'); !ok || width != 2 || len(pieces) != 2 || pieces[1] != "YZ" {
		t.Fatalf("appending to a block: %q %d %v", pieces, width, ok)
	}
	if text, linewise, _ := c.NamedRegister('c'); text != "ab\nde" || linewise {
		t.Fatalf("NamedRegister of a block: %q linewise %v", text, linewise)
	}
	// a block over lines deletes into 1, as Vim's op_delete; one line's into -
	d, _ := coreWith(t, "abc\ndef\nghi")
	vimSeq(d, "<ctv>jdG<ctv>d")
	if one, _, _ := d.NamedRegister('1'); one != "a\nd" {
		t.Errorf(`a block over two lines: "1 = %q`, one)
	}
	if small, _, _ := d.NamedRegister('-'); small != "g" {
		t.Errorf(`a block on one line: "- = %q`, small)
	}
	// a yank takes a straddling cluster's cells inside the block as spaces, at either edge
	for _, e := range []struct{ what, seq, want string }{
		{"the left edge", "ll<ctv>jjy", "c\n \nc"},
		{"the right edge", "l<ctv>jjy", "b\n \nb"},
	} {
		y, _ := coreWith(t, "abcd\nx한y\nabcd")
		vimSeq(y, e.seq)
		if got, _, _ := y.NamedRegister('"'); got != e.want {
			t.Errorf("a yank with a wide character straddling %s: %q, want %q", e.what, got, e.want)
		}
	}
}

// A paste over a block replaces it: the parts out, the text at its top left.
func TestVimBlockPaste(t *testing.T) {
	c, _ := coreWith(t, "abc\ndef")
	vimSeq(c, "l<ctv>j")
	c.HandlePaste("X")
	if got := c.Value(); got != "aXc\ndf" || c.Mode() != widget.ModeNormal {
		t.Fatalf("a paste over a block: %q, mode %v", got, c.Mode())
	}
	if vimSeq(c, "u"); c.Value() != "abc\ndef" {
		t.Fatalf("u after a paste over a block: %q, want it whole in one step", c.Value())
	}
}
