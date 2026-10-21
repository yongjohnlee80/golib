package widget

import (
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TABLE CORE — a table's behaviour apart from any view, as EditorCore is an editor's and
// WindowCore a window's: the rows it shows through a source, a cell cursor, a selection of cells,
// rows or a block, the loaded rows sorted by one or more fields and filtered by a predicate,
// copying, and asking a paged source for more rows as the cursor nears the end of those loaded.
// A view paints what the core says and reports its own
// geometry through TableLayout; one core serves a terminal view, a pixel view, and a Markdown table
// in an editor's buffer (TableText). Every method runs on the loop.

// TableField is one column as a source names it.
type TableField struct {
	Title string
	// Width is the column's width in cells; 0 measures it from the title and the loaded cells.
	Width int
	// Align is where a cell's text sits in its column; a Markdown delimiter's :--: says which.
	Align style.Align
}

// TableSource is the rows a TableCore shows. Like ListSource it is read on the loop only, and Cell
// must be cheap and non-blocking: a source fetches elsewhere and serves from memory. Any change to
// the fields, Len or a cell is followed, in the same loop callback, by TableCore.Changed.
type TableSource interface {
	Fields() []TableField
	// Len is the rows loaded.
	Len() int
	// Cell is one cell's exact value, newlines and tabs included: a view draws it on one line
	// through TableCellLine, and an open field edits it as it is.
	Cell(row, col int) string
}

// TablePager is a source with rows still to load.
type TablePager interface {
	// More stays true until the rows asked for arrive or none are left.
	More() bool
	// FetchMore asks for up to n more rows; a call while one is in flight is ignored. The rows
	// arrive later, followed by TableCore.Changed.
	FetchMore(n int)
}

// TableEditor is a source whose cells and shape can change. Each method is one undo step in the
// source; ok false refuses.
type TableEditor interface {
	SetCell(row, col int, text string) bool
	InsertRow(at int) bool
	DeleteRow(at int) bool
	InsertField(at int, f TableField) bool
	DeleteField(at int) bool
	SetAlign(col int, a style.Align) bool
}

// TableLayout is how a TableCore reaches its view, in the view's own units.
type TableLayout interface {
	// Reveal scrolls the view so the cell at display row and field col is visible.
	Reveal(row, col int)
	// PageRows is how many rows a page key moves.
	PageRows() int
	// Measure is the width of s in the view's units.
	Measure(s string) int
	// Changed asks for a repaint.
	Changed()
}

// TablePos is a cell: a display row and a field.
type TablePos struct{ Row, Col int }

// TableOption configures a TableCore.
type TableOption func(*TableCore)

// TableSortable lets the s key sort the loaded rows by the cursor's field.
func TableSortable(on bool) TableOption { return func(c *TableCore) { c.sortable = on } }

// TableRowNumbers shows each display row's number, counted from 1, in a gutter before the first
// field. A view draws it; the core only says whether it is on (RowNumbers) and how many digits the
// widest number has (RowNumberDigits).
func TableRowNumbers(on bool) TableOption { return func(c *TableCore) { c.rowNumbers = on } }

// TableCopyFormat is the text a table's copies are written in, and its register's pastes read
// back in. A paste from outside the table (a PasteEvent: a spreadsheet's cells) is TSV whatever it
// is.
type TableCopyFormat uint8

const (
	// TableCopyTSV is tab-separated: an editable table's quoted as EncodeTableText has it, a
	// read-only table's values joined by tabs. The default.
	TableCopyTSV TableCopyFormat = iota
	// TableCopyCSV is comma-separated, quoted as RFC 4180 has it, editable or not.
	TableCopyCSV
)

// String names the format: "tsv" or "csv".
func (f TableCopyFormat) String() string {
	switch f {
	case TableCopyTSV:
		return "tsv"
	case TableCopyCSV:
		return "csv"
	}
	return "unknown"
}

// Valid reports whether f is one of the declared formats.
func (f TableCopyFormat) Valid() bool { return f <= TableCopyCSV }

// delimiter is the format's field separator: what a copy writes between cells and a register's
// paste reads them apart by.
func (f TableCopyFormat) delimiter() rune {
	if f == TableCopyCSV {
		return ','
	}
	return '\t'
}

// TableCopyAs is the format a table's copies are written in (TableCopyTSV, the default, or
// TableCopyCSV). A format that is neither is a programmer error, refused as the core is built.
func TableCopyAs(f TableCopyFormat) TableOption {
	if !f.Valid() {
		panic(fatalOf("widget: TableCopyAs", "unknown copy format", f.String()))
	}
	return func(c *TableCore) { c.copyFormat = f }
}

// TableOnSort is called with the sort keys whenever the sort changes: SortKeys, SortBy, the s key,
// or a new source or a removed field ending it. Rows merged into an unchanged sort call nothing.
func TableOnSort(fn func(keys []TableSortKey)) TableOption {
	return func(c *TableCore) { c.onSort = fn }
}

// TableCopyLimit bounds what Copy and the y keys put on the clipboard, in bytes: a copy longer
// than that copies nothing, and TableOnCopyRefused hears of it, so the host can say why. 0, the
// default, is no limit.
func TableCopyLimit(bytes int) TableOption { return func(c *TableCore) { c.copyLimit = max(bytes, 0) } }

// TableOnCopyRefused is called with the limit when a copy past TableCopyLimit copied nothing.
func TableOnCopyRefused(fn func(limit int)) TableOption {
	return func(c *TableCore) { c.onCopyRefused = fn }
}

// TableOnCopy receives text copied with y, yy or Copy, after the copy limit has accepted it.
// A host can keep its editor register in step with the system clipboard. The callback is also
// a copy target when a backend has no clipboard; invoking it counts as delivery to the host.
func TableOnCopy(fn func(text string)) TableOption {
	return func(c *TableCore) { c.onCopy = fn }
}

// TableMenuSelects makes a right press outside the selection select what it was on before the menu
// opens: a cell (the cursor moves there), a row's number (its row), a title (its field). Off, the
// default, the selection stays wherever the press was, for the menu's rows to act on (the results
// tables of AutoDoc rely on it). TableDocument turns it on.
func TableMenuSelects(on bool) TableOption { return func(c *TableCore) { c.menuSelects = on } }

// TableFetchAhead is how near the end of the loaded rows, in rows, the cursor asks a pager for
// more, and how many it asks for; at least 1.
func TableFetchAhead(rows int) TableOption {
	return func(c *TableCore) { c.ahead = max(rows, 1) }
}

// TableOnActivate is called with the source row under the cursor when Enter is pressed.
func TableOnActivate(fn func(row int)) TableOption { return func(c *TableCore) { c.onActivate = fn } }

// TableOnCursor is called with the source row under the cursor whenever that row changes: a move,
// a new source, rows arriving under a sort. A sort that keeps the cursor on its row calls nothing.
// It may replace the source (SetSource) or change it in place: the operation that moved the cursor
// does nothing more on the old rows.
func TableOnCursor(fn func(row int)) TableOption { return func(c *TableCore) { c.onCursor = fn } }

const (
	tableAheadDefault = 100
	tableMeasureRows  = 1000 // a measured column reads at most this many loaded rows
	tableMeasureMax   = 40   // and grows no wider than this
)

// TableCore holds a table's behaviour apart from any view.
type TableCore struct {
	src    TableSource
	pager  TablePager
	edit   TableEditor
	commit TableCommitter // the source's compound edits, nil when it has none (tablecore_edit.go)
	edges  bool           // TableAppendEdges
	// The core's parts, each in the file of its concern, with its own reset for a new source.
	tableEditSession // the open field (tablecore_edit.go)
	tableSelection   // the anchor, what it covers, and the pointer's drag (tablecore_select.go)
	tableView        // the sort and the filter: the display order (tablecore_sort.go)
	tableKeyState    // the keys of a command not yet complete (tablecore_keys.go)
	// presses are a view's pointer presses (press): a double-click paired by source row and field, and
	// a press whose rows the owner replaced; reset with the source
	presses       pressPairer[TablePos]
	keysFirst     func(tui.KeyEvent) bool
	layout        TableLayout
	ctx           *tui.Context
	kinds         TableKinds // the source's cell kinds, nil when it has none
	cur           TablePos
	sortable      bool
	rowNumbers    bool
	rowCursor     bool            // the cursor covers its whole row (TableRowCursor's r): y copies the row
	copyFormat    TableCopyFormat // what copies are written in (TableCopyAs)
	ahead         int
	onActivate    func(row int)
	onCursor      func(row int)
	onSort        func(keys []TableSortKey)
	copyLimit     int // bytes; 0: none
	onCopyRefused func(limit int)
	onCopy        func(text string)
	reg           TableRegisterStore // the register, the host's or the core's own (tablecore_clipboard.go)
	onClipped     func()
	menuSelects   bool  // TableMenuSelects: a right press outside the selection selects what it was on
	announced     int   // the source row onCursor was last told, -1 for none
	measured      []int // measured widths, nil until asked for after a change
}

// NewTableCore is a core over src, which may be nil until SetSource.
func NewTableCore(src TableSource, opts ...TableOption) *TableCore {
	c := &TableCore{ahead: tableAheadDefault, announced: -1}
	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}
	c.SetSource(src)
	return c
}

