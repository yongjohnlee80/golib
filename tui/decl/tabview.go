package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// ---------------------------------------------------------------- TabView

// TabView is Qt Quick Controls 1's TabView: a bar of its Tabs' titles over the
// current Tab's content, one at a time.
//
//	TabView {
//	    Tab { title: "Edit"; Flex { … } }
//	    Tab { title: "Advanced"; Flex { … } }
//	}
//
// As in Qt, a Tab's content is made once, when it is first current, and kept: a
// Tab that is not current is not laid out, so it is neither painted nor given
// focus, but it is never destroyed, and what was typed in it is still there when
// a handler reads it from another tab. Ctrl+PageUp and Ctrl+PageDown change tabs from
// anywhere inside the view; with the bar focused, ←/→ and [ ] do, and a click
// on a title does.
type tabViewNode struct {
	widget.Base
	tabs    *widget.Tabs
	pages   []*tabNode
	changed func(args ...qml.SpecValue)
}

// tabNode is a declared Tab: a title and the one item under it.
type tabNode struct {
	widget.Base
	title   string
	content tui.Component
}

func buildTab(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, fmt.Errorf("a Tab needs exactly 1 child, its content, got %d (at %s)", len(b.Children), b.Pos)
	}
	n := &tabNode{content: b.Children[0]}
	consumed, err := readProps(b.Props, map[string]field{"title": into(&n.title, stringOf)})
	return n, consumed, err
}

func (n *tabNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.content)
}

func (n *tabNode) Layout(c tui.Constraints) tui.Size {
	sz := n.Context().LayoutChild(n.content, c)
	n.Context().PlaceChild(n.content, tui.Rect{W: sz.W, H: sz.H})
	return c.Constrain(sz)
}

func (*tabNode) Render(tui.Surface)         {}
func (*tabNode) HandleEvent(tui.Event) bool { return false }

func buildTabView(b Build) (tui.Component, []string, error) {
	if len(b.Children) == 0 {
		return nil, nil, fmt.Errorf("a TabView needs at least one Tab (at %s)", b.Pos)
	}
	n := &tabViewNode{changed: b.EmitterWith("currentIndexChanged")}
	var opts []widget.TabsOption
	for _, c := range b.Children {
		page, ok := c.(*tabNode)
		if !ok {
			return nil, nil, fmt.Errorf("a TabView holds Tabs only (at %s)", b.Pos)
		}
		n.pages = append(n.pages, page)
		opts = append(opts, widget.WithTab(page.title, page))
	}
	n.tabs = widget.NewTabs(append(opts, widget.WithKeepMounted(true))...)
	return n, nil, nil
}

func (n *tabViewNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.tabs)
	tui.SubscribeScoped(ctx, func(ev widget.TabChangedEvent) {
		if ev.Owner == n.tabs.NodeID() {
			n.changed(numberValue(ev.Index))
		}
	})
}

func (n *tabViewNode) Layout(c tui.Constraints) tui.Size {
	sz := n.Context().LayoutChild(n.tabs, c)
	n.Context().PlaceChild(n.tabs, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (*tabViewNode) Render(tui.Surface)         {}
func (*tabViewNode) HandleEvent(tui.Event) bool { return false }

// restyleTabView wears the palette on the bar: the window's colours behind the
// titles, each title as a button, the current one highlighted.
func restyleTabView(c tui.Component, p palette) {
	c.(*tabViewNode).tabs.SetStyles(p.look(roleWindow, roleWindowText), p.look(roleButton, roleButtonText),
		p.look(roleHighlight, roleHighlightedText))
}

var tabViewType = Type{
	Name:    "TabView",
	Build:   buildTabView,
	restyle: restyleTabView,
	Setters: map[string]Setter{
		// Qt's: the index of the current tab; one out of range is ignored
		"currentIndex": setter("a TabView", numberOf, func(n *tabViewNode, v float64) { n.tabs.Select(int(v)) }),
	},
	Getters: map[string]Getter{
		"currentIndex": func(c tui.Component) (qml.SpecValue, error) {
			return numberValue(c.(*tabViewNode).tabs.Active()), nil
		},
		"count": func(c tui.Component) (qml.SpecValue, error) {
			return numberValue(len(c.(*tabViewNode).pages)), nil
		},
	},
	Signals: map[string][]string{"currentIndexChanged": {"index"}},
}

var tabType = Type{
	Name:  "Tab",
	Build: buildTab,
	Ctor:  []string{"title"},
}
