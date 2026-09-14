package widget

import (
	"errors"
	"fmt"
	"iter"
	"slices"

	"github.com/yongjohnlee80/golib/tui"
)

// ANCHORED OVERLAY LAYERS.
//
// A popup placed relative to something — a dropdown under its field, a submenu
// beside the row that opened it — needs three things that no single phase can
// supply at once: the anchor's geometry, the popup's own desired size, and a
// decision about what to do when the preferred position does not fit.
//
// The anchor's geometry is not known until the frame is laid out. The popup's
// size is not known until it is measured, which is part of that same pass. So
// opening an anchored layer cannot be a synchronous placement: it is a
// REGISTRATION, and the host's own Layout resolves it afterwards. What a caller
// gets in exchange is the property that actually matters — nothing half-open is
// ever rendered.

// ErrEmptyLayerID is returned when an anchored layer is opened under no id.
var ErrEmptyLayerID = errors.New("widget: empty LayerID")

// ErrLayerAlreadyRegistered is returned when the same layer instance is already
// open under a different id.
var ErrLayerAlreadyRegistered = errors.New("widget: layer already registered under another id")

// ErrAnchorUnusable is returned when an AnchorSpec names an anchor this host
// cannot use: the zero ref, or one whose owner lies outside the host's content.
var ErrAnchorUnusable = errors.New("widget: anchor is not usable by this host")

// AnchorSpec says what a layer is anchored to and where it would prefer to sit.
type AnchorSpec struct {
	// Ref is the anchor, issued by the owning component's own Context.
	Ref tui.AnchorRef
	// Pref is the preferred side, alignment and offset. The zero value is
	// below the anchor, aligned to its start, unnudged — ordinary menu
	// behaviour, so a caller with no opinion supplies nothing.
	Pref Placement
}

// AnchorPolicy decides where a popup actually goes when its preferred position
// would not fit.
//
// PURE: given the same rectangles it returns the same answer. It receives no
// tree access, no component and no context, because placement is arithmetic and
// a policy that could reach the tree would be doing layout from outside the
// layout phase. The runtime bounds whatever it returns to the viewport, so a
// policy cannot place a layer off-screen even by mistake.
type AnchorPolicy interface {
	Place(anchor, viewport tui.Rect, want tui.Size, pref Placement) tui.Rect
}

// AnchorPolicyFunc adapts a plain function to AnchorPolicy.
type AnchorPolicyFunc func(anchor, viewport tui.Rect, want tui.Size, pref Placement) tui.Rect

// Place calls f.
func (f AnchorPolicyFunc) Place(anchor, viewport tui.Rect, want tui.Size, pref Placement) tui.Rect {
	return f(anchor, viewport, want, pref)
}

// FlipClipPolicy is the default: try the preferred side, flip to the opposite
// side when the popup does not fit there, and clip to the viewport if it fits
// on neither.
//
// Flipping before clipping is the order that keeps a menu usable. A dropdown
// near the bottom of the screen that clipped would show two of its rows; the
// same dropdown flipped above its field shows all of them, which is what every
// toolkit does and what a user expects without being taught.
type FlipClipPolicy struct{}

// Place implements AnchorPolicy.
func (FlipClipPolicy) Place(anchor, viewport tui.Rect, want tui.Size, pref Placement) tui.Rect {
	side := pref.Side
	if !side.Valid() {
		side = PlacementBelow
	}
	align := pref.Align
	if !align.Valid() {
		align = PlacementAlignStart
	}
	if px, py := originFor(anchor, side, want); !fits(px, py, want, viewport) {
		flipped := oppositeSide(side)
		if fx, fy := originFor(anchor, flipped, want); fits(fx, fy, want, viewport) {
			side = flipped
		}
	}
	x, y := originFor(anchor, side, want)
	x, y = alignAlong(x, y, anchor, side, align, want)
	x += pref.Offset.X
	y += pref.Offset.Y
	return clampInto(tui.Rect{X: x, Y: y, W: want.W, H: want.H}, viewport)
}

// originFor is the popup's top-left on the chosen side, before alignment.
func originFor(anchor tui.Rect, side PlacementSide, want tui.Size) (x, y int) {
	switch side {
	case PlacementAbove:
		return anchor.X, anchor.Y - want.H
	case PlacementRight:
		return anchor.X + anchor.W, anchor.Y
	case PlacementLeft:
		return anchor.X - want.W, anchor.Y
	default: // PlacementBelow
		return anchor.X, anchor.Y + anchor.H
	}
}

