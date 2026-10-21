package widget

// THE REGISTER, PASTE, CUT, AND ROWS AND FIELDS IN AND OUT. A yank goes to a register before the
// system clipboard: the host's (TableRegister; a TableDocument gives its EditorCore's, the same
// for every buffer and for Raw), or the core's own. p and P paste it: whole rows (V, yy) as new
// records, anything else over the cells from the cursor's. A paste of the system clipboard comes
// as a PasteEvent and is a block. Each edit is one CommitCells: one undo step, nothing written on
// a refusal.

// TableRegisterStore is where a table's yanks go and its pastes come from. EditorCore is one.
type TableRegisterStore interface {
	Register() (text string, linewise bool)
	SetRegister(text string, linewise bool)
}

// TableRegister gives the core the host's register; without it the core keeps one of its own.
func TableRegister(r TableRegisterStore) TableOption { return func(c *TableCore) { c.reg = r } }

// TableOnPasteClipped is called once when a paste was cut off at the table's edge: rows past the
// last in a view that cannot grow, fields past the last.
func TableOnPasteClipped(fn func()) TableOption { return func(c *TableCore) { c.onClipped = fn } }

// TablePasteKind is how a paste writes.
type TablePasteKind uint8

const (
	TablePasteBlock TablePasteKind = iota // over the cells from the cursor's: rows down, fields right
	TablePasteRows                        // each row a new record
)

// ownRegister is a core's register when the host gives none.
type ownRegister struct {
	text     string
	linewise bool
}

func (r *ownRegister) Register() (string, bool)         { return r.text, r.linewise }
func (r *ownRegister) SetRegister(text string, lw bool) { r.text, r.linewise = text, lw }

func (c *TableCore) register() TableRegisterStore {
	if c.reg == nil {
		c.reg = &ownRegister{}
	}
	return c.reg
}

// cellsCommitter is the source's compound edits, nil when it has none.
func (c *TableCore) cellsCommitter() TableCellsCommitter {
	if !c.Editable() {
		return nil
	}
	cc, _ := c.src.(TableCellsCommitter)
	return cc
}

// whole reports whether the view shows every source row in source order: no sort, no filter, and
// no rows still to load. Only such a view grows when a paste runs past its last row, and only in
// such a view are rows inserted.
func (c *TableCore) whole() bool {
	return c.keys == nil && c.filter == nil && (c.pager == nil || !c.pager.More())
}

// setRegister puts a yank in the register, then tries the clipboard; the yank holds whether the
// clipboard took it or not. An empty table takes nothing; past the copy limit nothing is taken,
// TableOnCopyRefused hears of it; both report false.
func (c *TableCore) setRegister(text string, rows bool) bool {
	if c.Len() == 0 { // an empty table yanks nothing
		return false
	}
	if c.copyLimit > 0 && len(text) > c.copyLimit {
		c.refuseCopy()
		return false
	}
	c.register().SetRegister(text, rows)
	c.copyString(text)
	return true
}

// PasteRegister is p (before false) and P: the register pasted, whole rows as new records after or
// before the cursor's row, anything else as a block from the cursor's cell. An empty register, or
// one that does not read (pastedRows), pastes nothing.
func (c *TableCore) PasteRegister(before bool) bool {
	text, rows := c.register().Register()
	block, err := pastedRowsIn(text, c.copyFormat.delimiter()) // the register holds what this table copies
	if err != nil || len(block) == 0 {
		return false
	}
	kind := TablePasteBlock
	if rows {
		kind = TablePasteRows
	}
	return c.PasteCells(block, kind, before)
}

