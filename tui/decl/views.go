package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// VIEWS OVER HOST MODELS — Qt's ListView and ComboBox.
//
//	ListView { model: App.connections; textRole: "label"; onActivated: App.use(index) }
//	ComboBox { id: engine; model: App.engines; textRole: "label"; valueRole: "id" }
//	...        onAccepted: App.save(engine.currentValue)
//
// A view shows its model's rows as text — the role textRole names, one row a
// line — and follows the model's changes itself: nothing is rebound when a row
// is inserted or the model is reset. `index` in a signal is the row, a number.
// A view unsubscribes from its model when the model is replaced and when the
// view is destroyed.

// modelView is what ListView and ComboBox share: the model, the subscription,
// and the role the rows show.
type modelView struct {
	model    ItemModel
	cancel   func()
	textRole string
	changed  func(Change)
	// shown are the keys of the rows as the view last showed them: what a
	// row NUMBER meant before a change, so the chosen record stays chosen
	// wherever the change moves it.
	shown []string
}

// keyAt is the key of the row the view showed at i; ok is false when no row
// was there. An empty key is a key like any other.
func (v *modelView) keyAt(i int) (key string, ok bool) {
	if i < 0 || i >= len(v.shown) {
		return "", false
	}
	return v.shown[i], true
}

// reshow records the model's rows as now shown, and returns the row the key
// is at now — -1 when it is gone, or when there was none (had false).
func (v *modelView) reshow(key string, had bool) int {
	v.shown = v.shown[:0]
	at := -1
	for i := range v.rows() {
		k := v.model.Key(Index{Row: i})
		v.shown = append(v.shown, k)
		if had && k == key && at < 0 {
			at = i
		}
	}
	return at
}

func (v *modelView) setModel(m ItemModel) {
	v.release()
	v.model = m
	if m != nil {
		v.cancel = m.Subscribe(v.changed)
	}
	v.changed(Change{Kind: Reset})
}

func (v *modelView) release() {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

func (v *modelView) rows() int {
	if v.model == nil {
		return 0
	}
	return v.model.RowCount(nil)
}

func (v *modelView) text(i int) string {
	if v.model == nil {
		return ""
	}
	return v.model.Data(Index{Row: i}, v.textRole).Raw
}

// ---------------------------------------------------------------- ListView

type listViewNode struct {
	widget.Base
	mv        modelView // composed, not embedded: its methods call each other
	list      *widget.List[int]
	activated func(args ...qml.SpecValue)
	moved     func(args ...qml.SpecValue)
}

// modelSource is a model's rows as a list's source: row i is i.
type modelSource struct{ v *modelView }

func (s modelSource) Len() int       { return s.v.rows() }
func (s modelSource) Item(i int) int { return i }

func buildListView(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a ListView takes no children; its rows are its model's (at %s)", b.Pos)
	}
	n := &listViewNode{activated: b.EmitterWith("activated"), moved: b.EmitterWith("currentIndexChanged")}
	consumed, err := b.ReadProps(map[string]field{"textRole": into(&n.mv.textRole, stringOf)})
	if err != nil {
		return nil, nil, err
	}
	n.list = widget.NewList(widget.WithSource[int](modelSource{&n.mv}, n.mv.text),
		widget.WithListOnActivate[int](func(i int) { n.activated(numberValue(i)) }),
		widget.WithListOnCursor[int](func(i int) { n.moved(numberValue(i)) }))
	n.mv.changed = func(Change) {
		key, had := n.mv.keyAt(n.currentIndex())
		n.list.RefreshSource()
		// The record the cursor was on stays under it; one that is gone
		// leaves the cursor where the list clamps it.
		if at := n.mv.reshow(key, had); at >= 0 {
			n.list.SetCursor(at)
		}
	}
	return n, consumed, nil
}

// Init mounts the list, which reports its rows to the ListView itself (WithListOnActivate).
func (n *listViewNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.list)
}

