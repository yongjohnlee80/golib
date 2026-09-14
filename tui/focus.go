package tui

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
	f, ok := n.comp.(Focusable)
	if !ok || !f.AcceptsFocus() {
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
		a.trace(TraceEvent{Kind: TraceFocus, Node: n.id, Prev: a.focused,
			Detail: "focus refused: target outside the active focus scope"})
		return
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
	a.trace(TraceEvent{Kind: TraceFocus, Node: id, Prev: old})
	if on := a.nodes[old]; on != nil {
		a.bubble(on, FocusEvent{Gained: false})
	}
	if nn := a.nodes[id]; nn != nil {
		a.bubble(nn, FocusEvent{Gained: true})
	}
	// A capture survives focus moving around inside the owner's own subtree and
	// ends when focus leaves it. Checked after the FocusEvents are delivered so
	// that a handler which moves focus onward is accounted for by the check
	// rather than racing it.
	a.captureCheckFocus()
	a.renderDirty = true // the cursor rule re-evaluates next frame
	a.queue.wakeUp()
}

// trapScopeOf returns the nearest ancestor (inclusive) implementing
// FocusScope with TrapsFocus() == true; nil means the root's implicit
// non-trapping scope.
func (a *App) trapScopeOf(n *node) *node {
	for ; n != nil; n = n.parent {
		if fs, ok := n.comp.(FocusScope); ok && fs.TrapsFocus() {
			return n
		}
	}
	return nil
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
		if n != scope {
			if fs, ok := n.comp.(FocusScope); ok && fs.TrapsFocus() {
				return // another trap's subtree is not in this ring
			}
		}
		if f, ok := n.comp.(Focusable); ok && f.AcceptsFocus() && n.visible() {
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
		f, ok := n.comp.(Focusable)
		if !ok || !f.AcceptsFocus() {
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

// repairFocus re-homes a dead focus: the first focusable in traversal order
// within the innermost surviving scope, or 0 (none) when the scope has no
// focusables. Runs inside the unmount cascade so no frame renders a
// dangling ID.
func (a *App) repairFocus() {
	ring := a.focusRing(a.currentScope())
	if len(ring) == 0 {
		a.trace(TraceEvent{Kind: TraceFocusRepair, Prev: a.focused,
			Detail: "no focusable in scope"})
		a.focused = 0
		// This assignment bypasses setFocus, so the capture check setFocus
		// performs has to be repeated here. This is the FINAL result of the
		// repair, not the temporary zero the unmount path writes before it has
		// chosen a restore target, so a loss decided here is decided once and
		// against the focus the tree actually ended up with.
		a.captureCheckFocus()
		return
	}
	a.trace(TraceEvent{Kind: TraceFocusRepair, Node: ring[0].id, Prev: a.focused,
		Detail: "re-homed to the first focusable in scope"})
	a.setFocus(ring[0].id)
}
