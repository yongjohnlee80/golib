package widget

import (
	"errors"
	"fmt"
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
//     requires zero manual wiring, and — unlike the bus broadcast this replaced — two
//     hosts in one application each serve only the widgets inside them.
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
	scrim *scrimLayer
	// anchored holds the layers positioned against an anchor rather than by
	// the stack's own alignment, in registration order.
	anchored []anchoredLayer
	// ctx is kept because the embedded Stack's own Context is private to the
	// tui package; the host needs one to resolve anchors and to schedule the
	// commit that closes the layers whose anchors have gone.
	ctx *tui.Context
	// lostAnchors are the ids noticed during layout and closed just after it.
	lostAnchors []LayerID
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
}

// Detach removes a previously attached Float.
//
// Attach used to be permanent, which made a Float usable only by an owner that
// lived as long as the application. Detaching runs the ordinary unmount cascade,
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
// It no longer subscribes to anything. The overlay protocol used to be a pair
// of Bus events every host in the application received; layers are now added
// and removed by the widget that owns them, through the host it resolved from
// the tree.
func (h *OverlayHost) Init(ctx *tui.Context) {
	h.ctx = ctx
	h.Stack.Init(ctx)
}

// addLayer mounts an unkeyed popup layer on top of the stack.
//
// ADDRESSED, not broadcast. This used to be a Bus subscription, so a request
// published by one widget reached EVERY mounted OverlayHost: with two hosts in
// one application, both tried to mount the same component and the runtime
// panicked — a component value mounts at most once — taking the application
// down for opening a dropdown. A widget now resolves the host that contains it
// and calls this.
//
// Idempotent. A duplicate open is an ordinary consequence of double activation,
// and the runtime refuses to mount one component twice.
func (h *OverlayHost) addLayer(layer tui.Component) {
	if layer == nil || h.hasLayer(layer) {
		return
	}
	h.Stack.Add(layer)
}

// removeLayer unmounts a popup layer, restoring focus through the runtime's
// scope stack. Idempotent, mirroring addLayer.
//
// The mounted check is the APP TEARDOWN case, and it is not hypothetical: the
// unmount cascade takes this host's children — the layers among them — before it
// reaches the widget whose own unmount hook closes its layer. By then the layer
// is still in the stack's list, because a cascade does not edit container
// bookkeeping, but its node is gone; unmounting it again panics. Asking whether
// it is still mounted is the difference between a clean shutdown and a crash on
// the way out.
func (h *OverlayHost) removeLayer(layer tui.Component) {
	if layer == nil || !h.hasLayer(layer) {
		return
	}
	if h.ctx == nil || !h.ctx.MountedComponent(layer) {
		return
	}
	h.Stack.Remove(layer)
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

// openModal mounts a dialog as the topmost layer, with its scrim beneath it if
// it asked for one.
//
// It reports an error rather than panicking on a failed mount, so Open can roll
// back and leave no trace. A descendant that is already mounted elsewhere is the
// reachable case: the runtime refuses it, and without the rollback the host was
// left holding a dialog it had half-added.
func (h *OverlayHost) openModal(m *Modal) (err error) {
	// Exactly one scrim exists at a time, and it belongs to whichever dialog is
	// on top. The one below is now covered, so its backdrop is removed before
	// the new pair goes on.
	hadScrim := h.scrim
	h.dropScrim()

	// Same preflight as the anchored path: a Modal already mounted elsewhere
	// must be refused before the add, because the unwind below removes the
	// layer, and removing an already-mounted component unmounts it from where
	// it legitimately lives.
	if h.ctx != nil && h.ctx.MountedComponent(m) {
		return fmt.Errorf("%w: the dialog is already mounted elsewhere", ErrModalNotMountable)
	}

	var scrim *scrimLayer
	if m.wantScrim {
		scrim = &scrimLayer{st: m.card.st}
		h.Stack.Add(scrim)
		h.scrim = scrim
	}

	// The mount is the step that can fail, and it fails by PANICKING from deep
	// inside the runtime — a descendant already mounted elsewhere is refused
	// there, not here, because only the runtime knows what is mounted. Recovering
	// is not a way of ignoring that: the failure is converted and returned. It is
	// the only way to get back the chance to undo what did happen.
	//
	// A failed mount is PARTIAL, which is what makes the unwind necessary rather
	// than cosmetic. The Modal node and its card are created before the offending
	// descendant is reached, so simply dropping the stack entry would leave a
	// mounted card whose body never mounted, and the next layout pass would panic
	// measuring a child that is not there. Removing the layer runs the ordinary
	// unmount cascade over whatever did mount, which is the rollback.
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		h.Stack.Remove(m) // unmounts the partial subtree, if any
		if scrim != nil {
			h.Stack.Remove(scrim)
		}
		h.scrim = nil
		if hadScrim != nil {
			// Whoever was topmost before keeps their backdrop: a refused open
			// must leave the stack exactly as it found it.
			h.restoreScrimForTop()
		}
		err = fmt.Errorf("%w: %v", ErrModalNotMountable, r)
	}()
	h.Stack.Add(m)
	h.modals = append(h.modals, m)
	return nil
}

