package widget

import (
	"encoding/binary"
	"hash/fnv"
	"image/color"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// TABLE — a tui DataTable drawn in pixels: the same TableCore (its rows, cell cursor, selection,
// sort and paging), laid out and painted by a pixel TableLayout. The keys and the core are the
// DataTable's; only the view differs.
//
//	+-----+--------------------------------------------------+
//	|     | NAME ▲¹         N ▼²     CITY                    |  header band: one shaped line, on a tint
//	+=====+==================================================+
//	|   1 | alice           9        Seoul                   |  one row band: its cells' line, and
//	|   2 | [bob]         100        Busan                   |  its number's in the gutter
//	|   3 | carol          41        Daegu                   |  the cursor's cell filled in its style
//	+-----+--------------------------------------------------+
//
// Each row band's cells are shaped together into one TextLayout (TextShaper.LayoutRuns), drawn by
// one DrawText, and shaped again only when the band's text, widths or look change. With row
// numbers (TableCore.RowNumberDigits) the band's number is a second layout, in the header's font
// and colour, in a gutter as wide as the last row's number. The fills are the page, the header's
// tint and rule, the gutter's tint, one per run of selected cells (InSelection decides each cell;
// a run the row above has too is the same fill, grown), and the cursor's cell.

// BindTable lays a DataTable out in pixels and draws it natively, the same widget to the host.
// A native style calls it once, before the first paint.
func BindTable(t *tuiwidget.DataTable) {
	if t == nil {
		return
	}
	t.BindLayout(newTableLayout(t))
}

// tableBand is a row band's text, shaped: the runs it was shaped from, and the key of what they
// were made from.
type tableBand struct {
	key  uint64
	runs []gui.TextRun
	text *gui.TextLayout
}

// tableColumn is a field shown: where its text goes, x to x+w.
type tableColumn struct {
	col  int
	x, w float32
}

type tableLayout struct {
	t      *tuiwidget.DataTable
	shaper *gui.TextShaper
	pal    gui.Palette // the window's palette, from the canvas the look was last adopted from

	look      uint64 // a hash of the colours, text size and font generation the bands were shaped in
	body      gui.Font
	head      gui.Font
	fg, bg    color.NRGBA
	muted     color.NRGBA
	tint      color.NRGBA // the header's ground
	rule      color.NRGBA // under the header
	cursor    color.NRGBA // the cursor's cell while the table has the keys, when its style gives no ground
	blurred   color.NRGBA // and while it has not
	selection color.NRGBA

	size      gui.Size // the view's, at the last paint
	rowH, pad float32
	top, left int // first row shown, first field shown

	header *tableBand
	bands  map[int]*tableBand // by display row: the last paint's
	nums   map[int]*tableBand // the row numbers, by display row: the last paint's
	empty  *tableBand
	plus   *tableBand // the edge's "+": over the edge field, and in the edge row's gutter

	// shownEdit is the open field as the last paint revealed it: a new edit, text or caret, or a
	// new size, reveals it again; the wheel's scroll stays while none changes
	shownEdit editMark
}

// editMark is an open field's edit, text and caret.
type editMark struct {
	e       *tuiwidget.TableEdit
	text    string
	ln, col int
}

var (
	_ tuiwidget.TableLayout        = (*tableLayout)(nil)
	_ tuiwidget.TableLayoutPointer = (*tableLayout)(nil)
	_ tuiwidget.TableLayoutHitter  = (*tableLayout)(nil)
	_ tuiwidget.TableLayoutFields  = (*tableLayout)(nil)
	_ gui.View                     = (*tableLayout)(nil)
)

// tableTextPx is the text size before the first paint, which gives the window's.
const tableTextPx = 14

func newTableLayout(t *tuiwidget.DataTable) *tableLayout {
	l := &tableLayout{t: t}
	l.sizeText(tableTextPx)
	return l
}

func (l *tableLayout) core() *tuiwidget.TableCore { return l.t.Core() }

// sizeText sizes the rows and the padding by the text's size.
func (l *tableLayout) sizeText(px float32) {
	l.body = gui.Font{Size: px}
	l.head = gui.Font{Size: px, Bold: true}
	l.rowH = float32(math.Ceil(float64(px * 1.7)))
	l.pad = float32(math.Round(float64(px * 0.6)))
}

// bodyRows is how many rows the view shows whole under its header; at least one.
func (l *tableLayout) bodyRows() int { return max(int((l.size.H-l.rowH)/l.rowH), 1) }

// Reveal implements tuiwidget.TableLayout: scroll so the cell is in sight. Before the first paint
// the view has no size; the paint reveals the cursor then.
func (l *tableLayout) Reveal(row, col int) {
	if l.size.W <= 0 || l.size.H <= 0 {
		return
	}
	l.revealDrawn(l.core().DrawnOf(row)) // the row as drawn: after o's new row, one further down
	l.revealField(col)
}

// revealDrawn scrolls so drawn row v shows.
func (l *tableLayout) revealDrawn(v int) {
	l.top = min(l.top, max(l.core().DrawnRows()-l.bodyRows(), 0)) // rows gone (a filter, a new result) leave no blank below the last
	l.top = max(min(max(l.top, v-l.bodyRows()+1), v), 0)
}

// revealField scrolls sideways so field col shows whole, the edge field after the last among them.
func (l *tableLayout) revealField(col int) {
	if col < l.left {
		l.left = col
	}
	for l.left < col && !l.fits(l.left, col) {
		l.left++
	}
}

// revealEdit scrolls the open field into sight when it is new or its text or caret moved: its
// first row, its caret's line under it as far as the view allows, and its field.
func (l *tableLayout) revealEdit() {
	core := l.core()
	e := core.Editing()
	if e == nil {
		l.shownEdit = editMark{}
		return
	}
	ln, col := e.Field.Caret()
	mark := editMark{e: e, text: e.Field.Value(), ln: ln, col: col}
	if mark == l.shownEdit {
		return
	}
	l.shownEdit = mark
	v := core.DrawnOf(e.Pos.Row)
	if e.Inserting() {
		v = e.Pos.Row
	}
	l.revealDrawn(v + max(ln, 0))
	l.top = min(l.top, v) // the field is drawn from its first row: that row stays shown
	l.revealField(e.Pos.Col)
}

// fits reports whether fields from..to fit the view's width when from is the first shown.
func (l *tableLayout) fits(from, to int) bool {
	w := l.gutter()
	for col := from; col <= to; col++ {
		w += l.fieldWidth(col) + 2*l.pad
	}
	return w <= l.size.W
}

// fieldWidth is field col's text width as it is drawn: the edge field's for the one after the last.
func (l *tableLayout) fieldWidth(col int) float32 {
	if col == len(l.core().Fields()) {
		return l.edgeWidth()
	}
	return float32(l.core().Width(col))
}

// edgeWidth is the edge field's text width: a square cell's room for a first value.
func (l *tableLayout) edgeWidth() float32 { return l.rowH }

// PageRows implements tuiwidget.TableLayout.
func (l *tableLayout) PageRows() int { return max(l.bodyRows()-1, 1) }

// Measure implements tuiwidget.TableLayout: the width of s in pixels, in the header's bold, the
// widest it is drawn in.
func (l *tableLayout) Measure(s string) int {
	s = tuiwidget.TableCellLine(s)
	if l.shaper == nil {
		return int(math.Ceil(float64(float32(len([]rune(s))) * l.body.Size * 0.6)))
	}
	m := l.shaper.Measure(s, l.head)
	return int(math.Ceil(float64(m.X[len(m.X)-1])))
}

// Changed implements tuiwidget.TableLayout.
func (l *tableLayout) Changed() { l.t.MarkDirty() }

// CellAt implements tuiwidget.TableLayoutPointer: the header row is TablePos{}, a row number is
// no cell. DataTable asks HitAt.
func (l *tableLayout) CellAt(x, y float32) (tuiwidget.TablePos, bool, bool) {
	if y < l.rowH {
		return tuiwidget.TablePos{}, true, true
	}
	row := l.top + int((y-l.rowH)/l.rowH)
	if row < l.core().DrawnRows() {
		kind, r := l.core().DrawnRow(row)
		if kind == tuiwidget.TableRowPending {
			return tuiwidget.TablePos{}, false, false
		}
		row = r
	}
	if col, ok := l.cellFieldAt(x); ok {
		return tuiwidget.TablePos{Row: row, Col: col}, false, true
	}
	return tuiwidget.TablePos{}, false, false
}

// HitAt implements tuiwidget.TableLayoutHitter: a field's title over the header row (the gutter's
// corner is nothing), a row's number over the gutter, a cell over a row band's field, and nothing
// past the last field, below the last row or above and left of the view.
func (l *tableLayout) HitAt(x, y float32) (tuiwidget.TablePos, tuiwidget.TableHit) {
	if x < 0 || y < 0 || x >= l.size.W || y >= l.size.H {
		return tuiwidget.TablePos{}, tuiwidget.TableHitNone
	}
	if y < l.rowH {
		if col, ok := l.fieldAt(x); ok {
			return tuiwidget.TablePos{Col: col}, tuiwidget.TableHitHeader
		}
		return tuiwidget.TablePos{}, tuiwidget.TableHitNone
	}
	v := l.top + int((y-l.rowH)/l.rowH)
	if v >= l.core().DrawnRows() {
		return tuiwidget.TablePos{}, tuiwidget.TableHitNone
	}
	kind, row := l.core().DrawnRow(v)
	switch {
	case kind == tuiwidget.TableRowPending:
		return tuiwidget.TablePos{}, tuiwidget.TableHitNone
	case x < l.gutter():
		if kind == tuiwidget.TableRowEdge {
			return tuiwidget.TablePos{}, tuiwidget.TableHitNone
		}
		return tuiwidget.TablePos{Row: row}, tuiwidget.TableHitGutter
	}
	if col, ok := l.cellFieldAt(x); ok {
		return tuiwidget.TablePos{Row: row, Col: col}, tuiwidget.TableHitCell
	}
	return tuiwidget.TablePos{}, tuiwidget.TableHitNone
}

// cellFieldAt is the field at x over a row: a field shown, or the edge field after the last.
func (l *tableLayout) cellFieldAt(x float32) (int, bool) {
	if col, ok := l.fieldAt(x); ok {
		return col, true
	}
	if e, ok := l.edgeColumn(l.columns(l.size.W)); ok && x >= e.x-l.pad && x < e.x+e.w+l.pad {
		return e.col, true
	}
	return 0, false
}

// edgeColumn is the edge field after the last (TableAppendEdges), when it is shown: past the last
// field, within the width.
func (l *tableLayout) edgeColumn(cols []tableColumn) (tableColumn, bool) {
	core := l.core()
	n := len(core.Fields())
	if !core.EdgeField() {
		return tableColumn{}, false
	}
	x := l.gutter() + l.pad
	switch {
	case len(cols) > 0 && cols[len(cols)-1].col != n-1:
		return tableColumn{}, false // the last field is past the right edge
	case len(cols) > 0:
		last := cols[len(cols)-1]
		x = last.x + last.w + 2*l.pad
	case l.left < n:
		return tableColumn{}, false
	}
	w := l.edgeWidth()
	if x+w > l.size.W {
		return tableColumn{}, false
	}
	return tableColumn{col: n, x: x, w: w}, true
}

// fieldAt is the field shown at x: its text and the padding either side of it, among the fields
// painted in the view's width. The gutter and past the last field shown are none.
func (l *tableLayout) fieldAt(x float32) (int, bool) {
	if x < l.gutter() {
		return 0, false
	}
	for _, c := range l.columns(l.size.W) {
		if x < c.x+c.w+l.pad {
			return c.col, true
		}
	}
	return 0, false
}

// gutter is the row numbers' width, their padding included: the last row's number of digits in
// the header's font, so it widens as rows arrive (9 rows to 10). 0 while the numbers are off.
func (l *tableLayout) gutter() float32 {
	d := l.core().RowNumberDigits()
	if d == 0 {
		return 0
	}
	return float32(l.Measure(strings.Repeat("0", d))) + 2*l.pad
}

// ScrollRows implements tuiwidget.TableLayoutPointer.
func (l *tableLayout) ScrollRows(n int) {
	l.top = min(max(l.top+n, 0), max(l.core().DrawnRows()-l.bodyRows(), 0))
}

// ScrollFields implements tuiwidget.TableLayoutFields: the first field shown moves by n, clamped
// so the last field (or the edge field) still shows.
func (l *tableLayout) ScrollFields(n int) {
	last := len(l.core().Fields()) - 1
	if l.core().EdgeField() {
		last++
	}
	l.left = min(max(l.left+n, 0), max(last, 0))
	l.t.MarkDirty()
}

// columns are the fields shown from the first, after the gutter, up to the width.
func (l *tableLayout) columns(width float32) []tableColumn {
	var out []tableColumn
	x := l.gutter()
	for col := l.left; col < len(l.core().Fields()) && x < width; col++ {
		w := float32(l.core().Width(col))
		out = append(out, tableColumn{col: col, x: x + l.pad, w: w})
		x += w + 2*l.pad
	}
	return out
}

// Paint draws the header, the rows from the first shown and their numbers, the selection and the
// cursor's cell: the rows the core draws (TableCore.DrawnRow), so o's new row, the edge row and the
// edge field show as DataTable shows them, and the open field over its cell.
func (l *tableLayout) Paint(c gui.Canvas) {
	l.shaper = c.Text()
	l.adoptLook(c)
	core := l.core()
	if size := c.Size(); size != l.size {
		l.size = size // a new size keeps the cursor in sight; the same size keeps the wheel's scroll
		l.Reveal(core.Cursor().Row, core.Cursor().Col)
		l.shownEdit = editMark{} // and the open field, revealed again at the new size, over the cursor
	}
	l.revealEdit()
	drawn := core.DrawnRows()
	l.top = min(l.top, max(drawn-1, 0))
	size := l.size
	cols := l.columns(size.W)
	edge, edgeShown := l.edgeColumn(cols)
	c.FillRect(gui.Rect{W: size.W, H: size.H}, gui.Solid(l.bg))
	c.FillRect(gui.Rect{W: size.W, H: l.rowH}, gui.Solid(l.tint))
	c.FillRect(gui.Rect{Y: l.rowH - 1, W: size.W, H: 1}, gui.Solid(l.rule))
	l.header = l.shapeHeader(l.header, cols)
	l.drawBand(c, l.header, 0, l.muted)
	if edgeShown || core.EdgeRow() {
		l.plus = l.shapeRuns(l.plus, []string{"+"}, []tableColumn{{w: l.rowH}}, []style.Align{style.AlignCenter}, l.head)
	}
	if edgeShown {
		l.drawBandAt(c, l.plus, edge.x-l.pad+(edge.w+2*l.pad-l.rowH)/2, 0, l.muted)
	}
	if drawn == 0 {
		l.empty = l.shapeRuns(l.empty, []string{l.t.EmptyText()}, []tableColumn{{x: l.pad, w: size.W - 2*l.pad}}, nil, l.body)
		l.drawBand(c, l.empty, l.rowH, l.muted)
		l.bands, l.nums = nil, nil
		return
	}
	last := min(l.top+l.bodyRows()+1, drawn) // drawn rows, and the one cut by the bottom edge
	gutter := l.gutter()
	if gutter > 0 {
		c.FillRect(gui.Rect{Y: l.rowH, W: gutter, H: l.rowH * float32(last-l.top)}, gui.Solid(l.tint))
	}
	l.paintSelection(c, cols, last)
	aligns := make([]style.Align, len(cols))
	for i, col := range cols {
		aligns[i] = core.Fields()[col.col].Align
	}
	bands := make(map[int]*tableBand, last-l.top)
	var nums map[int]*tableBand
	if gutter > 0 {
		nums = make(map[int]*tableBand, last-l.top)
	}
	// a number ends a pad before the gutter's edge, and may take the pad before it: a digit wider
	// than the 0 it is measured by is not cut
	numCol := []tableColumn{{w: gutter - l.pad}}
	numAlign := []style.Align{style.AlignRight}
	texts := make([]string, len(cols))
	cur := core.Cursor()
	for v := l.top; v < last; v++ {
		y := l.rowH * float32(v-l.top+1)
		kind, row := core.DrawnRow(v)
		switch kind {
		case tuiwidget.TableRowPending:
			continue // the open field covers it
		case tuiwidget.TableRowEdge:
			if gutter > 0 {
				l.drawBandAt(c, l.plus, gutter-l.pad-l.rowH, y, l.muted)
			}
			if cur.Row == row {
				l.paintCursorCell(c, cols, edge, edgeShown, cur, last, nil)
			}
			continue
		}
		for i, col := range cols {
			texts[i] = tuiwidget.TableCellLine(core.Cell(row, col.col))
		}
		bands[row] = l.shapeRuns(l.bands[row], texts, cols, aligns, l.body)
		l.drawBand(c, bands[row], y, l.fg)
		if row == cur.Row {
			l.paintCursorCell(c, cols, edge, edgeShown, cur, last, bands[row])
		}
		if nums != nil {
			nums[row] = l.shapeRuns(l.nums[row], []string{strconv.Itoa(row + 1)}, numCol, numAlign, l.head)
			l.drawBand(c, nums[row], y, l.muted)
		}
	}
	l.bands, l.nums = bands, nums
	l.paintField(c, cols, edge, edgeShown, last)
}

// paintCursorCell fills the cursor's cell, a field's or the edge field's, and repaints the cell's
// text from the row's band in the cursor's colour: the band is shaped once, and the ordinary
// foreground would vanish on a light cursor fill.
func (l *tableLayout) paintCursorCell(c gui.Canvas, cols []tableColumn, edge tableColumn, edgeShown bool,
	cur tuiwidget.TablePos, last int, band *tableBand) {
	fill := l.cursorFill()
	if cur.Col == len(l.core().Fields()) {
		if v := l.core().DrawnOf(cur.Row); edgeShown && v >= l.top && v < last {
			c.FillRect(gui.Rect{X: edge.x - l.pad, Y: l.rowH * float32(v-l.top+1), W: edge.w + 2*l.pad, H: l.rowH}, gui.Solid(fill))
		}
		return
	}
	from, to := cur, cur
	if l.core().RowCursor() { // the row cursor: every field of the cursor's row
		from.Col, to.Col = 0, len(l.core().Fields())-1
	}
	r, ok := l.cellsRect(cols, from, to, last)
	if !ok {
		return
	}
	c.FillRect(r, gui.Solid(fill))
	if band != nil && band.text != nil {
		cell := c.Sub(r)
		cell.DrawText(band.text, gui.Pt(-r.X, (l.rowH-band.text.Height)/2), gui.Solid(l.cursorText(fill)))
	}
}

// fieldMinChars is the open field's least width, in digits: a narrow field still shows a value
// being typed, as the terminal's does at 12 cells.
const fieldMinChars = 12

// paintField draws the open field over its cell, as wide as its field (at least fieldMinChars) and
// as tall as its value's lines, growing downward, with its caret; scrolled sideways so the caret
// shows.
func (l *tableLayout) paintField(c gui.Canvas, cols []tableColumn, edge tableColumn, edgeShown bool, last int) {
	core := l.core()
	e := core.Editing()
	if e == nil {
		return
	}
	v := core.DrawnOf(e.Pos.Row)
	if e.Inserting() {
		v = e.Pos.Row
	}
	if v < l.top || v >= last {
		return
	}
	var col tableColumn
	found := false
	for _, cc := range cols {
		if cc.col == e.Pos.Col {
			col, found = cc, true
		}
	}
	if !found && edgeShown && edge.col == e.Pos.Col {
		col, found = edge, true
	}
	if !found {
		return
	}
	w := max(col.w, float32(l.Measure(strings.Repeat("0", fieldMinChars))))
	w = max(min(w, l.size.W-col.x-l.pad), 1)
	y := l.rowH * float32(v-l.top+1)
	lines := strings.Split(e.Field.Value(), "\n")
	ln, at := e.Field.Caret()
	ln = min(max(ln, 0), len(lines)-1)
	h := max(min(len(lines), int((l.size.H-y)/l.rowH)), 1)
	top := max(ln-h+1, 0)
	caretX := float32(0)
	if m := l.shaper.Measure(lines[ln], l.body); len(m.X) > 0 {
		caretX = m.X[min(max(at, 0), len(m.X)-1)]
	}
	caretW := max(float32(math.Round(float64(l.body.Size/10))), 1)
	off := max(caretX+caretW-w, 0) // the caret kept in sight
	fill := l.cursorFill()
	text := l.cursorText(fill)
	c.FillRect(gui.Rect{X: col.x - l.pad, Y: y, W: w + 2*l.pad, H: l.rowH * float32(h)}, gui.Solid(fill))
	field := c.Sub(gui.Rect{X: col.x, Y: y, W: w, H: l.rowH * float32(h)})
	for i := range min(h, len(lines)-top) {
		t := l.shaper.LayoutRuns([]gui.TextRun{{Text: lines[top+i]}}, l.body)
		field.DrawText(t, gui.Pt(-off, l.rowH*float32(i)+(l.rowH-t.Height)/2), gui.Solid(text))
	}
	field.FillRect(gui.Rect{X: caretX - off, Y: l.rowH*float32(ln-top) + l.rowH*0.2, W: caretW, H: l.rowH * 0.6}, gui.Solid(text))
}

// drawBandAt draws a shaped band with its left edge at x, centred in the row band at y.
func (l *tableLayout) drawBandAt(c gui.Canvas, b *tableBand, x, y float32, col color.NRGBA) {
	if b == nil || b.text == nil || len(b.runs) == 0 {
		return
	}
	c.DrawText(b.text, gui.Pt(x, y+(l.rowH-b.text.Height)/2), gui.Solid(col))
}

// paintSelection fills the selected cells shown, each as InSelection decides: a Cells selection
// in reading order, whole rows for Rows, the rectangle for a Block. A row's adjacent selected
// fields are one run, and a run the row above has too is the same fill, grown, so a selection is
// a fill or three, never one per cell.
func (l *tableLayout) paintSelection(c gui.Canvas, cols []tableColumn, last int) {
	core := l.core()
	if _, _, ok := core.Selection(); !ok {
		return
	}
	var open, next []gui.Rect // the runs the row above ended with; this row's
	for v := l.top; v < last; v++ {
		y := l.rowH * float32(v-l.top+1)
		next = next[:0]
		kind, row := core.DrawnRow(v)
		for _, col := range cols {
			if kind != tuiwidget.TableRowData {
				break // o's new row and the edge row are in no selection
			}
			if !core.InSelection(row, col.col) {
				continue
			}
			x0, x1 := col.x-l.pad, col.x+col.w+l.pad
			if n := len(next); n > 0 && math.Abs(float64(next[n-1].X+next[n-1].W-x0)) < 0.01 {
				next[n-1].W = x1 - next[n-1].X
				continue
			}
			next = append(next, gui.Rect{X: x0, Y: y, W: x1 - x0, H: l.rowH})
		}
		for i, r := range next {
			if k := slices.IndexFunc(open, func(o gui.Rect) bool { return o.X == r.X && o.W == r.W }); k >= 0 {
				next[i].Y, next[i].H = open[k].Y, open[k].H+l.rowH
				open = slices.Delete(open, k, k+1)
			}
		}
		for _, r := range open {
			c.FillRect(r, gui.Solid(l.selection))
		}
		open, next = next, open
	}
	for _, r := range open {
		c.FillRect(r, gui.Solid(l.selection))
	}
}

// cursorFill is the cursor's ground in the DataTable's CursorStyle, which follows the focus
// (Cursor while the table has the keys, CursorBlurred while it has not): the style's background,
// or its foreground when it reverses; with neither, the accent's band, or a faint one when the
// style is faint, as the default CursorBlurred is.
func (l *tableLayout) cursorFill() color.NRGBA {
	st := l.t.CursorStyle()
	ground, set := st.GetBackground()
	if rev, _ := st.GetReverse(); rev {
		ground, set = st.GetForeground()
	}
	if set {
		if col, ok := colorOf(ground, l.t.Theme(), isDark(l.bg), l.pal); ok {
			return col
		}
	}
	if faint, _ := st.GetFaint(); faint {
		return l.blurred
	}
	return l.cursor
}

// cursorText reads the cursor style's foreground, falling back to the row's text, then makes
// either one legible on the fill actually painted. A blurred cursor may dim its fill, not its text.
func (l *tableLayout) cursorText(fill color.NRGBA) color.NRGBA {
	st := l.t.CursorStyle()
	fg, ok := st.GetForeground()
	if rev, _ := st.GetReverse(); rev {
		fg, ok = st.GetBackground()
	}
	text := l.fg
	if ok {
		if col, resolved := colorOf(fg, l.t.Theme(), isDark(l.bg), l.pal); resolved {
			text = col
		}
	}
	legible := style.Legible(style.RGB(text.R, text.G, text.B),
		style.RGB(fill.R, fill.G, fill.B), 4.5)
	if r, g, b, ok := legible.RGBValues(); ok {
		return color.NRGBA{R: r, G: g, B: b, A: 0xff}
	}
	return text
}

// cellsRect is the rectangle of the cells from..to that are shown, rows before last.
func (l *tableLayout) cellsRect(cols []tableColumn, from, to tuiwidget.TablePos, last int) (gui.Rect, bool) {
	top, bottom := max(l.core().DrawnOf(from.Row), l.top), min(l.core().DrawnOf(to.Row), last-1)
	var x0, x1 float32
	shown := false
	for _, c := range cols {
		if c.col >= from.Col && c.col <= to.Col {
			if !shown {
				x0, shown = c.x-l.pad, true
			}
			x1 = c.x + c.w + l.pad
		}
	}
	if !shown || bottom < top {
		return gui.Rect{}, false
	}
	return gui.Rect{X: x0, Y: l.rowH * float32(top-l.top+1), W: x1 - x0, H: l.rowH * float32(bottom-top+1)}, true
}

// drawBand draws a shaped band in the row band at y, centred in it.
func (l *tableLayout) drawBand(c gui.Canvas, b *tableBand, y float32, col color.NRGBA) {
	if b == nil || b.text == nil || len(b.runs) == 0 {
		return
	}
	c.DrawText(b.text, gui.Pt(0, y+(l.rowH-b.text.Height)/2), gui.Solid(col))
}

// shapeHeader is the header band: each title, and each sort key's mark in the gap after its
// field, as the terminal's DataTable marks them: ▲ or ▼ on every key's field, the first key a field
// has. With more than one key each mark carries the key's place, small (▲¹ ▼²). The first key's
// title says "(loaded rows)" while rows remain to load, when that fits.
func (l *tableLayout) shapeHeader(old *tableBand, cols []tableColumn) *tableBand {
	fields := l.core().Fields()
	keys, partial := l.core().SortedKeys()
	texts := make([]string, 0, 2*len(cols))
	for _, c := range cols {
		texts = append(texts, fields[c.col].Title)
	}
	type mark struct {
		text   string
		at, of int // the column shown; the key's place
	}
	var marks []mark
	noted := -1
	for i, c := range cols {
		for k, key := range keys {
			if key.Col != c.col {
				continue
			}
			m := mark{text: map[bool]string{false: "▲", true: "▼"}[key.Desc], at: i, of: k}
			if len(keys) > 1 {
				m.text += superscript(k + 1)
			}
			marks = append(marks, m)
			if k == 0 && partial {
				texts[i] += " (loaded rows)"
				noted = i
			}
			break
		}
	}
	slices.SortFunc(marks, func(a, b mark) int { return a.of - b.of }) // in key order
	for _, m := range marks {
		texts = append(texts, strconv.Itoa(m.at)+":"+m.text) // the column too: swapped places mark other fields
	}
	key := l.key(texts, cols, nil)
	if old != nil && old.key == key {
		return old
	}
	runs := make([]gui.TextRun, 0, len(cols)+len(marks))
	for i, c := range cols {
		title := texts[i]
		if i == noted && l.width(title, l.head) > c.w {
			title = fields[c.col].Title // no room for the note
		}
		if s := l.elide(title, c.w, l.head); s != "" {
			runs = append(runs, gui.TextRun{Text: s, X: c.x})
		}
	}
	for _, m := range marks {
		c := cols[m.at]
		runs = append(runs, gui.TextRun{Text: m.text, X: c.x + c.w + max(2*l.pad-l.width(m.text, l.head), 0)/2})
	}
	return &tableBand{key: key, runs: runs, text: l.shaper.LayoutRuns(runs, l.head)}
}

// shapeRuns is a band of texts, one per column, each cut to its column and placed as its align
// says; old is kept while what it was shaped from is the same.
func (l *tableLayout) shapeRuns(old *tableBand, texts []string, cols []tableColumn, aligns []style.Align, f gui.Font) *tableBand {
	key := l.key(texts, cols, aligns)
	if old != nil && old.key == key {
		return old
	}
	runs := make([]gui.TextRun, 0, len(cols))
	for i, c := range cols {
		s := l.elide(tuiwidget.TableCellLine(texts[i]), c.w, f)
		if s == "" {
			continue
		}
		x := c.x
		if i < len(aligns) {
			switch aligns[i] {
			case style.AlignRight:
				x += c.w - l.width(s, f)
			case style.AlignCenter:
				x += (c.w - l.width(s, f)) / 2
			}
		}
		runs = append(runs, gui.TextRun{Text: s, X: x})
	}
	return &tableBand{key: key, runs: runs, text: l.shaper.LayoutRuns(runs, f)}
}

// key is a hash of what a band is shaped from: its texts, the columns they go in, their aligns,
// and the look.
func (l *tableLayout) key(texts []string, cols []tableColumn, aligns []style.Align) uint64 {
	h := fnv.New64a()
	_ = binary.Write(h, binary.LittleEndian, l.look)
	for _, s := range texts {
		_ = binary.Write(h, binary.LittleEndian, uint32(len(s)))
		_, _ = h.Write([]byte(s))
	}
	for _, c := range cols {
		_ = binary.Write(h, binary.LittleEndian, [2]float32{c.x, c.w})
	}
	for _, a := range aligns {
		_, _ = h.Write([]byte{byte(a)})
	}
	return h.Sum64()
}

// width is s's width in f.
func (l *tableLayout) width(s string, f gui.Font) float32 {
	m := l.shaper.Measure(s, f)
	return m.X[len(m.X)-1]
}

// elide is s cut to w with an ellipsis, whole when it fits; "" when not even the ellipsis does.
func (l *tableLayout) elide(s string, w float32, f gui.Font) string {
	if s == "" || w <= 0 {
		return ""
	}
	m := l.shaper.Measure(s, f)
	if m.X[len(m.X)-1] <= w {
		return s
	}
	ell := l.width("…", f)
	for k := len(m.Clusters) - 1; k > 0; k-- {
		if m.X[k]+ell <= w {
			return s[:m.Clusters[k]] + "…"
		}
	}
	if ell <= w {
		return "…"
	}
	return ""
}

// adoptLook takes the window's colours, text size and fonts: the rows' text and ground from the
// cells under the view, the accent and muted text from the tui theme. A change shapes every band
// again and measures the widths again.
func (l *tableLayout) adoptLook(c gui.Canvas) {
	l.pal = gui.PaletteOf(c)
	lk := readLook(c, l.t.Theme(), l.fg, l.bg)
	if lk.key == l.look {
		return
	}
	l.look = lk.key
	l.fg, l.bg, l.muted = lk.fg, lk.bg, lk.muted
	l.tint = mix(lk.fg, lk.bg, 0.06)
	l.rule = mix(lk.muted, lk.bg, 0.45)
	l.cursor = mix(lk.accent, lk.bg, 0.35)
	l.blurred = mix(lk.fg, lk.bg, 0.16) // grey: the selection's accent tint stays its own
	l.selection = mix(lk.accent, lk.bg, 0.18)
	l.sizeText(lk.px)
	l.header, l.bands, l.nums, l.empty = nil, nil, nil, nil
	l.core().Remeasure()
}

// superscript is n in superscript digits: a sort key's place beside its mark.
func superscript(n int) string {
	const digits = "⁰¹²³⁴⁵⁶⁷⁸⁹"
	var b strings.Builder
	for _, d := range strconv.Itoa(n) {
		b.WriteString(string([]rune(digits)[d-'0']))
	}
	return b.String()
}
