package tui

// OVERLAY ANCHORING.
//
// A popup has to be placed relative to something: a dropdown under its field, a
// submenu beside the row that opened it. The obvious API — hand the popup its
// anchor's rectangle — is the one golib does not have, deliberately. A widget
// that receives another component's absolute rect is reasoning about foreign
// geometry, and "the runtime supplied the value" does not change who correlates
// and acts on it. It also cannot express the case that needs it most: a
// cascading menu's rows are inert model values that mount no component at all,
// so a Component-typed anchor cannot name the row a submenu hangs off.
//
// So anchoring is an ASSOCIATION DECLARED TO THE RUNTIME. The widget states an
// identity and a preference; the runtime resolves the geometry and places the
// popup. No widget touches a rect it does not own.

// RegionID names a sub-area a component declares inside its own rect — a menu
// row, a table cell, a span of text. It is scoped to the declaring node, so two
// components may use the same RegionID without colliding.
type RegionID string

// AnchorRef is an opaque handle to something a popup can be anchored to: a
// whole node, or a region inside one.
//
// OPAQUE, and issued only by the owner's own Context, because that is what
// makes ownership checkable. An earlier design let a caller name any Component
// and promised the runtime would enforce "self or descendant" — but a method on
// the overlay host receives no caller identity, so there was nothing to check
// the claim against. A ref that carries its owner cannot be forged by the
// component that consumes it.
//
// GENERATION-STAMPED, so a recycled row cannot be silently re-anchored. When an
// owner invalidates its anchors the generation moves, every ref it previously
// issued becomes stale, and the layers hanging off them are dismissed rather
// than quietly re-pointed at whatever now occupies that region.
//
// The zero value is not a valid anchor and is rejected wherever one is required.
type AnchorRef struct {
	owner  NodeID
	region RegionID // empty means the whole node
	gen    uint64
}

// Valid reports whether r names an owner at all. It does not report whether the
// owner is still mounted or the generation still current — those are resolution
// questions, answered where the anchor is used, because they change over time
// while this does not.
func (r AnchorRef) Valid() bool { return r.owner != 0 }

// Owner reports the node that issued this ref, so a host can check the anchor
// lies within the subtree it is responsible for. It deliberately exposes no
// geometry.
func (r AnchorRef) Owner() NodeID { return r.owner }

// NodeAnchor returns an anchor for this node's own rect.
func (c *Context) NodeAnchor() AnchorRef {
	return AnchorRef{owner: c.node.id, gen: c.node.anchorGen}
}

// DeclareRegion records a sub-area of this node in the node's OWN LOCAL
// coordinates and returns an anchor for it.
//
// Local, never absolute: a component knows where its own row is relative to
// itself, and that is the only frame it can speak about correctly. The runtime
// adds the node's position when it resolves the anchor, which also means a
// declared region follows its owner when the owner moves without the owner
// having to redeclare anything it did not change.
//
// Declared during Layout, when the owner has just computed where its rows are.
// Redeclaring the same RegionID replaces the previous rect — a menu whose model
// changed simply declares the new geometry, and the surviving IDs keep their
// association because a stable RegionID IS stable region identity.
func (c *Context) DeclareRegion(id RegionID, local Rect) AnchorRef {
	if c.node.regions == nil {
		c.node.regions = make(map[RegionID]Rect)
	}
	c.node.regions[id] = local
	return c.RegionAnchor(id)
}

// RegionAnchor returns an anchor for one of this node's regions WITHOUT
// declaring it.
//
// Declaring happens during Layout, when the owner knows where its rows are; an
// anchor is usually wanted later, from a handler deciding to open a popup. That
// handler has no geometry to declare and must not invent any — so it names the
// region and lets resolution fail later if the row is no longer laid out, which
// is exactly the anchor-loss path rather than a special case.
func (c *Context) RegionAnchor(id RegionID) AnchorRef {
	return AnchorRef{owner: c.node.id, region: id, gen: c.node.anchorGen}
}

