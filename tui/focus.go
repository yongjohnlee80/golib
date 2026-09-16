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

// mayEnterStackedTrap reports whether focus may move from the active trap into
// cand, a trapping scope that is NOT an ancestor-or-self of it.
//
// Ancestry answers the two shapes golib grew up with — a nested trap is inside
// the active scope, an unrelated one is never live at the same time — and says
// nothing useful about the third, which is the one an overlay host produces.
// There the two traps are COUSINS: each floatLayer is mounted inside its own
// Float, and the Floats are layers of one Stack. Nothing is an ancestor of
// anything, so ancestry can only ever refuse.
//
// The question that actually distinguishes a dialog in front from an unrelated
// panel beside is who OWNS the two branches. Document order does not: two
// trapping panels side by side in a Flex are also "one after the other", and
// deciding on that alone lets declaration order pick which unrelated panel may
// steal the keyboard. So the common ancestor has to say, itself, that its
// children are layers.
//
// Four things must hold, and each refuses a real arrangement:
//
//  1. cand is inside some trapping scope — otherwise this is the escape into
//     unconfined space the guard exists to stop.
//  2. their lowest common ancestor declares [FocusLayerHost] — so a Flex, a
//     Dock, a custom container, and two SEPARATE overlay hosts are all refused.
//  3. cand's branch of that host comes after the active one — a layer behind
//     cannot pull focus out of the one in front.
//  4. no later branch holds a live trap — only the TOPMOST dialog may be
//     entered, not merely one that happens to be above the active scope.
func (a *App) mayEnterStackedTrap(cand, active *node) bool {
	if cand == nil || active == nil {
		return false
	}
	host, candBranch, activeBranch := lcaBranches(cand, active)
	if host == nil || candBranch == nil || activeBranch == nil {
		return false
	}
	if !hostsLayers(host) {
		return false
	}
	ci, ai := childIndex(host, candBranch), childIndex(host, activeBranch)
	if ci < 0 || ai < 0 || ci <= ai {
		return false
	}
	return a.topmostUpTo(cand, host)
}

// topmostUpTo reports whether nothing is stacked in front of cand at ANY layer
// host between it and boundary, the boundary included.
//
// Checking only the boundary is not enough once layer hosts nest, and nesting
// them is ordinary: a Stack whose later branch is itself a Stack. Ask the outer
// host alone and it sees the inner host as one opaque branch with nothing after
// it, and happily reports a dialog as topmost while another sits in front of it
// INSIDE that branch. The candidate has to be in front at every level that
// stacks, not merely at the level where the two paths meet.
//
// Walked from the candidate outward, so each step asks the only question that
// host can answer: of ITS children, is the one leading to the candidate the last
// that holds a trap.
func (a *App) topmostUpTo(cand, boundary *node) bool {
	child := cand
	for n := cand.parent; n != nil; n = n.parent {
		if hostsLayers(n) {
			i := childIndex(n, child)
			if i < 0 {
				return false
			}
			for _, later := range n.children[i+1:] {
				if a.holdsLiveTrap(later) {
					return false
				}
			}
		}
		if n == boundary {
			return true
		}
		child = n
	}
	// Walked past the boundary without meeting it: the two are not related the
	// way the caller established, so nothing here may be granted.
	return false
}

// hostsLayers reports whether n declares its children to be stacked layers.
func hostsLayers(n *node) bool {
	h, ok := n.comp.(FocusLayerHost)
	return ok && h.HostsFocusLayers()
}

// lcaBranches returns the lowest common ancestor of x and y together with the
// immediate child of that ancestor on each path — the two BRANCHES whose order
// decides which is in front.
//
// A branch comes back nil when one node is an ancestor of the other, because
// then there is no branch to compare on that side. Callers treat that as a
// refusal; it is the nested shape, which ancestry has already answered.
func lcaBranches(x, y *node) (lca, bx, by *node) {
	branch := map[*node]*node{}
	var child *node
	for n := x; n != nil; n = n.parent {
		branch[n] = child
		child = n
	}
	child = nil
	for n := y; n != nil; n = n.parent {
		if b, ok := branch[n]; ok {
			return n, b, child
		}
		child = n
	}
	return nil, nil, nil
}

