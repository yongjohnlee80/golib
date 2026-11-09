package editor

import (
	"strings"

	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

// editor_selection.go: the Visual selection, one kind at a time. Characterwise (v), linewise (V) and
// blockwise (Ctrl-v) selections answer the same questions (is a cell in it, its text, its bounds,
// yank it, take it out for a paste) in their own way, and every caller asks visualSelection for
// the one in force rather than switching on the mode itself. A new kind is one more type here.

// visualSel is the Visual selection in force.
type visualSel interface {
	// contains reports whether (ln, col) is selected.
	contains(ln, col int) bool
	// text is the selected text: a block's pieces, one per line, joined with line breaks.
	text() string
	// bounds is the selection as a region from (row, col) up to, not including, (endRow, endCol);
	// a block, which is no single region, from its top left to its bottom right.
	bounds() (row, col, endRow, endCol int)
	// yank copies the selection into the register (linewise for V) and puts the cursor where Vim
	// leaves it.
	yank()
	// remove takes the selection's text out for a paste, the cursor where the paste goes.
	remove()
}

// visualSelection is the Visual selection in force, and false outside the Visual modes.
func (c *EditorCore) visualSelection() (visualSel, bool) {
	switch c.keys.mode {
	case ModeVisual:
		lo, hiEx := c.visualRange()
		return charSel{c, lo, hiEx}, true
	case ModeVisualLine:
		lo, hi := c.visualLines()
		return linesSel{c, lo, hi}, true
	case ModeVisualBlock:
		return blockSel{c, c.visualBlock()}, true
	}
	return nil, false
}

// charSel is a characterwise selection, [lo, hiEx).
type charSel struct {
	c        *EditorCore
	lo, hiEx txt.Pos
}

func (s charSel) contains(ln, col int) bool { return (txt.Pos{Ln: ln, Col: col}).In(s.lo, s.hiEx) }
func (s charSel) text() string              { return s.c.buf.TextIn(s.lo, s.hiEx) }
func (s charSel) bounds() (int, int, int, int) {
	return s.lo.Ln, s.lo.Col, s.hiEx.Ln, s.hiEx.Col
}
func (s charSel) yank() {
	s.c.regYank(s.text(), false)
	s.c.buf.Ln, s.c.buf.Col = s.lo.Ln, s.lo.Col
}
func (s charSel) remove() { s.c.buf.DeleteRegion(s.lo, s.hiEx) }

// linesSel is a linewise selection, lines lo through hi.
type linesSel struct {
	c      *EditorCore
	lo, hi int
}

func (s linesSel) contains(ln, _ int) bool { return ln >= s.lo && ln <= s.hi }
func (s linesSel) text() string            { return strings.Join(s.c.buf.Lines[s.lo:s.hi+1], "\n") }
func (s linesSel) bounds() (int, int, int, int) {
	return s.lo, 0, s.hi, len(s.c.buf.LineClusters(s.hi))
}
func (s linesSel) yank() {
	s.c.regYank(s.text(), true)
	s.c.buf.Ln, s.c.buf.Col = s.lo, 0
}
func (s linesSel) remove() {
	b := &s.c.buf
	b.Anchor = nil
	b.SetLine(s.lo, "") // the first line stays, emptied, for the paste
	b.RemoveLines(s.lo+1, s.hi+1)
	b.Ln, b.Col = s.lo, 0
}

// blockSel is a blockwise selection.
type blockSel struct {
	c *EditorCore
	v vblock
}

func (s blockSel) contains(ln, col int) bool {
	if ln < s.v.lo || ln > s.v.hi {
		return false
	}
	l := s.v.line(s.c.buf.LineClusters(ln))
	return col >= l.from && col < l.to
}
func (s blockSel) text() string {
	pieces, _ := s.c.blockPieces(s.v)
	return strings.Join(pieces, "\n")
}
func (s blockSel) bounds() (int, int, int, int) {
	top, bot := s.v.line(s.c.buf.LineClusters(s.v.lo)), s.v.line(s.c.buf.LineClusters(s.v.hi))
	return s.v.lo, top.from, s.v.hi, bot.to
}
func (s blockSel) yank()   { s.c.blockYank(s.v) }
func (s blockSel) remove() { s.c.blockRemove(s.v) } // the block's parts out, the paste at its top left
