package tui

// Directed focus: FocusInto and the subtree searches it and FocusWithin use.

// FocusInto is Context.FocusInto for a caller holding the App rather than a
// mounted component's Context — a declarative layer over the tree, say. Loop
// goroutine only.
func (a *App) FocusInto(comp Component) bool { return a.focusInto(comp, true) }

// focusInto is FocusInto; mayDefer is false for the post-layout attempt, which
// is the one attempt a deferral buys and never schedules another.
func (a *App) focusInto(comp Component, mayDefer bool) bool {
	if comp == nil {
		return false
	}
	n := a.byComp[comp]
	if n == nil {
		return false // not mounted
	}
	target := a.focusTargetIn(n)
	if target == nil {
		// Shown in this very turn — its pane made visible a moment ago —
		// and not laid out yet: focus it once it is, as Qt's
		// forceActiveFocus takes an item that is being shown. A hidden
		// subtree is not deferred; it takes none.
		if mayDefer && a.unplacedTargetIn(n) {
			a.pendingFocusInto = n.id
			a.invalidate(true)
		}
		return false
	}
	a.requestFocus(target)
	return withinScope(a.nodes[a.focused], n)
}

// FocusWithin is Context.FocusWithin for a caller holding the App rather than
// a mounted component's Context — a declarative host asking which of its panes
// holds the keyboard. False when nothing is focused or comp is not mounted.
// Loop goroutine only.
func (a *App) FocusWithin(comp Component) bool {
	n := a.byComp[comp]
	return n != nil && withinScope(a.nodes[a.focused], n)
}

// unplacedTargetIn reports whether n's subtree holds a component that takes
// focus now and is kept from it only by not having been laid out yet: mounted,
// accepting, under no hidden ancestor, and not measured by the last layout. A
// node the last layout measured and left with no cells — a pane clipped to
// nothing — is not waiting for a layout; another would leave it the same.
func (a *App) unplacedTargetIn(n *node) bool {
	if hiddenWithin(n) {
		return false
	}
	var walk func(n *node) bool
	walk = func(n *node) bool {
		if hidden(n.comp) {
			return false
		}
		if isFocusable(n) && n.mounted && !n.measured {
			return true
		}
		for _, ch := range n.children {
			if walk(ch) {
				return true
			}
		}
		return false
	}
	return walk(n)
}

// HoldsFocusable is Context.HoldsFocusable for a caller holding the App. It
// also reports whether comp is mounted: "holds none" is only an answer about
// a component that is. Loop goroutine only.
func (a *App) HoldsFocusable(comp Component) (holds, mounted bool) {
	n := a.byComp[comp]
	if n == nil {
		return false, false
	}
	return holdsFocusable(n), true
}

// focusableByDesign reports whether comp is focusable by design: it implements
// Focusable, and — when its focusability is a per-instance choice — was built
// to take focus (FocusDesigner). Not what it accepts now.
func focusableByDesign(comp Component) bool {
	if d, ok := comp.(FocusDesigner); ok {
		return d.FocusableByDesign()
	}
	_, ok := comp.(Focusable)
	return ok
}

// holdsFocusable reports whether n or a descendant is focusable by design.
func holdsFocusable(n *node) bool {
	if focusableByDesign(n.comp) {
		return true
	}
	for _, ch := range n.children {
		if holdsFocusable(ch) {
			return true
		}
	}
	return false
}

// focusTargetIn is what FocusInto focuses for n: n itself when it takes focus,
// else its validated nominee, else the first descendant, in document order,
// that has a target of its own. Nil when nothing in n takes focus now.
func (a *App) focusTargetIn(n *node) *node {
	if a.acceptsFocus(n) {
		return n
	}
	if t, _ := a.nominatedFocus(n); t != nil {
		return t
	}
	for _, ch := range n.children {
		if t := a.focusTargetIn(ch); t != nil {
			return t
		}
	}
	return nil
}
