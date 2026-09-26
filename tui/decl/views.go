package decl

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/parse/js"
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

func numberValue(i int) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.Itoa(i)}
}

func boolValue(b bool) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueBool, Raw: strconv.FormatBool(b)}
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
	consumed, err := readProps(b.Props, map[string]field{"textRole": into(&n.mv.textRole, stringOf)})
	if err != nil {
		return nil, nil, err
	}
	n.list = widget.NewList(widget.WithSource[int](modelSource{&n.mv}, n.mv.text))
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

func (n *listViewNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.list)
	tui.SubscribeScoped(ctx, func(ev widget.ActivateEvent) {
		if ev.Owner == n.list.NodeID() {
			n.activated(numberValue(ev.Index))
		}
	})
	tui.SubscribeScoped(ctx, func(ev widget.SelectionChangedEvent) {
		if ev.Owner == n.list.NodeID() {
			n.moved(numberValue(ev.Index))
		}
	})
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
		"currentIndex": setter("a ListView", numberOf, func(n *listViewNode, v float64) { n.list.SetCursor(int(v)) }),
	},
	Getters: map[string]Getter{
		"currentIndex": func(c tui.Component) (qml.SpecValue, error) {
			return numberValue(c.(*listViewNode).currentIndex()), nil
		},
	},
	Signals:   map[string][]string{"activated": {"index"}, "currentIndexChanged": {"index"}},
	Destroyed: func(c tui.Component) { c.(*listViewNode).mv.release() },
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
	consumed, err := readProps(b.Props, map[string]field{
		"textRole":        into(&n.mv.textRole, stringOf),
		"valueRole":       into(&n.valueRole, stringOf),
		"placeholderText": into(&placeholder, stringOf),
	})
	if err != nil {
		return nil, nil, err
	}
	var opts []widget.SelectOption[int]
	if placeholder != "" {
		opts = append(opts, widget.WithSelectPlaceholder[int](placeholder))
	}
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
	ctx.Mount(n.sel)
	tui.SubscribeScoped(ctx, func(ev widget.SelectionChangedEvent) {
		if ev.Owner == n.sel.NodeID() {
			n.activated(numberValue(ev.Index))
		}
	})
}

func (n *comboBoxNode) Layout(c tui.Constraints) tui.Size {
	sz := n.Context().LayoutChild(n.sel, c)
	n.Context().PlaceChild(n.sel, tui.Rect{W: sz.W, H: sz.H})
	return sz
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
	Ctor:  []string{"textRole", "valueRole", "placeholderText"},
	Setters: map[string]Setter{
		"model": setter("a ComboBox", modelOf, func(n *comboBoxNode, m ItemModel) { n.mv.setModel(m) }),
		// Qt's writable currentIndex: the row chosen, -1 for none. A row the
		// model does not have chooses none.
		"currentIndex": setter("a ComboBox", numberOf, func(n *comboBoxNode, v float64) { n.sel.SetSelectedIndex(int(v)) }),
	},
	Getters: map[string]Getter{
		"currentIndex": func(c tui.Component) (qml.SpecValue, error) {
			return numberValue(c.(*comboBoxNode).currentIndex()), nil
		},
		// The chosen row's valueRole — Qt's currentValue; a string "" when
		// nothing is chosen.
		"currentValue": func(c tui.Component) (qml.SpecValue, error) {
			n := c.(*comboBoxNode)
			i := n.currentIndex()
			if i < 0 || n.mv.model == nil {
				return qml.SpecValue{Kind: qml.SpecValueString}, nil
			}
			v := n.mv.model.Data(Index{Row: i}, n.valueRole)
			if v.Kind == qml.SpecValueInvalid {
				return qml.SpecValue{Kind: qml.SpecValueString}, nil
			}
			return v, nil
		},
	},
	Signals:   map[string][]string{"activated": {"index"}},
	Destroyed: func(c tui.Component) { c.(*comboBoxNode).mv.release() },
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
	template  *qml.SpecNode
	eval      func(qml.SpecValue, map[string]qml.SpecValue) (qml.SpecValue, error)
	normal    style.Color
	sink      func(error)
}

// tableColumnNode is a declared TableViewColumn: what the column shows.
type tableColumnNode struct {
	widget.Base
	role, title string
	width       int
}

func (*tableColumnNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*tableColumnNode) declarationOnly()                  {}
func (*tableColumnNode) Render(tui.Surface)                {}