// childIndex is the position of child among parent's children, or -1.
func childIndex(parent, child *node) int {
	for i, c := range parent.children {
		if c == child {
			return i
		}
	}
	return -1
}

// holdsLiveTrap reports whether n or any descendant is a trapping scope.
//
// It DESCENDS rather than testing the branch itself, because the branch usually
// is not the trap. A widget.Float is an ordinary child of its host and mounts
// its trapping layer inside itself, so the thing that traps sits one level down
// from the layer the host knows about.
//
// Everything in the tree is live by construction — a Float that is attached but
// not shown has no layer in the tree at all — so there is no mounted-ness to
// re-check here.
func (a *App) holdsLiveTrap(n *node) bool {
	if fs, ok := n.comp.(FocusScope); ok && fs.TrapsFocus() {
		return true
	}
	for _, ch := range n.children {
		if a.holdsLiveTrap(ch) {
			return true
		}
	}
	return false
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
	// Inside a batch the repair is deferred, not skipped. A composition that
	// unmounts three buttons and mounts two would otherwise repair five times
	// against intermediate lists that were never a state the caller asked for,
	// and land focus somewhere the final list does not justify.
	if a.batchDepth > 0 {
		a.batchRepair = true
		return
	}
	scope := a.currentScope()
	nom, retryNom := a.nominatedFocus(scope)
	if nom != nil {
		a.trace(TraceEvent{Kind: TraceFocusRepair, Node: nom.id, Prev: a.focused,
			Detail: "re-homed to the scope owner's nominated target"})
		a.setFocus(nom.id)
		return
	}
	if retryNom {
		a.pendingRepair = true
	}
	ring := a.focusRing(scope)
	if len(ring) == 0 {
		a.trace(TraceEvent{Kind: TraceFocusRepair, Prev: a.focused,
			Detail: "no focusable in scope"})
		// setFocus(0), not a direct assignment. This path was written for
		// unmount, where the dead id is already cleared and nobody is left to
		// notify — but it is now also reached from the public invalidation
		// seam, with a node that is still MOUNTED and has merely stopped
		// accepting focus. Assigning directly changed the focused id behind
		// that node's back: neither it nor its ancestors received the
		// focus-loss event the runtime promises.
		//
		// setFocus does the whole transition — the loser's bubbled
		// FocusEvent, the capture revalidation, the repaint and the wake-up —
		// and is a no-op when focus is already zero, so the unmount path is
		// unchanged.
		// Retried after the next layout. A repair that runs immediately after a
		// mount cannot see the nodes that were just added: focusability requires
		// a measure and a placement, and neither exists until the frame runs. So
		// "no focusable in scope" is genuinely ambiguous here — it means either
		// the scope is empty, or its candidates have not been laid out yet — and
		// giving up leaves focus nowhere for a dialog whose buttons are about to
		// appear. One retry after layout tells the two apart.
		a.pendingRepair = true
		a.setFocus(0)
		// And the capture check explicitly, because setFocus cannot do it on
		// every path that reaches here. The UNMOUNT caller zeroes focus before
		// calling repair, so setFocus(0) is then a no-op — correct for the
		// notification, since the dead node cannot be told anything, but it
		// also skips the capture revalidation that the same step used to
		// perform. A non-focusable capture owner would keep the pointer after
		// its only focused descendant died.
		//
		// Idempotent: once a loss has been delivered there is no owner left, so
		// the live path running this a second time does nothing.
		a.captureCheckFocus()
		return
	}
	a.trace(TraceEvent{Kind: TraceFocusRepair, Node: ring[0].id, Prev: a.focused,
		Detail: "re-homed to the first focusable in scope"})
	a.setFocus(ring[0].id)
}

// InvalidateFocusability revalidates the whole active focus scope after a
// component's focusability has changed, and repairs focus before returning.
//
// WHOLE SCOPE, not just the calling node, and that is the point rather than an
// excess of caution. Enabling the first control in a dialog where everything
// was disabled changes the eligibility of a node that is not the one that
// changed: the dialog itself had been accepting focus as the fallback, and now
// must stop. A per-node revalidation cannot see that.
//
// SYNCHRONOUS, because the caller has just made the currently focused node
// ineligible and every subsequent line — a traversal, a query, a repaint —
// would otherwise run against a focus that is already wrong. Deferring it would
// make "disabled" mean "disabled one frame from now".
func (c *Context) InvalidateFocusability() {
	a := c.app
	// Inside a batch this is deferred with everything else. Revalidating against
	// a half-applied list is exactly what the batch exists to prevent, and the
	// end-of-batch step runs this same revalidation once.
	if a.batchDepth > 0 {
		a.batchRepair = true
		return
	}
	a.revalidateFocus()
}

