package widget

import (
	"errors"
	"slices"

	"github.com/yongjohnlee80/golib/tui"
)

// ErrModalNotMountable is returned by Open when the dialog's own tree cannot be
// mounted — most often a descendant that is already mounted somewhere else. The
// host is left exactly as it was.
var ErrModalNotMountable = errors.New("widget: modal cannot be mounted")

// OverlayHost is the root-level container that manages full-screen overlays,
// floating modal dialogs, and ephemeral popups above the primary application UI.
//
// # The Overlay Layer Hierarchy
//
// OverlayHost embeds [tui.Stack], laying out layers such that subsequent layers
// paint on top of earlier ones, while mouse hit-testing evaluates top-to-bottom:
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ [OverlayHost] Layer Stack                                        │
//	│                                                                  │
//	│  Top Layer:    Ephemeral Popups ([Select] dropdown list)         │
//	│                - Added by the widget, through the host it found  │
//	│                - Focus-trapped; clicks outside close             │
//	│                ▲                                                 │
//	│  Mid Layer:    Floating Modals ([Float] dialogs)                 │
//	│                - Attached via [OverlayHost.Attach]               │
//	│                - Hidden until [Float.Show]; Esc dismisses        │
//	│                ▲                                                 │
//	│  Scrim Layer:  Dimmer Backdrop ("░" shaded cells)                │
//	│                - Visually dims base content during modals        │
//	│                ▲                                                 │
//	│  Base Layer:   Application UI Root (Panels, Splits, Flex)        │
//	│                - The primary interactive workspace               │
//	└──────────────────────────────────────────────────────────────────┘
//
// # How Widgets Discover the OverlayHost
//
// Applications wrap the root layout once during initialization:
//
//	root := widget.NewOverlayHost(body)
//
// Once mounted, floating components interact with OverlayHost through two distinct mechanisms:
//  1. Automatic Popups: [Select] and [Menu] project their layers onto the NEAREST
//     ENCLOSING host, which each resolves from the component tree and then calls
//     directly. A widget nested arbitrarily deep inside split panes or flex containers
//     requires zero manual wiring, and two hosts in one application each serve only the
//     widgets inside them, which a bus broadcast to every host could not.
//  2. Explicit Modals: [Float] instances are registered via [OverlayHost.Attach].
//     While hidden, a Float occupies zero cells, participates in no hit-testing, and
//     is omitted from tab navigation until activated by [Float.Show].
//
// # Architectural Invariants
//
//  1. Base Layer Anchoring: Layer index 0 in the underlying [tui.Stack] is permanently
//     occupied by the base application UI; overlays and popups strictly stack above index 0.
//  2. Automatic Popup Lifecycle: a widget that opened a layer closes it — on its own
//     transition, and on its own unmount — immediately restoring previous focus without
//     manual cleanup.
//  3. Reverse Hit-Testing Priority: Mouse hit-tests and pointer interactions are evaluated from
//     the topmost layer down, preventing clicks from penetrating through active modal dialogs.
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. Layer addition, removal, and modal triggers must run
//     on the application event loop goroutine.
//
// # Usage Examples
//
// 1. Setting up the root overlay host in an application:
//
//	rootLayout := widget.NewSplit(widget.Horizontal, sidebar, mainView, widget.WithRatio(0.25))
//	overlayHost := widget.NewOverlayHost(rootLayout)
//
// 2. Attaching a modal dialog to the overlay host:
//
//	modalDialog := widget.NewFloat(confirmBox,
//		widget.WithModal(true),
//		widget.WithDimBackground(true),
//		widget.WithAnchor(widget.Center),
//	)
//	overlayHost.Attach(modalDialog)
//
//	// When mounted on the event loop, trigger the modal:
//	// modalDialog.Show()
type OverlayHost struct {
	*tui.Stack

	// modals is the open dialog stack, bottom to top. The host tracks it
	// because "which dialog is topmost" is a question about global order that
	// no individual dialog can answer about itself.
	modals []*Modal
	// scrim is the single backdrop layer, owned by whichever dialog is on top.
	scrim *Scrim
	// anchored holds the layers positioned against an anchor rather than by
	// the stack's own alignment, in registration order.
	anchored []anchoredLayer
	// ctx is kept because the embedded Stack's own Context is private to the
	// tui package; the host needs one to resolve anchors and to schedule the
	// commit that closes the layers whose anchors have gone.
	ctx *tui.Context
	// lostAnchors are the ids noticed during layout and closed just after it.
	lostAnchors []LayerID
	// topmost are the floats kept above every other layer (AttachTopmost), in the order attached:
	// each layer added after them is moved beneath them.
	topmost []*Float
}

