package tui

// focusState is the focused node and the scopes it moved through, the tree-mutation batch, and the
// repairs waiting for a layout.
type focusState struct {
	focused    NodeID // 0 = none
	scopeStack []scopeEntry

	// batchDepth counts the nested Context.BatchTreeMutation calls currently
	// executing, and batchRepair records that something inside one asked for a
	// focus repair. Focus is a single global, so batching is APP-WIDE rather
	// than per-node: two compositions mutating in one batch still share the one
	// focused node, and a per-node depth would let each conclude it was the
	// outermost and repair against the other's half-applied tree.
	//
	// Only focus repair and focusability revalidation are deferred. Capture
	// loss, OnUnmount hooks and lifetime-context cancellation stay synchronous:
	// they are safety-critical, and deferring them would strand resources for
	// the length of the batch.
	batchDepth  int
	batchRepair bool

	// pendingRepair records that a focus repair ran while its only candidates
	// had not been laid out, and must be retried once the frame has run. See
	// repairFocus's empty-scope branch.
	pendingRepair bool

	// pendingFocusInto is a FocusInto whose target had not been laid out yet
	// — a pane shown in the same turn — to apply after the next layout; 0 for
	// none. A focus change before then cancels it. See FocusInto.
	pendingFocusInto NodeID
}

// Focus management is framework-owned end-to-end:
//
//  1. Single Active Focus: Exactly one NodeID on the App holds focus at any time
//     (0 indicates no component is currently focused).
//  2. Tab Traversal Order: Tab and Shift-Tab walk the focus ring pre-order depth-first
//     in document order (which corresponds to child mount and painter's z-order).
//  3. Trapping Focus Scopes: Modal dialogs, dropdowns, and floating windows implement
//     FocusScope with TrapsFocus() == true. Entering a trapping scope records the
//     prior focus target on a LIFO stack; when the scope unmounts, focus is restored
//     automatically to the previously focused node.
//  4. Dead-Focus & Invisible-Focus Repair: If a focused node is unmounted, disabled,
//     or hidden by layout (e.g. zoomed away in a Split or switched out in Tabs),
//     the runtime repairs focus to the first focusable candidate in the active scope
//     so no frame ever renders with a dangling or invisible focus ID.
//
// scopeEntry records a focus trap entered via RequestFocus: when the
// trapping scope unmounts, focus restores to the node focused before entry.
type scopeEntry struct {
	scope   NodeID // the trapping FocusScope node
	restore NodeID // focused node before the trap was entered
}

// requestFocus implements Context.RequestFocus. Ignored unless the
// component is Focusable and currently accepts focus (the visibility filter
// applies to traversal only — a component may legally request focus from
// Init, before any layout pass). requestFocusByID focuses the node with the
// given id if it exists and is focusable. Reports whether focus ended up
// there.
func (a *App) requestFocusByID(id NodeID) bool {
	if n := a.nodes[id]; n != nil {
		a.requestFocus(n)
	}
	return a.focused == id
}

func (a *App) requestFocus(n *node) {
	if !isFocusable(n) {
		return
	}
	if a.focused == n.id {
		return
	}
	// A trap must not be reachable around. Computing the active scope BEFORE
	// moving focus is what makes this safe to check: entering the first trap is
	// still legal, because the active scope is then the root; entering a nested
	// trap is still legal, because that trap's node lies within the outer scope.
	// Without this, an outside component could call RequestFocus while a modal
	// was mounted and dissolve the confinement from the outside.
	if scope := a.confinement(); scope != nil && !withinScope(n, scope) {
		// A DIALOG STACKED IN FRONT IS NOT "OUTSIDE" IN THE SENSE THIS GUARD
		// MEANS. Ancestry was standing in for confinement, and it tells the truth
		// for the two shapes golib grew up with: a nested trap lies inside its
		// parent, and an unrelated trap is never live at the same time. An overlay
		// host produces a third — each layer's trap is mounted inside its own
		// Float, and the Floats are layers of one Stack, so the two traps are
		// COUSINS and neither is an ancestor of the other. Ancestry can only refuse
		// them, which deadlocked the pair: the new dialog could not be focused
		// because it was not on the scope stack, and could not reach the stack
		// because nothing inside it could be focused. Downstream that is a dialog
		// that paints and then cannot be typed into, tabbed through or dismissed.
		//
		// Entry is granted by the container that OWNS the two branches, never by
		// document order — see mayEnterStackedTrap for why order alone is not
		// enough. Everything this guard exists for is unchanged: a node in no trap
		// stays unreachable, and a layer behind the active one still cannot pull
		// the keyboard out of the one in front.
		if !a.mayEnterStackedTrap(a.trapScopeOf(n), scope) {
			a.trace(TraceEvent{Kind: TraceFocus, Node: n.id, Prev: a.focused,
				Detail: "focus refused: target outside the active focus scope"})
			return
		}
	}
	newScope := a.trapScopeOf(n)
	var oldScope *node
	if on := a.nodes[a.focused]; on != nil {
		oldScope = a.trapScopeOf(on)
	}
	if newScope != nil && newScope != oldScope {
		// Entering a trap: remember where focus came from.
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: newScope.id, restore: a.focused})
		a.trace(TraceEvent{Kind: TraceScope, Node: newScope.id, Prev: a.focused, Detail: "open"})
		// A trap that has just become active must not leave a drag running
		// behind it. Checked here, at the push, rather than when the next
		// pointer event arrives: with no further mouse input the owner would
		// otherwise sit in its dragging state indefinitely.
		a.captureCheckScope()
	}
	a.setFocus(n.id)
}

