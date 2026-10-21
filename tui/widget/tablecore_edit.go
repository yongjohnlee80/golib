package widget

import "github.com/yongjohnlee80/golib/tui"

// TABLE EDITING — a cell of an editable source edited in place. The field is
// a TextArea holding the cell's exact value, newlines and tabs included; the table's one-line cell
// is only its display. A value kept unchanged writes nothing. A source that commits compounds
// (TableCommitter) applies each edit as one write, so a new row or field and its first value are
// one undo step, and only such a source offers the edges (TableAppendEdges) and o. An open edit is
// fenced to its source, the source's revision and the source row: any change drops it rather than
// writing it onto another cell.

// TableReadOnlySource is a source that can say it may not be edited now: a parse error, a limit.
type TableReadOnlySource interface {
	ReadOnly() string // why; "" when it may
}

// TableEdit is the cell being edited.
type TableEdit struct {
	// Pos is where the field is drawn: a display row and a field. The row is Len() on the edge
	// row, and the new row below the cursor for o (Inserting); the field is len(Fields()) on the
	// edge field.
	Pos   TablePos
	Field *TextArea // the value as it is being typed

	// gen is the core's source generation it was opened on (SetSource's count): the source itself
	// is not compared, since comparing one of an uncomparable type (a slice) panics
	gen         uint64
	rev         uint64 // the source's revision then (TableRevisioner)
	revd        bool   // the source has a revision
	srcRow, col int    // the source row (the one inserted, for a new row) and the field
	insertRow   bool   // a new row: the edge row, or o's
	insertField bool   // the edge field
	original    string // the cell's exact value when opened; "" for a new cell
}

// Inserting reports whether the edit is on a row not yet in the source: the edge row, or o's. A
// view draws an empty row at Pos.Row for it.
func (e *TableEdit) Inserting() bool { return e.insertRow }

// TableAppendEdges shows one empty row after the last and one empty field after the last, to type
// into, on a source that is a TableCommitter; on any other it does nothing.
func TableAppendEdges() TableOption { return func(c *TableCore) { c.edges = true } }

// TableOnEditDropped is told when an open edit is dropped by a change under it (the source's
// revision moving, a sort, a filter, a new source, CancelEdit) or refused by the source: a host says
// so ("the table changed; the edit was dropped").
func TableOnEditDropped(fn func()) TableOption { return func(c *TableCore) { c.onEditDropped = fn } }

// TableKeysFirst is asked for each key the table's own commands leave, while no field is open,
// before typing opens a cell with it: the host's actions on that key win (a document view's undo
// and redo). True takes the key.
func TableKeysFirst(fn func(k tui.KeyEvent) bool) TableOption {
	return func(c *TableCore) { c.keysFirst = fn }
}

// Editable reports whether the cells and shape can change now: an editor source not saying it is
// read only.
func (c *TableCore) Editable() bool {
	if c.edit == nil {
		return false
	}
	if ro, ok := c.src.(TableReadOnlySource); ok && ro.ReadOnly() != "" {
		return false
	}
	return true
}

// EdgeRow and EdgeField report whether the table shows the empty row after the last and the empty
// field after the last (TableAppendEdges, on an editable TableCommitter).
func (c *TableCore) EdgeRow() bool   { return c.edges && c.commit != nil && c.Editable() }
func (c *TableCore) EdgeField() bool { return c.EdgeRow() }

func (c *TableCore) edgeRows() int {
	if c.EdgeRow() {
		return 1
	}
	return 0
}

// Editing is the open edit, nil for none.
func (c *TableCore) Editing() *TableEdit { return c.editing }

// EditCell opens the field on the cursor's cell: its value, or typed in its place when typed is
// not "". It reports whether it opened.
func (c *TableCore) EditCell(typed string) bool {
	if !c.Editable() || c.editing != nil {
		return false
	}
	p, fields := c.cur, len(c.Fields())
	onRow, onField := p.Row >= c.Len(), p.Col >= fields
	if (onRow || onField) && !c.EdgeRow() {
		return false
	}
	e := &TableEdit{Pos: p, gen: c.srcGen, col: p.Col, insertRow: onRow, insertField: onField}
	if onRow {
		e.srcRow = c.loaded()
	} else {
		e.srcRow = c.SourceRow(p.Row)
		if !onField {
			e.original = c.src.Cell(e.srcRow, p.Col)
		}
	}
	c.open(e, typed)
	return true
}

