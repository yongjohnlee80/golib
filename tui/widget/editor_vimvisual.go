package widget

import "strings"

// VIM VISUAL OPERATORS — beyond y and d, Visual's c (and s), r{c}, ~ u U, > and <, and o, over the
// selection (characters, or whole lines in Visual line), with M2's and M3's operators, so the
// registers and undo behave as the same operator in Normal does. . repeats a Visual operation as
// Vim does: the same operator over a region of the same size from the cursor (the lines, and the
// characters on one line, or the end column on the last), and a c's typed text after it.

// visualRedo is a Visual operation as . replays it.
type visualRedo struct {
	op    Action
	lines bool // whole lines (Visual line)
	nLine int  // how many lines
	cols  int  // characters: on one line, how many; across lines, the last line's end column
	char  string
	count int
}

// visualOpRange is the selection as an operator's range.
func (c *EditorCore) visualOpRange() opRange {
	if c.keys.mode == ModeVisualLine {
		lo, hi := c.visualLines()
		return opRange{lo: taPos{lo, 0}, hiEx: taPos{hi, 0}, lines: true}
	}
	lo, hiEx := c.visualRange()
	return opRange{lo: lo, hiEx: hiEx}
}

// visualOp runs op over the selection, ends Visual, and records it for .: count is > and <'s
// units, char r's character.
func (c *EditorCore) visualOp(op Action, count int, char string) {
	r := c.visualOpRange()
	redo := visualRedo{op: op, lines: r.lines, nLine: r.hiEx.ln - r.lo.ln + 1, count: count, char: char}
	switch {
	case r.lines:
	case r.lo.ln == r.hiEx.ln:
		redo.cols = r.hiEx.col - r.lo.col
	default:
		redo.cols = r.hiEx.col
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
	c.buf.ln, c.buf.col = r.lo.ln, r.lo.col
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
	}
}

// visualReplay is . over a Visual operation: the same operator over a region of the same size from
// the cursor, cut to the text.
func (c *EditorCore) visualReplay(v visualRedo) {
	b := &c.buf
	last := min(b.ln+v.nLine-1, len(b.lines)-1)
	r := opRange{lo: taPos{b.ln, b.col}}
	switch {
	case v.lines:
		r = opRange{lo: taPos{b.ln, 0}, hiEx: taPos{last, 0}, lines: true}
	case v.nLine == 1:
		r.hiEx = taPos{b.ln, min(b.col+v.cols, len(b.lineClusters(b.ln)))}
	default:
		r.hiEx = taPos{last, min(v.cols, len(b.lineClusters(last)))}
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
	for ln := r.lo.ln; ln <= r.hiEx.ln; ln++ {
		cs := b.lineClusters(ln)
		from, to := 0, len(cs)
		if !r.lines {
			if ln == r.lo.ln {
				from = min(r.lo.col, len(cs))
			}
			if ln == r.hiEx.ln {
				to = min(r.hiEx.col, len(cs))
			}
		}
		if from < to {
			b.lines[ln] = strings.Join(cs[:from], "") + strings.Repeat(char, to-from) + strings.Join(cs[to:], "")
		}
	}
	b.touch(r.lo.ln)
	b.ln, b.col = r.lo.ln, r.lo.col
	b.anchor = nil
	c.clampNormal()
	c.edited()
}

// visualOther is Visual o: the cursor goes to the selection's other end, the anchor to where the
// cursor was.
func (c *EditorCore) visualOther() {
	b := &c.buf
	at := taPos{b.ln, b.col}
	b.ln, b.col = c.vAnchor.ln, c.vAnchor.col
	c.vAnchor = at
	b.desired = -1
	c.reveal()
	c.markDirty()
}