// setFocus moves focus and delivers the two FocusEvents: Gained=false to
// the loser, then Gained=true to the gainer; both events bubble up the tree
// so ancestor panels (such as widget.Box) can restyle their border chrome
// (the lazygit active-panel highlight pattern).
func (a *App) setFocus(id NodeID) {
	if a.focused == id {
		return
	}
	old := a.focused
	a.focused = id
	a.pendingFocusInto = 0 // focus moved: a deferred FocusInto is answered, or overtaken
	a.trace(TraceEvent{Kind: TraceFocus, Node: id, Prev: old})
	if on := a.nodes[old]; on != nil {
		a.bubble(on, FocusEvent{Gained: false})
	}
	if nn := a.nodes[id]; nn != nil {
		a.bubble(nn, FocusEvent{Gained: true})
		a.requestReveal(nn)
	}
	a.toolTipFocus(a.nodes[a.focused], a.keyFocus)
	// A capture survives focus moving around inside the owner's own subtree and
	// ends when focus leaves it. Checked after the FocusEvents are delivered so
	// that a handler which moves focus onward is accounted for by the check
	// rather than racing it.
	a.captureCheckFocus()
	a.invalidate(false) // the cursor rule re-evaluates next frame
}

// focusRing collects the tab stops in traversal order: pre-order
// depth-first in child (document) order, filtered to nodes that (a)
// implement Focusable, (b) report AcceptsFocus(), and (c) were laid out in
// the current frame with a non-empty Rect. Nested trapping scopes are
// excluded from an enclosing scope's ring — a trap confines traversal both
// in and out.
func (a *App) focusRing(scope *node) []*node {
	var out []*node
	var walk func(n *node)
	walk = func(n *node) {
		if hidden(n.comp) {
			return
		}
		if n != scope && trapsFocus(n) {
			return // another trap's subtree is not in this ring
		}
		if isFocusable(n) && n.visible() {
			out = append(out, n)
		}
		for _, ch := range n.children {
			walk(ch)
		}
	}
	if scope != nil {
		walk(scope)
	}
	return out
}

// currentScope resolves the active traversal boundary: the innermost live
// scope-stack entry first, otherwise the nearest trapping ancestor of the
// focused node, and finally the root.
func (a *App) currentScope() *node {
	// The innermost LIVE stack entry is the first authority. A mounted trap
	// governs while it is on the stack, whatever focus happens to be doing —
	// consulting the stack only when focused == 0 let a trap stop governing the
	// moment focus existed outside it, which is precisely when confinement
	// matters most. Entries are removed by the unmount cascade, so a surviving
	// entry means a live trap.
	for i := len(a.scopeStack) - 1; i >= 0; i-- {
		if sn := a.nodes[a.scopeStack[i].scope]; sn != nil && sn.mounted {
			return sn
		}
	}
	// No stack entry: a component may still trap by being an ancestor of the
	// focused node without having been entered through requestFocus.
	if fn := a.nodes[a.focused]; fn != nil {
		if s := a.trapScopeOf(fn); s != nil {
			return s
		}
	}
	return a.rootNode
}

