package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

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
	ed    *EditorCore
	extra func() []MenuItemModel // the host's menu rows (SetMenuRows)
}

var _ DocumentView = (*TableDocument)(nil)

// NewTableDocument is a table view of ed's text, read as CSV (comma ',') or TSV ('\t'). opts are
// the TableCore's, after TableAppendEdges.
func NewTableDocument(ed *EditorCore, comma rune, opts ...TableOption) *TableDocument {
	d := &TableDocument{src: NewTableCSV(ed, comma), ed: ed}
	// the editor's register: a yank in the table pastes in Raw, in another buffer, and back
	first := []TableOption{TableAppendEdges(), TableKeysFirst(d.historyKey), TableRegister(ed)}
	d.table = NewDataTable(NewTableCore(d.src, append(first, opts...)...))
	m := NewEditorMenu(d.table, nil)
	m.SetEnabled(true) // a new EditorMenu is off
	m.SetRows(d.menuRows)
	d.table.SetMenu(m)
	return d
}

// SetMenuRows adds the host's rows after the table's own; nil takes them away.
func (d *TableDocument) SetMenuRows(extra func() []MenuItemModel) { d.extra = extra }

// menuRows is the right-click menu: the table's commands, each showing its key from the key map
// (AccelCommand), disabled where it cannot run; then the host's.
func (d *TableDocument) menuRows() []MenuItemModel {
	c := d.Core()
	edit := c.cellsCommitter() != nil
	text, linewise := c.register().Register()
	selected := c.anchor != nil
	// a register's rows paste only into a whole view; fields go only while one would be left
	canPaste := edit && text != "" && (!linewise || c.whole())
	from, to := c.cur.Col, c.cur.Col
	if f, t, ok := c.Selection(); ok && c.kind != TableSelectRows {
		from, to = f.Col, t.Col
	}
	canDeleteFields := edit && to-from+1 < len(c.Fields())
	row := func(id ItemID, label string, cmd keymap.Command, on bool, run func()) MenuItemModel {
		r := NewCommand(id, label, CoreMenuAction{ID: tui.ActionID(id), Run: func(*EditorCore) { run() }})
		r.AccelCommand, r.Enabled = cmd.ID, on
		return r
	}
	rows := []MenuItemModel{
		row("table.yank", "Yank", TableYank, c.Len() > 0, func() { c.Copy() }),
		row("table.cut", "Cut", TableDeleteRow, edit && c.Len() > 0, func() { c.Cut(selected && c.kind == TableSelectRows) }),
		row("table.paste", "Paste", TablePasteAfter, canPaste, func() { c.PasteRegister(false) }),
		NewSeparator("table.sep.insert"),
		row("table.rowAbove", "Insert row above", TableOpenRowAbove, edit && c.whole(), func() { c.InsertRows(true) }),
		row("table.rowBelow", "Insert row below", TableOpenRow, edit && c.whole(), func() { c.InsertRows(false) }),
		row("table.fieldLeft", "Insert field left", keymap.Command{}, edit, func() { c.InsertFields(true) }),
		row("table.fieldRight", "Insert field right", keymap.Command{}, edit, func() { c.InsertFields(false) }),
		NewSeparator("table.sep.delete"),
		row("table.deleteRows", "Delete rows", keymap.Command{}, edit && c.Len() > 0, func() { c.DeleteRows() }),
		row("table.deleteFields", "Delete fields", keymap.Command{}, canDeleteFields, func() { c.DeleteFields() }),
		NewSeparator("table.sep.view"),
		row("table.selectAll", "Select all", keymap.Command{}, c.Len() > 0, func() { c.SelectAll() }),
	}
	numbers := NewCheck("table.rowNumbers", "Row numbers", CoreMenuAction{ID: "table.rowNumbers",
		Run: func(*EditorCore) { c.SetRowNumbers(!c.RowNumbers()) }})
	numbers.Checked = c.RowNumbers()
	rows = append(rows, numbers)
	if d.extra != nil {
		if more := d.extra(); len(more) > 0 {
			rows = append(append(rows, NewSeparator("table.sep.host")), more...)
		}
	}
	return rows
}

// historyKey runs the editor's undo or redo for a key its own key map gives them, before the
// table's typing could take it: a table edit is the core's, undone and redone as a text edit is.
func (d *TableDocument) historyKey(k tui.KeyEvent) bool {
	act, ok := d.ed.ActionOf(k)
	switch {
	case ok && act == ActUndo:
		d.ed.Undo()
	case ok && act == ActRedo:
		d.ed.Redo()
	default:
		return false
	}
	d.reread()
	return true
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
