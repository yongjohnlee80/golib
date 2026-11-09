package editor

import (
	"slices"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE EDITOR'S OWN RENDERED VIEW — a renderer the cell Editor draws its text through, as gui's
// Editor draws through its Renderer: the lines split into blocks, each block laid out as rows of
// cells, every cell mapped to the source cluster it shows (or to none, for a decoration). The block
// holding the cursor is laid as its source while text is typed in it (Insert or Replace on an
// editable buffer); in Normal and Visual every block stays rendered. A run of lines with no cell of
// theirs drawn (hidden metadata, a picture's source) is a fold: motions step over it as one, and
// an operator takes it whole. Every other rendered line keeps its source: it is moved through,
// selected and edited cluster by cluster.
//
// A document view, when one is on, is drawn over the renderer (an HTML or CSV file's page or
// table); the renderer draws the text otherwise.

// CellRenderer is the cell Editor's counterpart of gui's Renderer: it splits lines into blocks and
// lays one out as rows of cells.
type CellRenderer interface {
	// Blocks splits lines [from, to) into blocks covering them in order, without gaps.
	Blocks(lines []string, from, to int) []Block
	// LayOut lays block b out to width cells, measuring each cluster with measure (the Editor's
	// width policy). typing: b holds the cursor while text is typed, so it is laid as its source.
	LayOut(b Block, lines []string, width int, measure func(string) int, typing bool, th CellTheme) CellBlock
}

// CellBlockContext is a CellRenderer whose blocks the Editor may keep across an edit: a block whose
// lines, bounds (moved only by the lines an edit added or removed above it), typing and stamp are
// unchanged is not laid again. BlockContext stamps what block b's LayOut reads beyond its own lines'
// text (the lines around it that classify it, its renderer's settings): it must change whenever such
// an input changes LayOut's result. A renderer without it is laid whole on every change.
type CellBlockContext interface {
	BlockContext(b Block, lines []string) uint64
}

// CellBlock is a block laid out: its rows, and the widest of them in cells.
type CellBlock struct {
	Rows  []CellRow
	Width int
	// Picture is the picture the block is drawn as, its Rows its cells, when it is one (a diagram, an
	// image): the Editor reports an image's place on the view by it.
	Picture CellPicture
}

// CellRow is one display row. Anchor is the source line it stands for when it shows none of that
// line's clusters (an empty line, a line whose markup is all concealed), -1 otherwise; a row of
// decorations only (a box's rule) has none. Unnumbered: no line number in the gutter.
type CellRow struct {
	Cells      []RowCell
	Anchor     int
	Unnumbered bool
}

// ShowsNoSource reports whether the row shows none of a source line's clusters: decorations only,
// or nothing.
func (r CellRow) ShowsNoSource() bool {
	for _, c := range r.Cells {
		if c.Line >= 0 {
			return false
		}
	}
	return true
}

// RowCell is one cluster drawn: its text, its width in cells, its look, and the source cluster it
// shows (Line -1: a decoration, a box's rule or a bullet glyph).
type RowCell struct {
	Text      string
	W         int
	Style     style.Style
	Line, Col int
}

// CellTheme is what a CellRenderer draws with: the text's look and the editor's syntax styles,
// which a Markdown renderer reads its headings, emphasis, links and code from.
type CellTheme struct {
	Text   style.Style
	Syntax SyntaxStyles
}

// MarkdownStructure is a renderer that knows where its document's code and tables are: an editor
// runs list editing in its Rendered view outside code, and on a table's lines only where they are
// not rows. MarkdownBlocks is one, and so is a renderer that embeds it; gui's StructureRenderer
// has the same two methods.
type MarkdownStructure interface {
	InCode(lines []string, ln int) bool
	InTable(lines []string, ln int) bool
}

// RelayRendered has the Rendered view laid again at its next draw: each block's stamp
// (CellBlockContext) is taken again, and only a block whose stamp changed is laid anew. A host calls
// it when something a renderer reads beyond the text may have changed: a file a line embeds
// (MarkdownCellRenderer.Embeds), which no edit of the text tells the Editor of.
func (e *Editor) RelayRendered() {
	e.relayGen++
	e.MarkDirty()
}

// SetRenderer draws the Editor's Rendered view through r; nil: the Editor renders nothing itself
// (a document view, if one is set, is its Rendered view). A structured renderer's list editing
// replaces any earlier renderer's, and the view drawn is worked out again at once.
func (e *Editor) SetRenderer(r CellRenderer) {
	if e.render != nil { // the old renderer's list editing goes with it
		e.core.SetListEditingWhere(nil)
		e.core.SetListEditing(false)
	}
	e.render, e.renderGen = r, e.renderGen+1
	if r != nil {
		e.wireToggle() // Ctrl+T (the core's Rendered toggle) flips to the renderer's view too
	}
	if md, ok := r.(MarkdownStructure); ok {
		e.core.SetListEditingWhere(MarkdownListWhere(e.core, md))
	}
	if p, ok := r.(interface{ SetPictureHost(CellPictureHost) }); ok { // a renderer that draws pictures lays them here
		p.SetPictureHost(e)
	}
	if p, ok := r.(interface{ SetImageHost(CellImageHost) }); ok { // and one that draws images
		p.SetImageHost(e)
	}
	e.applyView()
}

// Renderer is the Editor's own renderer, nil for none.
func (e *Editor) Renderer() CellRenderer { return e.render }

// MarkdownListWhere is the lines a Markdown editor's list editing applies on (SetListEditingWhere):
// outside code, where "- x" is code, and on a table's lines only where they are not rows. A table's
// row is a row: the table keys (CoreTableEditing) run on the rows they read, the ones with both
// outer pipes, which no list item starts with; a row GFM reads without them takes neither, so its
// "- x" is not an item either. Both editors, the cell one and gui's, edit lists by it.
func MarkdownListWhere(c *EditorCore, md MarkdownStructure) func(ln int) bool {
	return func(ln int) bool {
		lines := c.Lines()
		if md.InCode(lines, ln) {
			return false
		}
		if !md.InTable(lines, ln) {
			return true
		}
		_, row := MarkdownTableCells(lines[ln])
		return row
	}
}

// hasRendered reports whether there is a Rendered view to switch to: a document view, or a renderer
// of the Editor's own. The header's switch and the title row's both ask it.
func (e *Editor) hasRendered() bool { return e.doc.Document() != nil || e.render != nil }

// typing reports whether text is being typed: Insert or Replace on an editable buffer. Only then
// is the cursor's block laid as its source.
func (l *cellLayout) typing() bool {
	m := l.ed.core.Mode()
	return !l.ed.core.ReadOnly() && (m == ModeInsert || m == ModeReplace)
}

// rendered reports whether the Editor draws its text through its renderer now: Rendered shown, a
// renderer set, and no document view drawn over it.
func (e *Editor) rendered() bool {
	return e.render != nil && e.ShownView() == ViewRendered && !e.doc.Active()
}

// cellTheme is what the renderer draws with now: the text's look and the core's syntax styles.
func (e *Editor) cellTheme() CellTheme {
	th := CellTheme{Text: e.cells.styles.Text}
	for k := range th.Syntax {
		if st, ok := e.core.SyntaxStyle(highlight.Style(k)); ok {
			th.Syntax[k] = st
		}
	}
	return th
}

// renderedKey is everything a Rendered layout depends on: the text, the width, the measure's
// owner, the block being typed in, and the look.
type renderedKey struct {
	gen    uint64  // the renderer set (SetRenderer): a renderer itself may not be comparable
	buf    *Buffer // the buffer shown: revisions are a buffer's own, two buffers may share one
	rev    uint64
	width  int
	typing int // the line being typed on, -1 when no text is typed
	theme  CellTheme
	pics   uint64          // the pictures laid (picGen): one arriving lays its block again
	relay  uint64          // the host's asks (RelayRendered): each block's stamp taken again
	policy tui.WidthPolicy // the measure's: unmounted, the default; mounted, the App's
}

// renderedLayout is the Editor's text through its renderer: the display rows in order, and each
// source line's rows (those holding a cell of it, or standing for it).
type renderedLayout struct {
	key      renderedKey
	blocks   []Block
	rows     []CellRow
	lineRows [][]int
	blockOf  []int         // each line's block, into blocks
	rowBlock []int         // each row's block, into blocks
	blockW   []int         // each block's width as laid out: wider than the room, it pans
	blockRow []int         // each block's first row, -1 for none
	blockPic []CellPicture // each block's picture, nil for none (CellBlock.Picture)
	blockN   []int         // each block's rows
	lines    []string      // the lines laid (a copy): the next layout keeps the blocks whose lines it still has
	typed    []bool        // each block laid as its source (typing)
	asked    []bool        // each block asked the Editor for a picture or an image, ready or not: laid again each time
	stamps   []uint64      // each block's CellBlockContext stamp
}

// renderedKey is the key of the Editor's Rendered layout at width now.
func (e *Editor) renderedKey(width int) renderedKey {
	key := renderedKey{gen: e.renderGen, buf: e.core.Buffer(), rev: e.core.Revision(), width: width, typing: -1, theme: e.cellTheme(), pics: e.picGen, relay: e.relayGen}
	if ctx := e.Context(); ctx != nil {
		key.policy = ctx.WidthPolicy()
	}
	if e.cells.typing() {
		key.typing = e.core.buf.Ln
	}
	return key
}

// laidAt is the Editor's Rendered layout at key, when it is kept; nil when it would be laid.
func (e *Editor) laidAt(key renderedKey) *renderedLayout {
	for _, r := range e.laid {
		if r != nil && r.key == key {
			return r
		}
	}
	return nil
}

// tallerThan reports whether the Rendered layout at width has more than h rows: the kept layout's
// count, or the blocks laid one by one until they pass h. It answers as renderedRows(width) would,
// without laying a long text whole only to learn it scrolls.
func (e *Editor) tallerThan(width, h int) bool {
	key := e.renderedKey(width)
	if r := e.laidAt(key); r != nil {
		return len(r.rows) > h
	}
	lines := e.core.buf.Lines
	n := 0
	for _, b := range e.render.Blocks(lines, 0, len(lines)) {
		typing := key.typing >= b.From && key.typing < b.To
		e.layAsked = false
		n += len(e.render.LayOut(b, lines, width, e.width, typing, key.theme).Rows)
		e.layAsked = false
		if n > h {
			return true
		}
	}
	return false
}

// renderedRows is the Editor's Rendered layout at width, laid again only when something it depends
// on changed (its key).
func (e *Editor) renderedRows(width int) *renderedLayout {
	lines := e.core.buf.Lines
	key := e.renderedKey(width)
	if r := e.laidAt(key); r != nil {
		return r
	}
	r := &renderedLayout{key: key, lineRows: make([][]int, len(lines)), blockOf: make([]int, len(lines)), lines: slices.Clone(lines)}
	r.blocks = e.render.Blocks(lines, 0, len(lines))
	stamper, _ := e.render.(CellBlockContext)
	keep := r.keeps(e.laid, stamper != nil)
	for bi, b := range r.blocks {
		typing := key.typing >= b.From && key.typing < b.To
		var stamp uint64
		if stamper != nil {
			stamp = stamper.BlockContext(b, lines)
		}
		var cb CellBlock
		if prev, oi, delta := keep(b); prev != nil && prev.typed[oi] == typing && !prev.asked[oi] && prev.stamps[oi] == stamp {
			cb = prev.laidBlock(oi, delta)
		} else {
			e.layAsked = false
			cb = e.render.LayOut(b, lines, width, e.width, typing, key.theme)
		}
		r.typed, r.asked, r.stamps = append(r.typed, typing), append(r.asked, e.layAsked), append(r.stamps, stamp)
		e.layAsked = false
		r.blockN = append(r.blockN, len(cb.Rows))
		r.blockW = append(r.blockW, cb.Width)
		first := -1
		if len(cb.Rows) > 0 {
			first = len(r.rows)
		}
		r.blockRow = append(r.blockRow, first)
		r.blockPic = append(r.blockPic, cb.Picture)
		for ln := b.From; ln < b.To && ln < len(lines); ln++ {
			r.blockOf[ln] = bi
		}
		for _, row := range cb.Rows {
			ri := len(r.rows)
			r.rows, r.rowBlock = append(r.rows, row), append(r.rowBlock, bi)
			seen := -1
			for _, c := range row.Cells {
				if c.Line >= 0 && c.Line < len(lines) && c.Line != seen {
					if rs := r.lineRows[c.Line]; len(rs) == 0 || rs[len(rs)-1] != ri {
						r.lineRows[c.Line] = append(rs, ri)
					}
					seen = c.Line
				}
			}
			if row.Anchor >= 0 && row.Anchor < len(lines) {
				if rs := r.lineRows[row.Anchor]; len(rs) == 0 || rs[len(rs)-1] != ri {
					r.lineRows[row.Anchor] = append(rs, ri)
				}
			}
		}
	}
	e.laid[1], e.laid[0] = e.laid[0], r // the newest first; the other width kept beside it
	return r
}

// keeps finds, for a block of r, the block of an earlier layout laid from the same lines: one at the
// same width, renderer, buffer, look and width policy, the block wholly in the lines before the first that
// changed at its bounds, or wholly in those after the last at its bounds less the lines' count
// change (delta). nil: none, the block is laid. Only a renderer that stamps its blocks keeps any.
func (r *renderedLayout) keeps(laid [2]*renderedLayout, stamped bool) func(b Block) (prev *renderedLayout, oi, delta int) {
	none := func(Block) (*renderedLayout, int, int) { return nil, 0, 0 }
	if !stamped {
		return none
	}
	var prev *renderedLayout
	for _, o := range laid {
		if o != nil && o.key.gen == r.key.gen && o.key.buf == r.key.buf && o.key.width == r.key.width && o.key.theme == r.key.theme && o.key.policy == r.key.policy {
			prev = o
			break
		}
	}
	if prev == nil {
		return none
	}
	n, m := len(r.lines), len(prev.lines)
	p := 0
	for p < min(n, m) && r.lines[p] == prev.lines[p] {
		p++
	}
	s := 0
	for s < min(n, m)-p && r.lines[n-1-s] == prev.lines[m-1-s] {
		s++
	}
	delta := n - m
	byFrom := make(map[int]int, len(prev.blocks))
	for i, b := range prev.blocks {
		byFrom[b.From] = i
	}
	return func(b Block) (*renderedLayout, int, int) {
		d := 0
		switch {
		case b.To <= p:
		case b.From >= n-s:
			d = delta
		default:
			return nil, 0, 0 // a line of it changed
		}
		oi, ok := byFrom[b.From-d]
		if !ok || prev.blocks[oi] != (Block{From: b.From - d, To: b.To - d}) {
			return nil, 0, 0
		}
		return prev, oi, d
	}
}

// laidBlock is block oi as laid, its rows moved delta lines down: a copy, the cells of a source
// line (Line, Anchor >= 0) shifted, a decoration's (-1) kept.
func (r *renderedLayout) laidBlock(oi, delta int) CellBlock {
	var rows []CellRow
	if first := r.blockRow[oi]; first >= 0 {
		rows = r.rows[first : first+r.blockN[oi]]
	}
	if delta != 0 {
		moved := make([]CellRow, len(rows))
		for i, row := range rows {
			row.Cells = slices.Clone(row.Cells)
			for j := range row.Cells {
				if row.Cells[j].Line >= 0 {
					row.Cells[j].Line += delta
				}
			}
			if row.Anchor >= 0 {
				row.Anchor += delta
			}
			moved[i] = row
		}
		rows = moved
	}
	return CellBlock{Rows: slices.Clip(rows), Width: r.blockW[oi], Picture: r.blockPic[oi]}
}

// firstRow is line ln's first display row; a line of a block drawn with no row of a line's (a
// picture) takes the block's first row; any other line with none (folded) the next line's that has
// one, or the last row.
func (r *renderedLayout) firstRow(ln int) int {
	if ln >= 0 && ln < len(r.lineRows) && len(r.lineRows[ln]) == 0 {
		if from, to := r.sourcelessRun(ln); r.picture(ln, from, to) {
			return r.blockRow[r.blockOf[ln]]
		}
	}
	for i := max(ln, 0); i < len(r.lineRows); i++ {
		if rs := r.lineRows[i]; len(rs) > 0 {
			return rs[0]
		}
	}
	return max(len(r.rows)-1, 0)
}

// topRow is the row line ln starts at as the view's top: its first row, or before it the rows of
// decorations only that open its block (a table's top rule), which stand for no line.
func (r *renderedLayout) topRow(ln int) int {
	ri := r.firstRow(ln)
	for ri > 0 && ri < len(r.rows) && r.rowBlock[ri-1] == r.rowBlock[ri] && r.rows[ri-1].Anchor < 0 && r.rows[ri-1].ShowsNoSource() {
		ri--
	}
	return ri
}

// atRow is the top line and the rows of it above the view that put display row start at the
// view's top.
func (r *renderedLayout) atRow(start int) (top, in int) {
	if len(r.rows) == 0 || start <= 0 {
		return 0, 0
	}
	start = min(start, len(r.rows)-1)
	top = r.lineOfRow(start)
	if start < len(r.rowBlock) { // a block's opening rows (a table's top rule) are its first line's
		if ln := r.blocks[r.rowBlock[start]].From; ln > top && r.topRow(ln) <= start {
			top = ln
		}
	}
	for top > 0 && r.topRow(top) > start { // a decoration row before its line's top: the line before
		top--
	}
	return top, max(start-r.topRow(top), 0)
}

// lineOfRow is the source line row ri belongs to: the first line with a cell on it, its Anchor, or
// the nearest such row before it.
func (r *renderedLayout) lineOfRow(ri int) int {
	for i := min(ri, len(r.rows)-1); i >= 0; i-- {
		row := r.rows[i]
		for _, c := range row.Cells {
			if c.Line >= 0 {
				return c.Line
			}
		}
		if row.Anchor >= 0 {
			return row.Anchor
		}
	}
	return 0
}

// sourcelessRun is the run of lines around ln in ln's block with no row of theirs: a fold.
func (r *renderedLayout) sourcelessRun(ln int) (from, to int) {
	b := r.blocks[r.blockOf[ln]]
	from, to = ln, ln+1
	for from > b.From && len(r.lineRows[from-1]) == 0 {
		from--
	}
	for to < b.To && len(r.lineRows[to]) == 0 {
		to++
	}
	return from, to
}

// caret is (ln, col)'s display row and x: the cell showing the cluster, on whichever of ln's rows
// shows it; concealed, the cell of the next shown cluster of ln, the one with the smallest column
// after col on any of ln's rows (not the first in the rows' order: a table cell's second line comes
// after the next cell's first); past ln's last shown cluster, the cell after it; a line showing none
// (an Anchor row), its first row's start; a folded line, its next row's start.
func (r *renderedLayout) caret(ln, col int) (row, x int) {
	if ln < 0 || ln >= len(r.lineRows) || len(r.lineRows[ln]) == 0 {
		return r.firstRow(ln), 0
	}
	rs := r.lineRows[ln]
	next, nextCol := -1, 0  // the next shown cluster at or after col: its row
	last, lastCol := -1, -1 // ln's last shown cluster: its row
	nextX, lastEnd := 0, 0
	for _, ri := range rs {
		at := 0
		for _, c := range r.rows[ri].Cells {
			if c.Line == ln {
				if c.Col >= col && (next < 0 || c.Col < nextCol) {
					next, nextCol, nextX = ri, c.Col, at
				}
				if c.Col > lastCol {
					last, lastCol, lastEnd = ri, c.Col, at+c.W
				}
			}
			at += c.W
		}
	}
	switch {
	case next >= 0:
		return next, nextX
	case last >= 0:
		return last, lastEnd
	}
	return rs[0], 0
}

// hit is the source under cell x of display row ri: the cluster whose cells hold x; a decoration
// takes the nearest source cell on its row, left first; a row of none, its Anchor's line start or
// the nearest line before it.
func (r *renderedLayout) hit(ri, x int) (ln, col int) {
	if len(r.rows) == 0 {
		return 0, 0
	}
	ri = max(0, min(ri, len(r.rows)-1))
	cells := r.rows[ri].Cells
	// each cell's span [from, to] in cells; x past the row's end is held to its last cell
	best, bestD := -1, 0
	at := 0
	for i, c := range cells {
		from, to := at, at+max(c.W, 1)-1
		at += c.W
		d := 0
		switch {
		case x < from:
			d = from - x
		case x > to && i < len(cells)-1:
			d = x - to
		}
		if c.Line < 0 {
			continue
		}
		// the nearest source cell by measured distance; a true tie takes the left one (the first)
		if best < 0 || d < bestD {
			best, bestD = i, d
		}
	}
	if best >= 0 {
		return cells[best].Line, cells[best].Col
	}
	if a := r.rows[ri].Anchor; a >= 0 {
		return a, 0
	}
	if ri < len(r.rowBlock) { // a row of decorations only: its block's line before it, else its first line
		bi := r.rowBlock[ri]
		for i := ri - 1; i >= 0 && r.rowBlock[i] == bi; i-- {
			if !r.rows[i].ShowsNoSource() || r.rows[i].Anchor >= 0 {
				return r.lineOfRow(i), 0
			}
		}
		return r.blocks[bi].From, 0
	}
	return r.lineOfRow(ri), 0
}

var _ EditorFoldLayout = (*cellLayout)(nil)

// FoldOf is the fold holding line: in the Editor's own Rendered view, while no text is typed, the
// run of its block's lines with no row of theirs (hidden metadata, a picture's source), which motions
// step over as one and an operator takes whole. A line with a row (prose, a heading, a table's row,
// an empty line by its Anchor) is never a fold.
func (l *cellLayout) FoldOf(line int) (from, to int, hidden, ok bool) {
	if l.typing() {
		return 0, 0, false, false
	}
	r := l.ed.rowMap().r
	if r == nil || line < 0 || line >= len(r.lineRows) || len(r.lineRows[line]) > 0 {
		return 0, 0, false, false
	}
	from, to = r.sourcelessRun(line)
	return from, to, !r.picture(line, from, to), true // a picture's whole fence is a fold that shows
}

// picture reports whether line's run of lines with no row, [from, to), is its whole block, drawn with
// rows none of which is a line's: a picture in place of its fence.
func (r *renderedLayout) picture(line, from, to int) bool {
	if line < 0 || line >= len(r.blockOf) || r.blockOf[line] >= len(r.blockRow) {
		return false
	}
	bi := r.blockOf[line]
	return r.blocks[bi].From == from && r.blocks[bi].To == to && r.blockRow[bi] >= 0
}
