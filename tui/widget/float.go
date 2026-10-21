package widget

import (
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Floating Window, Modal Dialog & Overlay Presentation Architecture
//
// Float provides an overlay-managed floating window that lives on a [tui.Stack] or
// [OverlayHost] layer. Designed for alerts, confirmation cards, form popups, and
// fullscreen-fractional tooling (such as file finders and log viewers), Float
// features customizable viewport anchoring, modal focus trapping, backdrop dimming,
// Esc dismissal, and automatic focus restoration.
//
// # Subsystem Role & Responsibilities
//
//  1. Layer-Bound Visibility: While hidden ([Float.Hide]), a Float occupies zero cells,
//     participates in no hit-testing, and presents no tab stops. When displayed ([Float.Show]),
//     it mounts an overlay layer that fills the available container bounds and arranges content.
//  2. Focus Trap ([tui.FocusScope]): When configured with [WithModal](true), the float acts
//     as a keyboard focus trap. Tab navigation cycles exclusively among focusable children
//     inside the dialog.
//  3. Focus Seeding & Scope Restoration: Upon Show(), focus is automatically seeded into
//     the first focusable descendant of the child tree. When dismissed via Esc or [Float.Hide],
//     the runtime unmounts the modal scope, automatically restoring focus to the component
//     that was active prior to opening.
//  4. Responsive Geometric Anchoring: Supports 9 cardinal alignment anchors ([Center],
//     [TopLeft], [BottomRight], etc.), explicit rectangle placement ([AtRect]), and
//     responsive screen-percentage constraints ([WithSizeFraction]).
//  5. Backdrop Scrim Dimming: When [WithDimBackground](true) is enabled, the surrounding
//     screen area beneath the modal card is shaded with a faint "░" stipple pattern,
//     visually receding background panels.
//
// # Concurrency & Goroutine Ownership
//
// All methods on Float ([Float.Show], [Float.Hide], [Float.Layout], [Float.Render])
// are strictly loop-goroutine-owned and must only be called on the main application
// loop (e.g. from event handlers, lifecycle hooks, or [tui.App.Update]).
//
// # Layout & Component Stack Hierarchy
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ [OverlayHost] / [tui.Stack] Full Window Area                     │
//	│                                                                  │
//	│  ┌────────────────────────────────────────────────────────────┐  │
//	│  │ floatLayer (FocusScope Trap + Scrim Canvas)                │  │
//	│  │ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░│  │
//	│  │ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░│  │
//	│  │ ░░░░░░░░░░┌───────────────────────────────┐░░░░░░░░░░░░░░░░│  │
//	│  │ ░░░░░░░░░░│ Child Component (e.g. [Box])  │░░░░░░░░░░░░░░░░│  │
//	│  │ ░░░░░░░░░░│ - Focused Input Field         │░░░░░░░░░░░░░░░░│  │
//	│  │ ░░░░░░░░░░│ - Action Buttons [OK] [Cancel]│░░░░░░░░░░░░░░░░│  │
//	│  │ ░░░░░░░░░░└───────────────────────────────┘░░░░░░░░░░░░░░░░│  │
//	│  │ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░│  │
//	│  └────────────────────────────────────────────────────────────┘  │
//	│                                                                  │
//	│  Base Application View (Dimmed beneath Scrim)                    │
//	└──────────────────────────────────────────────────────────────────┘
//
// # Architectural Invariants
//
//  1. Zero Footprint While Hidden:
//     Layout returns Size{0, 0} while hidden. It consumes zero cells, never hit-tests,
//     and will not receive dispatched events.
//  2. Fallback Tab Stop for Non-Focusable Modals:
//     If a modal float contains no focusable children (e.g. an informational alert text
//     box), the floatLayer itself acts as a fallback focus stop so that Escape keypresses
//     remain trapped and reliably dismiss the modal.
//  3. Notification Contract:
//     Every dismissal publishes an [OverlayDismissedEvent] stamped with the Float's
//     [tui.NodeID] and its reason: [DismissEscape] for the Escape key, [DismissProgrammatic]
//     for [Float.Hide]. The deprecated [DismissEvent] is published just before it.
//
// # Usage Examples
//
// 1. Creating a centered modal confirmation dialog:
//
//	form := widget.NewBox(
//		widget.NewText("Save changes before closing?"),
//		widget.WithTitle("Confirm"),
//	)
//	dialog := widget.NewFloat(
//		form,
//		widget.WithModal(true),
//		widget.WithDimBackground(true),
//		widget.WithAnchor(widget.Center),
//	)
//	host.Attach(dialog)
//
//	// Trigger display:
//	dialog.Show()
//
// 2. Responsive fractional search modal (90% width, 60% height):
//
//	searchView := widget.NewFloat(
//		finderBox,
//		widget.WithModal(true),
//		widget.WithSizeFraction(90, 60),
//		widget.WithAnchor(widget.Top),
//	)
type Float struct {
	Base
	child  tui.Component
	anchor Anchor
	modal  bool
	dim    bool
	wPct   int // 0 = natural width; else a percentage of the available area
	hPct   int

	shown   bool
	layer   *floatLayer
	content tui.Rect // where the layer last placed the child, in the float's cells

	scrim        style.Style
	windowTarget *floatWindowTarget
	area         tui.Size // the overlay area the float last laid out in
}

var _ tui.Component = (*Float)(nil)

// FloatOption customizes a Float under construction.
type FloatOption func(*Float)

// WithAnchor positions the content (default Center).
func WithAnchor(a Anchor) FloatOption { return func(f *Float) { f.anchor = a } }

// WithModal makes the float a focus trap with Esc-dismiss.
func WithModal(v bool) FloatOption { return func(f *Float) { f.modal = v } }

// WithSizeFraction sizes the float as a PERCENTAGE of the area it may
// use, per axis (1..100; 0 leaves that axis to the content's natural
// size). A working surface — a history browser, a log viewer, a picker
// over many rows — wants "most of the screen", and a fixed column count
// cannot express that: it overflows a narrow terminal and wastes a wide
// one, and it does not follow a resize.
//
//	widget.NewFloat(body, widget.WithModal(true),
//	    widget.WithSizeFraction(90, 90))   // 90% of the screen, both axes
//
// The fraction covers the float's whole box, border included, so two
// stacked floats at 90% and 80% read as a detail ON a list.
func WithSizeFraction(wPct, hPct int) FloatOption {
	return func(f *Float) { f.wPct, f.hPct = clampPct(wPct), clampPct(hPct) }
}

// overlayTier is a shown modal Float's tier: an open dialog, as a Modal is (tieredLayer).
func (f *Float) overlayTier() layerTier {
	if f.modal && f.shown {
		return tierDialog
	}
	return tierOrdinary
}

// WithDimBackground dims (scrims) the cells beneath while shown.
func WithDimBackground(v bool) FloatOption { return func(f *Float) { f.dim = v } }

// NewFloat builds a floating window around child — usually a Box, so title
// and border come along: NewFloat(NewBox(form, WithTitle("Confirm")),
// WithModal(true)).
func NewFloat(child tui.Component, opts ...FloatOption) *Float {
	if child == nil {
		panic("widget: NewFloat: nil child")
	}
	f := &Float{
		child:  child,
		anchor: Center,
		scrim:  style.New().Foreground(style.TokenTextMuted).Faint(true),
	}
	for _, o := range opts {
		if o != nil {
			o(f)
		}
	}
	return f
}

// Shown reports whether the float is currently visible.
func (f *Float) Shown() bool { return f.shown }

// Area is the overlay area the float was last laid out in, the whole of what it may cover; zero
// before its first layout. A placement kept as percentages (AtFraction, FractionOf) converts
// against it.
func (f *Float) Area() tui.Size { return f.area }

// ContentRect is where the float's content was last laid out, in the float's own cells (the
// float fills the overlay area, so they are the area's); zero while hidden. A native style draws a
// floating panel's shadow around it, as it does around a dialog's card (Modal.CardRect).
func (f *Float) ContentRect() tui.Rect {
	if !f.shown {
		return tui.Rect{}
	}
	return f.content
}

// Floating reports whether the float is placed free of the area's edges: centred, or at a
// rectangle (AtRect, AtFraction: a panel moved or resized off its edge). A float anchored at an
// edge or a corner is docked there, and a window maximised to its work area (WindowMod) is not
// floating either, reserved rows or not: it fills where a window may go.
func (f *Float) Floating() bool {
	if f.windowTarget != nil && f.windowTarget.Maximized() {
		return false
	}
	a := f.anchor
	if a.live != nil {
		return false
	}
	return a.atRect || a.atFraction || a.align == tui.AlignCenter
}

// raise moves the float to the top of its OverlayHost's ordinary layers (OverlayHost.Raise), or
// above the open dialogs when it is a shown modal one; a no-op outside an OverlayHost.
func (f *Float) raise() {
	if h, ok := hostFor[*OverlayHost](f.ctx); ok {
		h.Raise(f)
	}
}

// SetAnchor moves the float's content to another anchor: a drawer that opens from another edge.
func (f *Float) SetAnchor(a Anchor) {
	if f.windowTarget != nil {
		f.windowTarget.ownerPlacementChanged()
	}
	f.anchor = a
	f.RequestLayout()
}

// SetSizeFraction sizes the float anew, as WithSizeFraction does.
func (f *Float) SetSizeFraction(wPct, hPct int) {
	if f.windowTarget != nil {
		f.windowTarget.ownerPlacementChanged()
	}
	f.wPct, f.hPct = clampPct(wPct), clampPct(hPct)
	f.RequestLayout()
}

// Show mounts the float's layer onto its Stack position (loop goroutine).
// A modal Show seeds focus into the first focusable widget of the child
// subtree; hiding restores the previous focus via the runtime's scope
// stack.
func (f *Float) Show() {
	if f.ctx == nil {
		panic(errs.Fatal{Op: "widget", Rule: "Float.Show before mount — attach the Float to an OverlayHost (host.Attach) or a Stack layer first"})
	}
	if f.shown {
		return
	}
	f.layer = &floatLayer{owner: f}
	f.ctx.Mount(f.layer)
	f.shown = true
	f.RequestLayout()
	if f.modal {
		// An open dialog: above the panels and the dialogs open before it, as a Modal's Open
		// puts it, whenever this Float was attached. A panel raised since it was attached would
		// otherwise cover the dialog that holds the keyboard.
		f.raise()
		if !focusFirst(f.layer.Context(), f.child) {
			// No focusable content: the layer itself is the trap's tab stop (trapFallback), so
			// keys (Esc!) stay inside the modal.
			f.layer.focusSelf()
		}
	}
}

// Hide unmounts the layer (focus restores to the pre-Show node when the
// float was modal) and emits DismissEvent.
func (f *Float) Hide() { f.hide(DismissProgrammatic) }

// hide unmounts the layer and reports why: DismissEvent, then OverlayDismissedEvent with the
// reason, once each per hide. A Float already hidden publishes nothing.
func (f *Float) hide(reason DismissReason) {
	if !f.shown {
		return
	}
	layer := f.layer
	f.layer = nil
	f.shown = false
	f.ctx.Unmount(layer)
	f.RequestLayout()
	owner := f.NodeID()
	f.publish(DismissEvent{Owner: owner})
	f.publish(OverlayDismissedEvent{Owner: owner, Reason: reason})
}

// Init re-arms hidden state on (re)mount. Re-entrant.
func (f *Float) Init(ctx *tui.Context) {
	f.Base.Init(ctx)
	f.shown = false
	f.layer = nil
	// Unmounted — an App's teardown takes the whole tree — the layer went
	// with it. Forget it, so a Hide or Detach from an owner released later
	// is a no-op rather than an Unmount of something no longer mounted.
	ctx.OnUnmount(func() {
		f.shown = false
		f.layer = nil
	})
}

// Layout: zero cells while hidden; the full overlay area while shown (the
// layer positions the content inside it).
func (f *Float) Layout(c tui.Constraints) tui.Size {
	if !f.shown {
		return c.Constrain(tui.Size{W: 0, H: 0})
	}
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	area := tui.Size{W: w, H: h}
	f.ctx.AfterLayout("widget.float.area", func() { f.area = area }) // Area's: committed, so a measure moves nothing
	f.ctx.LayoutChild(f.layer, tui.Tight(area))
	f.ctx.PlaceChild(f.layer, tui.Rect{X: 0, Y: 0, W: w, H: h})
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints nothing; the layer and child do.
func (f *Float) Render(tui.Surface) {}

// ContainsPointer implements tui.PointerContainer: a Float is only the frame its layer fills, and
// contains no point of its own. The layer decides: a modal one contains every point, as the
// backdrop, and a non-modal one none, so the pointer reaches its content, or the application
// beneath wherever its content is not.
func (f *Float) ContainsPointer(x, y int) bool { return false }

// floatLayer is the full-area overlay layer: FocusScope trap (modal),
// backdrop scrim, Esc-dismiss, and anchor placement of the content.
type floatLayer struct {
	Base
	owner *Float
}

var (
	_ tui.Focusable     = (*floatLayer)(nil)
	_ tui.FocusDesigner = (*floatLayer)(nil)
	_ tui.FocusScope    = (*floatLayer)(nil)
)

// Init mounts the user child under the layer.
func (l *floatLayer) Init(ctx *tui.Context) {
	l.Base.Init(ctx)
	ctx.Mount(l.owner.child)
}

// ContainsPointer implements tui.PointerContainer. A modal layer is the backdrop and contains every
// point (HandleEvent catches what lands on it). A non-modal layer fills the area only to place its
// content, and contains none: the pointer reaches the content, which contains its own, or the
// application beneath. An always-shown non-modal float (the toasts) once took every click on the
// screen, the menu bar's among them.
func (l *floatLayer) ContainsPointer(x, y int) bool { return l.owner.modal }

// TrapsFocus implements tui.FocusScope: modal floats confine Tab.
func (l *floatLayer) TrapsFocus() bool { return l.owner.modal }

// AcceptsFocus implements tui.Focusable: the layer joins the ring only as
// the fallback stop of a modal whose content has no focusable now
// (trapFallback) — otherwise it would pollute the trap's Tab cycle.
func (l *floatLayer) AcceptsFocus() bool {
	return l.owner.modal && trapFallback(l.Context(), l.owner.child)
}

// FocusableByDesign implements tui.FocusDesigner. Only a MODAL float's layer
// was built to take focus — as that fallback stop; a non-modal one never
// does, so it is not focusable by design. modal is chosen at construction
// (WithModal); whether it is the stop now is AcceptsFocus's.
func (l *floatLayer) FocusableByDesign() bool { return l.owner.modal }

// Layout places the child per the owner's anchor (Float.place); a bound window places it in its
// work area (layoutManaged).
func (l *floatLayer) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	if l.owner.windowTarget != nil {
		return l.layoutManaged(c, w, h)
	}
	content := l.owner.place(tui.Rect{W: w, H: h}, l.layoutChild)
	l.ctx.PlaceChild(l.owner.child, content)
	l.ctx.AfterLayout("widget.float", func() { l.owner.content = content }) // ContentRect's: committed, so a measure moves nothing
	return c.Constrain(tui.Size{W: w, H: h})
}

// layoutChild lays the owner's child out in cc.
func (l *floatLayer) layoutChild(cc tui.Constraints) tui.Size {
	return l.ctx.LayoutChild(l.owner.child, cc)
}

// fixedRect is the rectangle f's anchor fixes in area, and whether it fixes one: a live rectangle
// first, then AtRect kept inside area, then AtFraction. Any other anchor aligns content of its own
// size (place).
func (f *Float) fixedRect(area tui.Rect) (tui.Rect, bool) {
	a := f.anchor
	if r, ok := a.resolve(area); ok {
		return r, true // a live rectangle, when there is one, wins over its fallback
	}
	switch {
	case a.atRect:
		return windowBoundsInArea(a.rect, area), true
	case a.atFraction:
		return fractionRect(a.frac, area), true
	}
	return tui.Rect{}, false
}

// place lays f's content out in area through layout and reports where it goes: the anchor's fixed
// rectangle, or else the content's own size, loose within area except an axis WithSizeFraction
// fixes to its share, aligned by the anchor.
func (f *Float) place(area tui.Rect, layout func(tui.Constraints) tui.Size) tui.Rect {
	if r, ok := f.fixedRect(area); ok {
		layout(tui.Tight(tui.Size{W: r.W, H: r.H}))
		return r
	}
	cc := tui.Loose(tui.Size{W: area.W, H: area.H})
	if f.wPct > 0 {
		cc.MinW = min(pctOf(area.W, f.wPct), area.W)
		cc.MaxW = cc.MinW
	}
	if f.hPct > 0 {
		cc.MinH = min(pctOf(area.H, f.hPct), area.H)
		cc.MaxH = cc.MinH
	}
	sz := layout(cc)
	x, y := alignOffset(f.anchor.align, tui.Size{W: area.W, H: area.H}, sz)
	return tui.Rect{X: area.X + x, Y: area.Y + y, W: sz.W, H: sz.H}
}

// alignOffset resolves a tui.Align inside avail for content of size sz.
func alignOffset(a tui.Align, avail, sz tui.Size) (x, y int) {
	x, y = a.Offset(avail, sz)
	return max(x, 0), max(y, 0)
}

// Render paints the scrim when dimming.
func (l *floatLayer) Render(s tui.Surface) {
	if l.owner.dim {
		paintScrim(s, l.Context(), "░", l.owner.scrim)
	}
}

// HandleEvent: modal layers consume Esc (dismiss) and all backdrop mouse
// events; non-modal layers consume nothing.
//
// A non-modal layer raises its float when the keyboard enters it: a FocusEvent
// bubbles up from the newly focused node, whether Tab, a click or a program
// moved it, and the layer is an ancestor of all its content. Observed, never
// consumed, as every container observes focus.
func (l *floatLayer) HandleEvent(ev tui.Event) bool {
	if !l.owner.modal {
		if fe, ok := ev.(tui.FocusEvent); ok && fe.Gained && !fe.Terminal {
			l.owner.raise()
		}
		return false
	}
	switch e := ev.(type) {
	case tui.KeyEvent:
		if _, ok := l.Context().KeyCommand(e, FloatClose); ok {
			l.owner.hide(DismissEscape)
			return true
		}
	case tui.MouseEvent:
		return true // the modal backdrop swallows the mouse
	}
	return false
}

// KeyScope is a Float's key-map scope.
func (l *floatLayer) KeyScope() keymap.Scope { return FloatScope }

// KeyBindings is a modal Float's Esc as m binds it; a modeless Float has none (tui.KeyBinder).
func (l *floatLayer) KeyBindings(m *keymap.Map) []keymap.Binding {
	if !l.owner.modal {
		return nil
	}
	return tui.BindingsOf(m, FloatClose)
}
