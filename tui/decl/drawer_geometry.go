package decl

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A Drawer's geometry: its panel in cells and in percentages of the Window, where its Float is
// anchored, and the strip it reserves. drawer.go is the Drawer; drawerFrame.Layout reads these.

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
// a drag can change them. A floating Drawer is at its percentages of the Window, and a move or
// resize in progress at the rectangle it would take; maximized, it is docked and the frame fills
// the Window.
func (n *drawerNode) place() {
	if n.movable && !n.windowTarget.maximized {
		switch {
		case n.windowTarget.moving != nil:
			n.float.SetAnchor(widget.AtRect(*n.windowTarget.moving))
			n.float.SetSizeFraction(0, 0)
			return
		case n.floating:
			n.float.SetAnchor(widget.AtFraction(n.frac[0], n.frac[1], n.frac[2], n.frac[3]))
			n.float.SetSizeFraction(0, 0)
			return
		}
	}
	if n.reserve && n.edge != tui.DockCenter {
		// over its strip, wherever the Window's Dock placed it this pass; maximized, it fills the
		// Window as any window does, the strip kept beneath
		n.float.SetAnchor(widget.AtLive(n.liveSlot, n.edgeAnchor()))
		n.float.SetSizeFraction(0, 0)
		return
	}
	if n.resizable || n.windowControls() {
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
func (n *drawerNode) placeAnchor() { n.float.SetAnchor(n.edgeAnchor()) }

// edgeAnchor is the Float's anchor at the Drawer's edge.
func (n *drawerNode) edgeAnchor() widget.Anchor {
	switch n.edge {
	case tui.DockRight:
		return widget.Right
	case tui.DockTop:
		return widget.Top
	case tui.DockBottom:
		return widget.Bottom
	case tui.DockCenter:
		return widget.Center
	default:
		return widget.Left
	}
}

// reserving reports whether the Drawer takes its strip of the Window from the page now: reserve set,
// at an edge, open, docked, and not minimized. Maximized it keeps the strip, so the page beneath
// never reflows; closed, minimized or floating it has none.
func (n *drawerNode) reserving() bool {
	if !n.reserve || n.edge == tui.DockCenter || !n.float.Shown() || n.floating {
		return false
	}
	return n.windowMods == nil || !n.windowMods.Core().State().Minimized
}

// liveSlot is the strip the Window's Dock placed for the Drawer this pass (AtLive's rectangle). A
// maximized Drawer is not over it: it fills the Window, its edge anchor offering the frame the whole
// of it, while the strip stays reserved beneath.
func (n *drawerNode) liveSlot() (tui.Rect, bool) {
	return n.slot, n.reserving() && !n.windowTarget.maximized && n.slot.W > 0 && n.slot.H > 0
}

// dockedCells is the panel docked in a Window of win: size across its edge, the whole edge along
// it, from the drag in progress when there is one (a grip's, or a window resize's preview).
func (n *drawerNode) dockedCells(win tui.Size) tui.Size {
	size := n.size
	if n.grip != nil && n.grip.Dragging() {
		if want, ok := n.grip.RequestedSize(); ok {
			size, _ = n.percentOf(want, win)
		}
	}
	sz := n.cells(size, 100, win)
	if p := n.windowTarget.preview; p != nil {
		sz = *p
	}
	return sz
}

// floatRect is a floating Drawer's rectangle in the Window, in cells.
func (n *drawerNode) floatRect() tui.Rect {
	return widget.FractionRect(n.frac[0], n.frac[1], n.frac[2], n.frac[3], n.area())
}

// area is the Window the Drawer is over, as a rectangle: where its Float last laid out, else where
// the frame last did.
func (n *drawerNode) area() tui.Rect {
	if a := n.float.Area(); a.W > 0 && a.H > 0 {
		return tui.Rect{W: a.W, H: a.H}
	}
	return tui.Rect{W: n.win.W, H: n.win.H}
}

// panelSize is the panel in a Window of viewport: from the grip's drag in progress when there is
// one, within the Drawer's bounds, else from size and length.
func (n *drawerNode) panelSize(viewport tui.Size) tui.Size {
	size, length := n.size, n.length
	if n.grip != nil && n.grip.Dragging() { // shown where the drag is, kept only when it ends
		if want, ok := n.grip.RequestedSize(); ok {
			size, length = n.percentOf(want, viewport)
		}
	}
	return n.cells(size, length, viewport)
}

// sizedByFloat reports whether the Float sized the panel and the frame only fills it: over its strip
// (the Window's Dock sized it, a drag in progress included), or a movable Drawer floating or being
// moved (the Float's AtFraction or AtRect).
func (n *drawerNode) sizedByFloat() bool {
	if n.windowTarget.moving == nil {
		if _, over := n.liveSlot(); over {
			return true
		}
	}
	return n.movable && !n.windowTarget.maximized && (n.floating || n.windowTarget.moving != nil)
}