// dismissModal performs one dismissal as a stack transition, unwinding anything
// above the target first.
//
// LIFO IS THE WHOLE POINT. Dialogs are a stack, and closing one in the middle of
// it while leaving the ones above open produces a state the model does not have:
// a dialog on top of nothing, still trapping focus, whose own dismissal will
// later try to unwind a modal that is already gone. So a non-top dismissal
// closes every dialog above the target first, topmost first, and each of those
// is a real dismissal with its own event — a listener counting closures counts
// all of them, not just the one that was asked for.
//
// The ones above close as DismissReplaced: they were not what the user acted on,
// and reporting the caller's reason for them would attribute an accept or a
// cancel to a dialog nobody answered.
func (h *OverlayHost) dismissModal(m *Modal, reason DismissReason) {
	// Topmost first, so each close sees a stack that is valid beneath it.
	//
	// The target's position is RE-DERIVED every iteration rather than captured
	// once, because each close runs a callback and a callback may dismiss another
	// dialog on this same stack. Indexing by a position taken before that ran
	// walked off the end of a slice the callback had already shortened.
	//
	// A target the host does not hold yields -1 and unwinds nothing, which is the
	// right answer rather than a special case: there is no transition to make on
	// a stack this dialog is not part of, and its own close below still settles
	// its flags. The counter bounds the loop so that no bookkeeping slip can turn
	// a re-entrant dismissal into a hang.
	for n := len(h.modals); n > 0; n-- {
		i := slices.Index(h.modals, m)
		if i < 0 || i == len(h.modals)-1 {
			break
		}
		above := h.modals[len(h.modals)-1]
		above.finishDismiss(DismissReplaced, func() { h.removeModal(above) })
	}
	m.finishDismiss(reason, func() { h.removeModal(m) })
}

// removeModal takes one dialog off the stack and hands the backdrop back to
// whoever is left on top.
func (h *OverlayHost) removeModal(m *Modal) {
	h.dropScrim()
	h.Stack.Remove(m)
	if i := slices.Index(h.modals, m); i >= 0 {
		h.modals = slices.Delete(h.modals, i, i+1)
	}
	h.restoreScrimForTop()
}

// restoreScrimForTop gives the topmost remaining dialog its backdrop, placed
// immediately beneath it.
//
// THE SURVIVING DIALOG IS MOVED, NEVER REMOUNTED. Inserting a layer below an
// existing one by removing and re-adding that one is not a reorder — it is a
// full unmount and remount, which gives the dialog a new NodeID, cancels its
// lifetime context, runs its unmount hooks, drops its focus and re-runs Init on
// everything inside it. All of that for a change the user cannot see, and none
// of it visible in a rendered-output test, because the screen looks identical
// either way. Stack.Move reorders and keeps the node.
func (h *OverlayHost) restoreScrimForTop() {
	n := len(h.modals)
	if n == 0 {
		return
	}
	top := h.modals[n-1]
	if !top.wantScrim {
		return
	}
	scrim := &scrimLayer{st: top.card.st}
	h.Stack.Add(scrim)
	h.scrim = scrim
	// Added on top, then moved to the slot directly below the surviving dialog.
	// Len()-2 is that slot: the scrim and the dialog are the last two layers.
	if l := h.Stack.Len(); l >= 2 {
		h.Stack.Move(scrim, l-2)
	}
}

// restyleScrim repaints the backdrop when its owning dialog is restyled. Only
// the topmost dialog owns one, so a restyle of any other is a no-op here.
func (h *OverlayHost) restyleScrim(owner *Modal, st *ModalStyle) {
	if h.scrim == nil || len(h.modals) == 0 || h.modals[len(h.modals)-1] != owner {
		return
	}
	h.scrim.st = st
	if ctx := h.scrim.Context(); ctx != nil {
		ctx.MarkDirty()
	}
}

// dropScrim removes the current backdrop, if any.
func (h *OverlayHost) dropScrim() {
	if h.scrim == nil {
		return
	}
	h.Stack.Remove(h.scrim)
	h.scrim = nil
}

// TopModal reports the dialog currently on top, or nil when none is open.
func (h *OverlayHost) TopModal() *Modal {
	if n := len(h.modals); n > 0 {
		return h.modals[n-1]
	}
	return nil
}

// focusInitial moves focus to the dialog's preferred starting control.
//
// The DEFAULT-role button is preferred unconditionally, not merely as a
// tie-break: a dialog's affirmative action is where a user expects to land, and
// choosing it only when nothing else qualified would make the preference depend
// on button order. Failing that, the first enabled button; failing that, the
// Modal itself, which is the target of last resort that keeps Escape reachable.
func (m *Modal) focusInitial() {
	// InitialFocus always nominates — a button, or failing that the Modal node
	// itself — so there is no "no nomination" case to handle here. Guarding for
	// one would be a branch nothing can reach.
	target, _ := m.InitialFocus()
	if ctx := m.Context(); ctx != nil {
		ctx.FocusComponent(target)
	}
	// Read back from where focus actually landed rather than assuming the
	// nomination was honoured: it can be refused, and a selection index derived
	// from the request instead of the result would then name the wrong button.
	m.refreshSelection()
}
