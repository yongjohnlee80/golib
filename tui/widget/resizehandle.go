package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// RESIZE ACTIONS AND THE GRIP.
//
// Keyboard parity is not a nicety here: the actions resolve on the WRAPPER, so
// a Resizable with its pointer disabled — or an application running without a
// mouse at all — resizes exactly the same way. The grip is a convenience over
// the same vocabulary, not a second implementation of it.

// ResizeBeginAction starts a gesture on one handle.
type ResizeBeginAction struct {
	// Handle is which edge, corner or divider the gesture is on. An unknown or
	// unsupported value is refused without mutation.
	Handle Handle
	// At is where the gesture started, in the RECEIVER's coordinates. The
	// widget measures the drag as a delta from it, so a resolver translating a
	// pointer event has to put it in the frame the size is measured in.
	At tui.Point
}

// ActionID returns the stable published name of this action.
func (ResizeBeginAction) ActionID() tui.ActionID { return "resize.begin" }

// ResizeUpdateAction reports the pointer's new position during a gesture.
type ResizeUpdateAction struct{ At tui.Point }

// ActionID returns the stable published name of this action.
func (ResizeUpdateAction) ActionID() tui.ActionID { return "resize.update" }

// ResizeStepAction moves by a discrete amount on each axis.
//
// The UNIT is part of the action rather than read from the widget, so a
// consumer binding a key can ask for a percentage step on a widget configured
// in cells without reconfiguring it. A zero step on both axes is not an action.
type ResizeStepAction struct {
	DX, DY int
	Unit   StepUnit
}

// ActionID returns the stable published name of this action.
func (ResizeStepAction) ActionID() tui.ActionID { return "resize.step" }

// ResizeSetAction asks for an exact size, routed through the same request path
// as the setter — so a programmatic size and a dragged one are clamped by one
// rule rather than two.
type ResizeSetAction struct{ Size tui.Size }

// ActionID returns the stable published name of this action.
func (ResizeSetAction) ActionID() tui.ActionID { return "resize.set" }

// ResizeEndAction finishes a gesture at the current size.
type ResizeEndAction struct{}

// ActionID returns the stable published name of this action.
func (ResizeEndAction) ActionID() tui.ActionID { return "resize.end" }

// ResizeCancelAction abandons a gesture and restores the REQUEST in force when
// it began. Bound to Escape by default.
type ResizeCancelAction struct{}

// ActionID returns the stable published name of this action.
func (ResizeCancelAction) ActionID() tui.ActionID { return "resize.cancel" }

// resizeKeys binds the wrapper's keyboard vocabulary.
//
// Shift-arrows rather than bare arrows: a resizable wraps arbitrary content, and
// bare arrows belong to whatever is inside it. A wrapper that stole them would
// make every scrollable child unusable.
func (r *Resizable) resizeKeys(ev tui.Event) (tui.Action, bool) {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind == tui.KeyRelease {
		return nil, false
	}
	if k.Code == tui.KeyEscape && k.Mods == 0 {
		return ResizeCancelAction{}, true
	}
	if k.Mods != tui.ModShift {
		return nil, false
	}
	// The widget's CONFIGURED unit, not a hardcoded one. Unit travels in the
	// action so a consumer's own binding can ask for something else — but the
	// built-in keys have to honour WithResizeStep, or its unit argument is dead
	// for every application that does not write its own resolver.
	switch k.Code {
	case tui.KeyRight:
		return ResizeStepAction{DX: 1, Unit: r.stepUnit}, true
	case tui.KeyLeft:
		return ResizeStepAction{DX: -1, Unit: r.stepUnit}, true
	case tui.KeyDown:
		return ResizeStepAction{DY: 1, Unit: r.stepUnit}, true
	case tui.KeyUp:
		return ResizeStepAction{DY: -1, Unit: r.stepUnit}, true
	}
	return nil, false
}

// HandleAction interprets the shared resize vocabulary.
//
// EVERY case validates before it mutates. An action is public input — a
// consumer's own resolver, a key binding, a DoAction from application code —
// so a malformed one is an ordinary occurrence rather than a programmer error
// worth a panic. Refusing without mutation is what keeps a rejected Begin from
// leaving live gesture state that a later Update would then act on.
func (r *Resizable) HandleAction(inv tui.ActionInvocation) bool {
	switch a := inv.Action.(type) {
	case ResizeBeginAction:
		return r.beginDrag(a)
	case ResizeUpdateAction:
		return r.dragTo(a.At)
	case ResizeStepAction:
		return r.stepBy(a)
	case ResizeSetAction:
		return r.setFromAction(a.Size)
	case ResizeEndAction:
		return r.endDrag()
	case ResizeCancelAction:
		return r.cancelDrag()
	}
	return false
}