// PasteCells pastes rows as kind, as one CommitCells. A block's top left is the cursor's cell; its
// display rows map to source rows through SourceRow, read once before the write. Rows past the
// last are appended only in a whole view (whole); otherwise they, and fields past the last, are cut
// off, and TableOnPasteClipped is told once, even when nothing is left to write. Rows are new
// records after (before: before) the cursor's source row, refused in a view that is not whole.
func (c *TableCore) PasteCells(rows [][]string, kind TablePasteKind, before bool) bool {
	cc := c.cellsCommitter()
	fields := len(c.Fields())
	if cc == nil || len(rows) == 0 || fields == 0 {
		return false
	}
	var op TableCellsOp
	clipped := false
	first := -1 // rows: the source row the first one goes in at, which a whole view shows there too
	put := func(row, col int, text string) {
		if col >= fields {
			clipped = true
			return
		}
		op.Cells = append(op.Cells, TableCell{Row: row, Col: col, Text: text})
	}
	switch kind {
	case TablePasteRows:
		if !c.whole() {
			return false
		}
		at := c.rowAt(before)
		first = at
		for k, r := range rows {
			op.InsertRows = append(op.InsertRows, at)
			for j, v := range r {
				put(at+k, j, v)
			}
		}
	default:
		n := c.loaded()
		appended := 0
		for k, r := range rows {
			d := c.cur.Row + k
			var src int
			switch {
			case d < c.Len():
				src = c.SourceRow(d)
			case c.whole():
				src = n + appended
				op.InsertRows = append(op.InsertRows, n)
				appended++
			default:
				clipped = true
				continue
			}
			for j, v := range r {
				put(src, c.cur.Col+j, v)
			}
		}
	}
	if clipped && c.onClipped != nil {
		c.onClipped()
	}
	if len(op.Cells) == 0 && len(op.InsertRows) == 0 {
		return false
	}
	if !cc.CommitCells(op) {
		return false
	}
	if c.changed() && first >= 0 {
		c.SetCursor(TablePos{first, c.cur.Col}) // on the first row pasted, as vim's p and P
	}
	return true
}

// Cut yanks the selection (or the cursor's cell) and then, in one CommitCells, deletes its rows
// (rows true: d on a V selection) or empties its cells. A yank refused (past TableCopyLimit) cuts
// nothing; a commit refused leaves the register as yanked and the document as it was.
func (c *TableCore) Cut(rows bool) bool {
	cc := c.cellsCommitter()
	if cc == nil || c.Len() == 0 {
		return false
	}
	if !c.yankFrom(false) { // what is cut is what is stored: the selection, or the cursor's cell
		return false
	}
	var op TableCellsOp
	if c.anchor == nil {
		op.Cells = []TableCell{{Row: c.SourceRow(c.cur.Row), Col: c.cur.Col}}
	} else {
		from, to, _ := c.Selection()
		for r := from.Row; r <= to.Row; r++ {
			src := c.SourceRow(r)
			if rows {
				op.DeleteRows = append(op.DeleteRows, src)
				continue
			}
			for col := range c.Fields() {
				if c.InSelection(r, col) {
					op.Cells = append(op.Cells, TableCell{Row: src, Col: col})
				}
			}
		}
	}
	if !cc.CommitCells(op) {
		return false
	}
	c.SetAnchor(nil)
	c.Changed()
	return true
}

// yank is y: the selection, or with none the cursor's cover (its cell, or with a row cursor its
// whole row, as yy), into the register (whole rows for a V selection or a row cursor), then the
// clipboard. It is every copy's path, y's, Copy's and a menu's Yank alike. It reports whether the
// register took it: past the copy limit it takes nothing, and TableOnCopyRefused hears of it.
func (c *TableCore) yank() bool { return c.yankFrom(c.rowCursor) }

// yankFrom is yank, a row cursor's row copied only when row says so: Cut stores the cell it empties,
// whatever the cursor covers, so a paste puts back what was removed.
func (c *TableCore) yankFrom(row bool) bool {
	if c.Len() == 0 {
		return false
	}
	if c.anchor == nil && row { // a selection wins over the cursor's cover
		return c.setRegister(c.rowText(c.cur.Row), true)
	}
	s, ok := c.copyText(c.copyLimit)
	if !ok {
		c.refuseCopy()
		return false
	}
	return c.setRegister(s, c.anchor != nil && c.kind == TableSelectRows)
}

