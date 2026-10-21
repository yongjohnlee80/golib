package widget

import "strings"

// THE SELECTION AND THE POINTER — vim's visual modes over a grid (its keys are tablecore_keys.go's). The selection runs from
// an anchor to the cursor, and its kind says which cells between them it covers: v's cells in
// reading order, V's whole rows, Ctrl+V's rectangle. A column is a rectangle from the first row to
// the last; there is no fourth kind for it. A press, a drag and a release drive the same selection,
// as they drive EditorCore's.

// TableSelectKind is what a selection covers between the anchor and the cursor.
type TableSelectKind int

const (
	// TableSelectCells (v) is the anchor's cell to the cursor's in reading order, as vim's
	// charwise Visual: the first row from its field to the last field, the rows between whole,
	// the last row up to its field.
	TableSelectCells TableSelectKind = iota
	// TableSelectRows (V) is whole rows, the anchor's row to the cursor's.
	TableSelectRows
	// TableSelectBlock (Ctrl+V) is the rectangle with the anchor and the cursor at its corners.
	TableSelectBlock
)

// tableSelection is the selection, from anchor to the cursor, and the pointer's drag.
type tableSelection struct {
	anchor *TablePos
	kind   TableSelectKind // what the selection between anchor and the cursor covers
	// The pointer's drag: where it was pressed, and on what.
	dragging bool
	dragFrom TablePos
	dragHit  TableHit
}

// reset ends the selection and a drag: a new source's rows are not the ones selected.
func (s *tableSelection) reset() { *s = tableSelection{} }

// TableHit is where a pointer is over a table.
type TableHit int

const (
	TableHitNone   TableHit = iota // off the rows and the fields
	TableHitCell                   // a cell: a press puts the cursor there, a drag selects cells
	TableHitHeader                 // a field's title: a press selects the field, every loaded row
	TableHitGutter                 // a row's number: a press selects the row
)

// tableCountMax bounds a typed count: far past any table's rows, and no overflow however many
// digits are typed.
const tableCountMax = 1 << 24

// SetAnchor starts a selection of cells (TableSelectCells) at a cell, running to the cursor; nil
// ends the selection.
func (c *TableCore) SetAnchor(p *TablePos) {
	if p == nil {
		c.anchor, c.kind = nil, TableSelectCells
		c.repaint()
		return
	}
	c.Select(TableSelectCells, *p)
}

// Select starts a selection of kind at anchor, running to the cursor; a selection already made is
// replaced. An empty table (no rows, or a filter keeping none) has nothing to select.
func (c *TableCore) Select(kind TableSelectKind, anchor TablePos) {
	if c.Len() == 0 {
		return
	}
	a := c.clampPos(anchor)
	c.anchor, c.kind = &a, kind
	c.repaint()
}

// SelectionKind is the kind of the selection; TableSelectCells when there is none.
func (c *TableCore) SelectionKind() TableSelectKind {
	if c.anchor == nil {
		return TableSelectCells
	}
	return c.kind
}

// SelectAll selects every loaded row (TableSelectRows): the anchor on the last row and the cursor
// on the first, in its field. The cursor goes to the top rather than the end, so selecting asks a
// paged source for no rows.
func (c *TableCore) SelectAll() {
	if c.Len() == 0 {
		return
	}
	c.Select(TableSelectRows, TablePos{c.Len() - 1, c.cur.Col})
	c.SetCursor(TablePos{0, c.cur.Col})
}

// Selection is the corners of the selection, top-left and bottom-right inclusive, of the box the
// anchor and the cursor span; ok is false without one. Which cells inside it are selected is the
// kind's (InSelection): all of them for a block, whole rows for Rows, reading order for Cells.
func (c *TableCore) Selection() (from, to TablePos, ok bool) {
	if c.anchor == nil {
		return TablePos{}, TablePos{}, false
	}
	a, b := *c.anchor, c.cur
	return TablePos{min(a.Row, b.Row), min(a.Col, b.Col)}, TablePos{max(a.Row, b.Row), max(a.Col, b.Col)}, true
}