// Bind attaches the view. With a nil ctx the core neither copies to the clipboard nor asks for a
// repaint beyond layout.Changed.
func (c *TableCore) Bind(ctx *tui.Context, l TableLayout) { c.ctx, c.layout = ctx, l }

// SetSource replaces the rows; the cursor, selection, sort and filter start again.
func (c *TableCore) SetSource(src TableSource) {
	c.src = src
	c.pager, _ = src.(TablePager)
	c.edit, _ = src.(TableEditor)
	c.commit, _ = src.(TableCommitter)
	c.kinds, _ = src.(TableKinds)
	dropped := c.tableEditSession.newSource()
	had := c.keys != nil
	c.cur = TablePos{}
	c.tableSelection.reset()
	c.tableView.reset()
	c.tableKeyState.reset()
	c.presses.Reset() // a source row index means another row now
	// the owner is told last, the new source in place: each step stops once a callback replaced it
	if dropped && !c.dropped() || !c.changed() {
		return
	}
	if had {
		c.sortChanged()
	}
}

// RowNumbers reports whether a view shows each row's number (TableRowNumbers).
func (c *TableCore) RowNumbers() bool { return c.rowNumbers }

// SetCopyFormat is TableCopyAs on a core already built. A format that is neither TSV nor CSV is
// taken as TSV, the default: a runtime setter normalizes rather than fails.
func (c *TableCore) SetCopyFormat(f TableCopyFormat) {
	if !f.Valid() {
		f = TableCopyTSV
	}
	c.copyFormat = f
}