func (n *listViewNode) Layout(c tui.Constraints) tui.Size {
	sz := n.Context().LayoutChild(n.list, c)
	n.Context().PlaceChild(n.list, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (*listViewNode) Render(tui.Surface)         {}
func (*listViewNode) HandleEvent(tui.Event) bool { return false }

func (n *listViewNode) currentIndex() int {
	if i, ok := n.list.Selected(); ok {
		return i
	}
	return -1
}

var listViewType = Type{
	Name:  "ListView",
	Build: buildListView,
	restyle: func(c tui.Component, p palette) {
		c.(*listViewNode).list.ResetStyles(p.viewStyles())
	},
	Ctor: []string{"textRole"},
	Setters: map[string]Setter{
		"model":        setter("a ListView", modelOf, func(n *listViewNode, m ItemModel) { n.mv.setModel(m) }),
		"currentIndex": setter("a ListView", truncOf, func(n *listViewNode, v int) { n.list.SetCursor(v) }),
	},
	Getters: map[string]Getter{
		"currentIndex": getter(func(n *listViewNode) (qml.SpecValue, error) {
			return numberValue(n.currentIndex()), nil
		}),
	},
	Signals:   map[string][]string{"activated": {"index"}, "currentIndexChanged": {"index"}},
	Destroyed: destroyer(func(n *listViewNode) { n.mv.release() }),
}

// ---------------------------------------------------------------- ComboBox

type comboBoxNode struct {
	widget.Base
	mv        modelView // composed, not embedded: its methods call each other
	valueRole string
	sel       *widget.Select[int]
	activated func(args ...qml.SpecValue)
}

func buildComboBox(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a ComboBox takes no children; its choices are its model's (at %s)", b.Pos)
	}
	n := &comboBoxNode{activated: b.EmitterWith("activated")}
	var placeholder string
	var maxWidth int
	consumed, err := b.ReadProps(map[string]field{
		"textRole":        into(&n.mv.textRole, stringOf),
		"valueRole":       into(&n.valueRole, stringOf),
		"placeholderText": into(&placeholder, stringOf),
		"maxWidth":        into(&maxWidth, cellsOf),
	})
	if err != nil {
		return nil, nil, err
	}
	var opts []widget.SelectOption[int]
	if placeholder != "" {
		opts = append(opts, widget.WithSelectPlaceholder[int](placeholder))
	}
	if maxWidth != 0 {
		if maxWidth < 3 {
			return nil, nil, fmt.Errorf("a ComboBox maxWidth must be at least 3 (at %s)", b.Pos)
		}
		opts = append(opts, widget.WithSelectMaxWidth[int](maxWidth))
	}
	opts = append(opts, widget.WithSelectOnChange[int](func(i int) { n.activated(numberValue(i)) }))
	n.sel = widget.NewSelect(opts...)
	n.mv.changed = func(Change) {
		key, had := n.mv.keyAt(n.currentIndex())
		n.refill()
		// The record chosen stays chosen wherever it moved; one that is gone
		// is no longer chosen — currentValue reads "", not another record.
		n.sel.SetSelectedIndex(n.mv.reshow(key, had))
	}
	return n, consumed, nil
}

// refill gives the select its model's rows as choices.
func (n *comboBoxNode) refill() {
	items := make([]widget.SelectItem[int], n.mv.rows())
	for i := range items {
		items[i] = widget.SelectItem[int]{Label: n.mv.text(i), Value: i}
	}
	n.sel.SetOptions(items)
}

func (n *comboBoxNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.sel) // it reports a choice to the ComboBox itself (WithSelectOnChange)
}

func (n *comboBoxNode) Layout(c tui.Constraints) tui.Size {
	fieldConstraints := c
	fieldConstraints.MinW = 0
	sz := n.Context().LayoutChild(n.sel, fieldConstraints)
	n.Context().PlaceChild(n.sel, tui.Rect{W: sz.W, H: sz.H})
	return c.Constrain(sz)
}

func (*comboBoxNode) Render(tui.Surface)         {}
func (*comboBoxNode) HandleEvent(tui.Event) bool { return false }

func (n *comboBoxNode) currentIndex() int {
	if i, ok := n.sel.Value(); ok {
		return i
	}
	return -1
}