// OpenRow is o: a new empty row below the cursor, its field open in the cursor's field. Nothing is
// inserted until the value is kept.
func (c *TableCore) OpenRow() bool {
	if !c.Editable() || c.editing != nil || c.commit == nil || c.cur.Col >= len(c.Fields()) {
		return false
	}
	at := 0
	if c.Len() > 0 && c.cur.Row < c.Len() {
		at = c.SourceRow(c.cur.Row) + 1
	} else {
		at = c.loaded()
	}
	row := min(c.cur.Row+1, c.Len())
	c.open(&TableEdit{Pos: TablePos{row, c.cur.Col}, gen: c.srcGen, srcRow: at, col: c.cur.Col, insertRow: true}, "")
	return true
}

func (c *TableCore) open(e *TableEdit, typed string) {
	if r, ok := c.src.(TableRevisioner); ok {
		e.rev, e.revd = r.Revision(), true
	}
	e.Field = NewTextArea()
	v := e.original
	if typed != "" {
		v = typed
	}
	e.Field.SetValue(v) // the cursor at its end
	c.editing, c.literal = e, false // a Ctrl+V is its own field's: one kept without its key ends with it
	c.dropPending()
	c.repaint()
}

// current reports whether e still names the cell it was opened on: the same source, at the same
// revision, the row and field still there. A source with no revision cannot say it is unchanged:
// a Changed drops its edit (TableCore.Changed).
func (c *TableCore) current(e *TableEdit) bool {
	if c.srcGen != e.gen || e.srcRow > c.loaded() || e.col > len(c.Fields()) {
		return false
	}
	if r, ok := c.src.(TableRevisioner); ok && e.revd && r.Revision() != e.rev {
		return false
	}
	return true
}

// KeepEdit writes the field's value, when it changed, and moves the cursor by dRow, dCol from the
// cell; a value unchanged writes nothing. A source that refuses, or an edit no longer current,
// drops it (TableOnEditDropped) and writes nothing.
func (c *TableCore) KeepEdit(dRow, dCol int) bool {
	e := c.editing
	if e == nil {
		return false
	}
	c.editing = nil
	if !c.current(e) {
		c.dropped()
		return false
	}
	if v := e.Field.Value(); v != e.original {
		var ok bool
		if c.commit != nil {
			op := TableOp{InsertRow: -1, InsertField: -1, Row: e.srcRow, Col: e.col, Text: v}
			if e.insertRow {
				op.InsertRow = e.srcRow
			}
			if e.insertField {
				op.InsertField = e.col
			}
			ok = c.commit.Commit(op)
		} else {
			ok = c.edit.SetCell(e.srcRow, e.col, v)
		}
		if !ok {
			c.dropped()
			return false
		}
		c.Changed()
		c.SetCursor(TablePos{c.displayRow(e.srcRow), e.col})
	} else if e.insertRow { // nothing typed in a new row: it is not inserted
		c.repaint()
	}
	c.Move(dRow, dCol)
	return true
}

// DropEdit closes the field and writes nothing.
func (c *TableCore) DropEdit() {
	if c.editing != nil {
		c.editing = nil
		c.repaint()
	}
}

// CancelEdit drops an open edit because of a change the core does not see (the view hidden, the
// document switched, the Rendered toggle), telling TableOnEditDropped.
func (c *TableCore) CancelEdit() {
	if c.editing != nil {
		c.editing = nil
		c.dropped()
	}
}

// dropped tells the host an edit was dropped, and repaints without its field.
func (c *TableCore) dropped() {
	if c.onEditDropped != nil {
		c.onEditDropped()
	}
	c.repaint()
}

// tableEditSession is the open field and what it was opened on.
type tableEditSession struct {
	editing       *TableEdit // the open field, nil for none
	srcGen        uint64     // SetSource's count: an open edit's source, by number
	onEditDropped func()     // TableOnEditDropped
	literal       bool       // Ctrl+V in the open field: a Tab next is typed
}

// newSource counts a new source, and closes the open field: the cell it was opened on is not the
// new source's. It reports whether a field was open, to be reported dropped.
func (e *tableEditSession) newSource() bool {
	e.srcGen++
	open := e.editing != nil
	e.editing = nil
	return open
}