// CopyFormat is the format the table's copies are written in.
func (c *TableCore) CopyFormat() TableCopyFormat { return c.copyFormat }

// SetRowCursor makes the cursor cover its whole row (on) or its cell; r toggles it. On a row, y
// copies the row (as yy does), and h and l still move the field the cell cursor returns to.
func (c *TableCore) SetRowCursor(on bool) {
	if c.rowCursor == on {
		return
	}
	c.rowCursor = on
	if c.layout != nil {
		c.layout.Changed()
	}
}

// RowCursor reports whether the cursor covers its whole row.
func (c *TableCore) RowCursor() bool { return c.rowCursor }

// CursorCovers reports whether the cursor covers display row row's field col: its cell, or with a
// row cursor every field of its row. A view draws the cursor where it covers.
func (c *TableCore) CursorCovers(row, col int) bool {
	return row == c.cur.Row && (c.rowCursor || col == c.cur.Col)
}

// SetRowNumbers turns the row numbers on or off.
func (c *TableCore) SetRowNumbers(on bool) {
	if c.rowNumbers == on {
		return
	}
	c.rowNumbers = on
	if c.layout != nil {
		c.layout.Changed()
	}
	if c.ctx != nil {
		c.ctx.MarkDirty()
		c.ctx.RequestLayout()
	}
}

// RowNumberDigits is the width of the row-number gutter in digits: those of Len(), the last row's
// number, so it grows as rows arrive (1 to 9 rows take one, 10 to 99 two). 0 while the numbers are
// off.
func (c *TableCore) RowNumberDigits() int {
	if !c.rowNumbers {
		return 0
	}
	return len(strconv.Itoa(max(c.Len(), 1)))
}

// Source is the rows the core shows.
func (c *TableCore) Source() TableSource { return c.src }

// Len is the display rows: the rows loaded, or with a filter those it keeps (Filtered). Fields
// is the columns.
func (c *TableCore) Len() int {
	if c.filter != nil {
		return len(c.order)
	}
	return c.loaded()
}

// loaded is the rows the source has loaded, shown or not.
func (c *TableCore) loaded() int {
	if c.src == nil {
		return 0
	}
	return c.src.Len()
}

func (c *TableCore) Fields() []TableField {
	if c.src == nil {
		return nil
	}
	return c.src.Fields()
}

// Changed is the source changed: the sort and the filter are applied to the rows now loaded, the
// cursor kept on the source row it was on (clamped when that row is gone, and on the first row
// when the filter no longer shows it), and the view repainted. A pager's new rows are filtered
// and merged into a sort without moving the cursor off its row.
func (c *TableCore) Changed() { c.changed() }

