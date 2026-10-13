package widget

import (
	"math"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// VIM VISUAL BLOCK — Ctrl-v: the lines from the anchor's to the cursor's, and the display cells
// between theirs, as Vim's virtual columns are. Its left edge is the smaller of the two places'
// first cells, its right edge the larger of their last; a wide character or a tab counts whole, and
// a line too short to reach the left edge has nothing in it. $ stretches the right edge to every
// line's own end until a motion sets a column again (Vim's curswant MAXCOL: the desired column).
//
// A line's part of the block is Vim's block_prep: a cluster straddling an edge is in the block,
// and an operator splits it into spaces — a yank keeps the cells inside, a delete the cells outside,
// so what is left stays aligned. The cells are the cell editor's: a wide character two, a tab
// tabCells, whatever the layout measures in.

// blockToEnd is the desired column $ leaves in Visual block: past every line's end.
const blockToEnd = math.MaxInt32

// blockCells is a run of clusters' width in the block's cells.
var blockCells = tabAware(tui.StringWidth)

// vblock is a Visual block: lines lo to hi, cells [left, right), or to each line's end.
type vblock struct {
	lo, hi      int
	left, right int
	toEnd       bool
}

// visualBlock is the block the anchor and the cursor make.
func (c *EditorCore) visualBlock() vblock {
	b := &c.buf
	a, p := c.vAnchor, taPos{b.ln, b.col}
	al, ar := c.cellSpan(a)
	pl, pr := c.cellSpan(p)
	return vblock{lo: min(a.ln, p.ln), hi: max(a.ln, p.ln), left: min(al, pl), right: max(ar, pr),
		toEnd: b.desired == blockToEnd}
}

// cellSpan is the cells [from, to) of the cluster at p; past a line's end, the one cell there.
func (c *EditorCore) cellSpan(p taPos) (from, to int) {
	cs := c.buf.lineClusters(p.ln)
	col := min(p.col, len(cs))
	from = cellsBefore(cs, col, blockCells)
	w := 1
	if col < len(cs) {
		w = max(blockCells(cs[col]), 1)
	}
	return from, from + w
}

// blockLine is one line's part of a block.
type blockLine struct {
	cs                 []string
	from, to           int // the clusters meeting the block: what a delete takes out
	inFrom, inTo       int // the clusters wholly inside it
	startIn, startOut  int // a cluster straddling the left edge: its cells inside the block, outside
	endIn, endOut      int // one straddling the right edge, the same
	width              int // the line's cells
	short              bool
	shortEnd           bool // the line ends before the block's right edge
	leftCell, rightCel int  // the block's edges on this line (the right one, its end with $)
}

// line is cs's part of v.
func (v vblock) line(cs []string) blockLine {
	starts := make([]int, len(cs)+1)
	for i, cl := range cs {
		starts[i+1] = starts[i] + blockCells(cl)
	}
	l := blockLine{cs: cs, width: starts[len(cs)], leftCell: v.left}
	right := v.right
	if v.toEnd {
		right = max(l.width, v.left)
	}
	l.rightCel = right
	l.shortEnd = l.width < right
	if l.width <= v.left {
		l.short = true
		l.from, l.to, l.inFrom, l.inTo = len(cs), len(cs), len(cs), len(cs)
		return l
	}
	from := 0
	for from < len(cs) && starts[from+1] <= v.left {
		from++
	}
	to := from
	for to < len(cs) && starts[to] < right {
		to++
	}
	l.from, l.to, l.inFrom, l.inTo = from, to, from, to
	if starts[from] < v.left {
		l.startOut, l.startIn = v.left-starts[from], min(starts[from+1], right)-v.left
		l.inFrom = from + 1
	}
	if starts[to] > right {
		l.endOut = starts[to] - right
		if to-1 >= l.inFrom {
			l.endIn, l.inTo = right-starts[to-1], to-1
		}
	}
	return l
}

// piece is what a yank takes from the line: the cells inside, a straddling cluster's as spaces.
func (l blockLine) piece() string {
	return spaces(l.startIn) + strings.Join(l.cs[l.inFrom:max(l.inFrom, l.inTo)], "") + spaces(l.endIn)
}

// deleted is the line with the block's part taken out, a straddling cluster's outside cells left
// as spaces.
func (l blockLine) deleted() string {
	return strings.Join(l.cs[:l.from], "") + spaces(l.startOut+l.endOut) + strings.Join(l.cs[l.to:], "")
}

func spaces(n int) string { return strings.Repeat(" ", max(n, 0)) }

// splitAt cuts cs at cell x: a cluster straddling it becomes spaces on both sides, and a line too
// short is padded with spaces to x when pad is set (else it is cut at its end).
func splitAt(cs []string, x int, pad bool) (before, after string) {
	w := 0
	for i, cl := range cs {
		cw := blockCells(cl)
		switch {
		case w == x:
			return strings.Join(cs[:i], ""), strings.Join(cs[i:], "")
		case w+cw > x:
			return strings.Join(cs[:i], "") + spaces(x-w), spaces(w+cw-x) + strings.Join(cs[i+1:], "")
		}
		w += cw
	}
	line := strings.Join(cs, "")
	if pad {
		line += spaces(x - w)
	}
	return line, ""
}

// blockPieces is the block's pieces and its width: the right edge's cells less the left's, or with
// $ the widest piece.
func (c *EditorCore) blockPieces(v vblock) (pieces []string, width int) {
	width = v.right - v.left
	if v.toEnd {
		width = 0
	}
	for ln := v.lo; ln <= v.hi; ln++ {
		p := v.line(c.buf.lineClusters(ln)).piece()
		pieces = append(pieces, p)
		if v.toEnd {
			width = max(width, blockCells(p))
		}
	}
	return pieces, width
}

// blockTopLeft puts the cursor at the block's top left: on its first line, where the left edge is
// once the line's straddling cluster is split, or the line's end when it is short.
func (c *EditorCore) blockTopLeft(v vblock) {
	b := &c.buf
	l := v.line(b.lineClusters(v.lo))
	b.ln, b.col = v.lo, l.from
	b.desired = -1
	c.clampNormal()
}

// blockYank is y over the block: the pieces into the register, blockwise, the cursor at its top
// left.
func (c *EditorCore) blockYank(v vblock) {
	pieces, width := c.blockPieces(v)
	c.yankEntry(regEntry{text: strings.Join(pieces, "\n"), block: true, width: width})
	c.blockTopLeft(v)
}

// blockDelete takes each line's part out, into the register (blockwise) when reg is set.
func (c *EditorCore) blockDelete(v vblock, reg bool) {
	b := &c.buf
	if reg {
		pieces, width := c.blockPieces(v)
		c.deleteEntry(regEntry{text: strings.Join(pieces, "\n"), block: true, width: width}, v.hi > v.lo)
	}
	c.beginGroup()
	for ln := v.lo; ln <= v.hi; ln++ {
		if l := v.line(b.lineClusters(ln)); l.from < l.to {
			b.lines[ln] = l.deleted()
		}
	}
	b.touch(v.lo)
	c.blockTopLeftAfterDelete(v)
	c.edited()
}

// blockTopLeftAfterDelete is the cursor at the left edge once the block is out: past the spaces a
// straddling cluster left.
func (c *EditorCore) blockTopLeftAfterDelete(v vblock) {
	b := &c.buf
	cs := b.lineClusters(v.lo)
	b.ln, b.col = v.lo, b.colForCells(v.lo, v.left, blockCells)
	if b.col > len(cs) {
		b.col = len(cs)
	}
	b.desired = -1
	b.anchor = nil
	c.clampNormal()
}

// blockReplace is r{c} over the block: every cell of it becomes char (a wide char takes two, and an
// odd cell left over is a space), a straddling cluster's outside cells spaces.
func (c *EditorCore) blockReplace(v vblock, char string) {
	b := &c.buf
	cw := max(blockCells(char), 1)
	c.beginGroup()
	for ln := v.lo; ln <= v.hi; ln++ {
		l := v.line(b.lineClusters(ln))
		if l.from == l.to {
			continue
		}
		in := min(l.width, l.rightCel) - l.leftCell
		b.lines[ln] = strings.Join(l.cs[:l.from], "") + spaces(l.startOut) + strings.Repeat(char, in/cw) +
			spaces(in%cw) + spaces(l.endOut) + strings.Join(l.cs[l.to:], "")
	}
	b.touch(v.lo)
	c.blockTopLeftAfterDelete(v)
	c.edited()
}

// blockCase is ~ u U over the block: the clusters wholly inside it.
func (c *EditorCore) blockCase(v vblock, op Action) {
	b := &c.buf
	f := caseOf(op)
	c.beginGroup()
	for ln := v.lo; ln <= v.hi; ln++ {
		l := v.line(b.lineClusters(ln))
		if l.inFrom >= l.inTo {
			continue
		}
		b.lines[ln] = strings.Join(l.cs[:l.inFrom], "") + f(strings.Join(l.cs[l.inFrom:l.inTo], "")) +
			strings.Join(l.cs[l.inTo:], "")
	}
	b.touch(v.lo)
	c.blockTopLeft(v)
	c.edited()
}

// blockShift is > and < over the block (Vim's v_b_>): on each line reaching the left edge, the
// text from there moves n units right, or left by as much of the blank run at the edge as there is.
func (c *EditorCore) blockShift(v vblock, dir, n int) {
	b := &c.buf
	unit := strings.Repeat(c.shiftUnit(), max(n, 1))
	c.beginGroup()
	for ln := v.lo; ln <= v.hi; ln++ {
		cs := b.lineClusters(ln)
		if v.line(cs).short {
			continue
		}
		before, after := splitAt(cs, v.left, false)
		b.lines[ln] = before + shiftLine(after, unit, dir)
	}
	b.touch(v.lo)
	c.blockTopLeft(v)
	c.edited()
}

// blockOther is O in Visual block: the cursor and the anchor swap columns, each keeping its line.
func (c *EditorCore) blockOther() {
	b := &c.buf
	ac, _ := c.cellSpan(c.vAnchor)
	pc, _ := c.cellSpan(taPos{b.ln, b.col})
	b.col = b.colForCells(b.ln, ac, blockCells)
	c.vAnchor.col = b.colForCells(c.vAnchor.ln, pc, blockCells)
	b.desired = -1
	c.reveal()
	c.markDirty()
}

// blockInsert is I, A or c over a block, waiting for its Insert to end: what was typed on the first
// line then goes in on the others.
type blockInsert struct {
	ln, col int   // where the Insert began
	n       int   // the first line's clusters then
	lines   []int // the other lines that take the text
	at      int   // the cell they take it at; -1 each line's end
	pad     bool  // a line too short is padded with spaces to at (A)
}

// blockStartInsert is I (append false) or A over the block: Insert before its left edge, or after
// its right edge, on the first line.
func (c *EditorCore) blockStartInsert(v vblock, appending bool) {
	b := &c.buf
	bi := &blockInsert{at: v.left}
	first := v.line(b.lineClusters(v.lo))
	b.ln = v.lo
	padded := false
	switch {
	case appending && v.toEnd:
		bi.at, b.col = -1, len(first.cs)
	case appending:
		bi.at, bi.pad = v.right, true
		if first.width < v.right {
			c.beginGroup() // the padding, and what is typed after it, are one step
			b.lines[v.lo] += spaces(v.right - first.width)
			b.touch(v.lo)
			b.col = len(b.lineClusters(v.lo))
			padded = true
			c.edited()
		} else {
			b.col = first.to // after a straddling cluster, as Vim's first line has it
		}
	default:
		b.col = first.from
	}
	for ln := v.lo + 1; ln <= v.hi; ln++ {
		if appending || blockCells(b.lines[ln]) >= v.left { // a line ending at the edge takes it too
			bi.lines = append(bi.lines, ln)
		}
	}
	c.enterBlockInsert(bi, padded)
}

// blockChange is c over the block: its parts out, then Insert at the left edge of the first line;
// the text typed goes in at the left edge of every other line that reaches it.
func (c *EditorCore) blockChange(v vblock) {
	b := &c.buf
	c.blockDelete(v, true)
	bi := &blockInsert{at: v.left}
	for ln := v.lo + 1; ln <= v.hi; ln++ {
		if blockCells(b.lines[ln]) >= v.left {
			bi.lines = append(bi.lines, ln)
		}
	}
	c.enterBlockInsert(bi, true)
}

// enterBlockInsert enters Insert at the cursor with bi waiting on its end; began is an edit that
// began this step already (c's delete, A's padding), which what is typed, and its copies, join.
func (c *EditorCore) enterBlockInsert(bi *blockInsert, began bool) {
	b := &c.buf
	b.anchor = nil
	c.vAnchor = taPos{}
	c.enterInsert()
	if began {
		c.hist.keepOpen()
	}
	bi.ln, bi.col, bi.n = b.ln, b.col, len(b.lineClusters(b.ln))
	c.blockIns = bi
	c.reveal()
	c.markDirty()
}

// blockInsertDone is the Insert's end: when the cursor is still on the first line and the line
// grew, the text from where Insert began, as long as the growth, goes in on the other lines.
func (c *EditorCore) blockInsertDone() {
	bi := c.blockIns
	c.blockIns = nil
	b := &c.buf
	if bi == nil || b.ln != bi.ln || bi.ln >= len(b.lines) {
		return
	}
	cs := b.lineClusters(bi.ln)
	grown := len(cs) - bi.n
	if grown <= 0 || bi.col+grown > len(cs) {
		return
	}
	text := strings.Join(cs[bi.col:bi.col+grown], "")
	c.beginGroup() // in Insert: the typing's step when it is still open
	for _, ln := range bi.lines {
		if ln >= len(b.lines) {
			continue
		}
		lcs := b.lineClusters(ln)
		if bi.at < 0 {
			b.lines[ln] += text
			continue
		}
		before, after := splitAt(lcs, bi.at, bi.pad)
		if !bi.pad && blockCells(before) < bi.at {
			continue
		}
		b.lines[ln] = before + text + after
	}
	b.touch(bi.ln)
	c.edited()
}
