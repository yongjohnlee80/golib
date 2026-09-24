package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
)

// Adapter implements decl.Restructurer for the nodes that can support it.
//
// "The ones that can" is the whole point. tui.Container is the toolkit's own
// statement about which widgets accept child changes after construction, and it
// is not universal: widget.Split takes both of its children as required
// constructor arguments and implements no Add, Remove or Move at all. Asking
// the component rather than assuming is what lets the engine rebuild a Split
// and splice a Flex in the same reload.
var _ decl.Restructurer = (*Adapter)(nil)

// CanRestructure reports whether the node's component accepts child changes.
//
// A node that was never built, or whose component is not a tui.Container,
// answers no — and the engine rebuilds it rather than attempting an edit that
// would fail halfway.
func (a *Adapter) CanRestructure(node decl.NodeID) bool {
	b, ok := a.nodes[node]
	if !ok {
		return false
	}
	_, isContainer := b.comp.(tui.Container)
	return isContainer
}

// InsertChild places child among parent's children at index at.
//
// tui.Container has no insert-at-index: Add appends. So this appends and then
// moves, which is correct because Move is an identity-preserving splice — the
// child never unmounts between the two calls, and a freshly added child has no
// state to lose in any case.
func (a *Adapter) InsertChild(parent, child decl.NodeID, at int) error {
	c, comp, err := a.containerAndChild("insert", parent, child)
	if err != nil {
		return err
	}
	c.Add(comp)
	n := countChildren(c)
	if at < 0 || at >= n {
		// Appended already put it last, which is what an out-of-range index
		// would have meant anyway. Calling Move here would panic instead.
		return nil
	}
	c.Move(comp, at)
	return nil
}

// RemoveChild detaches child from parent, unmounting its subtree.
func (a *Adapter) RemoveChild(parent, child decl.NodeID) error {
	c, comp, err := a.containerAndChild("remove", parent, child)
	if err != nil {
		return err
	}
	c.Remove(comp)
	return nil
}

// MoveChild relocates child to index to without unmounting it, which is what
// preserves its NodeID, context, in-flight tasks, hooks and focus.
//
// The index is bound-checked here rather than trusted, because tui.Container's
// Move PANICS on an out-of-range index — a fatal error, not a returned one. An
// engine bug would otherwise take the whole program down instead of surfacing
// as the refusal this seam is shaped to carry.
func (a *Adapter) MoveChild(parent, child decl.NodeID, to int) error {
	c, comp, err := a.containerAndChild("move", parent, child)
	if err != nil {
		return err
	}
	if n := countChildren(c); to < 0 || to >= n {
		return fmt.Errorf("move: index %d is out of range for %d children of node %d", to, n, parent)
	}
	c.Move(comp, to)
	return nil
}

// containerAndChild resolves both ends of a structural operation, or says
// precisely which end was missing.
func (a *Adapter) containerAndChild(op string, parent, child decl.NodeID) (tui.Container, tui.Component, error) {
	pb, ok := a.nodes[parent]
	if !ok {
		return nil, nil, fmt.Errorf("%s: node %d has no component", op, parent)
	}
	c, ok := pb.comp.(tui.Container)
	if !ok {
		return nil, nil, fmt.Errorf("%s: %q (node %d) is not a container", op, pb.typ, parent)
	}
	cb, ok := a.nodes[child]
	if !ok {
		return nil, nil, fmt.Errorf("%s: child node %d has no component", op, child)
	}
	return c, cb.comp, nil
}

func countChildren(c tui.Container) int {
	n := 0
	for range c.Children() {
		n++
	}
	return n
}