// changed is Changed, reporting whether the rows survived the owner's callbacks: an edit dropped,
// the cursor's row, the sort. The owner is told last, the rows arranged.
func (c *TableCore) changed() (live bool) {
	c.measured = nil
	drop := false
	if c.editing != nil {
		// a Raw edit, an undo, a reload: the cell may be another's now. A source with a revision
		// says whether it changed; one without cannot, so its edit is dropped
		_, revd := c.src.(TableRevisioner)
		if !revd || !c.current(c.editing) {
			c.editing = nil
			drop = true
		}
	}
	keep := -1
	if c.Len() > 0 {
		keep = c.SourceRow(c.cur.Row) // read through the order the cursor was placed in
	}
	before := len(c.keys)
	c.keys = c.validKeys(c.keys)
	c.filter = c.validFilter(c.filter)
	c.arrange()
	if keep >= 0 && keep < c.loaded() { // with the sort gone, too: its field may have been removed
		c.cur.Row = c.displayRow(keep)
	}
	c.clamp()
	if c.Len() == 0 {
		c.anchor, c.kind = nil, TableSelectCells
	}
	c.wantMore()
	c.repaint()
	if drop && !c.dropped() || !c.announce() {
		return false
	}
	if len(c.keys) != before {
		return c.sortChanged()
	}
	return true
}

// ScrollFields asks the view to move its first field shown by n: the view
// clamps it and keeps its own scroll; the cursor stays where it is.
func (c *TableCore) ScrollFields(n int) {
	if h, ok := c.layout.(TableLayoutFields); ok && n != 0 {
		h.ScrollFields(n)
		c.repaint()
	}
}

// TableLayoutFields is a layout that scrolls its fields sideways: Shift+wheel and a horizontal
// wheel.
type TableLayoutFields interface {
	ScrollFields(n int)
}

// Cursor is the cell under the cursor.
func (c *TableCore) Cursor() TablePos { return c.cur }

// SetCursor moves the cursor, clamped to the table.
func (c *TableCore) SetCursor(p TablePos) { c.cursorTo(p) }

// cursorTo is SetCursor, reporting whether the rows survived the owner's TableOnCursor: an
// operation that goes on after moving the cursor stops when it reports false.
func (c *TableCore) cursorTo(p TablePos) (live bool) {
	c.cur = p
	c.clamp()
	return c.reach()
}

// SelectSourceRow moves the cursor to the display row showing source row r, in its field; to the
// first row when the filter hides r.
func (c *TableCore) SelectSourceRow(r int) { c.cursorTo(TablePos{c.displayRow(r), c.cur.Col}) }

// Move moves the cursor by rows and fields.
func (c *TableCore) Move(dRow, dCol int) { c.cursorTo(TablePos{c.cur.Row + dRow, c.cur.Col + dCol}) }

// SourceRow is the source row shown at display row r.
func (c *TableCore) SourceRow(r int) int {
	if c.order != nil && r >= 0 && r < len(c.order) {
		return c.order[r]
	}
	return r
}

// Cell is the text at display row r, field col.
func (c *TableCore) Cell(r, col int) string {
	if c.src == nil || r < 0 || r >= c.Len() || col < 0 || col >= len(c.Fields()) {
		return ""
	}
	return c.src.Cell(c.SourceRow(r), col)
}

// Rows are the display rows [from, to) as text, in display order, the filter's kept rows only:
// what a view paints and what a copy or a Markdown export takes.
func (c *TableCore) Rows(from, to int) [][]string {
	from, to = max(from, 0), min(to, c.Len())
	n := len(c.Fields())
	out := make([][]string, 0, max(to-from, 0))
	for r := from; r < to; r++ {
		row := make([]string, n)
		for col := range n {
			row[col] = c.Cell(r, col)
		}
		out = append(out, row)
	}
	return out
}

// Width is field col's width in the layout's units (cells without one): its declared Width in
// cells, else measured from its title and the first loaded rows, at most tableMeasureMax cells.
func (c *TableCore) Width(col int) int {
	fields := c.Fields()
	if col < 0 || col >= len(fields) {
		return 0
	}
	if w := fields[col].Width; w > 0 {
		return c.cells(w)
	}
	if c.measured == nil {
		c.measured = make([]int, len(fields))
		limit := c.cells(tableMeasureMax)
		for i, f := range fields {
			if f.Width > 0 {
				continue
			}
			w := c.measure(f.Title)
			for r := range min(c.Len(), tableMeasureRows) {
				w = max(w, c.measure(tableCellLine(c.Cell(r, i)))) // as drawn: a newline is ⏎
			}
			c.measured[i] = min(max(w, c.cells(1)), limit)
		}
	}
	return c.measured[col]
}

// Remeasure drops the measured widths: the view's units changed (a new font, another layout), so
// the next Width measures again.
func (c *TableCore) Remeasure() { c.measured = nil }