func buildTableViewColumn(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a TableViewColumn takes no children (at %s)", b.Pos)
	}
	n := &tableColumnNode{}
	consumed, err := readProps(b.Props, map[string]field{
		"role":  into(&n.role, stringOf),
		"title": into(&n.title, stringOf),
		"width": into(&n.width, cellsOf),
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
	n := &tableViewNode{activated: b.EmitterWith("activated"), moved: b.EmitterWith("currentIndexChanged")}
	var consumed []string
	for _, prop := range b.Props {
		if prop.Name != "delegate" {
			continue
		}
		if prop.Value.Kind != qml.SpecValueTemplate || prop.Value.Template == nil {
			return nil, nil, fmt.Errorf("TableView.delegate takes a Text object template (at %s)", prop.Pos)
		}
		n.template = prop.Value.Template
		if n.template.Type != "Text" || n.template.ID != "" || len(n.template.Children) != 0 || len(n.template.Handlers) != 0 {
			return nil, nil, fmt.Errorf("TableView.delegate supports stateless Text only, without ids, handlers or children (at %s)", n.template.Pos)
		}
		seen := map[string]bool{}
		for _, p := range n.template.Props {
			if (p.Name != "text" && p.Name != "color") || seen[p.Name] {
				return nil, nil, fmt.Errorf("TableView.delegate Text accepts one text and optional color, not %q (at %s)", p.Name, p.Pos)
			}
			seen[p.Name] = true
		}
		if !seen["text"] {
			return nil, nil, fmt.Errorf("TableView.delegate Text needs text (at %s)", n.template.Pos)
		}
		consumed = append(consumed, "delegate")
	}
	n.eval, n.normal, n.sink = b.Eval, style.Default(), b.sink
	for _, c := range b.Children {
		col, ok := c.(*tableColumnNode)
		if !ok {
			return nil, nil, fmt.Errorf("a TableView holds TableViewColumns only; its rows are its model's (at %s)", b.Pos)
		}
		n.declared = append(n.declared, col)
	}
	n.table = widget.NewTable(n.columns(), widget.WithSource[int](modelSource{&n.mv}, func(int) string { return "" }))
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
	locals := map[string]qml.SpecValue{
		"row": numberValue(0), "column": numberValue(0),
		"model.display": {Kind: qml.SpecValueString, Raw: ""},
		"palette.text":  {Kind: qml.SpecValueObject, Obj: n.normal},
	}
	known := map[string]bool{}
	if m != nil {
		for _, r := range m.Roles() {
			known[r] = true
		}
	}
	for _, p := range n.template.Props {
		register := func(role string) error {
			if role == "display" {
				return nil
			}
			if m != nil && !known[role] {
				return fmt.Errorf("TableView.delegate: unknown model role %q (at %s)", role, p.Pos)
			}
			locals["model."+role] = qml.SpecValue{Kind: qml.SpecValueString, Raw: "normal"}
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
		v, err := n.eval(p.Value, locals)
		if err != nil {
			return fmt.Errorf("TableView.delegate %s (at %s): %w", p.Name, p.Pos, err)
		}
		if p.Name == "color" {
			if _, err := colorOf(v); err != nil {
				return err
			}
		}
	}
	return nil
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
		"model.display": n.mv.model.Data(ix, role),
		"palette.text":  {Kind: qml.SpecValueObject, Obj: n.normal},
	}
	if locals["model.display"].Kind == qml.SpecValueInvalid {
		locals["model.display"] = qml.SpecValue{Kind: qml.SpecValueString}
	}
	for _, name := range n.mv.model.Roles() {
		if name != "display" {
			v := n.mv.model.Data(ix, name)
			if v.Kind == qml.SpecValueInvalid {
				v = qml.SpecValue{Kind: qml.SpecValueString}
			}
			locals["model."+name] = v
		}
	}
	cell := widget.StyledCell{}
	for _, p := range n.template.Props {
		v, err := n.eval(p.Value, locals)
		if err != nil {
			if n.sink != nil {
				n.sink(err)
			}
			continue
		}
		switch p.Name {
		case "text":
			cell.Text = v.Raw
		case "color":
			c, err := colorOf(v)
			if err != nil {
				if n.sink != nil {
					n.sink(err)
				}
				continue
			}
			cell.Style = cell.Style.Foreground(c)
		}
	}
	return cell
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
			cols = append(cols, widget.TableColumn[int]{Title: d.title, Width: d.width, Cell: cell(d.role, 0)})
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
	ctx.Mount(n.table)
	list := n.table.List()
	tui.SubscribeScoped(ctx, func(ev widget.ActivateEvent) {
		if ev.Owner == list.NodeID() {
			n.activated(numberValue(ev.Index))
		}
	})
	tui.SubscribeScoped(ctx, func(ev widget.SelectionChangedEvent) {
		if ev.Owner == list.NodeID() {
			n.moved(numberValue(ev.Index))
		}
	})
}

func (n *tableViewNode) Layout(c tui.Constraints) tui.Size {
	sz := n.Context().LayoutChild(n.table, c)
	n.Context().PlaceChild(n.table, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (*tableViewNode) Render(tui.Surface)         {}
func (*tableViewNode) HandleEvent(tui.Event) bool { return false }

func (n *tableViewNode) currentIndex() int {
	if i, ok := n.table.Selected(); ok {
		return i
	}
	return -1
}

var tableViewType = Type{
	Name:  "TableView",
	Build: buildTableView,
	Ctor:  []string{"delegate"},
	restyle: func(c tui.Component, p palette) {
		n := c.(*tableViewNode)
		n.normal = style.Default()
		if text, ok := p[roleText]; ok {
			n.normal = text
		}
		n.table.List().MarkDirty()
	},
	Setters: map[string]Setter{
		"model": func(c tui.Component, v qml.SpecValue) error {
			m, err := modelOf(v)
			if err != nil {
				return err
			}
			n := c.(*tableViewNode)
			if err := n.checkTemplate(m); err != nil {
				return err
			}
			n.mv.setModel(m)
			return nil
		},
		"currentIndex": setter("a TableView", numberOf, func(n *tableViewNode, v float64) { n.table.List().SetCursor(int(v)) }),
	},
	Getters: map[string]Getter{
		"currentIndex": func(c tui.Component) (qml.SpecValue, error) {
			return numberValue(c.(*tableViewNode).currentIndex()), nil
		},
	},
	Signals:   map[string][]string{"activated": {"index"}, "currentIndexChanged": {"index"}},
	Destroyed: func(c tui.Component) { c.(*tableViewNode).mv.release() },
}

var tableViewColumnType = Type{
	Name:  "TableViewColumn",
	Build: buildTableViewColumn,
	Ctor:  []string{"role", "title", "width"},
}
