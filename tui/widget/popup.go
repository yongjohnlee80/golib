package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// A PANEL OPENED AT A POINT, DISMISSED LIGHTLY: a context menu's shape.
//
// The panel goes where the user pointed — a cell inside some widget — and goes
// away on Escape or on a press anywhere outside it. Two existing pieces each
// do half of that. An anchored layer (OpenAnchored) is placed against a
// widget, but at its natural size, so a press outside it lands on whatever is
// underneath and the layer never hears of it. A full-area layer (Select's
// popup) hears every press, but cannot tell where a widget is: it is that
// widget's sibling, not its ancestor, and an anchor resolves meaningfully only
// from an ancestor.
//
// popupLayer is both. It is opened as an anchored layer that fills the host,
// so every press reaches it, and the host — the anchor owner's ancestor —
// tells it where the anchor is before laying it out (anchorReceiver). It then
// places its panel at the anchored point itself.

// anchorReceiver is a layer that is told its anchor's rect, in the host's
// frame, before each layout. The layer fills the host, so the host's frame is
// the layer's own.
type anchorReceiver interface {
	receiveAnchor(anchor tui.Rect)
}

// popupHost is what a widget needs from the nearest OverlayHost to open a
// popup at a point.
type popupHost interface {
	openPopupAt(id LayerID, owner tui.Component, panel tui.Component, at tui.Point, onClosed func(DismissReason)) (*popupLayer, error)
	CloseAnchored(id LayerID, reason DismissReason)
}

// fillPolicy places a layer over the whole host, whatever its anchor.
type fillPolicy struct{}

func (fillPolicy) Place(_, viewport tui.Rect, _ tui.Size, _ Placement) tui.Rect { return viewport }

// openPopupAt opens panel at point at, in owner's local frame, as layer id.
//
// onClosed runs once when the layer leaves the tree, however it left: a row
// chosen, Escape, a press outside, the owner unmounted (anchor lost), or a
// replacement under the same id.
func (h *OverlayHost) openPopupAt(id LayerID, owner tui.Component, panel tui.Component, at tui.Point, onClosed func(DismissReason)) (*popupLayer, error) {
	ider, ok := owner.(interface{ Context() *tui.Context })
	if !ok || ider.Context() == nil {
		return nil, ErrAnchorUnusable
	}
	p := &popupLayer{host: h, id: id, panel: panel, at: at, onClosed: onClosed,
		reason: DismissProgrammatic}
	spec := AnchorSpec{Ref: ider.Context().NodeAnchor(), Pref: Placement{Side: PlacementBelow, Align: PlacementAlignStart}}
	if err := h.OpenAnchored(id, p, spec, fillPolicy{}); err != nil {
		return nil, err
	}
	// After mounting, as Float.Show does: the panel's first focusable control
	// takes focus, and the trap returns it to the owner when the layer goes.
	if !focusFirst(p.Context(), panel) {
		p.Context().RequestFocus()
	}
	return p, nil
}

// popupLayer fills its host: the backdrop is transparent, and the panel sits
// at the anchored point, flipped or clamped to stay on screen.
type popupLayer struct {
	Base
	host     *OverlayHost
	id       LayerID
	panel    tui.Component
	at       tui.Point // inside the anchor, where the panel opens
	anchor   tui.Rect  // the owner's rect, in this layer's frame; set by the host
	rect     tui.Rect  // where the panel was placed
	onClosed func(DismissReason)

	closing bool          // a close was asked from inside the panel's own dispatch
	reason  DismissReason // why the layer is leaving, for onClosed
}

var (
	_ tui.Focusable  = (*popupLayer)(nil)
	_ tui.FocusScope = (*popupLayer)(nil)
	_ anchorReceiver = (*popupLayer)(nil)
)

func (p *popupLayer) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	ctx.Mount(p.panel)
	ctx.OnUnmount(func() {
		if p.onClosed != nil {
			fn := p.onClosed
			p.onClosed = nil
			fn(p.reason)
		}
	})
}

// AcceptsFocus is the fallback for a panel with nothing focusable: the layer
// itself holds focus, so Escape still reaches it.
func (p *popupLayer) AcceptsFocus() bool { return true }

// TrapsFocus keeps Tab inside the panel while it is open.
func (p *popupLayer) TrapsFocus() bool { return true }

func (p *popupLayer) receiveAnchor(anchor tui.Rect) { p.anchor = anchor }

func (p *popupLayer) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	view := tui.Rect{X: 0, Y: 0, W: w, H: h}
	sz := p.ctx.LayoutChild(p.panel, tui.Loose(tui.Size{W: w, H: h}))
	point := tui.Rect{X: p.anchor.X + p.at.X, Y: p.anchor.Y + p.at.Y, W: 1, H: 1}
	// Below and to the right of the point, as a pointer menu opens; above it
	// when it would not fit below, and pulled back on screen at an edge.
	p.rect = FlipClipPolicy{}.Place(point, view, sz, Placement{Side: PlacementBelow, Align: PlacementAlignStart})
	p.ctx.PlaceChild(p.panel, p.rect)
	if p.closing {
		// A close asked from inside the panel's own dispatch (a row's action):
		// the layer leaves after this pass, never while that dispatch is still
		// running inside the panel.
		p.ctx.AfterLayout("popup.close", func() { p.host.CloseAnchored(p.id, p.reason) })
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

func (p *popupLayer) Render(tui.Surface) {} // transparent: only the panel paints

// requestClose closes the layer after the current dispatch, for a caller
// running inside the panel.
func (p *popupLayer) requestClose(reason DismissReason) {
	if p.closing {
		return
	}
	p.closing, p.reason = true, reason
	p.RequestLayout()
}

// close removes the layer now, for a caller that is the layer itself.
func (p *popupLayer) close(reason DismissReason) {
	p.reason = reason
	p.host.CloseAnchored(p.id, reason)
}

// HandleEvent is the light dismissal: Escape, or a press outside the panel.
// Everything the panel does not consume stops here, so nothing underneath
// reacts to the input that was meant for the menu.
func (p *popupLayer) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind != tui.KeyRelease && e.Code == tui.KeyEscape && e.Mods.Chord() == 0 {
			p.close(DismissEscape)
			return true
		}
	case tui.MouseEvent:
		if e.Kind == tui.MousePress && !p.rect.Contains(e.X, e.Y) {
			p.close(DismissCancel)
		}
		return true
	}
	return false
}