var _ tui.Container = (*OverlayHost)(nil)

// NewOverlayHost wraps the application UI as the bottom overlay layer.
func NewOverlayHost(base tui.Component) *OverlayHost {
	if base == nil {
		panic("widget: NewOverlayHost: nil base component")
	}
	h := &OverlayHost{Stack: tui.NewStack()}
	h.Stack.Add(base)
	return h
}

// Attach adds a Float as an overlay layer, hidden until Show. Pair it with
// Detach when the Float's owner goes away; a Float left attached outlives it.
func (h *OverlayHost) Attach(f *Float) {
	if f == nil {
		panic("widget: OverlayHost.Attach: nil Float")
	}
	if h.hasLayer(f) {
		return // already attached: attaching twice is one layer, not two
	}
	h.Stack.Add(f)
	h.raiseTopmost()
}

// AttachTopmost attaches a Float that stays above every other layer, the dialogs opened after it
// included: a notification over whatever is open (Qt's WindowStaysOnTopHint). It never takes the
// keyboard from a dialog under it, since only a modal Float traps focus.
func (h *OverlayHost) AttachTopmost(f *Float) {
	h.Attach(f)
	if !slices.Contains(h.topmost, f) {
		h.topmost = append(h.topmost, f)
	}
	h.raiseTopmost()
}

// raiseTopmost moves the topmost floats back above the layers added since, keeping their order.
func (h *OverlayHost) raiseTopmost() {
	for _, f := range h.topmost {
		if l := h.Stack.Len(); h.hasLayer(f) && l > 0 {
			h.Stack.Move(f, l-1)
		}
	}
}

// Raise moves an attached Float to the top of the ordinary layers: above every other panel and
// popup, and still beneath the open dialogs (a Modal with its scrim, a shown modal Float) and the
// topmost floats. A non-modal Float raises itself when the keyboard enters it, so the panel being
// typed into is the one drawn on top (Qt's QWidget::raise on activation). A shown modal Float is
// an open dialog: raising one moves it above the other dialogs, as showing it did. A topmost Float
// is above everything already, and a Float not attached here is left alone.
//
// It reorders the one layer and moves nothing else: focus stays where it is, the dialogs keep
// their order among themselves, and each keeps its scrim directly beneath it. The node is moved,
// never remounted.
func (h *OverlayHost) Raise(f *Float) {
	if f == nil || !h.hasLayer(f) || slices.Contains(h.topmost, f) {
		return
	}
	if h.tierOf(f) == tierDialog {
		h.moveBelow(f, h.isTopmost)
		return
	}
	h.moveBelow(f, h.pinned)
}

// moveBelow moves f directly beneath the lowest layer other than f that ceiling holds, or to the
// top when there is none. f is one of the host's layers (Raise checks it); Stack.Move ignores one
// that is not.
func (h *OverlayHost) moveBelow(f *Float, ceiling func(tui.Component) bool) {
	from, to := -1, -1
	for i, layer := range h.Stack.All() {
		switch {
		case layer == tui.Component(f):
			from = i
		case to < 0 && ceiling(layer):
			to = i
		}
	}
	switch {
	case to < 0:
		to = h.Stack.Len() - 1
	case from < to:
		to-- // Move's index is after the removal: f leaving shifts the ceiling down one
	}
	h.Stack.Move(f, to)
}

// layerTier is where an overlay layer stacks: above the ordinary layers sit the open dialogs with
// their scrim, and above those the topmost layers.
type layerTier int

const (
	tierOrdinary layerTier = iota // a panel, a popup, a hidden or non-modal Float
	tierDialog                    // an open dialog, its scrim, a shown modal Float
	tierTopmost                   // a topmost Float (AttachTopmost)
)

// tieredLayer is a layer that states its own tier; one that does not is ordinary, unless the host
// put it in a tier itself (a topmost Float, an open Modal, the scrim).
type tieredLayer interface {
	overlayTier() layerTier
}

// tierOf is layer's tier: the host's own placements first (AttachTopmost, openModal, the scrim),
// then what the layer states.
func (h *OverlayHost) tierOf(layer tui.Component) layerTier {
	switch {
	case slices.ContainsFunc(h.topmost, func(f *Float) bool { return tui.Component(f) == layer }):
		return tierTopmost
	case h.scrim != nil && layer == tui.Component(h.scrim),
		slices.ContainsFunc(h.modals, func(m *Modal) bool { return tui.Component(m) == layer }):
		return tierDialog
	}
	if t, ok := layer.(tieredLayer); ok {
		return t.overlayTier()
	}
	return tierOrdinary
}