// InSelection reports whether the cell at display row r, field col is selected.
func (c *TableCore) InSelection(r, col int) bool {
	if c.anchor == nil {
		return false
	}
	switch c.kind {
	case TableSelectRows:
		from, to, _ := c.Selection()
		return r >= from.Row && r <= to.Row && col >= 0 && col < len(c.Fields())
	case TableSelectBlock:
		from, to, _ := c.Selection()
		return r >= from.Row && r <= to.Row && col >= from.Col && col <= to.Col
	}
	start, end := c.readingOrder()
	switch {
	case r < start.Row || r > end.Row:
		return false
	case r == start.Row && col < start.Col, r == end.Row && col > end.Col:
		return false
	}
	return col >= 0 && col < len(c.Fields())
}

// readingOrder is the anchor and the cursor, the earlier in reading order first.
func (c *TableCore) readingOrder() (start, end TablePos) {
	a, b := *c.anchor, c.cur
	if b.Row < a.Row || b.Row == a.Row && b.Col < a.Col {
		a, b = b, a
	}
	return a, b
}

// CopyText is the selection, or the cursor's cell, as quoted TSV (EncodeTableText), a record per
// row: a block's rectangle, whole rows for Rows, and for Cells each row's selected fields in
// reading order. An empty table copies no text; an empty cell copies "", a cell holding nothing.
func (c *TableCore) CopyText() string {
	s, _ := c.copyText(0)
	return s
}

// copyText is CopyText, given up as soon as it grows past limit bytes (0: no limit): ok false, and
// the text so far is dropped, so a selection far past the limit is not built whole to be refused.
func (c *TableCore) copyText(limit int) (text string, ok bool) {
	if c.Len() == 0 {
		return "", true
	}
	if c.anchor == nil {
		s := c.copyRecord([]string{c.Cell(c.cur.Row, c.cur.Col)})
		return s, limit <= 0 || len(s) <= limit
	}
	from, to, _ := c.Selection()
	last := len(c.Fields()) - 1
	var b strings.Builder
	for r := from.Row; r <= to.Row; r++ {
		if r > from.Row {
			b.WriteByte('\n')
		}
		lo, hi := from.Col, to.Col
		switch c.kind {
		case TableSelectRows:
			lo, hi = 0, last
		case TableSelectCells:
			start, end := c.readingOrder()
			lo, hi = 0, last
			if r == start.Row {
				lo = start.Col
			}
			if r == end.Row {
				hi = end.Col
			}
		}
		c.writeRow(&b, r, lo, hi)
		if limit > 0 && b.Len() > limit {
			return "", false
		}
	}
	return b.String(), true
}

// rowText is display row r whole, as a copy writes it: yy's, and a row cursor's y.
func (c *TableCore) rowText(r int) string {
	var b strings.Builder
	c.writeRow(&b, r, 0, len(c.Fields())-1)
	return b.String()
}

// writeRow writes display row r's fields lo..hi as one quoted TSV record (EncodeTableText's).
func (c *TableCore) writeRow(b *strings.Builder, r, lo, hi int) {
	cells := make([]string, 0, hi-lo+1)
	for col := lo; col <= hi; col++ {
		cells = append(cells, c.Cell(r, col))
	}
	b.WriteString(c.copyRecord(cells))
}

// Copy is y: CopyText into the register (whole rows for a V selection), then onto the clipboard
// (tablecore_clipboard.go). Past the copy limit (TableCopyLimit) it takes nothing, tells
// TableOnCopyRefused, and reports false; otherwise true, whether the clipboard took it or not.
func (c *TableCore) Copy() bool { return c.yank() }

// SetCopyLimit is TableCopyLimit and TableOnCopyRefused on a core already built: a copy longer
// than bytes (0: no limit) copies nothing and calls refused, if it is not nil, with the limit.
func (c *TableCore) SetCopyLimit(bytes int, refused func(limit int)) {
	c.copyLimit, c.onCopyRefused = max(bytes, 0), refused
}

// SetOnCopy changes the host's copy target after the table has been built.
func (c *TableCore) SetOnCopy(fn func(text string)) { c.onCopy = fn }

func (c *TableCore) refuseCopy() {
	if c.onCopyRefused != nil {
		c.onCopyRefused(c.copyLimit)
	}
}