// oppositeSide is the side a popup flips to when its preferred one does not fit.
func oppositeSide(s PlacementSide) PlacementSide {
	switch s {
	case PlacementAbove:
		return PlacementBelow
	case PlacementRight:
		return PlacementLeft
	case PlacementLeft:
		return PlacementRight
	default:
		return PlacementAbove
	}
}

// alignAlong shifts the popup along the anchor's OTHER axis: horizontally for a
// popup above or below, vertically for one beside.
func alignAlong(x, y int, anchor tui.Rect, side PlacementSide, align PlacementAlign, want tui.Size) (int, int) {
	vertical := side == PlacementBelow || side == PlacementAbove
	switch align {
	case PlacementAlignCenter:
		if vertical {
			return x + (anchor.W-want.W)/2, y
		}
		return x, y + (anchor.H-want.H)/2
	case PlacementAlignEnd:
		if vertical {
			return x + anchor.W - want.W, y
		}
		return x, y + anchor.H - want.H
	}
	return x, y // PlacementAlignStart: already at the anchor's start edge
}

// fits reports whether a popup of want at (x, y) lies wholly inside viewport.
func fits(x, y int, want tui.Size, viewport tui.Rect) bool {
	return x >= viewport.X && y >= viewport.Y &&
		x+want.W <= viewport.X+viewport.W &&
		y+want.H <= viewport.Y+viewport.H
}

// clampInto slides r inside viewport, shrinking it only when it cannot fit.
//
// Sliding before shrinking, because a popup one column off the edge should move
// one column rather than lose a column of its content.
func clampInto(r, viewport tui.Rect) tui.Rect {
	if r.W > viewport.W {
		r.W = viewport.W
	}
	if r.H > viewport.H {
		r.H = viewport.H
	}
	if r.X+r.W > viewport.X+viewport.W {
		r.X = viewport.X + viewport.W - r.W
	}
	if r.Y+r.H > viewport.Y+viewport.H {
		r.Y = viewport.Y + viewport.H - r.H
	}
	if r.X < viewport.X {
		r.X = viewport.X
	}
	if r.Y < viewport.Y {
		r.Y = viewport.Y
	}
	return r
}

// anchoredLayer is one registered association.
//
// It deliberately does NOT cache the resolved rect. The node already holds where
// it was placed, and a second copy here would be a field written every frame and
// read by nothing — or, worse, read once and found stale.
type anchoredLayer struct {
	id   LayerID
	comp tui.Component
	spec AnchorSpec
	pol  AnchorPolicy
}

// OpenAnchored registers a layer anchored to spec, mounts it, and schedules the
// layout that will place it.
//
// A REGISTRATION TRANSACTION, not synchronous geometry: the anchor's rect and
// the layer's own size are both products of the layout pass that has not run
// yet. Atomic means no intermediate render — validation happens before any
// mutation, and a failure leaves the host exactly as it was.
//
// AN EXISTING id IS REPLACED rather than refused. Reopening a dropdown that is
// already showing is the ordinary way a user toggles one, and an error there
// would make every caller track open state the host already knows. The
// replacement is atomic too: the new layer is mounted before the old is
// unmounted, so a failure leaves the OLD layer open rather than leaving the id
// empty.
//
// A nil policy selects FlipClipPolicy. Whatever policy is used, the rect it
// returns is bounded to the viewport by the host.
func (h *OverlayHost) OpenAnchored(id LayerID, layer tui.Component, spec AnchorSpec, pol AnchorPolicy) error {
	if id == "" {
		return ErrEmptyLayerID
	}
	if layer == nil {
		return fmt.Errorf("%w: nil layer", ErrAnchorUnusable)
	}
	if !spec.Ref.Valid() {
		return fmt.Errorf("%w: the zero AnchorRef names no owner", ErrAnchorUnusable)
	}
	if !spec.Pref.Valid() {
		return fmt.Errorf("%w: placement preference is out of range", ErrAnchorUnusable)
	}
	if h.ctx == nil {
		return fmt.Errorf("%w: the host is not mounted", ErrAnchorUnusable)
	}
	// The anchor's owner must be inside this host, or the host would be placing
	// a layer against geometry belonging to a tree it does not lay out.
	if !h.ctx.SubtreeContains(spec.Ref.Owner()) {
		return fmt.Errorf("%w: its owner is outside this host", ErrAnchorUnusable)
	}
	// The same component cannot be two layers: it would need to be mounted
	// twice, which the runtime refuses, and the second id could never be closed
	// independently.
	for _, al := range h.anchored {
		if al.comp == layer && al.id != id {
			return fmt.Errorf("%w: %q", ErrLayerAlreadyRegistered, al.id)
		}
	}

	prev := h.indexAnchored(id)
	if prev >= 0 && h.anchored[prev].comp == layer {
		// Same layer, same id: re-registering updates the preference without
		// disturbing a mounted subtree that is already correct.
		h.anchored[prev].spec = spec
		h.anchored[prev].pol = policyOr(pol)
		h.ctx.RequestLayout()
		return nil
	}

	if err := h.mountLayer(layer); err != nil {
		return err // the old layer, if any, is untouched
	}
	if prev >= 0 {
		old := h.anchored[prev].comp
		h.anchored = slices.Delete(h.anchored, prev, prev+1)
		h.Stack.Remove(old)
	}
	h.anchored = append(h.anchored, anchoredLayer{
		id: id, comp: layer, spec: spec, pol: policyOr(pol),
	})
	h.ctx.RequestLayout()
	return nil
}