// InvalidateAnchors makes every AnchorRef this node has issued stale, and
// forgets its declared regions.
//
// It is the deliberate way to say "whatever was anchored to me no longer refers
// to anything I recognise" — a list that has scrolled its rows to entirely
// different content, say. Layers hanging off the stale refs are dismissed at the
// next commit rather than silently re-pointed at the row that now occupies the
// same place, which would attach a popup to the wrong thing while looking
// correct.
//
// Ordinary content changes do NOT need this. A stable RegionID is stable
// identity, so replacing a menu's model keeps the surviving rows' anchors valid
// on purpose; calling this there would close popups the caller wanted kept.
func (c *Context) InvalidateAnchors() {
	c.node.anchorGen++
	c.node.regions = nil
}

// SubtreeContains reports whether id names this node or one of its descendants.
//
// It is the ownership check a container performs before acting on an identity
// it was handed: an overlay host asked to anchor a layer to a node outside the
// content it lays out has been asked for something it cannot do correctly, and
// this is how it tells.
func (c *Context) SubtreeContains(id NodeID) bool {
	n := c.app.nodes[id]
	return n != nil && withinScope(n, c.node)
}

// MountedComponent reports whether comp is currently mounted anywhere in this
// App's tree.
//
// The question a container must answer BEFORE adopting a component it did not
// create. Finding out by trying is not an option here: the runtime refuses a
// double mount by panicking from inside the add, which leaves the container
// holding a child it has recorded but not mounted — and the obvious undo,
// removing it, unmounts the component from wherever it legitimately lives.
//
// Distinct from Mounted, which asks about the calling node itself. This asks
// about someone else, which is why it takes the component.
func (c *Context) MountedComponent(comp Component) bool {
	if comp == nil {
		return false
	}
	n := c.app.byComp[comp]
	return n != nil && n.mounted
}

// ResolveAnchor returns the rect an anchor currently names, IN THE CALLING
// NODE'S OWN LOCAL COORDINATES, and whether it still resolves at all.
//
// It answers false — anchor lost — when the owner is unmounted, when the ref's
// generation has been superseded, when the owner has not been laid out this
// frame, or when a named region is no longer declared. Each of those is the same
// thing from the caller's side: there is nowhere to hang the popup, so it should
// be dismissed rather than placed somewhere arbitrary.
//
// CALLER-LOCAL, deliberately not absolute. The caller is placing a child, and
// PlaceChild speaks the caller's local frame; handing back an absolute rect
// would make every caller subtract an origin it has no clean way to obtain, and
// would put foreign coordinates into widget code for no reason. Converting here
// also means the one subtraction lives where the two frames are both known.
//
// A caller should already have checked SubtreeContains for the ref's owner: this
// resolves an anchor anywhere in the tree, and a rect relative to a node that is
// not an ancestor of the anchor is arithmetic without a meaning.
func (c *Context) ResolveAnchor(r AnchorRef) (Rect, bool) {
	a := c.app
	n := a.nodes[r.owner]
	if n == nil || !n.mounted || n.anchorGen != r.gen || !n.visible() {
		return Rect{}, false
	}
	self := c.node.absRect
	if r.region == "" {
		return Rect{
			X: n.absRect.X - self.X,
			Y: n.absRect.Y - self.Y,
			W: n.absRect.W,
			H: n.absRect.H,
		}, true
	}
	local, ok := n.regions[r.region]
	if !ok {
		return Rect{}, false
	}
	// The region is stated in the owner's local frame, so the owner's own
	// position is what converts it. A region larger than its owner is not
	// clamped here: an owner may legitimately declare a row that its own
	// clipping hides, and deciding what to do about that belongs to the
	// placement policy rather than to resolution.
	return Rect{
		X: n.absRect.X + local.X - self.X,
		Y: n.absRect.Y + local.Y - self.Y,
		W: local.W,
		H: local.H,
	}, true
}
