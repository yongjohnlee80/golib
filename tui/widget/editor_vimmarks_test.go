package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// m{a–z} sets a mark; ' goes to its line's first non-blank, ` to its place; ” and “ to where the
// last jump left; a mark not set moves nothing.
func TestVimMarksJump(t *testing.T) {
	runEdits(t, []editCase{
		{what: "'a: the line's first non-blank", text: "  ab\nx\ny", col: 3, seq: "maG'a", want: "  ab\nx\ny", cursor: true, wantCol: 2},
		{what: "`a: the exact place", text: "  ab\nx\ny", col: 3, seq: "maG`a", want: "  ab\nx\ny", cursor: true, wantCol: 3},
		{what: "'' after G", text: "a\nb\nc", ln: 1, seq: "G''", want: "a\nb\nc", cursor: true, wantLn: 1},
		{what: "'' twice: back again", text: "a\nb\nc", ln: 1, seq: "G''''", want: "a\nb\nc", cursor: true, wantLn: 2},
		{what: "`` after %", text: "(ab)", col: 1, seq: "l%``", want: "(ab)", cursor: true, wantCol: 2},
		{what: "'' after a mark's jump", text: "a\nb\nc", seq: "majj'a''", want: "a\nb\nc", cursor: true, wantLn: 2},
		{what: "a mark not set: no move", text: "a\nb", ln: 1, seq: "'q", want: "a\nb", cursor: true, wantLn: 1},
		{what: "a name that is no mark: no move", text: "a\nb", ln: 1, seq: "m1gg'1", want: "a\nb", cursor: true},
		{what: "a column past a shortened line lands in it", text: "abcdef\nx", col: 5, seq: "maD`a", want: "abcde\nx", cursor: true, wantCol: 4},
	})
}

// ' and ` are operator motions: ' linewise, ` exclusive (and kept in register 1, as Vim's).
func TestVimMarksAsMotions(t *testing.T) {
	runEdits(t, []editCase{
		{what: "d'a", text: "1\n2\n3\n4", seq: "majjd'a", want: "4"},
		{what: "d'a upward", text: "1\n2\n3\n4", ln: 2, seq: "maggd'a", want: "4"},
		{what: "d`a", text: "abcdef", col: 1, seq: "ma$d`a", want: "af"},
		{what: "y`a then P", text: "abcdef", col: 1, seq: "ma$y`aP", want: "abcdebcdef"},
		{what: "d'q with q unset: nothing", text: "a\nb", seq: "d'qx", want: "\nb"},
	})
	runRegs(t, []regCase{
		{what: "d`a within a line: register 1", text: "abcdef", seq: "lma$d`a", regs: map[rune]string{'1': "bcde", '-': ""}},
	})
}