var comboBoxType = Type{
	Name:  "ComboBox",
	Build: buildComboBox,
	// The field wears the Button role, as Qt Quick Controls' non-editable
	// ComboBox does; with none set, golib's own look.
	restyle: func(c tui.Component, p palette) {
		st := style.New().Foreground(style.TokenForeground)
		if p.has(roleButton, roleButtonText) {
			st = p.look(roleButton, roleButtonText)
		}
		c.(*comboBoxNode).sel.SetFieldStyle(st)
	},
	Ctor: []string{"textRole", "valueRole", "placeholderText", "maxWidth"},
	Setters: map[string]Setter{
		"model": setter("a ComboBox", modelOf, func(n *comboBoxNode, m ItemModel) { n.mv.setModel(m) }),
		// Qt's writable currentIndex: the row chosen, -1 for none. A row the
		// model does not have chooses none.
		"currentIndex": setter("a ComboBox", truncOf, func(n *comboBoxNode, v int) { n.sel.SetSelectedIndex(v) }),
	},
	Getters: map[string]Getter{
		"currentIndex": getter(func(n *comboBoxNode) (qml.SpecValue, error) {
			return numberValue(n.currentIndex()), nil
		}),
		// The chosen row's valueRole — Qt's currentValue; a string "" when
		// nothing is chosen.
		"currentValue": getter(func(n *comboBoxNode) (qml.SpecValue, error) {
			i := n.currentIndex()
			if i < 0 || n.mv.model == nil {
				return strValue(""), nil
			}
			v := n.mv.model.Data(Index{Row: i}, n.valueRole)
			if v.Kind == qml.SpecValueInvalid {
				return strValue(""), nil
			}
			return v, nil
		}),
	},
	Signals:   map[string][]string{"activated": {"index"}},
	Destroyed: destroyer(func(n *comboBoxNode) { n.mv.release() }),
}

// ---------------------------------------------------------------- TableView

// TableView is Qt Quick Controls 1's TableView: rows of cells, under a header.
// Its columns are the MODEL's — ColumnCount, HeaderData, and each cell's Data
// — so a query's result shows whatever columns it has, and a ColumnsReset
// redraws them on the same view. Declared TableViewColumns override them with
// fixed roles, titles and widths:
//
//	TableView {
//	    model: App.users
//	    TableViewColumn { role: "name"; title: "NAME"; width: 20 }
//	    TableViewColumn { role: "role"; title: "ROLE"; width: 0 }   // 0: flex
//	}
type tableViewNode struct {
	widget.Base
	mv        modelView // composed, not embedded: its methods call each other
	declared  []*tableColumnNode
	table     *widget.Table[int]
	activated func(args ...qml.SpecValue)
	moved     func(args ...qml.SpecValue)
	// menu is contextMenuRequested(index, x, y), raised only when the document declares it
	menu     func(args ...qml.SpecValue)
	template *cellTemplate
	eval     func(qml.SpecValue, map[string]qml.SpecValue) (qml.SpecValue, error)
	normal   style.Color
	sink     func(error)
	// grid is the cell-cursor view (cellCursor: true), in place of table
	grid *widget.DataTable
	// shown is whichever of table and grid the node shows, asked instead of branching on which
	shown tableShown
	// orderBy is the grid's sort as text, as the document set it or the view wrote it back;
	// orderErr why it did not parse, "" when it did. orderChanged raises orderByChanged.
	orderBy, orderErr string
	orderChanged      func(args ...qml.SpecValue)
	ordering          bool // the node is applying orderBy: the core's news of it is not written back
	// filter is the grid's filter as WHERE text, as the document set it; filterErr why it did not
	// parse, "" when it did. Only the document changes it, so it has no Changed signal.
	filter, filterErr string
}

// tableColumnNode is a declared TableViewColumn: what the column shows.
type tableColumnNode struct {
	widget.Base
	role, title string
	width       int
	elide       widget.Elide
}

// elideModes are Qt's TableViewColumn.elideMode values (Qt::TextElideMode):
// where a cell too wide for its column loses text. ElideRight is Qt's default;
// ElideLeft keeps the end of a path, its file name. ElidePath is golib's: the
// start of a path, cut at a "/" so no name is cut.
var elideModes = enum[widget.Elide]{values: map[string]widget.Elide{
	"ElideLeft":   widget.ElideLeft,
	"ElideRight":  widget.ElideRight,
	"ElideMiddle": widget.ElideMiddle,
	"ElideNone":   widget.ElideNone,
	"ElidePath":   widget.ElidePath,
}}

