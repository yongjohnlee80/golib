package tui

import (
	"context"
)

// NodeID identifies a mounted component. 0 = none; assigned monotonically
// at mount; never reused for the App's lifetime.
type NodeID uint64

// node is the runtime's per-mount bookkeeping record. The authoritative
// table is App.nodes (map[NodeID]*node — every internal path is ID-keyed);
// App.byComp is the Component-keyed identity index serving the public
// Component-keyed API (Context.Unmount, Container.Remove,
// LayoutChild/PlaceChild). All fields are loop-goroutine-owned.
type node struct {
	id       NodeID
	comp     Component
	parent   *node
	children []*node // append order == document order == focus/paint order

	ctx    *Context
	cctx   context.Context    // derived from the parent node's context at mount
	cancel context.CancelFunc // unmount cancels; cascades to descendants for free
	hooks  []func()           // OnUnmount hooks, run LIFO at unmount

	mounted bool

	// resolvers is this node's two-layer action resolver chain, and
	// pointerPolicy its own pointer decision (PointerInherit = defer to an
	// ancestor). Both are loop-goroutine-owned like everything else here.
	resolvers     resolverSet
	pointerPolicy PointerPolicy

	// Anchor state. anchorGen stamps every AnchorRef this node issues, so
	// InvalidateAnchors can retire all of them at once by moving the number;
	// regions holds the sub-areas declared during Layout, in this node's own
	// local coordinates.
	anchorGen uint64
	regions   map[RegionID]Rect

	// Layout state. measured/placed reset each pass; a node is visible this
	// frame only when both are set with a non-empty rect.
	rect     Rect // parent-relative, set by PlaceChild
	absRect  Rect // absolute, derived after the pass
	size     Size // clamped Layout return
	measured bool // Layout ran this pass
	placed   bool // PlaceChild ran this pass
}

// visible reports whether n was laid out in the current frame with a
// non-empty Rect — the render/hit-test/tab-stop condition.
func (n *node) visible() bool {
	return n.measured && n.placed && !n.rect.Empty() && !hidden(n.comp)
}
