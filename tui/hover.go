package tui

// HOVER — which component the pointer is over.
//
// The App tracks it, because only the App knows the node under the pointer: a component hears
// motion only while the pointer is over it, so it never sees the pointer leave. The node is the
// one routing would hit-test for an uncaptured event (z-order, PointerContainer, ceilinged at the
// active trapping scope, so nothing behind a dialog is hovered). During a pointer capture only the
// owner can be hovered, and only while the pointer is inside it. Hover clears when the pointer
// leaves the screen (PointerLeaveEvent), when the terminal or window loses focus, and when the
// hovered node unmounts.

// HoverObserver opts a component into hover: the App calls HoverChanged when the pointer starts
// or stops being over it, on the loop goroutine, and repaints.
type HoverObserver interface {
	Component
	HoverChanged(hovered bool)
}

// PointerLeaveEvent says the pointer left the screen: a window's pointer leaving it. A backend
// emits it where it knows; a terminal never does.
type PointerLeaveEvent struct{}

func (PointerLeaveEvent) isEvent() {}

// Hovered reports whether the pointer is over this node.
func (c *Context) Hovered() bool { return c.node.id != 0 && c.app.hovered == c.node.id }

// trackHover follows the pointer to the node under it.
func (a *App) trackHover(e MouseEvent) {
	a.pointer = Point{X: e.X, Y: e.Y}
	a.setHovered(a.nodeUnderPointer())
}

// refreshHover re-checks hover at the pointer's last position: after a capture ends, the node
// under it may not be the owner.
func (a *App) refreshHover() { a.setHovered(a.nodeUnderPointer()) }

// nodeUnderPointer is the hovered node at the last pointer position, or nil.
func (a *App) nodeUnderPointer() *node {
	x, y := a.pointer.X, a.pointer.Y
	if a.captureOwner != 0 {
		owner := a.nodes[a.captureOwner]
		if owner != nil && owner.absRect.Contains(x, y) {
			return owner
		}
		return nil
	}
	if limit := a.confinement(); limit != nil {
		return hitTestNode(limit, x, y)
	}
	return a.hitTest(x, y)
}

// setHovered makes n the hovered node (nil: none), telling both observers.
func (a *App) setHovered(n *node) {
	var id NodeID
	if n != nil {
		id = n.id
	}
	if id == a.hovered {
		return
	}
	old := a.nodes[a.hovered]
	a.hovered = id
	a.notifyHover(old, false)
	a.notifyHover(n, true)
}

func (a *App) notifyHover(n *node, hovered bool) {
	if n == nil || !n.mounted {
		return
	}
	if o, ok := n.comp.(HoverObserver); ok {
		o.HoverChanged(hovered)
		a.renderDirty = true
		a.queue.wakeUp()
	}
}
