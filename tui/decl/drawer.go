package decl

import (
	"errors"
	"strconv"

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
//	    edge: Tui.Left      // Tui.Right, Tui.Top, Tui.Bottom, Tui.Center; Qt spells it Qt.LeftEdge
//	    size: 30            // a percentage of the Window across the edge
//	    length: 85          // golib's: a percentage along it, centred (100, the default: all of it)
//	    Frame { title: "explorer"; TreeView { … } }
//	}
//
// Tui.Center is golib's: the panel floats in the middle of the Window, length wide and size high.
//
// RESIZABLE (golib's; Qt's Drawer has none): resizable: true puts a grip, a square on the corner at
// the panel's inner edge (the top right of a bottom drawer, the bottom right of a left or top one or
// a centred one, the bottom left of a right one), that the pointer drags to resize the panel across
// its edge and along it. size and length follow the drag as percentages of the Window, kept
// between minimumSize (default 10) and 90, and minimumLength (default 20) and 100, and never under
// three cells; resized(size, length) is raised once, when the drag ends, for a host to keep them.
//
// It holds the keyboard while open, as Qt's modal Drawer does, and gives it back where it was
// when it closes; Escape closes it. modal: false (Qt's too) lets the keyboard go back to the page
// while it stays open, a panel beside the work rather than a question over it. edge, size and length are settable while the program runs: a
// preference can move it. open(), close() and toggle() (golib's: open when closed, else close),
// and opened() and closed().

// drawerNode is a Drawer: its Float, the overlay it is on, and where it opens from.
type drawerNode struct {
	widget.Base
	float   *widget.Float
	frame   *drawerFrame
	host    *widget.OverlayHost
	edge    tui.DockEdge
	size    int
	length  int
	opened  func()
	closed  func()
	content tui.Component
	// resizable: the content is in grip, and the frame sizes it from size and length
	resizable          bool
	grip               *widget.Resizable
	minSize, minLength int
	resized            func(args ...qml.SpecValue)
	win                tui.Size // the Window the frame last laid the panel out in
}

var _ Overlaid = (*drawerNode)(nil)

// defaultDrawerSize is a Drawer's size when its document gives none: a third of the Window.
const defaultDrawerSize = 33

func buildDrawer(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, errors.New("a Drawer holds exactly one child, its content")
	}
	modal, resizable := true, false
	consumed, err := readProps(b.Props, map[string]field{"modal": into(&modal, boolOf), "resizable": into(&resizable, boolOf)})
	if err != nil {
		return nil, nil, err
	}
	n := &drawerNode{edge: tui.DockLeft, size: defaultDrawerSize, length: 100, opened: b.Emitter("opened"), closed: b.Emitter("closed"),
		content: b.Children[0], resizable: resizable, minSize: 10, minLength: 10, resized: b.EmitterWith("resized")}
	if resizable { // a drag stops a resizable panel short of a sliver along its edge
		n.minLength = 20
	}
	n.frame = &drawerFrame{owner: n}
	n.frame.Label("Drawer")
	n.frame.Add(n.wrapped())
	n.float = widget.NewFloat(n.frame, widget.WithModal(modal))
	n.place()
	if b.Overlay != nil {
		n.SetOverlay(b.Overlay, nil)
	}
	return n, consumed, nil
}

// drawerGrip is a resizable Drawer's corner grip.
const drawerGrip = "□"

// wrapped is the content as the frame holds it: in a grip at the edge's inner corner when the Drawer
// is resizable, itself otherwise. A grip is made for the edge it is at, so a new edge makes a new one.
func (n *drawerNode) wrapped() tui.Component {
	if !n.resizable {
		return n.content
	}
	// a hollow square at the corner, the same in a terminal and a window (a full block is a cell
	// tall, not square; a corner-pointing triangle read as a stray glyph against the frame)
	handle := widget.HandleBottomRight
	switch n.edge {
	case tui.DockRight:
		handle = widget.HandleBottomLeft
	case tui.DockBottom:
		handle = widget.HandleTopRight
	}
	n.grip = widget.NewResizable(n.content, widget.WithHandles(handle), widget.WithHandleGlyph(drawerGrip),
		widget.WithResizeEnd(n.dragEnded))
	return n.grip
}

// dragEnded keeps where the drag left the panel, as percentages of the Window, and raises resized
// with them. A drag cancelled (Escape) ends here never: size and length were never touched, and the
// panel lays out at them again.
func (n *drawerNode) dragEnded(final tui.Size) {
	n.size, n.length = n.percentOf(final, n.win)
	if n.frame.ctx != nil {
		n.frame.ctx.RequestLayout()
	}
	n.resized(qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.Itoa(n.size)},
		qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.Itoa(n.length)})
}

// across reports whether the edge's size is a width (a left or right drawer) rather than a height.
func (n *drawerNode) across() bool { return n.edge == tui.DockLeft || n.edge == tui.DockRight }

// cells is a panel of size and length in a Window of win: size across the edge and length along
// it, rounded to the nearest cell, each at least three. Rounding both ways (here and percentOf)
// keeps a one-cell drag one cell where a percent is at most a cell (a Window of 100 cells or fewer
// across); on a wider one the panel moves in steps of a percent.
func (n *drawerNode) cells(size, length int, win tui.Size) tui.Size {
	w, h := (win.W*length+50)/100, (win.H*size+50)/100
	if n.across() {
		w, h = (win.W*size+50)/100, (win.H*length+50)/100
	}
	return tui.Size{W: min(max(w, 3), win.W), H: min(max(h, 3), win.H)}
}

