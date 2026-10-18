package widget

import "github.com/yongjohnlee80/golib/tui"

// TableDocument (ADR 1792303119 §4.5) is a document view of a CSV or TSV EditorCore: a DataTable
// over a TableCore over a TableCSV, with the edges. It reads the core, not the text it is shown,
// and reads it again (TableCSV.Sync) as it is shown and at each layout, so an edit made in Raw, an
// undo or a reload shows; its edits are the core's, so Raw shows them. Hidden (the Rendered view
// toggled off, the document switched, the view replaced), an open edit is dropped.
type TableDocument struct {
	*DataTable
	src *TableCSV
}

var _ DocumentView = (*TableDocument)(nil)

// NewTableDocument is a table view of ed's text, read as CSV (comma ',') or TSV ('\t'). opts are
// the TableCore's, after TableAppendEdges.
func NewTableDocument(ed *EditorCore, comma rune, opts ...TableOption) *TableDocument {
	src := NewTableCSV(ed, comma)
	core := NewTableCore(src, append([]TableOption{TableAppendEdges()}, opts...)...)
	return &TableDocument{DataTable: NewDataTable(core), src: src}
}

// Source is the CSV document the table edits.
func (d *TableDocument) Source() *TableCSV { return d.src }

// ShowDocument reads the core again; the text argument is not read: the core is.
func (d *TableDocument) ShowDocument([]byte) { d.sync() }

// sync reads the core when it changed, and tells the table: its open edit is dropped when the
// cell it was on changed under it.
func (d *TableDocument) sync() {
	if d.src.Sync() {
		d.Core().Changed()
	}
}

// Layout reads the core first, so an edit made elsewhere is what is laid out.
func (d *TableDocument) Layout(c tui.Constraints) tui.Size {
	d.sync()
	return d.DataTable.Layout(c)
}

// SetVisible shows or hides the table; hidden, an open edit is dropped.
func (d *TableDocument) SetVisible(v bool) {
	if !v {
		d.Core().CancelEdit()
	}
	d.DataTable.SetVisible(v)
}
