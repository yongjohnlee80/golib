package tui

// Focus management — framework-owned end to end (ADR-0004 §2.6): one focused
// NodeID on the App (0 = none), Tab/Shift-Tab traversal in mount (document)
// order, trapping focus scopes with restore-on-unmount, and focus repair so
// no frame ever renders with a dangling focus ID.

// scopeEntry records a focus trap entered via RequestFocus: when the
// trapping scope unmounts, focus restores to the node focused before entry
// (ADR-0004 §2.6.3).
type scopeEntry struct {
	scope   NodeID // the trapping FocusScope node
	restore NodeID // focused node before the trap was entered
}

// requestFocus implements Context.RequestFocus (ADR-0004 §2.6.1). Ignored
// unless the component is Focusable and currently accepts focus (the
// visibility filter applies to traversal only — a component may legally
// request focus from Init, before any layout pass).
// requestFocusByID focuses the node with the given id if it exists and is
// focusable. Reports whether focus ended up there.
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
		// Entering a trap: remember where focus came from
		// (ADR-0004 §2.6.3).
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: newScope.id, restore: a.focused})
		a.trace(TraceEvent{Kind: TraceScope, Node: newScope.id, Prev: a.focused, Detail: "open"})
	}
	a.setFocus(n.id)
}

// setFocus moves focus and delivers the two FocusEvents: Gained=false to the
// loser, then Gained=true to the gainer; both bubble so ancestor panels can
// restyle (the lazygit active-panel border pattern — ADR-0004 §2.5.3).
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
// non-trapping scope (ADR-0004 §2.6.3).
func (a *App) trapScopeOf(n *node) *node {
	for ; n != nil; n = n.parent {
		if fs, ok := n.comp.(FocusScope); ok && fs.TrapsFocus() {
			return n
		}
	}
	return nil
}

// focusRing collects the tab stops in traversal order (ADR-0004 §2.6.2):
// pre-order depth-first in child (document) order, filtered to nodes that
// (a) implement Focusable, (b) report AcceptsFocus(), and (c) were laid out
// in the current frame with a non-empty Rect. Nested trapping scopes are
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
// scope of the focused node, else the root (ADR-0004 §2.6.2/§2.6.3).
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

// focusStep advances focus by delta (+1 Tab, -1 Shift-Tab) within the
// current scope's ring, wrapping at the ends (ADR-0004 §2.6.2).
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
// focusables (ADR-0004 §2.6.4). Runs inside the unmount cascade so no frame
// renders a dangling ID.
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
