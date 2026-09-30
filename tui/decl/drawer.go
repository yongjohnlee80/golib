package decl

import (
	"errors"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// DRAWERS — Qt Quick Controls' Drawer: a panel that opens over the Window from one of its edges,
// and closes back into it. It takes no place in the layout, so opening one moves nothing beneath:
// an editor under it stays where it is.
//
//	Drawer {
//	    id: explorer
//	    edge: Tui.Left      // Tui.Right, Tui.Top, Tui.Bottom; Qt spells it Qt.LeftEdge
//	    size: 30            // a percentage of the Window across the edge
//	    length: 85          // golib's: a percentage along it, centred (100, the default: all of it)
//	    Frame { title: "explorer"; TreeView { … } }
//	}
//
// It holds the keyboard while open, as Qt's modal Drawer does, and gives it back where it was
// when it closes; Escape closes it. modal: false (Qt's too) lets the keyboard go back to the page
// while it stays open, a panel beside the work rather than a question over it. edge, size and length are settable while the program runs: a
// preference can move it. open(), close() and toggle() (golib's: open when closed, else close),
// and opened() and closed().

// drawerNode is a Drawer: its Float, the overlay it is on, and where it opens from.
type drawerNode struct {
	widget.Base
	float  *widget.Float
	frame  *drawerFrame
	host   *widget.OverlayHost
	edge   tui.DockEdge
	size   int
	length int
	opened func()
	closed func()
}

var _ Overlaid = (*drawerNode)(nil)

// defaultDrawerSize is a Drawer's size when its document gives none: a third of the Window.
const defaultDrawerSize = 33

func buildDrawer(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, errors.New("a Drawer holds exactly one child, its content")
	}
	modal := true
	consumed, err := readProps(b.Props, map[string]field{"modal": into(&modal, boolOf)})
	if err != nil {
		return nil, nil, err
	}
	n := &drawerNode{edge: tui.DockLeft, size: defaultDrawerSize, length: 100, opened: b.Emitter("opened"), closed: b.Emitter("closed")}
	n.frame = &drawerFrame{owner: n}
	n.frame.Label("Drawer")
	n.frame.Add(b.Children[0])
	n.float = widget.NewFloat(n.frame, widget.WithModal(modal))
	n.place()
	if b.Overlay != nil {
		n.SetOverlay(b.Overlay, nil)
	}
	return n, consumed, nil
}

// place anchors the Float at the edge, the size across it and the length along it, centred.
func (n *drawerNode) place() {
	switch n.edge {
	case tui.DockRight:
		n.float.SetAnchor(widget.Right)
		n.float.SetSizeFraction(n.size, n.length)
	case tui.DockTop:
		n.float.SetAnchor(widget.Top)
		n.float.SetSizeFraction(n.length, n.size)
	case tui.DockBottom:
		n.float.SetAnchor(widget.Bottom)
		n.float.SetSizeFraction(n.length, n.size)
	default:
		n.float.SetAnchor(widget.Left)
		n.float.SetSizeFraction(n.size, n.length)
	}
}

// setEdge is Drawer.edge's setter.
func (n *drawerNode) setEdge(e tui.DockEdge) {
	n.edge = e
	n.place()
}

// setSize is Drawer.size's setter: a percentage, 10 to 90.
func (n *drawerNode) setSize(pct int) {
	n.size = min(max(pct, 10), 90)
	n.place()
}

// setLength is Drawer.length's setter: a percentage of the Window along the edge, 10 to 100,
// centred there. Qt sizes a Drawer along its edge by its height (or width) and places it by y (or
// x); golib's panels are sized as fractions of the Window, so this is the one number.
func (n *drawerNode) setLength(pct int) {
	n.length = min(max(pct, 10), 100)
	n.place()
}

// SetOverlay implements Overlaid: the Drawer's Float becomes one of the host's layers, hidden
// until opened.
func (n *drawerNode) SetOverlay(host *widget.OverlayHost, _ func()) {
	if n.host == host {
		return
	}
	n.detach()
	n.host = host
	host.Attach(n.float)
}

func (n *drawerNode) detach() {
	if n.host != nil {
		n.host.Detach(n.float)
		n.host = nil
	}
}

var errDrawerOutsideWindow = errors.New("a Drawer opens over a Window, and this one is not in one; " +
	"inside a Go program, give the adapter WithOverlay(host)")

func (n *drawerNode) open() error {
	if n.host == nil {
		return errDrawerOutsideWindow
	}
	if n.float.Shown() {
		return nil
	}
	n.float.Show()
	n.opened()
	return nil
}

func (n *drawerNode) close() error {
	if n.float.Shown() {
		n.float.Hide()
		n.closed()
	}
	return nil
}

func (n *drawerNode) toggle() error {
	if n.float.Shown() {
		return n.close()
	}
	return n.open()
}

// A Drawer takes no place in the layout: it opens over it.
func (n *drawerNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (n *drawerNode) Render(tui.Surface)                {}

// drawerFrame is the Drawer's content as the Float shows it, closing on Escape while the keyboard
// is inside. A Container, so the first focusable widget in it is found when it opens.
type drawerFrame struct {
	tui.MultiChild
	ctx   *tui.Context
	owner *drawerNode
}

func (f *drawerFrame) Init(ctx *tui.Context) {
	f.ctx = ctx
	f.MultiChild.Init(ctx)
}

// Layout gives the one child all the Float offers: a drawer is its whole size.
func (f *drawerFrame) Layout(c tui.Constraints) tui.Size {
	sz := c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
	for _, child := range f.Items() {
		f.ctx.LayoutChild(child, tui.Tight(sz))
		f.ctx.PlaceChild(child, tui.Rect{W: sz.W, H: sz.H})
	}
	return sz
}

func (f *drawerFrame) Render(tui.Surface) {}

func (f *drawerFrame) HandleEvent(ev tui.Event) bool {
	k, ok := ev.(tui.KeyEvent)
	if ok && k.Kind == tui.KeyPress && k.Code == tui.KeyEscape && k.Mods.Chord() == 0 {
		_ = f.owner.close()
		return true
	}
	return false
}

// drawerType is the Drawer's entry in the standard vocabulary.
var drawerType = Type{
	Name:  "Drawer",
	Build: buildDrawer,
	Ctor:  []string{"modal"},
	Setters: map[string]Setter{
		"edge": setter("a Drawer", dockEdges.read, (*drawerNode).setEdge),
		"size": setter("a Drawer", func(v qml.SpecValue) (int, error) {
			f, err := numberOf(v)
			return int(f), err
		}, (*drawerNode).setSize),
		"length": setter("a Drawer", func(v qml.SpecValue) (int, error) {
			f, err := numberOf(v)
			return int(f), err
		}, (*drawerNode).setLength),
	},
	Methods: map[string]Method{
		"open":   NoArgMethod((*drawerNode).open),
		"close":  NoArgMethod((*drawerNode).close),
		"toggle": NoArgMethod((*drawerNode).toggle),
	},
	Destroyed: func(c tui.Component) { c.(*drawerNode).detach() },
}
