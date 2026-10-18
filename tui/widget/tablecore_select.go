package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
)

// THE SELECTION, THE KEYS AND THE POINTER — vim's visual modes over a grid. The selection runs from
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
		s := encodeRecord('\t', []string{c.Cell(c.cur.Row, c.cur.Col)}) // an empty cell is "", so it pastes as one
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

// writeRow writes display row r's fields lo..hi as one quoted TSV record (EncodeTableText's).
func (c *TableCore) writeRow(b *strings.Builder, r, lo, hi int) {
	cells := make([]string, 0, hi-lo+1)
	for col := lo; col <= hi; col++ {
		cells = append(cells, c.Cell(r, col))
	}
	b.WriteString(encodeRecord('\t', cells))
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

// HandleKey moves, selects, sorts, copies and activates:
//
//	[count] k j h l ↑ ↓ ← →   move count cells     [count] w b   next / previous field, across rows
//	PgUp PgDn                 a page               gg G          first / last row ([count]G, [count]gg: row count)
//	0 $ Home End              first / last field   v V Ctrl+V    start, switch or end a selection
//	y                         copy the selection (or the cursor's cell); yy the cursor's row
//	p P                       paste the register after / before (an editable source)
//	d x                       on a selection, cut it: d deletes a V selection's rows, x empties
//	Esc                       end a selection
//	s                         sort by this field (if sortable): up, down, off; one key, replacing a sort of several
//	Enter                     activate the cursor's row
//
// Each key is a command of scope tui.table (widget.TableUp …), so a key map can move it; the digits
// are the count. A g waits for its second g; any other key drops it and runs. Ctrl+V is the one Ctrl
// chord it takes by default, vim's block key: a read-only table has nothing to paste. Other Ctrl, Alt
// and Super chords are the application's.
func (c *TableCore) HandleKey(k tui.KeyEvent) bool {
	if c.editing != nil {
		return c.fieldKey(k)
	}
	if k.Kind == tui.KeyRelease {
		return false
	}
	if d, ok := countDigit(k); ok && (d != 0 || c.count > 0) {
		c.pendingG, c.yanked = false, false
		c.count = min(c.count*10+d, tableCountMax)
		return true
	}
	if c.pendingG && isKey(k, literalG...) {
		// g's grammar: the literal g after it is [count]gg, wherever a map moved the prefix
		row := max(c.count, 1) - 1
		c.dropPending()
		c.SetCursor(TablePos{row, c.cur.Col})
		return true
	}
	cmd, ok := c.ctx.KeyCommand(k, tableCommands...)
	if !ok {
		c.dropPending() // any other key drops a count or a g, and is the application's
		if c.keysFirst != nil && c.keysFirst(k) {
			return true
		}
		// on an editable table a printable key opens the cell's field with it typed; Space and : stay
		// the application's, as vim's leader and command line are
		if k.Text != "" && k.Text != " " && k.Text != ":" && k.Mods&^tui.ModShift == 0 && k.Code != tui.KeyTab && c.Editable() {
			return c.EditCell(k.Text)
		}
		return false
	}
	if cmd.ID == TableGoPrefix.ID {
		c.pendingG, c.yanked = true, false // the count waits with it, for [count]gg
		return true
	}
	n, counted := max(c.count, 1), c.count > 0
	wasG, yanked, deleting := c.pendingG, c.yanked, c.deleting
	c.dropPending()
	if wasG {
		n, counted = 1, false // the count went with the g it was typed for
	}
	switch cmd.ID {
	case TableUp.ID:
		c.Move(-n, 0)
	case TableDown.ID:
		c.Move(n, 0)
	case TableLeft.ID:
		c.Move(0, -n)
	case TableRight.ID:
		c.Move(0, n)
	case TableFieldNext.ID:
		c.moveFields(n)
	case TableFieldPrev.ID:
		c.moveFields(-n)
	case TablePageUp.ID:
		c.Move(-c.page(), 0)
	case TablePageDown.ID:
		c.Move(c.page(), 0)
	case TableLast.ID:
		row := c.Len() - 1
		if counted {
			row = n - 1
		}
		c.SetCursor(TablePos{row, c.cur.Col})
	case TableFieldFirst.ID:
		c.SetCursor(TablePos{c.cur.Row, 0})
	case TableFieldLast.ID:
		c.SetCursor(TablePos{c.cur.Row, len(c.Fields()) - 1})
	case TableVisual.ID:
		c.toggleSelect(TableSelectCells)
	case TableVisualRows.ID:
		c.toggleSelect(TableSelectRows)
	case TableVisualBlock.ID:
		c.toggleSelect(TableSelectBlock)
	case TableEscape.ID:
		if c.anchor == nil {
			return wasG || counted // a pending command dropped is the key's work; otherwise it is the application's
		}
		c.SetAnchor(nil)
	case TableYank.ID:
		switch {
		case c.anchor != nil:
			c.Copy()
		case yanked:
			var b strings.Builder
			c.writeRow(&b, c.cur.Row, 0, len(c.Fields())-1)
			c.setRegister(b.String(), true)
		default:
			c.Copy()
			c.yanked = true
		}
	case TableSort.ID:
		if !c.sortable {
			return false
		}
		c.sortKey(c.cur.Col)
	case TableActivate.ID:
		if c.Editable() { // an editable table's Enter edits the cell
			return c.EditCell("")
		}
		return c.activateCursor()
	case TableEditCell.ID:
		return c.EditCell("")
	case TableOpenRow.ID:
		return c.OpenRow()
	case TableClearCell.ID:
		if c.anchor != nil { // x on a selection cuts it, its cells emptied
			return c.Cut(false)
		}
		return c.clearCell()
	case TableDeleteRow.ID:
		if !c.Editable() {
			return false
		}
		if c.anchor != nil { // d on a selection cuts it at once: a V selection's rows go
			return c.Cut(c.kind == TableSelectRows)
		}
		if deleting {
			return c.deleteRow()
		}
		c.deleting = true
	case TablePasteAfter.ID:
		return c.PasteRegister(false)
	case TablePasteBefore.ID:
		return c.PasteRegister(true)
	case TableOpenRowAbove.ID:
		return c.OpenRowAbove()
	}
	return true
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

// countDigit is the digit a count is typed with: a digit key with no modifier held.
func countDigit(k tui.KeyEvent) (int, bool) {
	s, ok := tuikey.Of(k.Code, k.Mods, k.Text)
	if !ok || s.Mods != 0 || s.Code < '0' || s.Code > '9' {
		return 0, false
	}
	return int(s.Code - '0'), true
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
func (c *TableCore) dropPending() { c.count, c.pendingG, c.yanked, c.deleting = 0, false, false, false }

// clampPos is p inside the table.
func (c *TableCore) clampPos(p TablePos) TablePos {
	p.Row = min(max(p.Row, 0), max(c.Len()-1, 0))
	p.Col = min(max(p.Col, 0), max(len(c.Fields())-1, 0))
	return p
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
	if c.Len() == 0 && hit != TableHitHeader {
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
