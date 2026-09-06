package auth

// Node is one position in a policy tree. The interface is CLOSED — its only
// method is unexported — so the only nodes are Leaf, All and Any. That closure
// is what lets NewPolicy reason about a finished tree.
type Node interface {
	isNode()

	// identityBearing reports whether a SUCCESS of this node necessarily
	// carried an identity proof. The algebra:
	//
	//	leaf  — its Kind() == FactorIdentity
	//	All   — ANY child is identity-bearing (the others constrain it)
	//	Any   — EVERY branch is identity-bearing, since one contextual
	//	        branch would be a way in
	identityBearing() bool

	// eval returns the contributions of a successful evaluation.
	eval(ctx evalCtx) ([]scoped, error)
}

// scoped pairs a contribution with the declared kind of the factor that
// produced it, so merging can enforce the Subject rule.
type scoped struct {
	c    Contribution
	kind FactorKind
}

// leafNode wraps one Factor.
type leafNode struct{ f Factor }

// Leaf makes an external Factor into a Node — the only way one enters a tree.
func Leaf(f Factor) Node { return leafNode{f: f} }

func (leafNode) isNode()                 {}
func (n leafNode) identityBearing() bool { return n.f.Kind() == FactorIdentity }

// allNode requires every child.
type allNode struct{ children []Node }

// All builds a node satisfied only when every child is. All() with no children
// is a node that DENIES — distinct from an invalid policy, which is NewPolicy's
// concern.
func All(ns ...Node) Node { return allNode{children: ns} }

func (allNode) isNode() {}

func (n allNode) identityBearing() bool {
	for _, c := range n.children {
		if c.identityBearing() {
			return true
		}
	}
	return false
}

// anyNode is satisfied by its first passing branch.
type anyNode struct{ children []Node }

// Any builds a node satisfied by the first passing branch. Any() with no
// children DENIES.
func Any(ns ...Node) Node { return anyNode{children: ns} }

func (anyNode) isNode() {}

func (n anyNode) identityBearing() bool {
	if len(n.children) == 0 {
		return false
	}
	for _, c := range n.children {
		if !c.identityBearing() {
			return false
		}
	}
	return true
}
