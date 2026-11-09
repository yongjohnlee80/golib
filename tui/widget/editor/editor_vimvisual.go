package editor

import (
	"strings"

	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

// VIM VISUAL OPERATORS — beyond y and d, Visual's c (and s), r{c}, ~ u U, > and <, I and A, and o,
// over the selection (characters, or whole lines in Visual line), with M2's and M3's operators, so
// the registers and undo behave as the same operator in Normal does; a block's are
// editor_vimblock.go's. . repeats a Visual operation as Vim does: the same operator over a region
// of the same size from the cursor (the lines, and the characters on one line, or the end column on
// the last; a block's lines and cells), and a c's, I's or A's typed text after it.
//
// Outside a block, I and A are Vim's v_visop: linewise, Insert at the first line's start, or at the
// last line's end.

// visualRedo is a Visual operation as . replays it.
type visualRedo struct {
	op    Action
	lines bool // whole lines (Visual line)
	block bool // a block: cols is its width in cells, toEnd its $
	toEnd bool
	nLine int // how many lines
	cols  int // characters: on one line, how many; across lines, the last line's end column
	char  string
	count int
}

// visualOpRange is the selection as an operator's range.
func (c *EditorCore) visualOpRange() opRange {
	if c.keys.mode == ModeVisualLine {
		lo, hi := c.visualLines()
		return opRange{lo: txt.Pos{Ln: lo, Col: 0}, hiEx: txt.Pos{Ln: hi, Col: 0}, lines: true}
	}
	lo, hiEx := c.visualRange()
	return opRange{lo: lo, hiEx: hiEx}
}

// visualOp runs op over the selection, ends Visual, and records it for .: count is > and <'s
// units, char r's character.
func (c *EditorCore) visualOp(op Action, count int, char string) {
	if c.keys.mode == ModeVisualBlock {
		v := c.visualBlock()
		redo := visualRedo{op: op, block: true, toEnd: v.toEnd, nLine: v.hi - v.lo + 1, cols: v.right - v.left,
			count: count, char: char}
		c.exitVisual()
		c.recordVisual(redo, func() { c.blockApply(op, v, count, char) })
		return
	}
	r := c.wholeFolds(c.visualOpRange())
	redo := visualRedo{op: op, lines: r.lines, nLine: r.hiEx.Ln - r.lo.Ln + 1, count: count, char: char}
	switch {
	case r.lines:
	case r.lo.Ln == r.hiEx.Ln:
		redo.cols = r.hiEx.Col - r.lo.Col
	default:
		redo.cols = r.hiEx.Col
	}
	c.exitVisual()
	c.recordVisual(redo, func() { c.visualApply(op, r, count, char) })
}

// recordVisual runs apply as the change . repeats: the Visual operation, and a c's Insert after it.
func (c *EditorCore) recordVisual(redo visualRedo, apply func()) {
	d := &c.dot
	if d.replaying {
		apply()
		return
	}
	rev := c.rev
	*d = dotRecorder{on: true, rev: rev, last: d.last, atoms: []dotAtom{{kind: dotVisual, vis: &redo}}}
	apply()
	switch {
	case c.keys.mode == ModeInsert:
		d.inInsert = true // c: what is typed joins the change
	case c.rev != rev:
		d.last, d.on = d.atoms, false
	default:
		d.on = false
	}
}

// visualApply runs a Visual operator over r, the cursor at its start.
func (c *EditorCore) visualApply(op Action, r opRange, count int, char string) {
	c.buf.Ln, c.buf.Col = r.lo.Ln, r.lo.Col
	switch op {
	case ActVisualDelete:
		c.applyOp(ActDeletePrefix, r)
	case ActVisualChange:
		c.applyOp(ActChangePrefix, r)
	case ActVisualToggleCase:
		c.applyCase(ActCaseToggleOp, r)
	case ActVisualLower:
		c.applyCase(ActLowerOp, r)
	case ActVisualUpper:
		c.applyCase(ActUpperOp, r)
	case ActVisualIndent:
		c.applyShiftN(1, r, count)
	case ActVisualOutdent:
		c.applyShiftN(-1, r, count)
	case ActVisualReplace:
		c.replaceSelection(r, char)
	case ActVisualInsert, ActVisualAppend:
		b := &c.buf
		last := r.hiEx.Ln
		if !r.lines && r.hiEx.Col == 0 && last > r.lo.Ln {
			last-- // the break before it was selected, not the line
		}
		b.Ln, b.Col = r.lo.Ln, 0
		if op == ActVisualAppend {
			b.Ln, b.Col = last, len(b.LineClusters(last))
		}
		b.Desired = -1
		c.enterInsert()
		c.reveal()
		c.markDirty()
	}
}

// blockApply runs a Visual operator over block v.
func (c *EditorCore) blockApply(op Action, v vblock, count int, char string) {
	switch op {
	case ActVisualDelete:
		c.blockDelete(v)
	case ActVisualChange:
		c.blockChange(v)
	case ActVisualToggleCase:
		c.blockCase(v, ActCaseToggleOp)
	case ActVisualLower:
		c.blockCase(v, ActLowerOp)
	case ActVisualUpper:
		c.blockCase(v, ActUpperOp)
	case ActVisualIndent:
		c.blockShift(v, 1, count)
	case ActVisualOutdent:
		c.blockShift(v, -1, count)
	case ActVisualReplace:
		c.blockReplace(v, char)
	case ActVisualInsert, ActVisualAppend:
		c.blockStartInsert(v, op == ActVisualAppend)
	}
}

// visualReplay is . over a Visual operation: the same operator over a region of the same size from
// the cursor, cut to the text.
func (c *EditorCore) visualReplay(v visualRedo) {
	b := &c.buf
	last := min(b.Ln+v.nLine-1, len(b.Lines)-1)
	if v.block {
		left, _ := c.cellSpan(txt.Pos{Ln: b.Ln, Col: b.Col})
		c.blockApply(v.op, vblock{lo: b.Ln, hi: last, left: left, right: left + v.cols, toEnd: v.toEnd}, v.count, v.char)
		return
	}
	r := opRange{lo: txt.Pos{Ln: b.Ln, Col: b.Col}}
	switch {
	case v.lines:
		r = opRange{lo: txt.Pos{Ln: b.Ln, Col: 0}, hiEx: txt.Pos{Ln: last, Col: 0}, lines: true}
	case v.nLine == 1:
		r.hiEx = txt.Pos{Ln: b.Ln, Col: min(b.Col+v.cols, len(b.LineClusters(b.Ln)))}
	default:
		r.hiEx = txt.Pos{Ln: last, Col: min(v.cols, len(b.LineClusters(last)))}
	}
	if !r.lines && r.hiEx == r.lo {
		return
	}
	c.visualApply(v.op, r, v.count, v.char)
}

// replaceSelection is Visual r{c}: every character of r becomes char; line breaks stay.
func (c *EditorCore) replaceSelection(r opRange, char string) {
	b := &c.buf
	c.beginGroup()
	for ln := r.lo.Ln; ln <= r.hiEx.Ln; ln++ {
		cs := b.LineClusters(ln)
		from, to := 0, len(cs)
		if !r.lines {
			if ln == r.lo.Ln {
				from = min(r.lo.Col, len(cs))
			}
			if ln == r.hiEx.Ln {
				to = min(r.hiEx.Col, len(cs))
			}
		}
		if from < to {
			b.SetLine(ln, strings.Join(cs[:from], "")+strings.Repeat(char, to-from)+strings.Join(cs[to:], ""))
		}
	}
	b.Ln, b.Col = r.lo.Ln, r.lo.Col
	b.Anchor = nil
	c.clampNormal()
	c.edited()
}

// visualOther is Visual o: the cursor goes to the selection's other end, the anchor to where the
// cursor was.
func (c *EditorCore) visualOther() {
	b := &c.buf
	at := txt.Pos{Ln: b.Ln, Col: b.Col}
	b.Ln, b.Col = c.vAnchor.Ln, c.vAnchor.Col
	c.vAnchor = at
	b.Desired = -1
	c.reveal()
	c.markDirty()
}