// clearCell is x and Delete: the cell emptied, as a kept value of "".
func (c *TableCore) clearCell() bool {
	if !c.Editable() || c.cur.Row >= c.Len() || c.cur.Col >= len(c.Fields()) {
		return false
	}
	if c.Cell(c.cur.Row, c.cur.Col) == "" {
		return true
	}
	c.EditCell("")
	c.editing.Field.SetValue("")
	return c.KeepEdit(0, 0)
}

// deleteRow is dd: the cursor's row removed, one step.
func (c *TableCore) deleteRow() bool {
	if !c.Editable() || c.cur.Row >= c.Len() {
		return false
	}
	if !c.edit.DeleteRow(c.SourceRow(c.cur.Row)) {
		return false
	}
	c.Changed()
	return true
}

// fieldKey is a key while the field is open: Enter keeps and moves down, Tab and Shift+Tab keep and
// move across, Alt+Enter types a newline, Ctrl+V then Tab a tab, Esc drops; the rest is the
// TextArea's.
func (c *TableCore) fieldKey(k tui.KeyEvent) bool {
	e := c.editing
	if k.Kind == tui.KeyRelease {
		return true
	}
	if c.literal {
		c.literal = false
		if k.Code == tui.KeyTab && k.Mods == 0 {
			e.Field.insert("\t")
			c.repaint()
			return true
		}
	}
	cmd, ok := c.ctx.KeyCommand(k, tableFieldCommands...)
	if !ok {
		used := e.Field.HandleEvent(k)
		c.repaint()
		return used
	}
	switch cmd.ID {
	case TableFieldKeep.ID:
		c.KeepEdit(1, 0)
	case TableFieldNextCell.ID:
		c.KeepEdit(0, 1)
	case TableFieldPrevCell.ID:
		c.KeepEdit(0, -1)
	case TableFieldNewline.ID:
		e.Field.insert("\n")
		c.repaint()
	case TableFieldDrop.ID:
		c.DropEdit()
	case TableFieldLiteral.ID:
		c.literal = true
	}
	return true
}

// Paste is a PasteEvent: into the open field; with none open, the text read as rows (pastedRows)
// and pasted as a block from the cursor's cell. A text that does not read pastes nothing.
func (c *TableCore) Paste(text string) bool {
	if c.editing == nil {
		rows, err := pastedRows(text)
		return err == nil && len(rows) > 0 && c.PasteCells(rows, TablePasteBlock, false)
	}
	c.editing.Field.insert(text)
	c.repaint()
	return true
}

// TableRowKind is what a drawn row is: one of the core's display rows, o's new row not yet in the
// source, or the empty row after the last (TableAppendEdges).
type TableRowKind uint8

const (
	TableRowData    TableRowKind = iota // a display row
	TableRowPending                     // o's new row, inserted only when its cell is kept
	TableRowEdge                        // the empty row after the last
)

// PendingRow is the drawn row of o's new row, -1 for none: an open edit inserting a row that is not
// the edge row.
func (c *TableCore) PendingRow() int {
	e := c.editing
	if e == nil || !e.Inserting() || (e.Pos.Row >= c.Len() && c.EdgeRow()) {
		return -1
	}
	return e.Pos.Row
}

// DrawnRows is the rows a view draws: the display rows, o's new row and the edge row.
func (c *TableCore) DrawnRows() int {
	n := c.Len() + c.edgeRows()
	if c.PendingRow() >= 0 {
		n++
	}
	return n
}

// DrawnRow is drawn row v: what it is, and the display row it shows (Len() for the edge row, -1
// for o's new row).
func (c *TableCore) DrawnRow(v int) (TableRowKind, int) {
	r := v
	if p := c.PendingRow(); p >= 0 {
		switch {
		case v == p:
			return TableRowPending, -1
		case v > p:
			r--
		}
	}
	if r >= c.Len() {
		return TableRowEdge, r
	}
	return TableRowData, r
}

// DrawnOf is the drawn row of display row r (the edge row for Len()).
func (c *TableCore) DrawnOf(r int) int {
	if p := c.PendingRow(); p >= 0 && r >= p {
		return r + 1
	}
	return r
}