// copyString puts s on the clipboard and gives it to TableOnCopy; setRegister has checked it.
func (c *TableCore) copyString(s string) bool {
	delivered := false
	if c.ctx != nil {
		delivered = c.ctx.CopyToClipboard(s)
	}
	if c.onCopy != nil {
		c.onCopy(s)
		return true
	}
	return delivered
}

// activateCursor activates the cursor's row (TableOnActivate), reporting whether there was one to
// activate. The activate command and a double-click both come here: a double-click is not a key, so it
// does not follow the command's keys.
func (c *TableCore) activateCursor() bool {
	if c.onActivate == nil || c.Len() == 0 {
		return false
	}
	c.onActivate(c.SourceRow(c.cur.Row))
	return true
}

// sortKey is the s key on field col: a sort by it alone, ascending, then descending, then none.
// A sort of several keys is replaced by one.
func (c *TableCore) sortKey(col int) {
	switch {
	case len(c.keys) != 1 || c.keys[0].Col != col:
		c.SortKeys([]TableSortKey{{Col: col}})
	case !c.keys[0].Desc:
		c.SortKeys([]TableSortKey{{Col: col, Desc: true}})
	default:
		c.SortKeys(nil)
	}
}

// toggleSelect is v, V or Ctrl+V: a selection of kind starts at the cursor; one of another kind
// becomes this kind, keeping its anchor; one of this kind ends.
func (c *TableCore) toggleSelect(kind TableSelectKind) {
	switch {
	case c.anchor == nil:
		c.Select(kind, c.cur)
	case c.kind == kind:
		c.SetAnchor(nil)
	default:
		c.kind = kind
		c.repaint()
	}
}

// moveFields moves the cursor n fields in reading order, onto the next row past the last field
// and the previous before the first, as vim's w and b cross lines.
func (c *TableCore) moveFields(n int) {
	fields := len(c.Fields())
	if fields == 0 || c.Len() == 0 {
		return
	}
	at := min(max(c.cur.Row*fields+c.cur.Col+n, 0), c.Len()*fields-1)
	c.SetCursor(TablePos{at / fields, at % fields})
}

// dropPending forgets a command not yet complete: a count, a g waiting, a y to double.
func (c *TableCore) dropPending() { c.tableKeyState.reset() }

// clampPos is p inside the table.
func (c *TableCore) clampPos(p TablePos) TablePos {
	p.Row = min(max(p.Row, 0), max(c.Len()-1, 0))
	p.Col = min(max(p.Col, 0), max(len(c.Fields())-1, 0))
	return p
}

// pairPress records a view's press on the cell at display position p with the App's count, and
// reports whether it completes a double-click: on the same SOURCE row and field, so a sort or the
// wheel moving another row under the pressed cell does not pair (pressPairer).
func (c *TableCore) pairPress(count int, p TablePos) bool {
	return c.presses.Pair(count, TablePos{Row: c.SourceRow(p.Row), Col: p.Col})
}

// PressAt is a press of the pointer at p, on what hit says. A press is a COMMAND BOUNDARY, not
// merely a cursor move: a count and a pending g are dropped, never completed against the pressed
// place, and a selection ends. Then:
//   - on a cell the cursor goes there, and a drag from it may select cells (DragTo); nothing is
//     selected until the pointer leaves the pressed cell;
//   - on a field's title (p.Col) that field is selected, every loaded row (a block), and a drag
//     across the titles widens it;
//   - on a row's number (p.Row) that row is selected (Rows), and a drag widens it by rows.
//
// It reports whether the press placed the cursor; a press on nothing (TableHitNone) only ends.
func (c *TableCore) PressAt(p TablePos, hit TableHit) bool {
	c.dropPending()
	c.dragging = false
	if c.anchor != nil {
		c.SetAnchor(nil)
	}
	switch hit {
	case TableHitCell:
		c.SetCursor(p)
		c.dragFrom = c.cur
	case TableHitGutter:
		c.SetCursor(TablePos{p.Row, c.cur.Col})
		c.Select(TableSelectRows, c.cur)
		c.dragFrom = c.cur
	case TableHitHeader:
		c.dragFrom = c.clampPos(TablePos{Col: p.Col})
		c.selectFields(c.dragFrom.Col)
	default:
		return false
	}
	c.dragging, c.dragHit = true, hit
	return true
}

