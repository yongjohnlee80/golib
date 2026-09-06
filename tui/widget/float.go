package widget

import (
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Anchor positions a Float's content within the overlay area.
type Anchor struct {
	align  tui.Align
	atRect bool
	rect   tui.Rect
}

// The alignment anchors. Center is the Float default.
var (
	Center      = Anchor{align: tui.AlignCenter}
	TopLeft     = Anchor{align: tui.AlignTopLeft}
	Top         = Anchor{align: tui.AlignTop}
	TopRight    = Anchor{align: tui.AlignTopRight}
	Left        = Anchor{align: tui.AlignLeft}
	Right       = Anchor{align: tui.AlignRight}
	BottomLeft  = Anchor{align: tui.AlignBottomLeft}
	Bottom      = Anchor{align: tui.AlignBottom}
	BottomRight = Anchor{align: tui.AlignBottomRight}
)

// AtRect anchors the float at an explicit overlay-relative rectangle.
func AtRect(r tui.Rect) Anchor { return Anchor{atRect: true, rect: r} }

// Float is a floating window living on an overlay Stack layer (ADR-0007
// §2.5; ADR-0004 §2.7.4). Attach it to an OverlayHost (host.Attach) or add
// it to any full-area tui.Stack layer, then toggle with Show/Hide. While
// hidden it occupies zero cells (invisible, not hit-testable, no tab stop).
//
// Modal floats install a focus trap (an internal FocusScope layer — Tab
// cycles inside only), consume Esc as dismiss, and optionally dim the
// backdrop. Hiding unmounts the trap layer, so the runtime restores focus
// to the previously focused node. Non-modal floats don't
// trap and are dismissed by their owner.
//
// Backdrop dimming note: the ADR describes a compositor attribute
// transform; the core compositor doesn't expose one in v1, so DimBackground
// paints a faint "░" scrim over the backdrop instead — same visual intent,
// widget-local implementation.
type Float struct {
	Base
	child  tui.Component
	anchor Anchor
	modal  bool
	dim    bool
	wPct   int // 0 = natural width; else a percentage of the available area
	hPct   int

	shown bool
	layer *floatLayer

	scrim style.Style
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

func clampPct(p int) int {
	switch {
	case p <= 0:
		return 0
	case p > 100:
		return 100
	}
	return p
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
		if focusFirst(f.child) {
			f.layer.fallback = false
		} else {
			// No focusable content: the layer itself becomes the trap's
			// single tab stop so keys (Esc!) stay inside the modal.
			f.layer.fallback = true
			f.layer.focusSelf()
		}
	}
}

// Hide unmounts the layer (focus restores to the pre-Show node when the
// float was modal) and emits DismissEvent.
func (f *Float) Hide() {
	if !f.shown {
		return
	}
	layer := f.layer
	f.layer = nil
	f.shown = false
	f.ctx.Unmount(layer)
	f.RequestLayout()
	f.publish(DismissEvent{Owner: f.NodeID()})
}

// Init re-arms hidden state on (re)mount. Re-entrant.
func (f *Float) Init(ctx *tui.Context) {
	f.Base.Init(ctx)
	f.shown = false
	f.layer = nil
}

// Layout: zero cells while hidden; the full overlay area while shown (the
// layer positions the content inside it).
func (f *Float) Layout(c tui.Constraints) tui.Size {
	if !f.shown {
		return c.Constrain(tui.Size{W: 0, H: 0})
	}
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	f.ctx.LayoutChild(f.layer, tui.Tight(tui.Size{W: w, H: h}))
	f.ctx.PlaceChild(f.layer, tui.Rect{X: 0, Y: 0, W: w, H: h})
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints nothing; the layer and child do.
func (f *Float) Render(tui.Surface) {}

// floatLayer is the full-area overlay layer: FocusScope trap (modal),
// backdrop scrim, Esc-dismiss, and anchor placement of the content.
type floatLayer struct {
	Base
	owner    *Float
	fallback bool // no focusable content: the layer is the trap's tab stop
}

var (
	_ tui.Focusable  = (*floatLayer)(nil)
	_ tui.FocusScope = (*floatLayer)(nil)
)

// Init mounts the user child under the layer.
func (l *floatLayer) Init(ctx *tui.Context) {
	l.Base.Init(ctx)
	ctx.Mount(l.owner.child)
}

// TrapsFocus implements tui.FocusScope: modal floats confine Tab.
func (l *floatLayer) TrapsFocus() bool { return l.owner.modal }

// AcceptsFocus implements tui.Focusable: the layer joins the ring only as
// the fallback stop of a modal whose content has no focusable — otherwise
// it would pollute the trap's Tab cycle.
func (l *floatLayer) AcceptsFocus() bool { return l.owner.modal && l.fallback }

// Layout places the child per the owner's anchor.
func (l *floatLayer) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	a := l.owner.anchor
	var sz tui.Size
	var x, y int
	if a.atRect {
		sz = l.ctx.LayoutChild(l.owner.child, tui.Tight(tui.Size{W: a.rect.W, H: a.rect.H}))
		x, y = a.rect.X, a.rect.Y
	} else if l.owner.wPct > 0 || l.owner.hPct > 0 {
		// A fraction of the area, per axis; the unset axis stays natural.
		want := tui.Size{W: w, H: h}
		if l.owner.wPct > 0 {
			want.W = max(w*l.owner.wPct/100, 1)
		}
		if l.owner.hPct > 0 {
			want.H = max(h*l.owner.hPct/100, 1)
		}
		if l.owner.wPct > 0 && l.owner.hPct > 0 {
			sz = l.ctx.LayoutChild(l.owner.child, tui.Tight(want))
		} else {
			sz = l.ctx.LayoutChild(l.owner.child, tui.Constraints{
				MinW: want.W, MaxW: want.W, MinH: 0, MaxH: want.H,
			})
			if l.owner.hPct > 0 {
				sz = l.ctx.LayoutChild(l.owner.child, tui.Constraints{
					MinW: 0, MaxW: want.W, MinH: want.H, MaxH: want.H,
				})
			}
		}
		x, y = alignOffset(a.align, tui.Size{W: w, H: h}, sz)
	} else {
		sz = l.ctx.LayoutChild(l.owner.child, tui.Loose(tui.Size{W: w, H: h}))
		x, y = alignOffset(a.align, tui.Size{W: w, H: h}, sz)
	}
	l.ctx.PlaceChild(l.owner.child, tui.Rect{X: x, Y: y, W: sz.W, H: sz.H})
	return c.Constrain(tui.Size{W: w, H: h})
}

// alignOffset resolves a tui.Align inside avail for content of size sz.
func alignOffset(a tui.Align, avail, sz tui.Size) (x, y int) {
	switch a {
	case tui.AlignTop, tui.AlignCenter, tui.AlignBottom:
		x = (avail.W - sz.W) / 2
	case tui.AlignTopRight, tui.AlignRight, tui.AlignBottomRight:
		x = avail.W - sz.W
	}
	switch a {
	case tui.AlignLeft, tui.AlignCenter, tui.AlignRight:
		y = (avail.H - sz.H) / 2
	case tui.AlignBottomLeft, tui.AlignBottom, tui.AlignBottomRight:
		y = avail.H - sz.H
	}
	return max(x, 0), max(y, 0)
}

// Render paints the scrim when dimming.
func (l *floatLayer) Render(s tui.Surface) {
	if !l.owner.dim {
		return
	}
	sz := s.Size()
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, "░", l.owner.scrim)
}

// HandleEvent: modal layers consume Esc (dismiss) and all backdrop mouse
// events; non-modal layers consume nothing.
func (l *floatLayer) HandleEvent(ev tui.Event) bool {
	if !l.owner.modal {
		return false
	}
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind != tui.KeyRelease && e.Code == tui.KeyEscape && e.Mods == 0 {
			l.owner.Hide()
			return true
		}
	case tui.MouseEvent:
		return true // the modal backdrop swallows the mouse
	}
	return false
}
