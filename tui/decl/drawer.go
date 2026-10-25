package decl

import (
	"errors"
	"math"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// DRAWERS — Qt Quick Controls' Drawer: a panel that opens over the Window from one of its edges,
// and closes back into it. It takes no place in the layout, so opening one moves nothing beneath:
// an editor under it stays where it is. reserve: true (golib's) makes a docked one take its strip
// from the page instead (drawer_reserve.go).
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
// Its Shortcuts, declared beside its one content item, are live while focus is inside it, before
// the Window's (see FocusScope); a Shortcut may claim Escape or q from the Drawer's own closing.
//
// It holds the keyboard while open, as Qt's modal Drawer does, and gives it back where it was
// when it closes; Escape closes it. closeOnQ: true also closes on q when its focused child leaves
// that key unhandled; the default is false so a terminal can type q. modal: false (Qt's too) lets
// the keyboard go back to the page while it stays open. edge, size and length are settable while a
// preference can move it. open(), close() and toggle() (golib's: open when closed, else close),
// and opened() and closed().

// drawerNode is a Drawer: its Float, the overlay it is on (OverlayLayer), and where it opens from.
type drawerNode struct {
	widget.Base
	OverlayLayer
	float   *widget.Float
	frame   *drawerFrame
	edge    tui.DockEdge
	size    int
	length  int
	opened  func()
	closed  func()
	content tui.Component
	// shortcuts are the Drawer's own keys, live while focus is inside it
	shortcuts []*shortcutNode
	// keyScope is the key-map scope of its keyId Shortcuts.
	keyScope keymap.Scope
	// resizable: the content is in grip, and the frame sizes it from size and length
	resizable          bool
	grip               *widget.Resizable
	minSize, minLength int
	resized            func(args ...qml.SpecValue)
	win                tui.Size // the Window the frame last laid the panel out in
	windowResize       bool
	windowConfig       windowModConfig
	windowCollector    widget.WindowCollector
	windowEmit         func(...qml.SpecValue)
	windowTarget       *drawerWindowTarget
	windowMods         *widget.WindowMod
	modal              bool
	closeOnQ           bool
	// movable: a move or resize floats it at frac, percentages of the Window (x, y, w, h), until
	// floating is set false; placed(x, y, width, height) reports where each move or resize ended
	movable  bool
	floating bool
	frac     [4]float64
	placed   func(args ...qml.SpecValue)
	// reserve: docked and shown, it takes its strip of the Window from the page (reserving), its
	// Float over the strip its Window's Dock placed for it (slot, from reserveSlot)
	reserve bool
	slot    tui.Rect
	onEdge  func(tui.DockEdge) // the Window's: the strip follows a new edge
}

var _ Overlaid = (*drawerNode)(nil)

// defaultDrawerSize is a Drawer's size when its document gives none: a third of the Window.
const defaultDrawerSize = 33

func buildDrawer(b Build) (tui.Component, []string, error) {
	shortcuts, rest := splitShortcuts(b.Children)
	content, err := oneContent(rest, "Drawer", b.Pos)
	if err != nil {
		return nil, nil, err
	}
	modal, resizable, reserve := true, false, false
	var keyScope keymap.Scope
	closeOnQ := false
	// the window controls are a WindowMod's (windowModFields), but windowResize turns resizing on:
	// a Drawer's resizable is its grip
	wc := newWindowModConfig()
	fields := windowModFields(&wc)
	fields["windowResize"] = into(&wc.resize, boolOf)
	fields["modal"], fields["closeOnQ"], fields["resizable"] = into(&modal, boolOf), into(&closeOnQ, boolOf), into(&resizable, boolOf)
	fields["keyScope"], fields["reserve"] = into(&keyScope, scopeOf), into(&reserve, boolOf)
	consumed, err := b.ReadProps(fields)
	if err != nil {
		return nil, nil, err
	}
	windowResize, movable := wc.resize, wc.move
	if wc.minimize && (modal || b.WindowCollector == nil) {
		return nil, nil, errors.New("minimizable Drawer requires modal: false and WithWindowCollector")
	}
	if movable && windowResize && wc.moveButton == wc.resizeButton {
		return nil, nil, errors.New("a Drawer's moveButton and resizeButton must differ")
	}
	n := &drawerNode{edge: tui.DockLeft, size: defaultDrawerSize, length: 100, opened: b.Emitter("opened"), closed: b.Emitter("closed"),
		content: content, shortcuts: shortcuts, keyScope: keyScope, resizable: resizable || windowResize, minSize: 10, minLength: 10, resized: b.EmitterWith("resized"),
		windowResize: windowResize, windowConfig: wc, windowCollector: b.WindowCollector, modal: modal, closeOnQ: closeOnQ, windowEmit: b.EmitterWith("changed"),
		movable: movable, frac: [4]float64{25, 25, 50, 50}, placed: b.EmitterWith("placed"), reserve: reserve}
	if n.resizable { // a drag stops a resizable panel short of a sliver along its edge
		n.minLength = 20
	}
	n.frame = &drawerFrame{owner: n}
	n.windowTarget = &drawerWindowTarget{owner: n}
	n.frame.Label("Drawer")
	n.frame.Add(n.wrapped())
	n.float = widget.NewFloat(n.frame, widget.WithModal(modal))
	n.OverlayLayer = NewOverlayLayer(n.float, "a Drawer")
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
	content := n.content
	// a hollow square at the corner, the same in a terminal and a window (a full block is a cell
	// tall, not square; a corner-pointing triangle read as a stray glyph against the frame)
	handle := widget.HandleBottomRight
	switch n.edge {
	case tui.DockRight:
		handle = widget.HandleBottomLeft
	case tui.DockBottom:
		handle = widget.HandleTopRight
	}
	n.grip = nil
	if n.resizable && !n.windowResize {
		n.grip = widget.NewResizable(content, widget.WithHandles(handle), widget.WithHandleGlyph(drawerGrip), widget.WithResizeEnd(n.dragEnded))
		content = n.grip
	}
	if !n.windowControls() {
		return content
	}
	opts := append(n.windowConfig.options(n.windowCollector, n.windowChanged),
		widget.WithWindowTarget(n.windowTarget), widget.WithWindowResizeHandle(handle))
	if n.windowConfig.close {
		opts = append(opts, widget.WithWindowCloseHandler(func() bool {
			shown := n.float.Shown()
			if shown {
				n.float.SetVisible(true)
				_ = n.close()
			}
			return shown
		}))
	}
	n.windowMods = widget.NewWindowMod(content, opts...)
	return n.windowMods
}

func (n *drawerNode) windowControls() bool {
	return n.windowResize || n.movable || n.windowConfig.maximize || n.windowConfig.minimize || n.windowConfig.close
}

func (n *drawerNode) heldShortcuts() (keymap.Scope, []*shortcutNode) { return n.keyScope, n.shortcuts }

// WindowBehavior exposes capabilities to a separate declarative presentation.
func (n *drawerNode) WindowBehavior() *widget.WindowCore {
	if n.windowMods == nil {
		return nil
	}
	return n.windowMods.Core()
}

func (n *drawerNode) windowChanged(e widget.WindowChangedEvent) {
	switch {
	case n.movable && (e.Operation == widget.WindowMove || e.Operation == widget.WindowResize):
		// a movable Drawer floats where a move or resize ended, as percentages of the Window: the
		// drag's rectangle is done with first, or place would keep it at those cells
		n.windowTarget.preview, n.windowTarget.moving = nil, nil
		x, y, w, h := widget.FractionOf(e.Bounds, n.area())
		n.floating, n.frac = true, [4]float64{x, y, w, h}
		n.place()
		n.placed(floatValue(x), floatValue(y), floatValue(w), floatValue(h))
	case e.Operation == widget.WindowResize:
		n.windowTarget.preview = nil
		n.dragEnded(tui.Size{W: e.Bounds.W, H: e.Bounds.H})
	}
	n.windowEmit(windowChangedArgs(e)...)
}

func (n *drawerNode) stopWindowDrag() {
	if n.windowMods != nil {
		n.windowMods.Core().HandleAction(tui.ActionInvocation{Action: widget.WindowDragCancelAction{}, Origin: tui.OriginProgrammatic})
	}
	n.windowTarget.preview = nil
	if n.windowTarget.moving != nil {
		n.windowTarget.moving = nil
		n.place()
	}
}

// dragEnded keeps where the drag left the panel, as percentages of the Window, and raises resized
// with them. A drag cancelled (Escape) ends here never: size and length were never touched, and the
// panel lays out at them again.
func (n *drawerNode) dragEnded(final tui.Size) {
	n.size, n.length = n.percentOf(final, n.win)
	if n.frame.ctx != nil {
		n.frame.ctx.RequestLayout()
	}
	n.resized(numberValue(n.size),
		numberValue(n.length))
}

// setEdge is Drawer.edge's setter. A resizable Drawer's grip moves to the new edge's inner corner,
// and a reserving one's strip to the new edge.
func (n *drawerNode) setEdge(e tui.DockEdge) {
	n.stopWindowDrag()
	moved := e != n.edge
	n.edge = e
	if moved && n.onEdge != nil {
		n.onEdge(e)
	}
	if moved && n.resizable {
		if n.windowMods != nil {
			n.frame.Remove(n.windowMods)
		} else if n.grip != nil {
			n.frame.Remove(n.grip)
		}
		n.frame.Add(n.wrapped())
	}
	n.place()
}

// setMinimumSize and setMinimumLength are a resizable Drawer's bounds, percentages of the Window.
func (n *drawerNode) setMinimumSize(pct int) {
	n.stopWindowDrag()
	n.minSize = min(max(pct, 1), 90)
	n.size = max(n.size, n.minSize)
	n.place()
}

func (n *drawerNode) setMinimumLength(pct int) {
	n.stopWindowDrag()
	n.minLength = min(max(pct, 1), 100)
	n.length = max(n.length, n.minLength)
	n.place()
}

// setSize is Drawer.size's setter: a percentage, minimumSize (10 unless set) to 90.
func (n *drawerNode) setSize(pct int) {
	n.stopWindowDrag()
	n.size = min(max(pct, n.minSize), 90)
	n.place()
}

// setLength is Drawer.length's setter: a percentage of the Window along the edge, 10 to 100,
// centred there. Qt sizes a Drawer along its edge by its height (or width) and places it by y (or
// x); golib's panels are sized as fractions of the Window, so this is the one number.
func (n *drawerNode) setLength(pct int) {
	n.stopWindowDrag()
	n.length = min(max(pct, n.minLength), 100)
	n.place()
}

// setFloating is Drawer.floating's setter: false docks a floated Drawer at its edge again, true
// floats it at floatX, floatY, floatWidth and floatHeight. Only a movable Drawer floats.
func (n *drawerNode) setFloating(on bool) {
	n.stopWindowDrag()
	n.floating = on && n.movable
	n.place()
}

// setFrac is a setter of one of floatX, floatY, floatWidth and floatHeight: a percentage of the
// Window, 0 to 100.
func (n *drawerNode) setFrac(i int, pct float64) {
	n.stopWindowDrag()
	n.frac[i] = min(max(pct, 0), 100)
	n.place()
}

// open shows the Drawer over its Window (OverlayLayer.Open). Open already, a minimized one is
// restored.
func (n *drawerNode) open() error {
	opened, err := n.Open()
	switch {
	case err != nil:
		return err
	case opened:
		n.opened()
	case n.windowMods != nil && n.windowMods.Core().State().Minimized:
		n.windowMods.Restore()
	}
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
	if n.windowMods != nil && n.windowMods.Core().State().Minimized {
		return n.open()
	}
	if n.float.Shown() {
		return n.close()
	}
	return n.open()
}

// A Drawer takes no place in the layout: it opens over it.
func (n *drawerNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (n *drawerNode) Render(tui.Surface)                {}

// drawerFrame is the Drawer's content as the Float shows it, firing the Drawer's Shortcuts and
// closing on Escape while the keyboard is inside. A Container, so the first focusable widget in it is found when it opens.
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
	n := f.owner
	if n.sizedByFloat() {
		// the Window is the Float's area
		f.notePlacement(func() (tui.Size, bool) {
			a := n.float.Area()
			return a, a.W > 0 && a.H > 0
		})
		f.fill(sz)
		return sz
	}
	if n.windowControls() {
		viewport := sz
		sz = n.panelSize(viewport)
		if n.windowTarget.maximized {
			sz = viewport
		} else if n.windowTarget.preview != nil {
			sz = *n.windowTarget.preview
		}
		f.notePlacement(func() (tui.Size, bool) { return viewport, true })
	} else if n.resizable && n.grip != nil {
		n.win = sz
		sz = n.panelSize(sz)
	}
	f.fill(sz)
	return sz
}

// fill lays the frame's child out at sz, the panel's whole size.
func (f *drawerFrame) fill(sz tui.Size) {
	for _, child := range f.All() {
		f.ctx.LayoutChild(child, tui.Tight(sz))
		f.ctx.PlaceChild(child, tui.Rect{W: sz.W, H: sz.H})
	}
}

// notePlacement records, once the layout is applied, the Window the panel is in (win, when it
// knows) and the panel's bounds in it, for the window controls.
func (f *drawerFrame) notePlacement(win func() (tui.Size, bool)) {
	n := f.owner
	f.ctx.AfterLayout("drawer-window-placement", func() {
		if w, ok := win(); ok {
			n.win = w
		}
		if r, ok := n.float.Context().ResolveAnchor(f.ctx.NodeAnchor()); ok {
			n.windowTarget.bounds = r
		}
	})
}

func (f *drawerFrame) Render(tui.Surface) {}

func (f *drawerFrame) HandleEvent(ev tui.Event) bool {
	if fireShortcut(f.ctx, f.owner.keyScope, f.owner.shortcuts, ev) {
		return true
	}
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind != tui.KeyPress {
		return false // a held Esc closes once
	}
	if f.closes(k) {
		_ = f.owner.close()
		return true
	}
	return false
}

// closes reports whether k is one of the Drawer's own closing keys.
func (f *drawerFrame) closes(k tui.KeyEvent) bool {
	cmd, ok := f.ctx.KeyCommand(k, f.closeCommands()...)
	return ok && (cmd.ID == widget.DrawerClose.ID || cmd.ID == widget.DrawerCloseOnQ.ID)
}

// closeCommands are the Drawer's own closing commands: Esc, and q with closeOnQ.
func (f *drawerFrame) closeCommands() []keymap.Command {
	if f.owner.closeOnQ {
		return []keymap.Command{widget.DrawerClose, widget.DrawerCloseOnQ}
	}
	return []keymap.Command{widget.DrawerClose}
}

// drawerType is the Drawer's entry in the standard vocabulary.
var drawerType = Type{
	Name:  "Drawer",
	Build: buildDrawer,
	Ctor: []string{"modal", "closeOnQ", "resizable", "windowResize", "movable", "maximizable", "minimizable", "closable", "label", "key",
		"dragModifier", "moveButton", "resizeButton", "keyScope", "reserve"},
	Signals: map[string][]string{"resized": {"size", "length"}, "placed": {"x", "y", "width", "height"},
		"changed": {"operation", "key", "x", "y", "width", "height", "maximized", "minimized"}},
	Setters: map[string]Setter{
		"edge":          setter("a Drawer", drawerEdges.read, (*drawerNode).setEdge),
		"size":          setter("a Drawer", truncOf, (*drawerNode).setSize),
		"length":        setter("a Drawer", truncOf, (*drawerNode).setLength),
		"minimumSize":   setter("a Drawer", truncOf, (*drawerNode).setMinimumSize),
		"minimumLength": setter("a Drawer", truncOf, (*drawerNode).setMinimumLength),
		"floating":      setter("a Drawer", boolOf, (*drawerNode).setFloating),
		"floatX":        drawerFracSetter(0),
		"floatY":        drawerFracSetter(1),
		"floatWidth":    drawerFracSetter(2),
		"floatHeight":   drawerFracSetter(3),
	},
	Methods: map[string]Method{
		"open":           NoArgMethod((*drawerNode).open),
		"close":          NoArgMethod((*drawerNode).close),
		"toggle":         NoArgMethod((*drawerNode).toggle),
		"toggleMaximize": drawerWindow(windowInvoke(widget.WindowMaximizeAction{})),
		"minimize":       drawerWindow(windowInvoke(widget.WindowMinimizeAction{})),
		"restore":        drawerWindow(windowInvoke(widget.WindowRestoreAction{})),
		"resizeBy":       drawerWindow(windowStep(false)),
		"moveBy":         drawerWindow(windowStep(true)),
	},
	Destroyed: destroyer((*drawerNode).Detach),
}

// drawerFracSetter is the setter of the floating rectangle's i-th percentage.
func drawerFracSetter(i int) Setter {
	return setter("a Drawer", windowPercentOf, func(n *drawerNode, pct float64) { n.setFrac(i, pct) })
}

// windowPercentOf reads a percentage of the Window: a finite number (a floating rectangle's floatX,
// floatY, floatWidth or floatHeight, a Drawer's or a FloatWindow's).
func windowPercentOf(v qml.SpecValue) (float64, error) {
	f, err := numberOf(v)
	if err == nil && (math.IsNaN(f) || math.IsInf(f, 0)) {
		err = errors.New("a percentage must be a finite number")
	}
	return f, err
}

// KeyScope is the Drawer's keyScope: the frame is where its Shortcuts fire (tui.KeyScoped).
func (f *drawerFrame) KeyScope() keymap.Scope { return f.owner.keyScope }

// KeyBindings is the Drawer's Shortcuts as m binds them, then its own closing keys (tui.KeyBinder).
func (f *drawerFrame) KeyBindings(m *keymap.Map) []keymap.Binding {
	return append(shortcutBindings(m, f.owner.keyScope, f.owner.shortcuts), tui.BindingsOf(m, f.closeCommands()...)...)
}
