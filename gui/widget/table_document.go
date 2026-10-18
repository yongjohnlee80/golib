package widget

import tuiwidget "github.com/yongjohnlee80/golib/tui/widget"

// NewTableDocument is a CSV (comma ',') or TSV ('\t') document view of ed for the Editor
// (SetDocumentView): the terminal's TableDocument, its DataTable laid out in pixels (BindTable).
// The keys, the edits, the open field and undo are the shared core's; only the drawing is the
// window's. opts are the TableCore's.
func NewTableDocument(ed *tuiwidget.EditorCore, comma rune, opts ...tuiwidget.TableOption) *tuiwidget.TableDocument {
	d := tuiwidget.NewTableDocument(ed, comma, opts...)
	BindTable(d.Table())
	return d
}
