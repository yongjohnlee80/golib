package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

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
//	│                - Dynamically added via internal bus handshake    │
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
//  1. Automatic Popups: [Select] projects its open option list onto the overlay host
//     via an internal unexported bus handshake ([overlayOpenEvent] / [overlayCloseEvent]).
//     A [Select] nested arbitrarily deep inside split panes or flex containers requires
//     zero manual wiring to project its dropdown above the UI.
//  2. Explicit Modals: [Float] instances are registered via [OverlayHost.Attach].
//     While hidden, a Float occupies zero cells, participates in no hit-testing, and
//     is omitted from tab navigation until activated by [Float.Show].
//
// # Architectural Invariants
//
//  1. Base Layer Anchoring: Layer index 0 in the underlying [tui.Stack] is permanently
//     occupied by the base application UI; overlays and popups strictly stack above index 0.
//  2. Automatic Popup Lifecycle: Dynamic popup layers pushed via [overlayOpenEvent] are
//     removed upon [overlayCloseEvent], immediately restoring previous focus without manual cleanup.
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
	// ctx is kept because the embedded Stack's own Context is private to the
	// tui package; the host needs one to schedule the post-mount focus step.
	ctx *tui.Context
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

// Attach adds a Float as a permanent (hidden-until-Show) overlay layer.
func (h *OverlayHost) Attach(f *Float) {
	if f == nil {
		panic("widget: OverlayHost.Attach: nil Float")
	}
	h.Stack.Add(f)
}

// Init chains the Stack's child mounting and subscribes to the package's
// overlay protocol: open requests mount a popup layer on top; close
// requests unmount it (which restores focus through the runtime's scope
// stack —).
func (h *OverlayHost) Init(ctx *tui.Context) {
	h.ctx = ctx
	h.Stack.Init(ctx)
	tui.SubscribeScoped(ctx, func(ev overlayOpenEvent) {
		h.Stack.Add(ev.layer)
	})
	tui.SubscribeScoped(ctx, func(ev overlayCloseEvent) {
		h.Stack.Remove(ev.layer)
	})
}

// --- Modal stacking ---
//
// The host owns stack ORDER, so it owns the two things that depend on it:
// which dialog is topmost, and where the scrim goes. A Modal states whether it
// wants a backdrop; it never asks whether it is the top one, because answering
// that would mean reaching outside itself into global state.

// openModal mounts a dialog as the topmost layer, with its scrim beneath it if
// it asked for one, and moves focus into it.
func (h *OverlayHost) openModal(m *Modal) {
	// Exactly one scrim exists at a time, and it belongs to whichever dialog is
	// on top. The one below is now covered, so its backdrop is removed before
	// the new pair goes on.
	h.dropScrim()

	if m.wantScrim {
		h.scrim = &scrimLayer{st: m.card.st}
		h.Stack.Add(h.scrim)
	}
	h.modals = append(h.modals, m)
	h.Stack.Add(m)

	// Focus moves in after mounting, so the ring already contains the dialog's
	// buttons when the target is chosen.
	// Focus is moved in a scheduled step rather than inline: the dialog and its
	// buttons have only just been added, so their nodes are not laid out yet
	// and the focus ring would not contain them.
	if h.ctx != nil {
		h.ctx.App().Update(func() { m.focusInitial() })
	}
}

// closeModal unmounts a dialog and restores the backdrop to whichever dialog is
// left on top.
func (h *OverlayHost) closeModal(m *Modal) {
	h.dropScrim()
	h.Stack.Remove(m)
	for i, om := range h.modals {
		if om == m {
			h.modals = append(h.modals[:i], h.modals[i+1:]...)
			break
		}
	}
	// A dialog underneath becomes topmost, and gets its backdrop back. Without
	// this, closing a stacked dialog would leave the one beneath it undimmed.
	if n := len(h.modals); n > 0 {
		top := h.modals[n-1]
		if top.wantScrim {
			h.scrim = &scrimLayer{st: top.card.st}
			// Re-added BELOW the surviving dialog: remove and re-add it so the
			// scrim lands underneath rather than on top of the dialog it dims.
			h.Stack.Remove(top)
			h.Stack.Add(h.scrim)
			h.Stack.Add(top)
		}
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
	for _, b := range m.card.buttons {
		if b != nil && b.Enabled() && b.Role() == ButtonRoleDefault {
			if ctx := b.Context(); ctx != nil {
				ctx.RequestFocus()
				m.refreshSelection()
				return
			}
		}
	}
	for _, b := range m.card.buttons {
		if b != nil && b.Enabled() {
			if ctx := b.Context(); ctx != nil {
				ctx.RequestFocus()
				m.refreshSelection()
				return
			}
		}
	}
	if ctx := m.Context(); ctx != nil {
		ctx.RequestFocus()
	}
	m.selected = -1
}
