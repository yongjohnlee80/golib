package widget

// FOLDS — runs of lines a layout draws as one: a rendered block with none of its source drawn (a
// diagram's picture), or hidden (metadata). Vim's closed folds, kept by the layout,
// which opens them while text is typed. Over a fold, Normal and Visual mode behave as Vim's
// fold-behavior says: a vertical move counts it as one row and lands on its first line, in its
// first column; an operator takes it whole. A hidden fold is no row: a move passes it. The core asks
// the layout (EditorFoldLayout), as it asks for rows (EditorRowLayout); without one, nothing folds.

// EditorFoldLayout is an EditorLayout that draws some runs of lines as one.
type EditorFoldLayout interface {
	// FoldOf is the fold holding line: lines [from, to) drawn as one. hidden: drawn as nothing
	// (zero height). ok false: line is drawn as itself.
	FoldOf(line int) (from, to int, hidden, ok bool)
}

// noFolds is the fold layout of a core whose layout has none.
type noFolds struct{}

func (noFolds) FoldOf(int) (int, int, bool, bool) { return 0, 0, false, false }

func (c *EditorCore) foldLayout() EditorFoldLayout {
	if fl, ok := c.layout.(EditorFoldLayout); ok {
		return fl
	}
	return noFolds{}
}

// lineStep is the line one row down or up from ln: out of ln's fold from its edge, then onto the
// next row, a visible fold's first line, past any hidden fold. ok is false when no row lies that
// way: the text's end, or only hidden lines beyond it.
func (c *EditorCore) lineStep(ln int, down bool) (int, bool) {
	fl := c.foldLayout()
	last := len(c.buf.textLines) - 1
	edge := func(from, to int) int {
		if down {
			return to - 1
		}
		return from
	}
	if from, to, _, ok := fl.FoldOf(ln); ok {
		ln = edge(from, to)
	}
	for {
		switch {
		case down && ln < last:
			ln++
		case !down && ln > 0:
			ln--
		default:
			return 0, false
		}
		from, to, hidden, ok := fl.FoldOf(ln)
		if !ok {
			return ln, true
		}
		if !hidden {
			return from, true
		}
		ln = edge(from, to)
	}
}

// rowsFrom is n rows down or up from ln, as far as there are; moved is false when not one.
func (c *EditorCore) rowsFrom(ln, n int, down bool) (to int, moved bool) {
	to = ln
	for range n {
		next, ok := c.lineStep(to, down)
		if !ok {
			break
		}
		to, moved = next, true
	}
	return to, moved
}

// lineTarget is j and k's target over folds: |delta| rows down (delta > 0) or up, at the sticky
// column (kept in desired, as verticalTarget keeps it), or on a fold its first line's column 0,
// where Vim draws a fold's cursor.
func (c *EditorCore) lineTarget(delta int) (ln, col int) {
	b := &c.buf
	if b.desired < 0 {
		b.desired = b.cellsAt(b.ln, b.col, c.measure)
	}
	n := delta
	if n < 0 {
		n = -n
	}
	ln, _ = c.rowsFrom(b.ln, n, delta > 0)
	if _, _, _, folded := c.foldLayout().FoldOf(ln); folded {
		return ln, 0
	}
	return ln, b.colForCells(ln, b.desired, c.measure)
}

// inFold reports whether line ln is in a fold.
func (c *EditorCore) inFold(ln int) bool {
	_, _, _, ok := c.foldLayout().FoldOf(ln)
	return ok
}

// wholeFolds is r widened to whole folds: an end in a fold moves to the fold's edge, and the range
// becomes linewise, as Vim's operators take a closed fold whole.
func (c *EditorCore) wholeFolds(r opRange) opRange {
	fl := c.foldLayout()
	lo, hi := r.lo.ln, r.hiEx.ln
	if !r.lines && r.hiEx.col == 0 && hi > lo {
		hi-- // the range ends with the line break before hiEx: hiEx's line is not in it
	}
	from, _, _, loIn := fl.FoldOf(lo)
	_, to, _, hiIn := fl.FoldOf(hi)
	if !loIn && !hiIn {
		return r
	}
	if loIn {
		lo = from
	}
	if hiIn {
		hi = to - 1
	}
	return opRange{lo: taPos{lo, 0}, hiEx: taPos{hi, 0}, lines: true}
}
