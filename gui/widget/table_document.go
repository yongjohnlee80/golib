package widget

import (
	"github.com/yongjohnlee80/golib/tui/widget/editor"
	"github.com/yongjohnlee80/golib/tui/widget/table"
)

// NewTableDocument is a CSV (comma ',') or TSV ('\t') document view of ed for the Editor
// (SetDocumentView): the terminal's TableDocument, its DataTable laid out in pixels (BindTable).
// The keys, the edits, the open field and undo are the shared core's; only the drawing is the
// window's. opts are the TableCore's.
func NewTableDocument(ed *editor.EditorCore, comma rune, opts ...table.TableOption) *table.TableDocument {
	d := table.NewTableDocument(ed, comma, opts...)
	BindTable(d.Table())
	return d
}