// stepBy resizes by one step in the given direction, in the unit the ACTION
// asked for rather than the one the widget was configured with.
//
// It works from the EFFECTIVE size rather than the request, so stepping from an
// auto wrapper starts at what is on screen instead of at nothing — and stepping
// a wrapper the parent has clamped moves from where it actually is.
func (r *Resizable) stepBy(a ResizeStepAction) bool {
	if !a.Unit.Valid() {
		return false
	}
	if a.DX == 0 && a.DY == 0 {
		return false // a step of nothing is not a step
	}
	if !r.sized {
		return false // nothing has been laid out; there is no size to step from
	}
	base := r.committed
	sw, sh := r.stepFor(base, a.Unit)
	r.SetSize(tui.Size{W: base.W + a.DX*sw, H: base.H + a.DY*sh})
	return true
}

// setFromAction routes ResizeSetAction through the ordinary request path, so a
// size asked for by action and one asked for by setter are clamped by one rule.
func (r *Resizable) setFromAction(s tui.Size) bool {
	r.SetSize(s)
	return true
}

// stepFor is how many cells one step moves on each axis, in the given unit.
func (r *Resizable) stepFor(base tui.Size, unit StepUnit) (w, h int) {
	if unit == StepPercent {
		// At least one cell: a percentage of a small box rounds to zero, and a
		// keypress that provably cannot move anything is worse than a slow one.
		return max(base.W*r.step/100, 1), max(base.H*r.step/100, 1)
	}
	return r.step, r.step
}

// beginDrag records the gesture's origin and everything cancel has to put back.
//
// VALIDATED FIRST. A handle outside the declared set, or one this receiver
// cannot draw — a divider belongs to Split, not to a box — is refused with no
// drag stored, so the Update and End that follow are inert too. Accepting
// Handle(255) and storing a live drag was the defect: the gesture then had a
// direction of zero on both axes and swallowed every subsequent action.
func (r *Resizable) beginDrag(a ResizeBeginAction) bool {
	if !a.Handle.resizes() || !r.supports(a.Handle) {
		return false
	}
	if !r.sized {
		return false
	}
	r.drag = &resizeDrag{
		handle: a.Handle,
		origin: a.At,
		// beginSize is the EFFECTIVE size, which is what a drag delta is
		// measured from; beginRequested and beginMode are what cancel restores.
		// They are different values whenever a clamp is in force, and conflating
		// them made cancelling write the clamped size back as the user's
		// request — so a cancelled drag silently changed what would be
		// persisted, which is the opposite of what cancel means.
		beginSize:      r.committed,
		beginRequested: r.requested,
		beginMode:      r.mode,
	}
	return true
}

// supports reports whether this wrapper was configured with the handle. A
// gesture on a grip it does not have cannot have come from one of its own
// affordances, and honouring it would resize the box by an edge with nothing
// on screen to grab.
func (r *Resizable) supports(h Handle) bool {
	for _, have := range r.handles {
		if have == h {
			return true
		}
	}
	return false
}

// dragTo resizes to the size the pointer's current position implies.
//
// The delta is measured from the press position, not from the previous motion,
// so a drag that wanders and returns lands exactly where it started rather than
// accumulating rounding.
func (r *Resizable) dragTo(at tui.Point) bool {
	d := r.drag
	if d == nil {
		return false
	}
	w := d.beginSize.W + (at.X-d.origin.X)*d.handle.dx()
	h := d.beginSize.H + (at.Y-d.origin.Y)*d.handle.dy()
	r.SetSize(tui.Size{W: w, H: h})
	return true
}

// endDrag finishes at the current size.
func (r *Resizable) endDrag() bool {
	if r.drag == nil {
		return false
	}
	r.drag = nil
	r.releaseGrip()
	return true
}

// cancelDrag restores the REQUEST and the mode captured at the start.
//
// The request, not the effective size: a drag begun on a wrapper asking for
// 50x10 and clamped to 12x6 must leave the request at 50x10, or cancelling has
// quietly accepted the clamp as the user's choice — and the next terminal that
// has room would show 12x6 for a box they never resized.
//
// The MODE too: cancelling a drag that switched an auto wrapper to explicit must
// return it to auto, or the wrapper silently stops tracking its child because
// the user changed their mind.
func (r *Resizable) cancelDrag() bool {
	d := r.drag
	if d == nil {
		return false
	}
	r.drag = nil
	r.releaseGrip()
	r.requested, r.mode = d.beginRequested, d.beginMode
	r.RequestLayout()
	return true
}