// focusFromPointer focuses the first focusable node at or above the pointer
// target, provided that node lies inside the ACTIVE focus scope (
// steps 1-4). It is called for a primary press only, BEFORE the event is
// delivered, so a widget handling the press already sees itself focused and one
// gesture both focuses and acts.
//
// The boundary is [App.currentScope], deliberately NOT trapScopeOf(a.focused).
// Those agree everywhere except one state golib supports on purpose: when a
// focused child dies while its trap survives with no remaining focusables,
// [App.repairFocus] leaves focused = 0 and the scope stays on scopeStack. With
// no focused node there is nothing to walk from, so trapScopeOf yields nil, a
// naive rule would conclude there is no active trap, and a click outside would
// escape a trap that is still standing. currentScope already handles that path.
//
// Containment is an ANCESTOR walk, deliberately NOT focusRing membership. A ring
// excludes nested trapping scopes' subtrees — that is right for Tab traversal,
// which must not wander into an unentered modal, and wrong for the pointer, which
// is exactly how a user ENTERS one. Using the ring refused every click into a
// nested trap while the active scope was root, i.e. while nothing was restricted
// at all. Rings stay for traversal; the pointer guard asks only "is this candidate
// inside the active scope".
//
// Motion, wheel and release never reach here — the wheel scrolls the pane under
// the pointer without taking keyboard focus from elsewhere. A press on dead
// space (no hit) or with no focusable ancestor leaves focus unchanged; neither
// ever CLEARS it.
// It returns the node it focused, or nil if it focused nothing, so the caller can
// verify that candidate STILL owns focus before delivering the press.
func (a *App) focusFromPointer(target *node) *node {
	scope := a.currentScope()
	for n := target; n != nil; n = n.parent {
		if !isFocusable(n) {
			continue
		}
		if !withinScope(n, scope) {
			// Outside the active scope: refuse rather than escape the trap, and
			// leave focus exactly where it was.
			a.trace(TraceEvent{Kind: TraceFocus, Node: n.id, Prev: a.focused,
				Detail: "pointer refused: candidate outside the active focus scope"})
			return nil
		}
		a.requestFocus(n)
		return n
	}
	return nil
}

// withinScope reports whether n is scope or one of its descendants. A nil scope
// is unrestricted.
func withinScope(n, scope *node) bool {
	if scope == nil {
		return true
	}
	for p := n; p != nil; p = p.parent {
		if p == scope {
			return true
		}
	}
	return false
}

// focusStep advances focus by delta (+1 Tab, -1 Shift-Tab) within the
// current scope's ring, wrapping at the ends.
func (a *App) focusStep(delta int) {
	ring := a.focusRing(a.currentScope())
	if len(ring) == 0 {
		return
	}
	cur := -1
	for i, n := range ring {
		if n.id == a.focused {
			cur = i
			break
		}
	}
	var next int
	switch {
	case cur >= 0:
		next = (cur + delta + len(ring)) % len(ring)
	case delta > 0:
		next = 0 // nothing focused: Tab lands on the first stop
	default:
		next = len(ring) - 1 // Shift-Tab lands on the last
	}
	a.setFocus(ring[next].id)
}

// acceptsFocus reports whether n is a live focus candidate right now.
//
// A node under a HIDDEN ANCESTOR is not one, whatever its own placement says.
// Hiding reaches everything under the hidden node at once, but the placement a
// node carries is only corrected by the next layout — so between a hide and
// that layout (the same handler hiding a pane and moving focus, say), the
// pane's children still look placed, and focus could be put where nothing is
// drawn.
func (a *App) acceptsFocus(n *node) bool {
	return n != nil && n.mounted && n.visible() && !hiddenWithin(n) && isFocusable(n)
}

// isFocusable reports whether n's component takes focus now: it implements Focusable and accepts
// it. Whether the node is live (mounted, laid out, shown) is acceptsFocus's question; this one is
// the component's.
func isFocusable(n *node) bool {
	f, ok := n.comp.(Focusable)
	return ok && f.AcceptsFocus()
}

// trapsFocus reports whether n's component is a FocusScope that traps focus now.
func trapsFocus(n *node) bool {
	fs, ok := n.comp.(FocusScope)
	return ok && fs.TrapsFocus()
}

// hiddenWithin reports whether n or any ancestor is hidden (Hideable). Hiding reaches a subtree at
// once, before the next layout corrects its placement flags.
func hiddenWithin(n *node) bool {
	for p := n; p != nil; p = p.parent {
		if hidden(p.comp) {
			return true
		}
	}
	return false
}
