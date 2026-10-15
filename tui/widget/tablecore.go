package widget

import (
	"slices"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TABLE CORE — a table's behaviour apart from any view, as EditorCore is an editor's and
// WindowCore a window's: the rows it shows through a source, a cell cursor, a range of cells
// selected, the loaded rows sorted by a field, copying, and asking a paged source for more rows as
// the cursor nears the end of those loaded. A view paints what the core says and reports its own
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
	// Cell is the display text of one cell, one line.
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

// TableFetchAhead is how near the end of the loaded rows, in rows, the cursor asks a pager for
// more, and how many it asks for; at least 1.
func TableFetchAhead(rows int) TableOption {
	return func(c *TableCore) { c.ahead = max(rows, 1) }
}

// TableOnActivate is called with the source row under the cursor when Enter is pressed.
func TableOnActivate(fn func(row int)) TableOption { return func(c *TableCore) { c.onActivate = fn } }

// TableOnCursor is called with the source row under the cursor whenever that row changes: a move,
// a new source, rows arriving under a sort. A sort that keeps the cursor on its row calls nothing.
func TableOnCursor(fn func(row int)) TableOption { return func(c *TableCore) { c.onCursor = fn } }

const (
	tableAheadDefault = 100
	tableMeasureRows  = 1000 // a measured column reads at most this many loaded rows
	tableMeasureMax   = 40   // and grows no wider than this
)

// TableCore holds a table's behaviour apart from any view.
type TableCore struct {
	src        TableSource
	pager      TablePager
	edit       TableEditor
	layout     TableLayout
	ctx        *tui.Context
	cur        TablePos
	anchor     *TablePos
	order      []int // display row → source row; nil: source order
	sortBy     int   // -1: unsorted
	desc       bool
	sortable   bool
	ahead      int
	onActivate func(row int)
	onCursor   func(row int)
	announced  int   // the source row onCursor was last told, -1 for none
	measured   []int // measured widths, nil until asked for after a change
}

// NewTableCore is a core over src, which may be nil until SetSource.
func NewTableCore(src TableSource, opts ...TableOption) *TableCore {
	c := &TableCore{sortBy: -1, ahead: tableAheadDefault, announced: -1}
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

// SetSource replaces the rows; the cursor, selection and sort start again.
func (c *TableCore) SetSource(src TableSource) {
	c.src = src
	c.pager, _ = src.(TablePager)
	c.edit, _ = src.(TableEditor)
	c.cur, c.anchor, c.order, c.sortBy, c.desc = TablePos{}, nil, nil, -1, false
	c.Changed()
}

// Source is the rows the core shows.
func (c *TableCore) Source() TableSource { return c.src }

// Editable reports whether the source's cells and shape can change.
func (c *TableCore) Editable() bool { return c.edit != nil }

// Len is the rows loaded; Fields the columns.
func (c *TableCore) Len() int {
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

// Changed is the source changed: the sort is applied to the rows now loaded, the cursor kept on
// the cells that still exist, and the view repainted. A pager's new rows are merged into a sort.
func (c *TableCore) Changed() {
	c.measured = nil
	if c.sortBy >= 0 && c.sortBy < len(c.Fields()) {
		c.order = c.sorted(c.sortBy, c.desc)
	} else {
		c.order, c.sortBy = nil, -1
	}
	c.clamp()
	c.wantMore()
	c.announce()
	c.repaint()
}

// Cursor is the cell under the cursor.
func (c *TableCore) Cursor() TablePos { return c.cur }

// SetCursor moves the cursor, clamped to the table.
func (c *TableCore) SetCursor(p TablePos) {
	c.cur = p
	c.clamp()
	c.reach()
}

// SelectSourceRow moves the cursor to the display row showing source row r, in its field.
func (c *TableCore) SelectSourceRow(r int) { c.SetCursor(TablePos{c.displayRow(r), c.cur.Col}) }

// Move moves the cursor by rows and fields.
func (c *TableCore) Move(dRow, dCol int) { c.SetCursor(TablePos{c.cur.Row + dRow, c.cur.Col + dCol}) }

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

// Rows are the LOADED display rows [from, to) as text, in display order: what a view paints and
// what a copy or a Markdown export takes.
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
				w = max(w, c.measure(c.Cell(r, i)))
			}
			c.measured[i] = min(max(w, c.cells(1)), limit)
		}
	}
	return c.measured[col]
}

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
// the sort. Numbers sort as numbers when both cells are numbers.
func (c *TableCore) SortBy(col int, desc bool) {
	keep := c.SourceRow(c.cur.Row)
	if col < 0 || col >= len(c.Fields()) {
		c.order, c.sortBy, c.desc = nil, -1, false
	} else {
		c.sortBy, c.desc = col, desc
		c.order = c.sorted(col, desc)
	}
	c.cur.Row = c.displayRow(keep)
	c.reach()
	c.repaint()
}

// Sorted is the sort: the field (-1 when none), its direction, and whether rows remain to load,
// so a view can say "sorted: loaded rows".
func (c *TableCore) Sorted() (col int, desc, partial bool) {
	return c.sortBy, c.desc, c.sortBy >= 0 && c.pager != nil && c.pager.More()
}

func (c *TableCore) sorted(col int, desc bool) []int {
	order := make([]int, c.Len())
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		r := compareCells(c.src.Cell(a, col), c.src.Cell(b, col))
		if desc {
			r = -r
		}
		return r
	})
	return order
}

