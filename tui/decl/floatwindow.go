package decl

import (
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// FLOATWINDOWS (golib's) — a window floating over the Window that never closes: it opens with its
// Window and stays for as long as the Window does. A user moves it (Alt with the left button, by
// default), resizes it (Alt with the right) and, with minimizable, minimizes it to the Window's
// collector; a document moves it with floatX, floatY, floatWidth and floatHeight, percentages of
// the Window as a floating Drawer's are.
//
//	FloatWindow {
//	    id: editorWindow
//	    label: "Editor"; key: "editor"   // its entry where it minimizes to
//	    movable: true; resizable: true; minimizable: true
//	    maximumWidth: App.pageWidth      // cells; 0 (the default): none
//	    Frame { … }                      // one child, its content
//	}
//
// It has no close and claims no keys of its own: every key its content leaves (Escape among them) is
// the Window's. It is not modal, and focus moving into it raises it over the other floats.
//
// maximumWidth and maximumHeight hold it, live: a resize stops there, and so does its placement,
// so a Window that grows, or a maximum that shrinks, never stretches it past them. Held to a
// maximum it keeps its centre where its percentages put it.
//
// placed(x, y, width, height) reports its percentages where a move or resize ended, for a host to
// keep; changed is the WindowMod's.
//
// A tile manager may govern it (widget.GovernedWindow): anchored over its tile, its core given the
// tile's target, so a resize moves the tile's shared border and a move is refused. Its percentages,
// maximum and placed are its own floating state, untouched while governed: Release puts it back at
// them.

// floatWindowNode is a FloatWindow: its content in a WindowMod, in a non-modal Float on the
// Window's overlay (OverlayLayer). Its geometry is the Float's own window target, which the
// WindowMod's core finds by its provider probe, so the node writes none.
type floatWindowNode struct {
	widget.Base
	OverlayLayer
	float *widget.Float
	mod   *widget.WindowMod
	// frac is where it floats, x, y, width and height as percentages of the Window
	frac   [4]float64
	placed func(args ...qml.SpecValue)
	emit   func(args ...qml.SpecValue)
	// tileGovernance is its side of a TileManager's governing it (tilemember.go)
	tileGovernance
}

var (
	_ OpensWithWindow = (*floatWindowNode)(nil)
	_ windowBehaver   = (*floatWindowNode)(nil)
	_ tileMember      = (*floatWindowNode)(nil)
)

func buildFloatWindow(b Build) (tui.Component, []string, error) {
	c, consumed, err := readWindow(b, windowMotionFields)
	if err != nil {
		return nil, nil, err
	}
	n := &floatWindowNode{frac: [4]float64{0, 0, 100, 100}, placed: b.EmitterWith("placed"), emit: b.EmitterWith("changed"),
		tileGovernance: tileGovernance{govKey: b.ID}}
	n.mod = widget.NewWindowMod(b.Children[0], c.options(b.WindowCollector, n.windowChanged)...)
	n.float = widget.NewFloat(n.mod, widget.WithModal(false), widget.WithAnchor(n.anchor()))
	n.OverlayLayer = NewOverlayLayer(n.float, "a FloatWindow")
	if b.Overlay != nil {
		n.SetOverlay(b.Overlay, nil)
	}
	return n, consumed, nil
}

// OpenWithWindow shows it over its Window (OverlayLayer.Open); shown already, it does nothing.
func (n *floatWindowNode) OpenWithWindow() error {
	_, err := n.Open()
	return err
}

// WindowBehavior is its window's core, for a dock or a taskbar beside it.
func (n *floatWindowNode) WindowBehavior() *widget.WindowCore { return n.mod.Core() }

// onScreen is what forceActiveFocus focuses into: its window, over the Window.
func (n *floatWindowNode) onScreen() tui.Component { return n.mod }

// anchor is where the Float places it. Governed, it is over its tile, as the tile composes it (no
// maximum), docked at its tile's edge while the tile has no cells (governedAnchor): a governed window
// never floats. Otherwise it is at its percentages of the area it is placed in, held to its maximum
// (inArea), else, with no area yet, the percentages themselves.
func (n *floatWindowNode) anchor() widget.Anchor {
	if n.governed() {
		return n.governedAnchor()
	}
	return widget.AtLiveIn(n.inArea, widget.AtFraction(n.frac[0], n.frac[1], n.frac[2], n.frac[3]))
}

// Govern anchors it over rect, its tile, and gives its core the tile's target (widget.GovernedWindow).
// A drag of its own in flight is cancelled by the new anchor (Float.SetAnchor cancels it, its
// checkpoint restored, before the anchor is set), in the TileManager's commit phase, where ending
// the drag's capture is legal.
func (n *floatWindowNode) Govern(rect func() (tui.Rect, bool), target any) {
	n.govern(rect, target)
	n.float.SetAnchor(n.anchor())
	n.float.SetTiled(true) // under the floating windows, however the keyboard raises it
	n.mod.Core().Retarget(target)
}

// Release gives it back its own placement, at its percentages, and its core the Float's target.
func (n *floatWindowNode) Release() {
	n.release()
	n.float.SetAnchor(n.anchor())
	n.float.SetTiled(false)
	n.mod.Core().Retarget(nil)
}

// setTileEdge is the TileManager's: its tile is at e, live, so a governed window docks at the new
// edge while its tile has no cells.
func (n *floatWindowNode) setTileEdge(e tui.DockEdge) {
	n.tileGovernance.setTileEdge(e)
	if n.governed() {
		n.float.SetAnchor(n.anchor())
	}
}

// inArea is its rectangle in area: its percentages, each side held to the maximum and centred on
// where the percentages put it.
func (n *floatWindowNode) inArea(area tui.Rect) (tui.Rect, bool) {
	r := widget.FractionRect(n.frac[0], n.frac[1], n.frac[2], n.frac[3], area)
	limit := n.mod.Core().MaxSize()
	if limit.W > 0 && r.W > limit.W {
		r.X, r.W = r.X+(r.W-limit.W)/2, limit.W
	}
	if limit.H > 0 && r.H > limit.H {
		r.Y, r.H = r.Y+(r.H-limit.H)/2, limit.H
	}
	return r, area.W > 0 && area.H > 0
}

// windowChanged keeps where a move or resize ended as its percentages, and anchors there again: the
// drag left the Float at the cells it reached. Governed, the operation was the tile's (a shared
// border's, committed by the tile's target), not its floating state's: it only reports it.
func (n *floatWindowNode) windowChanged(e widget.WindowChangedEvent) {
	if !n.governed() && (e.Operation == widget.WindowMove || e.Operation == widget.WindowResize) {
		a := n.float.Area()
		x, y, w, h := widget.FractionOf(e.Bounds, tui.Rect{W: a.W, H: a.H})
		n.frac = [4]float64{x, y, w, h}
		n.float.SetAnchor(n.anchor())
		n.placed(floatValue(x), floatValue(y), floatValue(w), floatValue(h))
	}
	n.emit(windowChangedArgs(e)...)
}

// setFrac is a setter of floatX, floatY, floatWidth or floatHeight: a percentage of the Window, 0
// to 100. A drag in progress is cancelled first, as a Drawer's is.
func (n *floatWindowNode) setFrac(i int, pct float64) {
	n.stopDrag()
	n.frac[i] = min(max(pct, 0), 100)
	n.float.SetAnchor(n.anchor())
}

// setMaximum is maximumWidth's (w) or maximumHeight's setter: cells, 0 for none. The resize and the
// placement read the one value (WindowCore.MaxSize).
func (n *floatWindowNode) setMaximum(w bool, cells int) {
	limit := n.mod.Core().MaxSize()
	if w {
		limit.W = cells
	} else {
		limit.H = cells
	}
	n.mod.Core().SetMaxSize(limit)
	n.float.RequestLayout()
}

func (n *floatWindowNode) stopDrag() {
	n.mod.Core().HandleAction(tui.ActionInvocation{Action: widget.WindowDragCancelAction{}, Origin: tui.OriginProgrammatic})
}

// A FloatWindow takes no place in the layout: it floats over it.
func (n *floatWindowNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (n *floatWindowNode) Render(tui.Surface)                {}

// floatWindowFracSetter is the setter of its floating rectangle's i-th percentage.
func floatWindowFracSetter(i int) Setter {
	return setter("a FloatWindow", windowPercentOf, func(n *floatWindowNode, pct float64) { n.setFrac(i, pct) })
}

// floatWindowMaximumSetter is maximumWidth's (w) or maximumHeight's setter.
func floatWindowMaximumSetter(w bool) Setter {
	return setter("a FloatWindow", truncOf, func(n *floatWindowNode, cells int) { n.setMaximum(w, cells) })
}

// floatWindowOf is a FloatWindow's window, for the WindowMod methods.
var floatWindowOf = windowMethod(func(n *floatWindowNode) (*widget.WindowMod, error) { return n.mod, nil })

// floatWindowType is the FloatWindow's entry in the standard vocabulary.
var floatWindowType = Type{
	Name: "FloatWindow", Children: Exactly(1).Because("its content"), Build: buildFloatWindow,
	Ctor:    []string{"movable", "resizable", "minimizable", "label", "key", "dragModifier", "moveButton", "resizeButton"},
	Signals: map[string][]string{"placed": {"x", "y", "width", "height"}, "changed": {"operation", "key", "x", "y", "width", "height", "maximized", "minimized"}},
	Setters: map[string]Setter{
		"maximumWidth":  floatWindowMaximumSetter(true),
		"maximumHeight": floatWindowMaximumSetter(false),
		"floatX":        floatWindowFracSetter(0),
		"floatY":        floatWindowFracSetter(1),
		"floatWidth":    floatWindowFracSetter(2),
		"floatHeight":   floatWindowFracSetter(3),
	},
	Methods: map[string]Method{
		"minimize": floatWindowOf(windowInvoke(widget.WindowMinimizeAction{})),
		"restore":  floatWindowOf(windowInvoke(widget.WindowRestoreAction{})),
		"moveBy":   floatWindowOf(windowStep(true)),
		"resizeBy": floatWindowOf(windowStep(false)),
	},
	Getters: map[string]Getter{
		"minimized": getter(func(n *floatWindowNode) (qml.SpecValue, error) {
			return boolValue(n.mod.Core().State().Minimized), nil
		}),
	},
	Destroyed: destroyer((*floatWindowNode).Detach),
}
