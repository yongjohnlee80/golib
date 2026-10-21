package tui

// Focus repair: re-validating the focused node after the tree or its focusability changes.

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
		// Still legal: focus stays where the user put it, as Qt's does. A
		// scope owner's nomination says where focus STARTS and where it goes
		// when it has to move; it never takes the keyboard off a control that
		// can still hold it — a list replaced under the cursor, a button added
		// beside the focused one.
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
