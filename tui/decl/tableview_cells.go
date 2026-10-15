package decl

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A TableView with a cellCursor shows its model through a TableCore and a DataTable: a cursor on
// cells, the loaded rows sortable (sortable: true), and a model that is a Fetcher asked for more
// rows as the cursor nears the end of those loaded.
//
//	TableView { model: App.result; cellCursor: true; sortable: true; onActivated: App.open(index) }
//
// index in its signals and currentIndex are the MODEL's row, whatever the sort shows.

// modelTable is a TableView's model as a TableCore's source.
type modelTable struct{ n *tableViewNode }

func (s modelTable) Fields() []widget.TableField {
	n := s.n
	if len(n.declared) > 0 {
		out := make([]widget.TableField, len(n.declared))
		for i, d := range n.declared {
			out[i] = widget.TableField{Title: d.title, Width: d.width}
		}
		return out
	}
	if n.mv.model == nil {
		return nil
	}
	out := make([]widget.TableField, n.mv.model.ColumnCount(nil))
	for c := range out {
		out[c].Title = n.mv.model.HeaderData(c)
	}
	return out
}

func (s modelTable) Len() int { return s.n.mv.rows() }

func (s modelTable) Cell(row, col int) string {
	n := s.n
	if n.mv.model == nil {
		return ""
	}
	if len(n.declared) > 0 {
		if col < 0 || col >= len(n.declared) {
			return ""
		}
		return n.mv.model.Data(Index{Row: row}, n.declared[col].role).Raw
	}
	return n.mv.model.Data(Index{Row: row, Column: col}, "").Raw
}

// pagedModelTable is a modelTable whose model is a Fetcher: a TablePager at the root.
type pagedModelTable struct {
	modelTable
	f Fetcher
}

func (p pagedModelTable) More() bool    { return p.f.CanFetchMore(Index{}) }
func (p pagedModelTable) FetchMore(int) { p.f.FetchMore(Index{}) }

// cellSource is the node's model as a source: a pager when the model is a Fetcher.
func (n *tableViewNode) cellSource() widget.TableSource {
	if f, ok := n.mv.model.(Fetcher); ok {
		return pagedModelTable{modelTable{n}, f}
	}
	return modelTable{n}
}

// buildCells makes the node's DataTable.
func (n *tableViewNode) buildCells(sortable bool) {
	core := widget.NewTableCore(nil,
		widget.TableSortable(sortable),
		widget.TableOnActivate(func(row int) { n.activated(numberValue(row)) }),
		widget.TableOnCursor(func(row int) { n.moved(numberValue(row)) }))
	n.grid = widget.NewDataTable(core)
	n.mv.changed = n.followCells
}

// followCells applies a model change: a reset is a new source (the cursor and sort start again);
// anything else, rows arriving among them, keeps them.
func (n *tableViewNode) followCells(c Change) {
	core := n.grid.Core()
	if c.Kind == Reset || c.Kind == ColumnsReset {
		core.SetSource(n.cellSource())
		return
	}
	core.Changed()
}

// TableOf is the DataTable a TableView with a cellCursor shows, nil for any other component: what
// a style wrapping TableView binds a native view to.
func TableOf(c tui.Component) *widget.DataTable {
	if n, ok := c.(*tableViewNode); ok {
		return n.grid
	}
	return nil
}
