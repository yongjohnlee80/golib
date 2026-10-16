package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A TableView with a cellCursor shows its model through a TableCore and a DataTable: a cursor on
// cells, the loaded rows sortable (sortable: true), and a model that is a Fetcher asked for more
// rows as the cursor nears the end of those loaded.
//
//	TableView { model: App.result; cellCursor: true; sortable: true; onActivated: App.open(index) }
//
// index in its signals and currentIndex are the MODEL's row, whatever the sort shows. orderBy is
// the sort as ORDER BY text: setting it parses it and sorts (an error leaves the sort as it was
// and says why in orderByError), and the s key writes its sort back to it. filter is the rows shown
// as WHERE text, set and kept the same way, its error in filterError. A model that knows each
// cell's kind (widget.TableKinds) sorts and filters by kind, when no TableViewColumns are declared.

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

// kindedModelTable and kindedPagedModelTable add the model's cell kinds.
type kindedModelTable struct {
	modelTable
	k widget.TableKinds
}

func (s kindedModelTable) Kind(row, col int) widget.TableCellKind { return s.k.Kind(row, col) }

type kindedPagedModelTable struct {
	pagedModelTable
	k widget.TableKinds
}

func (s kindedPagedModelTable) Kind(row, col int) widget.TableCellKind { return s.k.Kind(row, col) }

// cellSource is the node's model as a source: a pager when the model is a Fetcher, and with its
// kinds when it has them and the columns are its own (a declared column names a role, not one of
// the model's columns, so its kinds are not the model's).
func (n *tableViewNode) cellSource() widget.TableSource {
	f, paged := n.mv.model.(Fetcher)
	k, kinded := n.mv.model.(widget.TableKinds)
	kinded = kinded && len(n.declared) == 0
	switch {
	case paged && kinded:
		return kindedPagedModelTable{pagedModelTable{modelTable{n}, f}, k}
	case paged:
		return pagedModelTable{modelTable{n}, f}
	case kinded:
		return kindedModelTable{modelTable{n}, k}
	}
	return modelTable{n}
}

// buildCells makes the node's DataTable.
func (n *tableViewNode) buildCells(sortable bool) {
	core := widget.NewTableCore(nil,
		widget.TableSortable(sortable),
		widget.TableOnActivate(func(row int) { n.activated(numberValue(row)) }),
		widget.TableOnCursor(func(row int) { n.moved(numberValue(row)) }),
		widget.TableOnSort(n.sortChanged))
	n.grid = widget.NewDataTable(core)
	n.mv.changed = n.followCells
}

// followCells applies a model change: a reset is a new source (the cursor starts again, and
// orderBy and filter are applied to its fields); anything else, rows arriving among them, keeps
// them.
func (n *tableViewNode) followCells(c Change) {
	core := n.grid.Core()
	if c.Kind == Reset || c.Kind == ColumnsReset {
		n.ordering = true
		core.SetSource(n.cellSource())
		n.ordering = false
		n.applyOrder()
		n.applyFilter()
		return
	}
	core.Changed()
}

// setOrderBy is the orderBy setter: the text is kept as written, and sorts the rows when it parses.
func (n *tableViewNode) setOrderBy(text string) {
	n.orderBy = text
	n.applyOrder()
}

// applyOrder sorts by orderBy. Text that does not parse leaves the sort as it was, and orderErr
// says why.
func (n *tableViewNode) applyOrder() {
	core := n.grid.Core()
	keys, err := widget.ParseTableOrder(n.orderBy, core.Fields())
	if err != nil {
		n.orderErr = err.Error()
		return
	}
	n.orderErr = ""
	n.ordering = true
	core.SortKeys(keys)
	n.ordering = false
}

// setFilter is the filter setter: the text is kept as written, and filters the rows when it parses.
func (n *tableViewNode) setFilter(text string) {
	n.filter = text
	n.applyFilter()
}

// applyFilter filters by filter. Text that does not parse leaves the filter as it was, and
// filterErr says why.
func (n *tableViewNode) applyFilter() {
	core := n.grid.Core()
	p, err := widget.ParseTableWhere(n.filter, core.Fields())
	if err != nil {
		n.filterErr = err.Error()
		return
	}
	n.filterErr = ""
	core.SetFilter(p)
}

// sortChanged writes a sort the view made itself (the s key, a field removed) back to orderBy,
// and raises orderByChanged.
func (n *tableViewNode) sortChanged(keys []widget.TableSortKey) {
	if n.ordering {
		return
	}
	text := widget.FormatTableOrder(keys, n.grid.Core().Fields())
	n.orderErr = ""
	if text == n.orderBy {
		return
	}
	n.orderBy = text
	n.orderChanged(qml.SpecValue{Kind: qml.SpecValueString, Raw: text})
}

// cellsSetter is a setter for a property only a TableView with a cellCursor has.
func cellsSetter[V any](name string, read func(qml.SpecValue) (V, error), apply func(*tableViewNode, V)) Setter {
	return func(c tui.Component, v qml.SpecValue) error {
		n, ok := c.(*tableViewNode)
		if !ok {
			return fmt.Errorf("not a TableView")
		}
		if n.grid == nil {
			return fmt.Errorf("TableView.%s needs cellCursor: true", name)
		}
		val, err := read(v)
		if err != nil {
			return err
		}
		apply(n, val)
		return nil
	}
}

// TableOf is the DataTable a TableView with a cellCursor shows, nil for any other component: what
// a style wrapping TableView binds a native view to.
func TableOf(c tui.Component) *widget.DataTable {
	if n, ok := c.(*tableViewNode); ok {
		return n.grid
	}
	return nil
}
