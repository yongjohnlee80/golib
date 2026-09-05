package tui

// Focus management — framework-owned end to end: one focused NodeID on the
// App (0 = none), Tab/Shift-Tab traversal in mount (document) order,
// trapping focus scopes with restore-on-unmount, and focus repair so no
// frame ever renders with a dangling focus ID.

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
	newScope := a.trapScopeOf(n)
	var oldScope *node
	if on := a.nodes[a.focused]; on != nil {
		oldScope = a.trapScopeOf(on)
	}
	if newScope != nil && newScope != oldScope {
		// Entering a trap: remember where focus came from.
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: newScope.id, restore: a.focused})
		a.trace(TraceEvent{Kind: TraceScope, Node: newScope.id, Prev: a.focused, Detail: "open"})
	}
	a.setFocus(n.id)
}

// setFocus moves focus and delivers the two FocusEvents: Gained=false to
// the loser, then Gained=true to the gainer; both bubble so ancestor panels
// can restyle (the lazygit active-panel border pattern.5.3).
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
	a.renderDirty = true // the cursor rule re-evaluates next frame (ADR-0004 §2.3)
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

// currentScope resolves the traversal boundary: the innermost trapping
// scope of the focused node, else the root.
func (a *App) currentScope() *node {
	if fn := a.nodes[a.focused]; fn != nil {
		if s := a.trapScopeOf(fn); s != nil {
			return s
		}
	} else if len(a.scopeStack) > 0 {
		// No live focused node (repair path): fall back to the innermost
		// surviving trap, if any.
		if sn := a.nodes[a.scopeStack[len(a.scopeStack)-1].scope]; sn != nil {
			return sn
		}
	}
	return a.rootNode
}

// focusFromPointer focuses the first focusable node at or above the pointer
// target, provided that node lies inside the ACTIVE focus scope (ADR-0010 §2.1
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
		return
	}
	a.trace(TraceEvent{Kind: TraceFocusRepair, Node: ring[0].id, Prev: a.focused,
		Detail: "re-homed to the first focusable in scope"})
	a.setFocus(ring[0].id)
}
