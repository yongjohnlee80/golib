package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// OverlayHost is the package's concrete realization of "the Stack overlay
// layer": a root-level tui.Stack
// whose bottom layer is the application UI and whose upper layers are
// overlays. Wrap the app root once —
//
//	root := widget.NewOverlayHost(body)
//
// — and the widgets that float will find it:
//
//   - Select mounts its open option list here (discovered via an internal
//     Bus handshake, so a Select buried anywhere in the tree needs no
//     wiring). Without a mounted OverlayHost, opening a Select is a no-op.
//   - Float attaches here explicitly (host.Attach), then Show/Hide toggles
//     it. A Float added to any other full-area Stack layer works the same.
//
// OverlayHost embeds tui.Stack: it is a Container and lays out layers per
// the Stack contract (later layers paint on top; hit-testing visits them in
// reverse).
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
