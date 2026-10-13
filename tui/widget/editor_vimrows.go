package widget

// VIM gj AND gk — down and up by display row in wrapped text, keeping the x within the row, as
// Vim's nv_screengo (src/normal.c). The layout knows the rows (EditorRowLayout); one without it has
// a row per line. As an operator's motion they are charwise and exclusive, not linewise as j and
// k are, as Vim's are. The x is kept across consecutive gj and gk, as Vim's curswant is: any other
// move of the cursor ends it (cursorMoved), even one that comes back to the same place.

// EditorRowLayout is an EditorLayout that wraps lines into display rows, for gj and gk. Positions
// are in the layout's units (cells in the terminal, pixels in the GUI). A break space the wrap
// consumed is drawn at the end of the row before it, and maps there.
type EditorRowLayout interface {
	// RowCount is how many display rows line takes: 1 unwrapped.
	RowCount(line int) int
	// RowOf is (line, col)'s display row and its x within the row.
	RowOf(line, col int) (row, x int)
	// ColAt is the column on display row `row` of line under x: the cluster whose span holds x,
	// or past the row's end its last cluster, or the break space the wrap consumed after it.
	ColAt(line, row, x int) int
}

// lineRows is the row layout of a core whose layout has none: a row per line.
type lineRows struct{ c *EditorCore }

func (r lineRows) RowCount(int) int { return 1 }
func (r lineRows) RowOf(line, col int) (int, int) {
	return 0, r.c.buf.cellsAt(line, col, r.c.measure)
}
func (r lineRows) ColAt(line, _, x int) int {
	cs := r.c.buf.lineClusters(line)
	return min(r.c.buf.colForCells(line, x, r.c.measure), max(len(cs)-1, 0))
}

func (c *EditorCore) rowLayout() EditorRowLayout {
	if rl, ok := c.layout.(EditorRowLayout); ok {
		return rl
	}
	return lineRows{c}
}

// rowSticky is gj and gk's x: ok until the cursor moves by anything else (kept marks their own
// move, for cursorMoved), and only where they left it (at), against a move from outside a key.
type rowSticky struct {
	x    int
	at   taPos
	ok   bool
	kept bool
}

// rowTarget is count display rows down (gj) or up (gk) from the cursor, at the kept x; moved is
// false at the text's first or last row.
func (c *EditorCore) rowTarget(down bool, count int) (to taPos, moved bool) {
	b := &c.buf
	rl := c.rowLayout()
	ln := b.ln
	row, x := rl.RowOf(ln, b.col)
	if s := c.rowSticky; s.ok && s.at == (taPos{b.ln, b.col}) {
		x = s.x
	}
	for range max(count, 1) {
		switch {
		case down && row+1 < rl.RowCount(ln):
			row++
		case down && ln+1 < len(b.lines):
			ln, row = ln+1, 0
		case !down && row > 0:
			row--
		case !down && ln > 0:
			ln--
			row = rl.RowCount(ln) - 1
		default:
			continue // the text's end: as far as it goes
		}
		moved = true
	}
	to = taPos{ln, rl.ColAt(ln, row, x)}
	c.rowSticky = rowSticky{x: x, at: to, ok: true}
	return to, moved
}

// rowMove is gj or gk as a move: the cursor at the target, in Normal on a cluster.
func (c *EditorCore) rowMove(down bool, count int) {
	b := &c.buf
	to, moved := c.rowTarget(down, count)
	if !moved {
		return
	}
	c.hist.close()
	b.ln, b.col = to.ln, to.col
	b.desired = -1
	if c.keys.mode == ModeNormal {
		c.clampNormal()
	}
	c.rowSticky.at, c.rowSticky.kept = taPos{b.ln, b.col}, true
	c.reveal()
	c.markDirty()
}