// InsertRows inserts one empty record after (before: before) the cursor's source row, one step; a
// view that is not whole (whole) refuses.
func (c *TableCore) InsertRows(before bool) bool {
	cc := c.cellsCommitter()
	if cc == nil || !c.whole() || len(c.Fields()) == 0 {
		return false
	}
	at := c.rowAt(before)
	if !cc.CommitCells(TableCellsOp{InsertRows: []int{at}}) {
		return false
	}
	if c.changed() {
		c.SetCursor(TablePos{at, c.cur.Col}) // on the row inserted: a whole view shows source row at there
	}
	return true
}

// InsertFields inserts one empty field after (before: before) the cursor's, one step.
func (c *TableCore) InsertFields(before bool) bool {
	cc := c.cellsCommitter()
	if cc == nil || len(c.Fields()) == 0 {
		return false
	}
	at := c.cur.Col
	if !before {
		at++
	}
	if !cc.CommitCells(TableCellsOp{InsertFields: []TableFieldAt{{At: at}}}) {
		return false
	}
	if c.changed() && !before {
		c.SetCursor(TablePos{c.cur.Row, at})
	}
	return true
}

// DeleteRows deletes the selection's rows, or the cursor's, one step.
func (c *TableCore) DeleteRows() bool {
	cc := c.cellsCommitter()
	if cc == nil || c.Len() == 0 {
		return false
	}
	from, to := c.cur.Row, c.cur.Row
	if f, t, ok := c.Selection(); ok {
		from, to = f.Row, t.Row
	}
	var op TableCellsOp
	for r := from; r <= to; r++ {
		op.DeleteRows = append(op.DeleteRows, c.SourceRow(r))
	}
	if !cc.CommitCells(op) {
		return false
	}
	c.SetAnchor(nil)
	c.Changed()
	return true
}

// DeleteFields deletes the selection's fields, or the cursor's, one step; the last field is refused.
func (c *TableCore) DeleteFields() bool {
	cc := c.cellsCommitter()
	if cc == nil || len(c.Fields()) == 0 {
		return false
	}
	from, to := c.cur.Col, c.cur.Col
	if f, t, ok := c.Selection(); ok && c.kind != TableSelectRows {
		from, to = f.Col, t.Col
	}
	var op TableCellsOp
	for col := from; col <= to; col++ {
		op.DeleteFields = append(op.DeleteFields, col)
	}
	if !cc.CommitCells(op) {
		return false
	}
	c.SetAnchor(nil)
	c.Changed()
	return true
}

// OpenRowAbove is O: a new row above the cursor's with its field open, inserted only when kept,
// as o's below. Like o, it opens in a sorted or filtered view too: the row is inserted in source
// order, before the cursor's source row (rows pasted and InsertRows refuse such a view instead).
// The policy is o's, kept as it was.
func (c *TableCore) OpenRowAbove() bool {
	if !c.Editable() || c.editing != nil || c.commit == nil || c.cur.Col >= len(c.Fields()) {
		return false
	}
	at := c.loaded()
	if c.Len() > 0 && c.cur.Row < c.Len() {
		at = c.SourceRow(c.cur.Row)
	}
	c.open(&TableEdit{Pos: TablePos{c.cur.Row, c.cur.Col}, gen: c.srcGen, srcRow: at, col: c.cur.Col, insertRow: true}, "")
	return true
}

// rowAt is the source row a record goes in at, after (before: before) the cursor's row: past the
// last on an empty table and on the edge row (TableAppendEdges), whose display row is Len().
func (c *TableCore) rowAt(before bool) int {
	if c.cur.Row >= c.Len() {
		return c.loaded()
	}
	at := c.SourceRow(c.cur.Row)
	if !before {
		at++
	}
	return at
}