// percentOf is the size and length of a panel sz in a Window of win, rounded, within the Drawer's
// bounds; the ones it has when win is not known.
func (n *drawerNode) percentOf(sz, win tui.Size) (size, length int) {
	if win.W <= 0 || win.H <= 0 {
		return n.size, n.length
	}
	pct := func(v, of int) int { return (v*200 + of) / (2 * of) }
	across, along := pct(sz.H, win.H), pct(sz.W, win.W)
	if n.across() {
		across, along = pct(sz.W, win.W), pct(sz.H, win.H)
	}
	return min(max(across, n.minSize), 90), min(max(along, n.minLength), 100)
}

// place anchors the Float at the edge, the size across it and the length along it, centred. A
// resizable Drawer's Float is sized by its content, which the frame sizes from size and length, so
// a drag can change them.
func (n *drawerNode) place() {
	if n.resizable {
		n.placeAnchor()
		n.float.SetSizeFraction(0, 0)
		return
	}
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
	case tui.DockCenter:
		n.float.SetAnchor(widget.Center)
		n.float.SetSizeFraction(n.length, n.size)
	default:
		n.float.SetAnchor(widget.Left)
		n.float.SetSizeFraction(n.size, n.length)
	}
}

// placeAnchor anchors a resizable Drawer's Float at its edge.
func (n *drawerNode) placeAnchor() {
	switch n.edge {
	case tui.DockRight:
		n.float.SetAnchor(widget.Right)
	case tui.DockTop:
		n.float.SetAnchor(widget.Top)
	case tui.DockBottom:
		n.float.SetAnchor(widget.Bottom)
	case tui.DockCenter:
		n.float.SetAnchor(widget.Center)
	default:
		n.float.SetAnchor(widget.Left)
	}
}

// setEdge is Drawer.edge's setter. A resizable Drawer's grip moves to the new edge's inner corner.
func (n *drawerNode) setEdge(e tui.DockEdge) {
	moved := e != n.edge
	n.edge = e
	if moved && n.resizable && n.grip != nil {
		n.frame.Remove(n.grip)
		n.frame.Add(n.wrapped())
	}
	n.place()
}

// setMinimumSize and setMinimumLength are a resizable Drawer's bounds, percentages of the Window.
func (n *drawerNode) setMinimumSize(pct int) {
	n.minSize = min(max(pct, 1), 90)
	n.size = max(n.size, n.minSize)
	n.place()
}

func (n *drawerNode) setMinimumLength(pct int) {
	n.minLength = min(max(pct, 1), 100)
	n.length = max(n.length, n.minLength)
	n.place()
}

// setSize is Drawer.size's setter: a percentage, minimumSize (10 unless set) to 90.
func (n *drawerNode) setSize(pct int) {
	n.size = min(max(pct, n.minSize), 90)
	n.place()
}

// setLength is Drawer.length's setter: a percentage of the Window along the edge, 10 to 100,
// centred there. Qt sizes a Drawer along its edge by its height (or width) and places it by y (or
// x); golib's panels are sized as fractions of the Window, so this is the one number.
func (n *drawerNode) setLength(pct int) {
	n.length = min(max(pct, n.minLength), 100)
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

// Layout gives the one child all the Float offers: a drawer is its whole size. A resizable Drawer's
// Float offers the whole Window, and the frame takes the panel's size from it: from the drag in
// progress when there is one, within the Drawer's bounds, else from size and length.
func (f *drawerFrame) Layout(c tui.Constraints) tui.Size {
	sz := c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
	if n := f.owner; n.resizable && n.grip != nil {
		n.win = sz
		size, length := n.size, n.length
		if n.grip.Dragging() { // shown where the drag is, kept only when it ends
			if want, ok := n.grip.RequestedSize(); ok {
				size, length = n.percentOf(want, sz)
			}
		}
		sz = n.cells(size, length, sz)
	}
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
	Name:    "Drawer",
	Build:   buildDrawer,
	Ctor:    []string{"modal", "resizable"},
	Signals: map[string][]string{"resized": {"size", "length"}},
	Setters: map[string]Setter{
		"edge": setter("a Drawer", drawerEdges.read, (*drawerNode).setEdge),
		"size": setter("a Drawer", func(v qml.SpecValue) (int, error) {
			f, err := numberOf(v)
			return int(f), err
		}, (*drawerNode).setSize),
		"length": setter("a Drawer", func(v qml.SpecValue) (int, error) {
			f, err := numberOf(v)
			return int(f), err
		}, (*drawerNode).setLength),
		"minimumSize": setter("a Drawer", func(v qml.SpecValue) (int, error) {
			f, err := numberOf(v)
			return int(f), err
		}, (*drawerNode).setMinimumSize),
		"minimumLength": setter("a Drawer", func(v qml.SpecValue) (int, error) {
			f, err := numberOf(v)
			return int(f), err
		}, (*drawerNode).setMinimumLength),
	},
	Methods: map[string]Method{
		"open":   NoArgMethod((*drawerNode).open),
		"close":  NoArgMethod((*drawerNode).close),
		"toggle": NoArgMethod((*drawerNode).toggle),
	},
	Destroyed: func(c tui.Component) { c.(*drawerNode).detach() },
}