// pinned reports whether layer stays above the ordinary layers.
func (h *OverlayHost) pinned(layer tui.Component) bool { return h.tierOf(layer) > tierOrdinary }

// isTopmost reports whether layer is one of the topmost layers.
func (h *OverlayHost) isTopmost(layer tui.Component) bool { return h.tierOf(layer) == tierTopmost }

// Detach removes a previously attached Float.
//
// Without it Attach would be permanent, and a Float usable only by an owner that
// lives as long as the application. Detaching runs the ordinary unmount cascade,
// so a Float that holds the pointer releases it and one holding focus has focus
// returned through the runtime's scope stack — the same cleanup any other
// unmount performs, which is exactly why it must go through the tree rather
// than be dropped from a list.
//
// Detaching something that is not attached is a no-op, so a caller unwinding
// after a partial setup does not have to track what succeeded.
func (h *OverlayHost) Detach(f *Float) {
	if f == nil || !h.hasLayer(f) {
		return
	}
	// Hidden FIRST, so the Float unwinds its own layer, focus restore and
	// dismissal exactly as an ordinary close would. Removing it while it still
	// believed itself shown left shown=true with nothing mounted, and a later
	// Show then took the already-shown early return and mounted nothing at all —
	// a Float that could be detached once and never used again.
	f.Hide()
	h.Stack.Remove(f)
	h.topmost = slices.DeleteFunc(h.topmost, func(t *Float) bool { return t == f })
}

// hasLayer reports whether c is currently one of the host's layers. It reads the
// stack rather than a private list, so it cannot come to disagree with what is
// actually mounted.
func (h *OverlayHost) hasLayer(c tui.Component) bool {
	for _, layer := range h.Stack.All() {
		if layer == c {
			return true
		}
	}
	return false
}

// Init chains the Stack's child mounting.
//
// It subscribes to nothing: layers are added and removed by the widget that owns
// them, through the host it resolved from the tree, so no host receives another's
// requests.
func (h *OverlayHost) Init(ctx *tui.Context) {
	h.ctx = ctx
	h.Stack.Init(ctx)
}

// Layout lays the stack out, then re-places the anchored layers against their
// anchors and dismisses any whose anchor has gone.
//
// The stack pass runs first and unchanged, so an anchored layer is measured and
// placed like any other child; this pass then overrides the position of the ones
// that have an anchor. Measuring them a second time is the price of composing
// with Stack rather than reimplementing it, and Layout is a pure function of its
// constraints, so the second answer is the first answer.
//
// Anchor loss is committed AFTER the walk. Closing unmounts, and unmounting a
// child while laying the children out is the mutation the layout phase forbids.
func (h *OverlayHost) Layout(c tui.Constraints) tui.Size {
	size := h.Stack.Layout(c)
	if len(h.anchored) == 0 || h.ctx == nil {
		return size
	}
	viewport := tui.Rect{X: 0, Y: 0, W: size.W, H: size.H}
	// REPLACED, never accumulated. A host can be measured more than once in a
	// single pass — a parent trying two constraints, a commit dirtying layout —
	// and only the LAST measurement describes the geometry that will be
	// painted. Appending made an anchor lost at one trial size stay lost even
	// after a later measurement found it perfectly valid, so the commit closed
	// a popup that was on screen and correctly placed.
	h.lostAnchors = h.placeAnchored(h.ctx, viewport)
	if len(h.lostAnchors) > 0 {
		// The COMMIT phase, not a scheduled update: closing an anchor-lost
		// overlay is a geometry-derived side effect, which is exactly what
		// commit exists for. Deferring it to a later turn instead would paint
		// one frame containing a popup the host has already decided to close.
		h.ctx.AfterLayout("anchor-loss", h.commitAnchorLoss)
	}
	return size
}

// commitAnchorLoss closes the layers whose anchors vanished during the last
// layout. Separate from the pass that noticed, because closing is a tree
// mutation and the pass that noticed was a layout.
func (h *OverlayHost) commitAnchorLoss() {
	lost := h.lostAnchors
	h.lostAnchors = nil
	for _, id := range lost {
		h.CloseAnchored(id, DismissAnchorLost)
	}
}

// --- Modal stacking ---
//
// The host owns stack ORDER, so it owns the two things that depend on it:
// which dialog is topmost, and where the scrim goes. A Modal states whether it
// wants a backdrop; it never asks whether it is the top one, because answering
// that would mean reaching outside itself into global state.
