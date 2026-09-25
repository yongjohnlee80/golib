package decl

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
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
	changed  func()
}

func (v *modelView) setModel(m ItemModel) {
	v.release()
	v.model = m
	if m != nil {
		v.cancel = m.Subscribe(func(Change) { v.changed() })
	}
	v.changed()
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

// ---------------------------------------------------------------- ListView

type listViewNode struct {
	widget.Base
	modelView
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
	consumed, err := readProps(b.Props, map[string]field{"textRole": into(&n.textRole, stringOf)})
	if err != nil {
		return nil, nil, err
	}
	n.list = widget.NewList(widget.WithSource[int](modelSource{&n.modelView}, n.text))
	n.changed = n.list.RefreshSource
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
	Ctor:  []string{"textRole"},
	Setters: map[string]Setter{
		"model":        setter("a ListView", modelOf, func(n *listViewNode, m ItemModel) { n.setModel(m) }),
		"currentIndex": setter("a ListView", numberOf, func(n *listViewNode, v float64) { n.list.SetCursor(int(v)) }),
	},
	Getters: map[string]Getter{
		"currentIndex": func(c tui.Component) (qml.SpecValue, error) {
			return numberValue(c.(*listViewNode).currentIndex()), nil
		},
	},
	Signals:   map[string][]string{"activated": {"index"}, "currentIndexChanged": {"index"}},
	Destroyed: func(c tui.Component) { c.(*listViewNode).release() },
}

// ---------------------------------------------------------------- ComboBox

type comboBoxNode struct {
	widget.Base
	modelView
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
		"textRole":        into(&n.textRole, stringOf),
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
	n.changed = n.refill
	return n, consumed, nil
}

// refill gives the select its model's rows as choices.
func (n *comboBoxNode) refill() {
	items := make([]widget.SelectItem[int], n.rows())
	for i := range items {
		items[i] = widget.SelectItem[int]{Label: n.text(i), Value: i}
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
		"model": setter("a ComboBox", modelOf, func(n *comboBoxNode, m ItemModel) { n.setModel(m) }),
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
			if i < 0 || n.model == nil {
				return qml.SpecValue{Kind: qml.SpecValueString}, nil
			}
			v := n.model.Data(Index{Row: i}, n.valueRole)
			if v.Kind == qml.SpecValueInvalid {
				return qml.SpecValue{Kind: qml.SpecValueString}, nil
			}
			return v, nil
		},
	},
	Signals:   map[string][]string{"activated": {"index"}},
	Destroyed: func(c tui.Component) { c.(*comboBoxNode).release() },
}