// Marks follow the text: lines put in or taken out above move them; a mark on a removed line is
// gone; a split carries the marks after the break to the new line, and a join the joined line's onto
// its new place (Vim's mark_adjust and mark_col_adjust).
func TestVimMarksFollowTheText(t *testing.T) {
	runEdits(t, []editCase{
		{what: "a line opened above", text: "a\nb\nc", ln: 2, seq: "mcggOx<esc>'c", want: "x\na\nb\nc", cursor: true, wantLn: 3},
		{what: "a line deleted above", text: "a\nb\nc", ln: 2, seq: "mcggdd'c", want: "b\nc", cursor: true, wantLn: 1},
		{what: "lines put above", text: "a\nb", ln: 1, seq: "mbggyyP'b", want: "a\na\nb", cursor: true, wantLn: 2},
		{what: "the marked line deleted: the mark is gone", text: "a\nb\nc", ln: 1, seq: "mbddgg'b", want: "a\nc", cursor: true},
		{what: "two edits before any read: the top, then below the mark", text: "0\n1\n2\n3\n4", ln: 2, seq: "maggddGonew<esc>'a",
			want: "1\n2\n3\n4\nnew", cursor: true, wantLn: 1},
		{what: "an equal line count put back: the deleted line's mark is gone, a later one moved",
			text: "a\nb\nc\nd", ln: 1, seq: "mxGmygg2yyj2dd\"0P'x'y", want: "a\na\nb\nd", cursor: true, wantLn: 3},
		{what: "a split: the mark after the break goes to the new line", text: "abcdef", col: 3, seq: "mahi<cr><esc>gg`a",
			want: "ab\ncdef", cursor: true, wantLn: 1, wantCol: 1},
		{what: "a split: a mark before the break stays", text: "abcdef", col: 0, seq: "malli<cr><esc>gg$`a",
			want: "ab\ncdef", cursor: true},
		{what: "J: the joined line's mark onto its new place", text: "ab\n  cd", ln: 1, col: 3, seq: "makJ0`a",
			want: "ab cd", cursor: true, wantCol: 4},
		{what: "J: a mark in the dropped indent lands where the text starts", text: "ab\n  cd", ln: 1, col: 0, seq: "makJ0`a",
			want: "ab cd", cursor: true, wantCol: 3},
		{what: "gJ keeps the indent", text: "ab\n  cd", ln: 1, col: 3, seq: "makgJ0`a", want: "ab  cd", cursor: true, wantCol: 5},
		{what: "3J: each joined line's mark", text: "a\nb\nc", ln: 2, seq: "mcgg3J0`c", want: "a b c", cursor: true, wantCol: 4},
		{what: "a delete across lines: the last line's marks join, a middle line's go",
			text: "abc\nmid\nxyz", ln: 2, col: 1, seq: "my$mzkmmggl" + "d`y`z", want: "ayz", cursor: true, wantCol: 2},
		{what: "a delete across lines: the middle mark is gone", text: "abc\nmid\nxyz", ln: 2, col: 1, seq: "mykmmggld`y$'m",
			want: "ayz", cursor: true, wantCol: 2},
	})
}

// Undo and redo bring back the marks a change erased or moved, as Vim's u_undoredo: a mark set when
// the change began goes back to its place; one set since keeps its own.
func TestVimMarksUndoRedo(t *testing.T) {
	runEdits(t, []editCase{
		{what: "u brings back a deleted line's mark", text: "a\nb\nc", ln: 1, seq: "mbddugg'b", want: "a\nb\nc", cursor: true, wantLn: 1},
		{what: "<C-r> takes it away again", text: "a\nb\nc", ln: 1, seq: "mbddu<redo>gg'b", want: "a\nc", cursor: true},
		{what: "a mark set after the change keeps its place, moved with the undo", text: "a\nb", seq: "ddmau'a", want: "a\nb",
			cursor: true, wantLn: 1},
		{what: "u of a split puts the moved mark back", text: "abcdef", col: 3, seq: "mahi<cr><esc>ugg`a", want: "abcdef",
			cursor: true, wantCol: 3},
	})
}

// SetValue is a new document: no marks, the previous jump's included.
func TestVimMarksSetValueClears(t *testing.T) {
	c, _ := coreWith(t, "a\nb")
	c.SetLine(1, 0)
	vimSeq(c, "mbgg")
	c.SetValue("a\nb")
	c.SetLine(0, 0)
	vimSeq(c, "'b''")
	if ln, col := c.Line(); ln != 0 || col != 0 {
		t.Fatalf("marks survived SetValue: the cursor went to (%d,%d)", ln, col)
	}
}

// A viewer sets marks and jumps to them; d'a there is refused, and bubbles (#285).
func TestVimMarksOnAViewer(t *testing.T) {
	c, _ := coreWith(t, "a\nb\nc", widget.CoreReadOnly(true))
	vimSeq(c, "majj'a")
	if ln, _ := c.Line(); ln != 0 {
		t.Fatalf("a viewer's 'a went to line %d", ln)
	}
	if c.HandleKey(key('d')) || c.Value() != "a\nb\nc" {
		t.Fatal("a viewer consumed d")
	}
}