// compareCells orders two cells: as numbers when both are, else as text.
func compareCells(a, b string) int {
	x, errA := strconv.ParseFloat(strings.TrimSpace(a), 64)
	y, errB := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if errA == nil && errB == nil {
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	return strings.Compare(a, b)
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

// SetAnchor starts (or with nil ends) a range selection at a cell; the range runs to the cursor.
func (c *TableCore) SetAnchor(p *TablePos) {
	c.anchor = p
	c.repaint()
}

// Selection is the selected range, top-left and bottom-right inclusive; ok is false without one.
func (c *TableCore) Selection() (from, to TablePos, ok bool) {
	if c.anchor == nil {
		return TablePos{}, TablePos{}, false
	}
	a, b := *c.anchor, c.cur
	return TablePos{min(a.Row, b.Row), min(a.Col, b.Col)}, TablePos{max(a.Row, b.Row), max(a.Col, b.Col)}, true
}

// InSelection reports whether the cell is in the selected range.
func (c *TableCore) InSelection(r, col int) bool {
	from, to, ok := c.Selection()
	return ok && r >= from.Row && r <= to.Row && col >= from.Col && col <= to.Col
}

// CopyText is the selection, or the cursor's cell, as tab-separated lines.
func (c *TableCore) CopyText() string {
	from, to, ok := c.Selection()
	if !ok {
		from, to = c.cur, c.cur
	}
	var b strings.Builder
	for r := from.Row; r <= to.Row; r++ {
		if r > from.Row {
			b.WriteByte('\n')
		}
		for col := from.Col; col <= to.Col; col++ {
			if col > from.Col {
				b.WriteByte('\t')
			}
			b.WriteString(c.Cell(r, col))
		}
	}
	return b.String()
}

// Copy puts CopyText on the clipboard.
func (c *TableCore) Copy() bool {
	if c.ctx == nil || c.Len() == 0 {
		return false
	}
	return c.ctx.CopyToClipboard(c.CopyText())
}

// HandleKey moves, selects, sorts, copies and activates:
//
//	↑ ↓ ← → / k j h l   one cell        PgUp PgDn   a page
//	g G                 first, last row  0 $ Home End   first, last field
//	v                   start or end a range selection  Esc   end it
//	y                   copy             s   sort by this field (if sortable): up, down, off
//	Enter               activate the cursor's row
//
// Ctrl, Alt and Super chords are the application's.
func (c *TableCore) HandleKey(k tui.KeyEvent) bool {
	if k.Kind == tui.KeyRelease || k.Mods&nonTextMods != 0 {
		return false
	}
	code := k.Code
	if k.Text != "" {
		code = []rune(k.Text)[0]
	}
	switch code {
	case 'k', tui.KeyUp:
		c.Move(-1, 0)
	case 'j', tui.KeyDown:
		c.Move(1, 0)
	case 'h', tui.KeyLeft:
		c.Move(0, -1)
	case 'l', tui.KeyRight:
		c.Move(0, 1)
	case tui.KeyPageUp:
		c.Move(-c.page(), 0)
	case tui.KeyPageDown:
		c.Move(c.page(), 0)
	case 'g':
		c.SetCursor(TablePos{0, c.cur.Col})
	case 'G':
		c.SetCursor(TablePos{c.Len() - 1, c.cur.Col})
	case '0', tui.KeyHome:
		c.SetCursor(TablePos{c.cur.Row, 0})
	case '$', tui.KeyEnd:
		c.SetCursor(TablePos{c.cur.Row, len(c.Fields()) - 1})
	case 'v':
		if c.anchor != nil {
			c.SetAnchor(nil)
		} else {
			p := c.cur
			c.SetAnchor(&p)
		}
	case tui.KeyEscape:
		if c.anchor == nil {
			return false
		}
		c.SetAnchor(nil)
	case 'y':
		c.Copy()
	case 's':
		if !c.sortable {
			return false
		}
		switch {
		case c.sortBy != c.cur.Col:
			c.SortBy(c.cur.Col, false)
		case !c.desc:
			c.SortBy(c.cur.Col, true)
		default:
			c.SortBy(-1, false)
		}
	case tui.KeyEnter:
		if c.onActivate == nil || c.Len() == 0 {
			return false
		}
		c.onActivate(c.SourceRow(c.cur.Row))
	default:
		return false
	}
	return true
}

func (c *TableCore) page() int {
	if c.layout != nil {
		return max(c.layout.PageRows(), 1)
	}
	return 10
}

func (c *TableCore) clamp() {
	c.cur.Row = min(max(c.cur.Row, 0), max(c.Len()-1, 0))
	c.cur.Col = min(max(c.cur.Col, 0), max(len(c.Fields())-1, 0))
}

// reach reveals the cursor and asks a pager for more rows when the cursor is near the end of
// those loaded.
func (c *TableCore) reach() {
	if c.layout != nil {
		c.layout.Reveal(c.cur.Row, c.cur.Col)
	}
	c.wantMore()
	c.announce()
	c.repaint()
}

// announce tells onCursor the cursor's source row when it changed.
func (c *TableCore) announce() {
	r := -1
	if c.Len() > 0 {
		r = c.SourceRow(c.cur.Row)
	}
	if r == c.announced {
		return
	}
	c.announced = r
	if c.onCursor != nil && r >= 0 {
		c.onCursor(r)
	}
}

// wantMore asks a pager for rows while the cursor is within ahead rows of the end of those loaded:
// on a new source with none, after the cursor moves, and after a page arrives.
func (c *TableCore) wantMore() {
	if c.pager != nil && c.pager.More() && c.cur.Row >= c.Len()-c.ahead {
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