func (*tableColumnNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*tableColumnNode) declarationOnly()                  {}
func (*tableColumnNode) Render(tui.Surface)                {}

func buildTableViewColumn(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a TableViewColumn takes no children (at %s)", b.Pos)
	}
	n := &tableColumnNode{}
	consumed, err := b.ReadProps(map[string]field{
		"role":      into(&n.role, stringOf),
		"title":     into(&n.title, stringOf),
		"width":     into(&n.width, cellsOf),
		"elideMode": into(&n.elide, elideModes.read),
	})
	if err != nil {
		return nil, nil, err
	}
	if n.role == "" {
		return nil, nil, fmt.Errorf("a TableViewColumn needs the role it shows (at %s)", b.Pos)
	}
	return n, consumed, nil
}

func buildTableView(b Build) (tui.Component, []string, error) {
	n := &tableViewNode{activated: b.EmitterWith("activated"), moved: b.EmitterWith("currentIndexChanged"),
		orderChanged: b.EmitterWith("orderByChanged")}
	// contextMenuRequested is opt-in by its declaration: a TableView with no handler for it leaves
	// a right press unhandled, as before, for whatever is above it. A reload that adds the handler
	// rebuilds the node (a new signal), so the declaration is read here once.
	if b.Emitters["contextMenuRequested"] != nil {
		n.menu = b.EmitterWith("contextMenuRequested")
	}
	var consumed []string
	var cells, sortable bool
	for _, prop := range b.Props {
		switch prop.Name {
		case "cellCursor", "sortable":
			v, err := boolOf(prop.Value)
			if err != nil {
				return nil, nil, fmt.Errorf("TableView.%s: %w (at %s)", prop.Name, err, prop.Pos)
			}
			if prop.Name == "cellCursor" {
				cells = v
			} else {
				sortable = v
			}
			consumed = append(consumed, prop.Name)
			continue
		}
	}
	n.eval, n.normal, n.sink = b.Eval, style.Default(), b.sink
	var err error
	if n.template, err = parseCellTemplate(b, "TableView", "display"); err != nil {
		return nil, nil, err
	}
	if n.template != nil {
		consumed = append(consumed, "delegate")
	}
	for _, c := range b.Children {
		col, ok := c.(*tableColumnNode)
		if !ok {
			return nil, nil, fmt.Errorf("a TableView holds TableViewColumns only; its rows are its model's (at %s)", b.Pos)
		}
		n.declared = append(n.declared, col)
	}
	if cells {
		switch {
		case n.template != nil:
			return nil, nil, fmt.Errorf("a TableView with a cellCursor takes no delegate (at %s)", b.Pos)
		case n.menu != nil:
			return nil, nil, fmt.Errorf("a TableView with a cellCursor has no contextMenuRequested (at %s)", b.Pos)
		}
		n.buildCells(sortable)
		return n, consumed, nil
	}
	if sortable {
		return nil, nil, fmt.Errorf("TableView.sortable needs cellCursor: true (at %s)", b.Pos)
	}
	n.table = widget.NewTable(n.columns(), widget.WithSource[int](modelSource{&n.mv}, func(int) string { return "" }),
		widget.WithListOnActivate[int](func(i int) { n.activated(numberValue(i)) }),
		widget.WithListOnCursor[int](func(i int) { n.moved(numberValue(i)) }),
		widget.WithListOnContextMenu[int](func(i int, at tui.Point) {
			if n.menu != nil {
				at := n.inView(n.table.List(), at)
				n.menu(numberValue(i), numberValue(at.X), numberValue(at.Y))
			}
		}))
	n.shown = listShown{n.table}
	n.table.List().SetContextMenu(n.menu != nil)
	n.mv.changed = n.follow
	if n.template != nil {
		if err := n.checkTemplate(nil); err != nil {
			return nil, nil, err
		}
		n.table.SetCellPresenter(n.presentCell)
	}
	return n, consumed, nil
}

