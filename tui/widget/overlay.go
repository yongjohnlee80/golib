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
	h.Stack.Init(ctx)
	tui.SubscribeScoped(ctx, func(ev overlayOpenEvent) {
		h.Stack.Add(ev.layer)
	})
	tui.SubscribeScoped(ctx, func(ev overlayCloseEvent) {
		h.Stack.Remove(ev.layer)
	})
}
