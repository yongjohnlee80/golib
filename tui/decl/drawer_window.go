package decl

import (
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

type drawerWindowTarget struct {
	owner     *drawerNode
	bounds    tui.Rect
	preview   *tui.Size
	maximized bool
}

func (g *drawerWindowTarget) Bounds() tui.Rect { return g.bounds }
func (g *drawerWindowTarget) Area() tui.Rect   { return tui.Rect{W: g.owner.win.W, H: g.owner.win.H} }
func (g *drawerWindowTarget) RequestedBounds() tui.Rect {
	n := g.owner
	sz := n.cells(n.size, n.length, n.win)
	if g.maximized {
		sz = n.win
	} else if g.preview != nil {
		sz = *g.preview
	}
	r := tui.Rect{W: sz.W, H: sz.H}
	switch n.edge {
	case tui.DockRight:
		r.X, r.Y = n.win.W-sz.W, (n.win.H-sz.H)/2
	case tui.DockTop:
		r.X = (n.win.W - sz.W) / 2
	case tui.DockBottom:
		r.X, r.Y = (n.win.W-sz.W)/2, n.win.H-sz.H
	case tui.DockCenter:
		r.X, r.Y = (n.win.W-sz.W)/2, (n.win.H-sz.H)/2
	default:
		r.Y = (n.win.H - sz.H) / 2
	}
	return r
}
func (g *drawerWindowTarget) SetBounds(r tui.Rect) bool {
	n := g.owner
	if n.win.W <= 0 || n.win.H <= 0 || g.maximized {
		return false
	}
	want := tui.Size{W: min(max(r.W, 0), n.win.W), H: min(max(r.H, 0), n.win.H)}
	size, length := n.percentOf(want, n.win)
	want = n.cells(size, length, n.win)
	current := g.RequestedBounds()
	if want.W == current.W && want.H == current.H {
		return false
	}
	g.preview = &want
	n.frame.ctx.RequestLayout()
	return true
}
func (g *drawerWindowTarget) Checkpoint() func() {
	n := g.owner
	size, length, edge, maximized := n.size, n.length, n.edge, g.maximized
	return func() {
		n.size, n.length, n.edge = size, length, edge
		g.preview, g.maximized = nil, maximized
		n.place()
		n.frame.ctx.RequestLayout()
	}
}
func (g *drawerWindowTarget) ToggleMaximize() bool {
	g.maximized = !g.maximized
	g.owner.frame.ctx.RequestLayout()
	return true
}
func (g *drawerWindowTarget) Maximized() bool { return g.maximized }
func (g *drawerWindowTarget) Modal() bool     { return g.owner.modal }
func (g *drawerWindowTarget) WindowVisible() bool {
	return g.owner.float.Shown() && g.owner.float.Visible()
}
func (g *drawerWindowTarget) SetWindowVisible(v bool) bool {
	f := g.owner.float
	if !f.Shown() || f.Visible() == v {
		return false
	}
	f.SetVisible(v)
	return true
}

func drawerWindowInvoke(action tui.Action) Method {
	return NoArgMethod(func(n *drawerNode) error {
		if n.windowMods == nil || !n.windowMods.InvokeInput(action) {
			return ErrWindowOperationUnavailable
		}
		return nil
	})
}

func drawerWindowStep(move bool) Method {
	step := windowStep(move)
	return func(component tui.Component, args []qml.SpecValue) error {
		n, ok := component.(*drawerNode)
		if !ok || n.windowMods == nil {
			return ErrWindowOperationUnavailable
		}
		return step(n.windowMods, args)
	}
}