// cells is n cells in the layout's units.
func (c *TableCore) cells(n int) int { return c.measure(strings.Repeat("0", n)) }

func (c *TableCore) measure(s string) int {
	if c.layout != nil {
		return c.layout.Measure(s)
	}
	return len([]rune(s))
}

// SortBy orders the LOADED rows by field col, ascending or descending, as a view of the source:
// the source keeps its order, and rows that arrive later are merged in by Changed. col < 0 ends
// the sort. It is SortKeys with one key; the order of two cells is SortKeys's.
func (c *TableCore) SortBy(col int, desc bool) {
	if col < 0 || col >= len(c.Fields()) {
		c.SortKeys(nil)
		return
	}
	c.SortKeys([]TableSortKey{{Col: col, Desc: desc}})
}

// Sorted is the sort's first key: the field (-1 when none), its direction, and whether rows remain
// to load, so a view can say "sorted: loaded rows". SortedKeys has every key.
func (c *TableCore) Sorted() (col int, desc, partial bool) {
	keys, partial := c.SortedKeys()
	if len(keys) == 0 {
		return -1, false, false
	}
	return keys[0].Col, keys[0].Desc, partial
}

// compareCells orders two cells: as exact decimals when both are (parseDecimal, as the WHERE
// filter reads numbers), else as text.
func compareCells(a, b string) int { return comparePlain(readPlainCell(a), readPlainCell(b)) }

// plainCell is a cell without a kind as the sort reads it: its text, and its number when the text
// is a decimal.
type plainCell struct {
	text string
	num  *big.Rat // nil: not a decimal
}

func readPlainCell(text string) plainCell {
	n, _ := parseDecimal(strings.TrimSpace(text))
	return plainCell{text: text, num: n}
}

func comparePlain(a, b plainCell) int {
	if a.num != nil && b.num != nil {
		return a.num.Cmp(b.num)
	}
	return strings.Compare(a.text, b.text)
}

func (c *TableCore) displayRow(src int) int {
	if c.order == nil {
		return src
	}
	if i := slices.Index(c.order, src); i >= 0 {
		return i
	}
	return 0
}

func (c *TableCore) page() int {
	if c.layout != nil {
		return max(c.layout.PageRows(), 1)
	}
	return 10
}

// clamp keeps the cursor in the table, the edges (TableAppendEdges) included.
func (c *TableCore) clamp() {
	edge := c.edgeRows()
	c.cur.Row = min(max(c.cur.Row, 0), max(c.Len()-1+edge, 0))
	c.cur.Col = min(max(c.cur.Col, 0), max(len(c.Fields())-1+edge, 0))
}

// reach reveals the cursor and asks a pager for more rows when the cursor is near the end of
// those loaded: what a move of the cursor does.
func (c *TableCore) reach() (live bool) {
	if c.layout != nil {
		c.layout.Reveal(c.cur.Row, c.cur.Col)
	}
	c.wantMore()
	c.repaint()
	return c.announce()
}

// show reveals the cursor without asking a pager for anything: what a sort and a filter do. They
// rearrange the rows loaded, and only the cursor's moving (or rows arriving) may fetch ahead.
func (c *TableCore) show() (live bool) {
	if c.layout != nil {
		c.layout.Reveal(c.cur.Row, c.cur.Col)
	}
	c.repaint()
	return c.announce()
}

// announce tells onCursor the cursor's source row when it changed, and reports whether the rows
// survived it (pressPairer's epoch: the owner may replace them there).
func (c *TableCore) announce() (live bool) {
	r := -1
	if c.Len() > 0 && c.cur.Row < c.Len() { // the edge row is no source row
		r = c.SourceRow(c.cur.Row)
	}
	if r == c.announced {
		return true
	}
	c.announced = r
	return c.presses.Survives(func() {
		if c.onCursor != nil && r >= 0 {
			c.onCursor(r)
		}
	})
}

// wantMore asks a pager for rows while the cursor is within ahead rows of the end of those loaded:
// on a new source with none, after the cursor moves, and after a page arrives. Under a filter the
// cursor's place is that of its row among all the loaded rows (loadedPlace), not among the rows
// shown, so a filter hiding rows asks for none the unfiltered view would not. A sort and a filter
// change never call it (show): only the cursor's moving and rows arriving do.
func (c *TableCore) wantMore() {
	if c.pager != nil && c.pager.More() && c.loadedPlace() >= c.loaded()-c.ahead {
		c.pager.FetchMore(c.ahead)
	}
}

func (c *TableCore) repaint() {
	if c.layout != nil {
		c.layout.Changed()
	}
	if c.ctx != nil {
		c.ctx.MarkDirty()
	}
}
