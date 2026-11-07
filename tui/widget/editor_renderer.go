package widget

import (
	"github.com/yongjohnlee80/golib/highlight"
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

// CellBlock is a block laid out: its rows, and the widest of them in cells.
type CellBlock struct {
	Rows  []CellRow
	Width int
}

// CellRow is one display row. Anchor is the source line it stands for when it shows none of that
// line's clusters (an empty line, a line whose markup is all concealed), -1 otherwise; a row of
// decorations only (a box's rule) has none. Unnumbered: no line number in the gutter.
type CellRow struct {
	Cells      []RowCell
	Anchor     int
	Unnumbered bool
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
}

// renderedLayout is the Editor's text through its renderer: the display rows in order, and each
// source line's rows (those holding a cell of it, or standing for it).
type renderedLayout struct {
	key      renderedKey
	blocks   []Block
	rows     []CellRow
	lineRows [][]int
	blockOf  []int // each line's block, into blocks
}

// renderedRows is the Editor's Rendered layout at width, laid again only when something it depends
// on changed (its key).
func (e *Editor) renderedRows(width int) *renderedLayout {
	l := &e.cells
	lines := e.core.buf.textLines
	key := renderedKey{gen: e.renderGen, buf: e.core.Buffer(), rev: e.core.Revision(), width: width, typing: -1, theme: e.cellTheme()}
	if l.typing() {
		key.typing = e.core.buf.ln
	}
	for _, r := range e.laid {
		if r != nil && r.key == key {
			return r
		}
	}
	r := &renderedLayout{key: key, lineRows: make([][]int, len(lines)), blockOf: make([]int, len(lines))}
	r.blocks = e.render.Blocks(lines, 0, len(lines))
	for bi, b := range r.blocks {
		typing := key.typing >= b.From && key.typing < b.To
		cb := e.render.LayOut(b, lines, width, e.width, typing, key.theme)
		for ln := b.From; ln < b.To && ln < len(lines); ln++ {
			r.blockOf[ln] = bi
		}
		for _, row := range cb.Rows {
			ri := len(r.rows)
			r.rows = append(r.rows, row)
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

// firstRow is line ln's first display row; a line with none (folded) takes the next line's that has
// one, or the last row.
func (r *renderedLayout) firstRow(ln int) int {
	for i := max(ln, 0); i < len(r.lineRows); i++ {
		if rs := r.lineRows[i]; len(rs) > 0 {
			return rs[0]
		}
	}
	return max(len(r.rows)-1, 0)
}

// atRow is the top line and the rows of it above the view that put display row start at the
// view's top.
func (r *renderedLayout) atRow(start int) (top, in int) {
	if len(r.rows) == 0 || start <= 0 {
		return 0, 0
	}
	start = min(start, len(r.rows)-1)
	top = r.lineOfRow(start)
	for top > 0 && r.firstRow(top) > start { // a decoration row before its line's first: the line before
		top--
	}
	return top, max(start-r.firstRow(top), 0)
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

// cellAt is the cell of (ln, col) on row ri and the x it starts at: the cell showing the cluster,
// else the first shown cluster of ln after col on the row; ok false when the row shows none of ln
// at or after col.
func (r *renderedLayout) cellAt(ri, ln, col int) (x int, ok bool) {
	at := 0
	for _, c := range r.rows[ri].Cells {
		if c.Line == ln && c.Col >= col {
			return at, true
		}
		at += c.W
	}
	return 0, false
}

// caret is (ln, col)'s display row and x: the row showing the cluster (or, concealed, the next
// shown one of ln), else past the line's last cell on its last row; an Anchor row's start; a
// folded line's next row's start.
func (r *renderedLayout) caret(ln, col int) (row, x int) {
	if ln < 0 || ln >= len(r.lineRows) || len(r.lineRows[ln]) == 0 {
		return r.firstRow(ln), 0
	}
	rs := r.lineRows[ln]
	for _, ri := range rs {
		if x, ok := r.cellAt(ri, ln, col); ok {
			return ri, x
		}
	}
	last := rs[len(rs)-1]
	end := 0
	for _, c := range r.rows[last].Cells {
		end += c.W
		if c.Line == ln {
			x = end // past ln's last cell
		}
	}
	return last, x
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
	return from, to, true, true
}
