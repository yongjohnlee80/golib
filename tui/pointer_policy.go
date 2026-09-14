package tui

// PER-NODE POINTER POLICY.
//
// "This widget does not take mouse input" has to gate BOTH the semantic and the
// raw path, or disabling the mouse on a composite widget disables only half of
// it: the parts that read raw events go quiet while the parts driven by
// resolvers keep working.
//
// It is also INHERITED rather than checked per node. A composite widget's
// interactive parts are usually its own mounted children, and children are
// hit-tested before their parent — so a flag consulted only on the node the
// consumer set it on is a flag the runtime never reaches in time. A resize
// handle would capture the pointer under its own default-enabled policy before
// anything asked the wrapper whose mouse the consumer had just turned off.

// PointerPolicy says whether a node accepts pointer input.
type PointerPolicy uint8

const (
	// PointerInherit takes the nearest ancestor's decision, and is the default
	// so that setting a policy on a container covers everything it contains.
	PointerInherit PointerPolicy = iota
	// PointerEnabled accepts pointer input for this node and its subtree.
	PointerEnabled
	// PointerDisabled refuses pointer input for this node and its subtree.
	PointerDisabled
)

// String names the policy for traces and test failures.
func (p PointerPolicy) String() string {
	switch p {
	case PointerInherit:
		return "inherit"
	case PointerEnabled:
		return "enabled"
	case PointerDisabled:
		return "disabled"
	}
	return "unknown"
}

// SetPointerPolicy sets this node's own policy, which its descendants inherit
// unless they set one of their own.
//
// Disabling while this node or a descendant holds the pointer CANCELS that
// capture. Leaving it running would strand a gesture that no further pointer
// event can finish, because the events that would finish it are the ones now
// being refused.
func (c *Context) SetPointerPolicy(p PointerPolicy) {
	c.node.pointerPolicy = p
	c.app.captureCheckPolicy()
}

// EffectivePointerPolicy reports the policy actually in force for this node:
// the nearest ancestor-or-self that states one, defaulting to enabled at the
// root. It never returns PointerInherit.
func (c *Context) EffectivePointerPolicy() PointerPolicy {
	return effectivePointerPolicy(c.node)
}

// effectivePointerPolicy walks ancestor-or-self to the nearest stated policy.
// The root default is PointerEnabled: a tree that has said nothing about the
// mouse takes the mouse, which is what every existing component already
// assumes.
func effectivePointerPolicy(n *node) PointerPolicy {
	for ; n != nil; n = n.parent {
		if n.pointerPolicy != PointerInherit {
			return n.pointerPolicy
		}
	}
	return PointerEnabled
}

// pointerDerived reports whether ev is subject to pointer policy. Only actual
// pointer input is: a key that happens to arrive while the mouse is disabled is
// not pointer input, and gating it would make PointerDisabled mean "inert".
func pointerDerived(ev Event) bool {
	_, ok := ev.(MouseEvent)
	return ok
}

// captureCheckPolicy ends a capture whose owner may no longer take pointer
// input, so a policy change cannot leave a drag running that nothing is allowed
// to finish.
//
// The loss reads as CANCELLED rather than as its own reason: from the owner's
// side this is indistinguishable from the program calling CancelGesture, which
// is exactly what a policy change is — the program deciding this gesture is
// over.
func (a *App) captureCheckPolicy() {
	owner := a.nodes[a.captureOwner]
	if owner == nil {
		return
	}
	if effectivePointerPolicy(owner) == PointerDisabled {
		a.loseCapture(CaptureLostCancelled)
	}
}