// checkTemplate vets expression scope even for an empty model. A template
// cannot hide an unsupported expression or unknown role behind zero rows.
func (n *tableViewNode) checkTemplate(m ItemModel) error {
	if n.template == nil {
		return nil
	}
	var roles []string
	if m != nil {
		roles = m.Roles()
	}
	return n.template.check(m != nil, roles, map[string]qml.SpecValue{
		"row": numberValue(0), "column": numberValue(0),
		"model.display": {Kind: qml.SpecValueString, Raw: ""},
		"palette.text":  {Kind: qml.SpecValueObject, Obj: n.normal},
	})
}

// presentCell evaluates one stateless QML Text template only when the native
// table paints this visible cell. Application policy remains in the model.
func (n *tableViewNode) presentCell(row, col int) widget.StyledCell {
	if n.mv.model == nil {
		return widget.StyledCell{}
	}
	ix := Index{Row: row, Column: col}
	role := ""
	if col < len(n.declared) {
		role = n.declared[col].role
	}
	locals := map[string]qml.SpecValue{
		"row": numberValue(row), "column": numberValue(col),
		"model.display": modelValue(n.mv.model.Data(ix, role)),
		"palette.text":  {Kind: qml.SpecValueObject, Obj: n.normal},
	}
	for _, name := range n.mv.model.Roles() {
		if name != "display" {
			locals["model."+name] = modelValue(n.mv.model.Data(ix, name))
		}
	}
	text, st := n.template.present(locals, "")
	return widget.StyledCell{Text: text, Style: st}
}

// columns are the table's columns now: the declared ones, else the model's.
func (n *tableViewNode) columns() []widget.TableColumn[int] {
	cell := func(role string, col int) func(int) string {
		return func(row int) string {
			if n.mv.model == nil {
				return ""
			}
			return n.mv.model.Data(Index{Row: row, Column: col}, role).Raw
		}
	}
	var cols []widget.TableColumn[int]
	if len(n.declared) > 0 {
		for _, d := range n.declared {
			cols = append(cols, widget.TableColumn[int]{Title: d.title, Width: d.width, Cell: cell(d.role, 0), Elide: d.elide})
		}
		return cols
	}
	count := 0
	if n.mv.model != nil {
		count = n.mv.model.ColumnCount(nil)
	}
	for c := 0; c < count; c++ {
		cols = append(cols, widget.TableColumn[int]{Title: n.mv.model.HeaderData(c), Cell: cell("", c)})
	}
	return cols
}

// follow applies a model change: new columns when the model's changed and
// none are declared, and the rows every time.
// sourcesChanged repaints the visible cells when a delegate may read a source (sourceFollower): the
// engine resolves them at paint time, while the List keeps the viewport and focus.
func (n *tableViewNode) sourcesChanged() {
	if n.template != nil {
		n.table.List().MarkDirty()
	}
}

func (n *tableViewNode) follow(c Change) {
	key, had := n.mv.keyAt(n.currentIndex())
	defer func() {
		if at := n.mv.reshow(key, had); at >= 0 {
			n.table.List().SetCursor(at)
		}
	}()
	if len(n.declared) == 0 && (c.Kind == Reset || c.Kind == ColumnsReset) {
		n.table.SetColumns(n.columns())
	} else {
		n.table.List().RefreshSource()
	}
}

func (n *tableViewNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.shown.component()) // a list's rows report to the TableView itself (WithListOnActivate and siblings)
}

// inView is a point in the row list's cells in the TableView's own: under its header, wherever the
// table placed the list. A list not laid out yet is at the view's origin.
func (n *tableViewNode) inView(list tui.Component, at tui.Point) tui.Point {
	lc, ok := list.(interface{ Context() *tui.Context })
	if !ok || lc.Context() == nil {
		return at
	}
	r, ok := n.Context().ResolveAnchor(lc.Context().NodeAnchor())
	if !ok {
		return at
	}
	return tui.Point{X: at.X + r.X, Y: at.Y + r.Y}
}