// releaseGrip ends the pointer capture through the node that took it.
func (r *Resizable) releaseGrip() {
	if r.gripCtx != nil {
		r.gripCtx.ReleasePointer()
		r.gripCtx = nil
	}
}

// HandleEvent cleans up when the runtime revokes the capture: the drag is over,
// and the size reached so far stands. A capture loss is not a cancellation —
// the user did not ask to undo anything, and silently reverting their drag
// would be a change they never made.
func (r *Resizable) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.PointerCaptureLostEvent); ok {
		r.drag = nil
		r.gripCtx = nil
	}
	return false
}

// resizeDrag is the state one pointer gesture needs.
type resizeDrag struct {
	handle Handle
	origin tui.Point
	// beginSize is what the drag delta is measured from (effective), while
	// beginRequested and beginMode are what cancel puts back. Three fields
	// rather than two because the first two differ whenever a clamp is in
	// force, and cancel must not confuse them.
	beginSize      tui.Size
	beginRequested tui.Size
	beginMode      SizeMode
}

// resizeHandle is one grip: a real component, so it is hit-tested, captured,
// styled and themed by the runtime rather than by hand-rolled coordinate
// arithmetic in its parent.
type resizeHandle struct {
	Base
	owner  *Resizable
	handle Handle
}

// A GRIP DOES NOT IMPLEMENT tui.Focusable, and its absence is the mechanism
// rather than an omission: the runtime asks for the interface, so a type that
// does not have it can never be a tab stop. Adding nodes to the tree must not
// pollute traversal, and a resize grip is a pointer and action affordance, not
// a place the keyboard stops.
//
// Written as absence rather than as AcceptsFocus() returning false because the
// two are not equivalent to a reader: a method returning false invites someone
// to make it conditional later, which is exactly how a grip becomes a tab stop
// in one edit.

// Init installs the grip's pointer resolver.
func (g *resizeHandle) Init(ctx *tui.Context) {
	g.Base.Init(ctx)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(g.resolve))
}

// resolve maps the grip's own pointer events onto the wrapper's vocabulary.
//
// Coordinates are translated into the WRAPPER's frame before they leave here,
// because that is the frame the size is measured in — and the grip is the only
// thing that knows the offset between the two.
func (g *resizeHandle) resolve(ev tui.Event) (tui.Action, bool) {
	e, ok := ev.(tui.MouseEvent)
	if !ok || (e.Button != tui.MouseLeft && e.Kind != tui.MouseMotion) {
		return nil, false
	}
	at := g.toOwner(e.X, e.Y)
	switch e.Kind {
	case tui.MousePress:
		return ResizeBeginAction{Handle: g.handle, At: at}, true
	case tui.MouseMotion:
		if g.owner.drag == nil {
			return nil, false
		}
		return ResizeUpdateAction{At: at}, true
	case tui.MouseRelease:
		if g.owner.drag == nil {
			return nil, false
		}
		return ResizeEndAction{}, true
	}
	return nil, false
}

// toOwner converts grip-local coordinates into the wrapper's frame.
func (g *resizeHandle) toOwner(x, y int) tui.Point {
	r := g.owner.gripRect(g.handle, g.owner.committed)
	return tui.Point{X: r.X + x, Y: r.Y + y}
}

// HandleAction forwards to the wrapper and takes the capture here, because only
// the node whose handler is running may take the pointer.
func (g *resizeHandle) HandleAction(inv tui.ActionInvocation) bool {
	handled := g.owner.HandleAction(inv)
	if _, ok := inv.Action.(ResizeBeginAction); ok && handled {
		if ctx := g.Context(); ctx != nil {
			ctx.CapturePointer()
			g.owner.gripCtx = ctx
		}
	}
	return handled
}

// HandleEvent forwards a capture loss to the wrapper, which owns the gesture
// state. The grip holds the capture; the wrapper holds what it means.
func (g *resizeHandle) HandleEvent(ev tui.Event) bool {
	return g.owner.HandleEvent(ev)
}

// Layout takes exactly the cell the wrapper placed it in.
func (g *resizeHandle) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}

// Render paints the grip glyph.
func (g *resizeHandle) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	st := g.owner.st.Handle()
	if g.owner.drag != nil && g.owner.drag.handle == g.handle {
		st = g.owner.st.Active()
	}
	// An edge grip repeats along its length; a corner grip is one cell.
	for y := range sz.H {
		for x := range sz.W {
			s.SetCell(x, y, g.owner.glyph, st)
		}
	}
}