// selectFields selects the fields from the press's to col over every loaded row: the anchor on the
// last row and the cursor on the first, so the cursor stays in sight near the titles and a paged
// source is asked for no rows.
func (c *TableCore) selectFields(col int) {
	if c.Len() == 0 {
		c.SetCursor(TablePos{0, col})
		return
	}
	c.Select(TableSelectBlock, TablePos{c.Len() - 1, c.dragFrom.Col})
	c.SetCursor(TablePos{0, col})
}

// Dragging reports whether a press started a drag that has not ended: the view keeps the pointer
// (Context.CapturePointer) while it lasts.
func (c *TableCore) Dragging() bool { return c.dragging }

// DragTo extends the drag to p, which is read for what the press was on: a cell's drag selects
// cells (TableSelectCells) once p leaves the pressed cell, a row number's takes p's row, a title's
// p's field.
func (c *TableCore) DragTo(p TablePos) {
	if !c.dragging {
		return
	}
	p = c.clampPos(p)
	switch c.dragHit {
	case TableHitCell:
		if c.anchor == nil {
			if p == c.dragFrom {
				return // not moved off the pressed cell yet: still a click
			}
			c.Select(TableSelectCells, c.dragFrom)
		}
		c.SetCursor(p)
	case TableHitGutter:
		c.SetCursor(TablePos{p.Row, c.cur.Col})
	case TableHitHeader:
		c.selectFields(p.Col)
	}
}

// EndDrag ends the drag; the selection, if any, stays for the keys to copy.
func (c *TableCore) EndDrag() { c.dragging = false }

// selectForMenu is a right press at p, on what hit says, before the menu opens. Inside the
// selection nothing changes: the menu's rows act on it. Outside, a cell moves the cursor there
// and ends the selection; a row's number selects that row; a field's title selects that field.
// None of them starts a drag or keeps the pointer.
func (c *TableCore) selectForMenu(p TablePos, hit TableHit) {
	if !c.menuSelects || c.Len() == 0 && hit != TableHitHeader {
		return
	}
	c.dropPending()
	switch hit {
	case TableHitCell:
		if !c.InSelection(p.Row, p.Col) {
			c.SetAnchor(nil)
			c.SetCursor(p)
		}
	case TableHitGutter:
		if !c.rowSelected(p.Row) {
			c.SetAnchor(nil)
			c.SetCursor(TablePos{p.Row, c.cur.Col})
			c.Select(TableSelectRows, c.cur)
		}
	case TableHitHeader:
		if !c.fieldSelected(p.Col) {
			c.SetAnchor(nil)
			c.dragFrom = c.clampPos(TablePos{Col: p.Col})
			c.selectFields(c.dragFrom.Col)
		}
	}
}

// rowSelected reports whether every field of display row r is selected.
func (c *TableCore) rowSelected(r int) bool {
	for col := range c.Fields() {
		if !c.InSelection(r, col) {
			return false
		}
	}
	return c.anchor != nil
}

// fieldSelected reports whether field col is selected on every loaded row.
func (c *TableCore) fieldSelected(col int) bool {
	from, to, ok := c.Selection()
	return ok && from.Row == 0 && to.Row == c.Len()-1 && c.InSelection(0, col) && c.InSelection(c.Len()-1, col)
}

// copyRecord is one row of a copy. An editable table's copies paste back (p, a PasteEvent), so
// they are quoted TSV (EncodeTableText's: an empty cell is "", a value with a tab, a newline or a
// quote is quoted). A read-only table's are its values joined by tabs, as they always were: what a
// reader copies out of a result, a JSON value as it is.
func (c *TableCore) copyRecord(cells []string) string {
	if c.copyFormat == TableCopyCSV || c.Editable() { // CSV is quoted, editable or not
		return encodeRecord(c.copyFormat.delimiter(), cells)
	}
	return strings.Join(cells, "\t")
}