// revalidateFocus is the whole-scope check: repair unless the focused node is
// still a legal target of the active scope. It is the body InvalidateFocusability
// runs directly and the batch boundary runs once at the end, so the deferred
// path and the immediate path cannot come to mean different things.
func (a *App) revalidateFocus() {
	// Eligible AND inside the scope this is revalidating. A focused node can be
	// perfectly focusable while sitting outside the active scope — the trap
	// rules make that state reachable — and returning early for it would leave
	// the scope unrepaired while reporting that it had been checked.
	scope := a.currentScope()
	if n := a.nodes[a.focused]; n != nil && withinScope(n, scope) && a.acceptsFocus(n) {
		// Still legal, but possibly not what the scope's owner would choose. A
		// provider's preference applies on EVERY repair, not only when focus
		// died: SetButtons that adds a Default-role button while a plain one
		// holds focus leaves focus legal and wrong, and an early return here is
		// what made that state reachable.
		nom, retryNom := a.nominatedFocus(scope)
		if nom != nil && nom.id != a.focused {
			a.trace(TraceEvent{Kind: TraceFocusRepair, Node: nom.id, Prev: a.focused,
				Detail: "moved to the scope owner's nominated target"})
			a.setFocus(nom.id)
		}
		if retryNom {
			// The nominee exists but is not laid out yet, so it cannot legally
			// take focus in this turn. Focus is currently VALID, so nothing here
			// forces a retry on its own — and without one the dialog's
			// preference is silently lost for a control that appears next frame.
			a.pendingRepair = true
		}
		return
	}
	a.repairFocus()
}

// nominatedFocus asks the component owning scope where focus should land, and
// returns its nominee only if the nominee survives validation. A nil node means
// "no usable nomination" and the caller falls back to document order.
//
// The second result says RETRY AFTER LAYOUT: the nominee is real and inside the
// scope, and failed only because it has not been measured and placed yet.
// Focusability requires a layout, so a nomination made in the same turn as the
// mount that created it can never be honoured immediately — and treating that
// as "no nomination" loses the preference for exactly the controls that were
// just added, which is the common case rather than an edge one.
//
// A nomination is a PREFERENCE, never an instruction. The provider is ordinary
// component code and can nominate a button it has just unmounted, one it
// disabled, or one belonging to a different dialog; honouring any of those would
// park focus somewhere the user cannot reach and leave the scope with no live
// focus at all. Validation is what makes the seam safe to expose.
func (a *App) nominatedFocus(scope *node) (target *node, retryAfterLayout bool) {
	if scope == nil {
		return nil, false
	}
	p, ok := scope.comp.(InitialFocusProvider)
	if !ok {
		return nil, false
	}
	// Consulted exactly once. The nominee is used as given and never asked
	// again, so a provider cannot nominate another provider and recurse.
	comp, ok := p.InitialFocus()
	if !ok || comp == nil {
		return nil, false
	}
	n := a.byComp[comp]
	if n == nil {
		return nil, false // not mounted at all
	}
	// Outside the provider's own subtree. A provider may only place focus within
	// itself; nominating a sibling's control would let a dialog steal focus out
	// of the scope that is supposed to confine it.
	if !withinScope(n, scope) {
		return nil, false
	}
	if !a.acceptsFocus(n) {
		// Mounted and in scope, but not a live candidate. Not-yet-laid-out is
		// the one reason worth waiting for; anything else — hidden, disabled,
		// unmounted — is a standing answer and falls back now.
		return nil, n.mounted && !n.visible()
	}
	return n, false
}

// acceptsFocus reports whether n is a live focus candidate right now.
func (a *App) acceptsFocus(n *node) bool {
	if n == nil || !n.mounted || !n.visible() {
		return false
	}
	f, ok := n.comp.(Focusable)
	return ok && f.AcceptsFocus()
}
