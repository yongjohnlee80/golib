package widget

import "github.com/yongjohnlee80/golib/tui"

// TableDocument is a document view of a CSV or TSV EditorCore: a DataTable over a TableCore over a
// TableCSV, with the edges. It reads the core, not the text it is shown, and reads it again
// (TableCSV.Sync) as it is shown and at each layout, so an edit made in Raw, an undo or a reload
// shows; its edits are the core's, so Raw shows them. Hidden (the Rendered view toggled off, the
// document switched, the view replaced), an open edit is dropped.
//
// It holds its DataTable as its one child rather than embedding it: the focus goes into the table,
// and the table's own methods stay the table's.
type TableDocument struct {
	Base
	table *DataTable
	src   *TableCSV
}

var _ DocumentView = (*TableDocument)(nil)

// NewTableDocument is a table view of ed's text, read as CSV (comma ',') or TSV ('\t'). opts are
// the TableCore's, after TableAppendEdges.
func NewTableDocument(ed *EditorCore, comma rune, opts ...TableOption) *TableDocument {
	src := NewTableCSV(ed, comma)
	core := NewTableCore(src, append([]TableOption{TableAppendEdges()}, opts...)...)
	return &TableDocument{table: NewDataTable(core), src: src}
}

// Source is the CSV document the table edits.
func (d *TableDocument) Source() *TableCSV { return d.src }

// Table is the table's view; Core its behaviour.
func (d *TableDocument) Table() *DataTable { return d.table }
func (d *TableDocument) Core() *TableCore  { return d.table.Core() }

// Init mounts the table.
func (d *TableDocument) Init(ctx *tui.Context) {
	d.Base.Init(ctx)
	ctx.Mount(d.table)
}

// ShowDocument reads the core again; the text argument is not read: the core is.
func (d *TableDocument) ShowDocument([]byte) { d.reread() }

// reread reads the core when it changed, and tells the table: its open edit is dropped when the
// cell it was on changed under it.
func (d *TableDocument) reread() {
	if d.src.Sync() {
		d.table.Core().Changed()
	}
}

// Layout reads the core first, so an edit made elsewhere is what is laid out, and gives the table
// the whole area.
func (d *TableDocument) Layout(c tui.Constraints) tui.Size {
	d.reread()
	ctx := d.Context()
	sz := ctx.LayoutChild(d.table, c)
	ctx.PlaceChild(d.table, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

// Render paints nothing of its own: the table paints itself.
func (d *TableDocument) Render(tui.Surface) {}

// SetVisible shows or hides the table; hidden, an open edit is dropped.
func (d *TableDocument) SetVisible(v bool) {
	if !v {
		d.table.Core().CancelEdit()
	}
	d.Base.SetVisible(v)
}
