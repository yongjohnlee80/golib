package decl

import (
	"fmt"
	"maps"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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
	n.show(gridShown{n.grid})
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
	n.orderChanged(strValue(text))
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

// cellTemplate is a view's delegate: a stateless Text template, parsed and vetted once and presented
// per row (TreeView) or per visible cell (TableView). One copy of the rules, so a new property or a
// new local is written once for both views.
type cellTemplate struct {
	owner    string // the view's type, as its errors name it
	node     *qml.SpecNode
	eval     func(qml.SpecValue, map[string]qml.SpecValue) (qml.SpecValue, error)
	sink     func(error)
	implicit string // a role the view supplies itself (TableView's "display"), not checked against the model
}

// textName is the Text type's name (widgets.go registers it): the one type a cell delegate may be.
const textName = "Text"

// parseCellTemplate reads owner's delegate from b: a Text object template with one text and an
// optional color, and no id, handlers or children. nil when b writes none; the last one written
// wins, as a property does.
func parseCellTemplate(b Build, owner, implicit string) (*cellTemplate, error) {
	var t *cellTemplate
	for _, prop := range b.Props {
		if prop.Name != "delegate" {
			continue
		}
		if prop.Value.Kind != qml.SpecValueTemplate || prop.Value.Template == nil {
			return nil, fmt.Errorf("%s.delegate takes a Text object template (at %s)", owner, prop.Pos)
		}
		node := prop.Value.Template
		if node.Type != textName || node.ID != "" || len(node.Children) != 0 || len(node.Handlers) != 0 {
			return nil, fmt.Errorf("%s.delegate supports stateless Text only, without ids, handlers or children (at %s)", owner, node.Pos)
		}
		seen := map[string]bool{}
		for _, p := range node.Props {
			if (p.Name != "text" && p.Name != "color") || seen[p.Name] {
				return nil, fmt.Errorf("%s.delegate Text accepts one text and optional color, not %q (at %s)", owner, p.Name, p.Pos)
			}
			seen[p.Name] = true
		}
		if !seen["text"] {
			return nil, fmt.Errorf("%s.delegate Text needs text (at %s)", owner, node.Pos)
		}
		t = &cellTemplate{owner: owner, node: node, eval: b.Eval, sink: b.sink, implicit: implicit}
	}
	return t, nil
}

// check vets the template even for an empty model, so it cannot hide an unsupported expression or an
// unknown role behind zero rows. base is the view's own locals (palette.text; a table's row, column
// and model.display); hasModel says roles are the model's, so a model.<role> the template reads must
// be one of them. Every property must evaluate, and color to a color.
func (t *cellTemplate) check(hasModel bool, roles []string, base map[string]qml.SpecValue) error {
	locals := maps.Clone(base)
	known := map[string]bool{}
	for _, r := range roles {
		known[r] = true
	}
	for _, p := range t.node.Props {
		register := func(role string) error {
			if role == t.implicit && t.implicit != "" {
				return nil
			}
			if hasModel && !known[role] {
				return fmt.Errorf("%s.delegate: unknown model role %q (at %s)", t.owner, role, p.Pos)
			}
			locals["model."+role] = strValue("normal")
			return nil
		}
		if p.Value.Kind == qml.SpecValueRef && len(p.Value.Path) == 2 && p.Value.Path[0] == "model" {
			if err := register(p.Value.Path[1]); err != nil {
				return err
			}
		}
		if p.Value.Expr != nil {
			var refErr error
			p.Value.Expr.Walk(func(e *js.Expr) bool {
				if e.Kind == js.ExprMember && e.Left != nil && e.Left.Kind == js.ExprIdent && e.Left.Raw == "model" {
					if err := register(e.Name); err != nil {
						refErr = err
						return false
					}
				}
				return true
			})
			if refErr != nil {
				return refErr
			}
		}
		v, err := t.eval(p.Value, locals)
		if err != nil {
			return fmt.Errorf("%s.delegate %s (at %s): %w", t.owner, p.Name, p.Pos, err)
		}
		if p.Name == "color" {
			if _, err := colorOf(v); err != nil {
				return err
			}
		}
	}
	return nil
}

// present evaluates the template over locals: its text (text when it has none that evaluates) and
// its color. A property that fails to evaluate goes to the sink and is left as it was.
func (t *cellTemplate) present(locals map[string]qml.SpecValue, text string) (string, style.Style) {
	var st style.Style
	for _, p := range t.node.Props {
		v, err := t.eval(p.Value, locals)
		if err != nil {
			t.report(err)
			continue
		}
		switch p.Name {
		case "text":
			text = v.Raw
		case "color":
			c, err := colorOf(v)
			if err != nil {
				t.report(err)
				continue
			}
			st = st.Foreground(c)
		}
	}
	return text, st
}

func (t *cellTemplate) report(err error) {
	if t.sink != nil {
		t.sink(err)
	}
}

// modelValue is a model's value as a delegate reads it: an invalid one is an empty string.
func modelValue(v qml.SpecValue) qml.SpecValue {
	if v.Kind == qml.SpecValueInvalid {
		return strValue("")
	}
	return v
}

// tableShown is the widget a TableView shows its rows in: a row list (widget.Table) or, with a
// cellCursor, a cell grid (widget.DataTable). The node asks it instead of branching on which.
type tableShown interface {
	component() tui.Component
	current() int // the source row under the cursor; -1 when there is none
	setCurrent(row int)
	restyle(p palette)
}

// listShown is a TableView's row list.
type listShown struct{ t *widget.Table[int] }

func (s listShown) component() tui.Component { return s.t }

func (s listShown) current() int {
	if i, ok := s.t.Selected(); ok {
		return i
	}
	return -1
}

func (s listShown) setCurrent(row int) { s.t.List().SetCursor(row) }

func (s listShown) restyle(p palette) {
	s.t.ResetStyles(p.viewStyles())
	if p.has(roleBase, roleText) {
		s.t.SetHeaderStyle(p.look(roleBase, roleText).Bold(true).Underline(true))
	} else {
		s.t.SetHeaderStyle(style.Style{})
	}
}

// gridShown is a TableView's cell grid (cellCursor: true).
type gridShown struct{ g *widget.DataTable }

func (s gridShown) component() tui.Component { return s.g }

func (s gridShown) current() int {
	core := s.g.Core()
	if core.Len() == 0 {
		return -1
	}
	return core.SourceRow(core.Cursor().Row)
}

func (s gridShown) setCurrent(row int) { s.g.Core().SelectSourceRow(row) }

func (s gridShown) restyle(p palette) {
	vs := p.viewStyles()
	st := widget.DataTableStyles{Cell: vs.Row, Cursor: vs.CursorRow, CursorBlurred: vs.CursorBlurred, Selected: vs.SelectedRow}
	if p.has(roleBase, roleText) {
		st.Header = p.look(roleBase, roleText).Bold(true) // ruled off, not underlined
	}
	// a selection is the inactive highlight, Qt's selection without the keys: the region shows under
	// the cursor, which keeps the highlight
	if p.has(roleInactiveHighlight, roleInactiveHighlightText) {
		st.Selected = p.viewCursor(roleInactiveHighlight, roleInactiveHighlightText)
	}
	s.g.SetStyles(st)
}