// mountLayer adds a layer to the stack, converting a failed mount into an error
// rather than letting it escape as a panic from inside the runtime.
func (h *OverlayHost) mountLayer(layer tui.Component) (err error) {
	defer func() {
		if r := recover(); r != nil {
			h.Stack.Remove(layer) // undo a partial mount, as openModal does
			err = fmt.Errorf("%w: %v", ErrModalNotMountable, r)
		}
	}()
	h.Stack.Add(layer)
	return nil
}

// policyOr substitutes the default for a nil or typed-nil policy.
func policyOr(p AnchorPolicy) AnchorPolicy {
	if p == nil {
		return FlipClipPolicy{}
	}
	return p
}

// CloseAnchored unmounts the layer registered under id and publishes its
// dismissal. Unknown ids are a no-op, mirroring the overlay protocol's
// idempotence rule: a caller closing something already closed has got what it
// asked for.
func (h *OverlayHost) CloseAnchored(id LayerID, reason DismissReason) {
	i := h.indexAnchored(id)
	if i < 0 {
		return
	}
	al := h.anchored[i]
	owner := tui.NodeID(0)
	var bus *tui.Bus
	if h.ctx != nil {
		bus = h.ctx.Bus()
	}
	if ider, ok := al.comp.(interface{ NodeID() tui.NodeID }); ok {
		owner = ider.NodeID()
	}
	h.anchored = slices.Delete(h.anchored, i, i+1)
	h.Stack.Remove(al.comp)
	if bus != nil {
		bus.Publish(OverlayDismissedEvent{Owner: owner, Layer: id, Reason: reason})
	}
}

// AnchoredLayers enumerates the open anchored ids in registration order.
func (h *OverlayHost) AnchoredLayers() iter.Seq[LayerID] {
	return func(yield func(LayerID) bool) {
		for _, al := range h.anchored {
			if !yield(al.id) {
				return
			}
		}
	}
}

// indexAnchored reports where id is registered, or -1.
func (h *OverlayHost) indexAnchored(id LayerID) int {
	return slices.IndexFunc(h.anchored, func(al anchoredLayer) bool { return al.id == id })
}

// placeAnchored resolves every anchored layer during the host's own Layout:
// measure the layer, resolve its anchor, ask the policy, place the result.
//
// A layer whose anchor no longer resolves is DISMISSED rather than placed
// somewhere arbitrary. That is the whole point of anchor loss: the thing the
// popup was attached to is gone, so the popup has no meaning, and leaving it
// floating where the anchor used to be is worse than closing it.
//
// Dismissals are collected and performed after the walk, because closing
// unmounts, and unmounting a child in the middle of laying the children out is
// the mutation the layout phase forbids.
func (h *OverlayHost) placeAnchored(ctx *tui.Context, viewport tui.Rect) (lost []LayerID) {
	for _, al := range h.anchored {
		anchor, ok := ctx.ResolveAnchor(al.spec.Ref)
		if !ok {
			lost = append(lost, al.id)
			continue
		}
		want := ctx.LayoutChild(al.comp, tui.Loose(tui.Size{W: viewport.W, H: viewport.H}))
		r := al.pol.Place(anchor, viewport, want, al.spec.Pref)
		// Bounded by the host whatever the policy returned, so a consumer
		// policy cannot place a layer off-screen.
		r = clampInto(r, viewport)
		ctx.PlaceChild(al.comp, r)
	}
	return lost
}
