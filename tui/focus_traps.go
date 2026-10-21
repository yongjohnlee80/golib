package tui

// Trap admission: whether focus may enter a stacked trapping scope, and the tree walks that decide it.

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
	if trapsFocus(n) {
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
		if trapsFocus(n) {
			return n
		}
	}
	return nil
}
