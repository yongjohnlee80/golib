package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// wrappedEditor is a soft-wrapped terminal Editor laid out w cells wide.
func wrappedEditor(t *testing.T, text string, w int) *widget.Editor {
	t.Helper()
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText(text)), widget.WithEditorWrap(widget.WrapSoft))
	ed.Layout(tui.Constraints{MaxW: w, MaxH: 10})
	return ed
}

func atCol(t *testing.T, ed *widget.Editor, what string, ln, col int) {
	t.Helper()
	if l, c := ed.Core().Line(); l != ln || c != col {
		t.Errorf("%s: cursor (%d,%d), want (%d,%d)", what, l, c, ln, col)
	}
}

// gj and gk over the terminal Editor's wrapped rows: "abc defg" at 4 cells is the rows "abc" and
// "defg", the space between them consumed by the wrap and drawn at the end of the first.
func TestVimDisplayRows(t *testing.T) {
	ed := wrappedEditor(t, "abc defg", 4)
	c := ed.Core()
	c.SetLine(0, 1)
	vimSeq(c, "gj")
	atCol(t, ed, "gj", 0, 5)
	vimSeq(c, "gk")
	atCol(t, ed, "gk", 0, 1)
	c.SetLine(0, 3) // the consumed break space
	vimSeq(c, "gj")
	atCol(t, ed, "gj from the break space", 0, 7)
	vimSeq(c, "gk")
	atCol(t, ed, "gk back onto it", 0, 3)
	c.SetLine(0, 1)
	vimSeq(c, "gk")
	atCol(t, ed, "gk on the first row", 0, 1)
	c.SetLine(0, 5)
	vimSeq(c, "gj")
	atCol(t, ed, "gj on the last row", 0, 5)

	d := wrappedEditor(t, "abc defg", 4)
	d.Core().SetLine(0, 1)
	vimSeq(d.Core(), "dgj")
	if got := d.Core().Value(); got != "aefg" {
		t.Errorf("dgj, charwise and exclusive: %q", got)
	}
}

// Wide characters keep their cells: "한국어abc" at 4 cells is "한국", "어ab", "c".
func TestVimDisplayRowsWide(t *testing.T) {
	ed := wrappedEditor(t, "한국어abc", 4)
	c := ed.Core()
	c.SetLine(0, 4) // b, x 3 on the second row
	vimSeq(c, "gk")
	atCol(t, ed, "gk onto the wide character holding x", 0, 1)
	vimSeq(c, "2gj")
	atCol(t, ed, "2gj", 0, 5)
}

// The x is kept across gj and gk, through a short row; and across lines.
func TestVimDisplayRowsSticky(t *testing.T) {
	ed := wrappedEditor(t, "abcdefgh\nab\nabcdefgh", 20)
	c := ed.Core()
	c.SetLine(0, 6)
	vimSeq(c, "gj")
	atCol(t, ed, "gj onto a short line", 1, 1)
	vimSeq(c, "gj")
	atCol(t, ed, "gj keeps the x", 2, 6)
	vimSeq(c, "hgk")
	atCol(t, ed, "another motion resets it", 1, 1)

	// h then l back to the same place still ends the x (Neovim lands at (2,1))
	r := wrappedEditor(t, "abcdefgh\nab\nabcdefgh", 20)
	r.Core().SetLine(0, 6)
	vimSeq(r.Core(), "gjhlgj")
	atCol(t, r, "a round trip back to the same place", 2, 1)
}

// A core whose layout has no rows: a row per line, still charwise as an operator's motion.
func TestVimDisplayRowsWithoutARowLayout(t *testing.T) {
	runEdits(t, []editCase{
		{what: "gj as j by x", text: "abcd\nab", col: 3, seq: "gj", want: "abcd\nab", cursor: true, wantLn: 1, wantCol: 1},
		{what: "dgj charwise exclusive", text: "abcd\nefgh", col: 1, seq: "dgj", want: "afgh"},
		{what: "dgj onto a shorter line: to its last character (Neovim)", text: "abcd\nef", col: 3, seq: "dgj", want: "abcf"},
		{what: "gk on the first line", text: "abcd\nab", col: 2, seq: "gk", want: "abcd\nab", cursor: true, wantCol: 2},
	})
}