func (n *tableViewNode) Layout(c tui.Constraints) tui.Size {
	child := n.shown.component()
	sz := n.Context().LayoutChild(child, c)
	n.Context().PlaceChild(child, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (*tableViewNode) Render(tui.Surface)         {}
func (*tableViewNode) HandleEvent(tui.Event) bool { return false }

func (n *tableViewNode) currentIndex() int { return n.shown.current() }

var tableViewType = Type{
	Name:  "TableView",
	Build: buildTableView,
	Ctor:  []string{"delegate", "cellCursor", "sortable"},
	restyle: func(c tui.Component, p palette) {
		n := c.(*tableViewNode)
		n.normal = style.Default()
		if text, ok := p[roleText]; ok {
			n.normal = text
		}
		n.shown.restyle(p)
	},
	Setters: map[string]Setter{
		"model": rawSetter(func(n *tableViewNode, v qml.SpecValue) error {
			m, err := modelOf(v)
			if err != nil {
				return err
			}
			if err := n.checkTemplate(m); err != nil {
				return err
			}
			n.mv.setModel(m)
			return nil
		}),
		// golib's: each row's number in a gutter before the first field
		"rowNumbers": cellsSetter("rowNumbers", boolOf, func(n *tableViewNode, on bool) {
			n.grid.Core().SetRowNumbers(on)
		}),
		// golib's: what y copies, "tsv" (the default) or "csv" (widget.TableCopyAs)
		"copyFormat": cellsSetter("copyFormat", copyFormats.read, func(n *tableViewNode, f widget.TableCopyFormat) {
			n.grid.Core().SetCopyFormat(f)
		}),
		// golib's: the cursor covers its whole row (r toggles it): y copies the row
		"rowCursor": cellsSetter("rowCursor", boolOf, func(n *tableViewNode, on bool) {
			n.grid.Core().SetRowCursor(on)
		}),
		// golib's: the sort of the loaded rows as ORDER BY text, `name [ASC|DESC], …`; "" none
		"orderBy": cellsSetter("orderBy", stringOf, (*tableViewNode).setOrderBy),
		// golib's: the loaded rows shown, as WHERE text over the fields; "" every row
		"filter": cellsSetter("filter", stringOf, (*tableViewNode).setFilter),
		// golib's: a right press opens the menu whose rows the host gives (TableOf(c).Menu().SetRows)
		"contextMenu": cellsSetter("contextMenu", boolOf, func(n *tableViewNode, on bool) {
			if n.grid.Menu() == nil {
				n.grid.SetMenu(widget.NewEditorMenu(n.grid, nil))
			}
			n.grid.Menu().SetEnabled(on)
		}),
		"currentIndex": setter("a TableView", truncOf, func(n *tableViewNode, v int) { n.shown.setCurrent(v) }),
	},
	Getters: map[string]Getter{
		"currentIndex": getter(func(n *tableViewNode) (qml.SpecValue, error) {
			return numberValue(n.currentIndex()), nil
		}),
		"orderBy": getter(func(n *tableViewNode) (qml.SpecValue, error) {
			return strValue(n.orderBy), nil
		}),
		"orderByError": getter(func(n *tableViewNode) (qml.SpecValue, error) {
			return strValue(n.orderErr), nil
		}),
		"filter": getter(func(n *tableViewNode) (qml.SpecValue, error) {
			return strValue(n.filter), nil
		}),
		"filterError": getter(func(n *tableViewNode) (qml.SpecValue, error) {
			return strValue(n.filterErr), nil
		}),
	},
	Signals: map[string][]string{"activated": {"index"}, "currentIndexChanged": {"index"},
		// a right press on a row (the cursor moved to it first), or Shift+F10 or the Menu key on
		// the cursor row; index -1 below the rows; x, y in the view's own cells, where a menu opens
		"contextMenuRequested": {"index", "x", "y"},
		// golib's: the view changed its sort (the s key), and wrote it to orderBy
		"orderByChanged": {"orderBy"}},
	Destroyed: destroyer(func(n *tableViewNode) { n.mv.release() }),
}

var tableViewColumnType = Type{
	Name:  "TableViewColumn",
	Build: buildTableViewColumn,
	Ctor:  []string{"role", "title", "width", "elideMode"},
}

// copyFormats are a cell-cursor TableView's copyFormat values.
var copyFormats = enum[widget.TableCopyFormat]{values: map[string]widget.TableCopyFormat{"Tsv": widget.TableCopyTSV, "Csv": widget.TableCopyCSV}}
