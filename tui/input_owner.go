package tui

// A LAYER THAT BELONGS TO SOMETHING ELSE.
//
// A popup is mounted where it can be drawn on top — as a layer of an overlay host — not under the
// component it serves. A cascading menu's submenu is the plain case: the level is the host's
// child, a sibling of the menu that opened it. Tree ancestry therefore says nothing about whom the
// level belongs to, and the input rules that are decided by ancestry decide wrongly for it.
//
// The one that matters is confinement. While a trapping scope is active, pointer input is
// ceilinged at that scope's subtree. A menu inside the scope — the right-click menu's own popup
// layer, a modal dialog — opens its level outside it, so every press on the level was taken by the
// scope instead: the level painted, highlighted under the keyboard, and never heard the mouse.
//
// So the container that mounts such a layer says whose it is, with an anchor the owner issued.
// From then on the layer is inside every scope its owner is inside, for the pointer: hit-testing,
// the bubbling ceiling, pointer capture, a reserved press. Focus is not extended. The owner keeps
// the keyboard, as a menu keeps it for its whole cascade; a layer that needs focus of its own is a
// trapping layer and is entered as one.

// SetChildInputOwner makes child, a layer this container has mounted, take pointer input on
// behalf of the node that issued owner: wherever an active trapping scope admits that node, it
// admits child and child's subtree too. The zero ref clears it.
//
// An overlay host calls it for each layer it anchors, since an anchored layer is part of the
// thing it hangs off. It reports false, changing nothing, when child is not a mounted child of
// this node, or when owner names a node that is not mounted.
func (c *Context) SetChildInputOwner(child Component, owner AnchorRef) bool {
	a := c.app
	cn := a.byComp[child]
	if cn == nil || !cn.mounted || cn.parent != c.node {
		return false
	}
	if !owner.Valid() {
		cn.inputOwner = 0
		return true
	}
	on := a.nodes[owner.owner]
	if on == nil || !on.mounted {
		return false
	}
	cn.inputOwner = on.id
	return true
}

// inputOwnerOf is the live node n takes pointer input on behalf of, or nil.
func (a *App) inputOwnerOf(n *node) *node {
	if n.inputOwner == 0 {
		return nil
	}
	if o := a.nodes[n.inputOwner]; o != nil && o.mounted {
		return o
	}
	return nil
}

// boundLayerOf is the nearest layer at or above n that has an input owner, or nil.
func (a *App) boundLayerOf(n *node) *node {
	for p := n; p != nil; p = p.parent {
		if a.inputOwnerOf(p) != nil {
			return p
		}
	}
	return nil
}

// inInputScope reports whether n may receive pointer input while scope is the active trapping
// scope: it lies inside scope, or it lies in a layer whose owner does — directly or through a
// chain of such layers, as a submenu of a submenu does. A nil scope admits everything.
//
// Ancestry is checked first and at every step, so a layer that is inside the scope by the tree is
// admitted as before whatever its owner. The walk is bounded by the node count: owners that point
// at each other in a ring end it rather than spin.
func (a *App) inInputScope(n, scope *node) bool {
	if scope == nil {
		return true
	}
	for hops := 0; n != nil && hops <= len(a.nodes); hops++ {
		if withinScope(n, scope) {
			return true
		}
		layer := a.boundLayerOf(n)
		if layer == nil {
			return false
		}
		n = a.inputOwnerOf(layer)
	}
	return false
}

// pointerTarget is the node a pointer event at (x, y) is routed to when nothing holds a capture:
// the topmost node there, and while a scope traps, the topmost node the scope admits
// (inInputScope). Routing and hover both ask this, so the node a press reaches and the node the
// pointer is over cannot disagree.
//
// Under a trap the search runs from the root, skipping what the scope does not admit, so a layer
// the scope owns wins over the scope's own content beneath it, as it is drawn. A point the walk
// cannot reach from the root — a scope placed outside an ancestor's rect — is still searched from
// the scope itself, as it always was.
func (a *App) pointerTarget(x, y int, limit *node) *node {
	if limit == nil {
		return a.hitTest(x, y)
	}
	if a.rootNode != nil {
		if t := a.hitTestAdmitted(a.rootNode, x, y, limit); t != nil {
			return t
		}
	}
	return hitTestNode(limit, x, y)
}

// hitTestAdmitted is hitTestNode keeping only nodes the scope admits: one it does not is passed
// over as if absent, and the search goes on to what lies beneath it.
func (a *App) hitTestAdmitted(n *node, x, y int, scope *node) *node {
	if !n.visible() || !n.absRect.Contains(x, y) {
		return nil
	}
	for i := len(n.children) - 1; i >= 0; i-- {
		if t := a.hitTestAdmitted(n.children[i], x, y, scope); t != nil {
			return t
		}
	}
	if pc, ok := n.comp.(PointerContainer); ok && !pc.ContainsPointer(x-n.absRect.X, y-n.absRect.Y) {
		return nil
	}
	if !a.inInputScope(n, scope) {
		return nil
	}
	return n
}

// pointerCeiling is where a pointer event delivered to target stops bubbling: the active scope
// for a target inside it, and for a target the scope admits through an owner, the owned layer
// itself — its tree parents are the host and what lies behind the scope, which the trap exists to
// keep the event from. Nil when nothing traps.
func (a *App) pointerCeiling(target, limit *node) *node {
	if limit == nil || target == nil || withinScope(target, limit) {
		return limit
	}
	if layer := a.boundLayerOf(target); layer != nil {
		return layer
	}
	return limit
}
