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

// ResizeStepAction grows or shrinks the wrapper by one configured step.
type ResizeStepAction struct {
	// DW and DH are the direction on each axis: -1, 0 or +1. The STEP SIZE is
	// the wrapper's own configuration, not the action's, so a consumer binding
	// its own key cannot accidentally resize by a different amount than the
	// grips do.
	DW, DH int
}

// ActionID returns the stable published name of this action.
func (ResizeStepAction) ActionID() tui.ActionID { return "resize.step" }

// ResizeBeginAction starts a pointer drag on one handle.
type ResizeBeginAction struct {
	Handle Handle
	// X and Y are where the press landed, in the wrapper's own coordinates, so
	// the drag can be measured as a delta from it.
	X, Y int
}

// ActionID returns the stable published name of this action.
func (ResizeBeginAction) ActionID() tui.ActionID { return "resize.begin" }

// ResizeDragAction reports the pointer's new position during a drag.
type ResizeDragAction struct{ X, Y int }

// ActionID returns the stable published name of this action.
func (ResizeDragAction) ActionID() tui.ActionID { return "resize.drag" }

// ResizeEndAction finishes a drag at the current size.
type ResizeEndAction struct{}

// ActionID returns the stable published name of this action.
func (ResizeEndAction) ActionID() tui.ActionID { return "resize.end" }

// ResizeCancelAction abandons a drag and restores the size captured at its
// start. Bound to Escape by default.
type ResizeCancelAction struct{}

// ActionID returns the stable published name of this action.
func (ResizeCancelAction) ActionID() tui.ActionID { return "resize.cancel" }

// resizeKeys binds the wrapper's keyboard vocabulary.
//
// Shift-arrows rather than bare arrows: a resizable wraps arbitrary content, and
// bare arrows belong to whatever is inside it. A wrapper that stole them would
// make every scrollable child unusable.
func resizeKeys(ev tui.Event) (tui.Action, bool) {
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
	switch k.Code {
	case tui.KeyRight:
		return ResizeStepAction{DW: 1}, true
	case tui.KeyLeft:
		return ResizeStepAction{DW: -1}, true
	case tui.KeyDown:
		return ResizeStepAction{DH: 1}, true
	case tui.KeyUp:
		return ResizeStepAction{DH: -1}, true
	}
	return nil, false
}

// HandleAction interprets the resize vocabulary.
func (r *Resizable) HandleAction(inv tui.ActionInvocation) bool {
	switch a := inv.Action.(type) {
	case ResizeStepAction:
		return r.stepBy(a.DW, a.DH)
	case ResizeBeginAction:
		return r.beginDrag(a)
	case ResizeDragAction:
		return r.dragTo(a.X, a.Y)
	case ResizeEndAction:
		return r.endDrag()
	case ResizeCancelAction:
		return r.cancelDrag()
	}
	return false
}

// stepBy resizes by one configured step in the given direction.
//
// It works from the EFFECTIVE size rather than the request, so stepping from an
// auto wrapper starts at what is on screen instead of at nothing — and stepping
// a wrapper the parent has clamped moves from where it actually is.
func (r *Resizable) stepBy(dw, dh int) bool {
	if dw == 0 && dh == 0 {
		return false
	}
	base := r.committed
	if !r.sized {
		return false // nothing has been laid out; there is no size to step from
	}
	sw, sh := r.stepFor(base)
	r.SetSize(tui.Size{W: base.W + dw*sw, H: base.H + dh*sh})
	return true
}

// stepFor is how many cells one step moves on each axis.
func (r *Resizable) stepFor(base tui.Size) (w, h int) {
	if r.stepUnit == StepPercent {
		// At least one cell: a percentage of a small box rounds to zero, and a
		// keypress that provably cannot move anything is worse than a slow one.
		return max(base.W*r.step/100, 1), max(base.H*r.step/100, 1)
	}
	return r.step, r.step
}

// beginDrag records the gesture's origin and the size to restore on cancel.
func (r *Resizable) beginDrag(a ResizeBeginAction) bool {
	if !r.sized {
		return false
	}
	r.drag = &resizeDrag{
		handle:    a.Handle,
		originX:   a.X,
		originY:   a.Y,
		beginSize: r.committed,
		beginMode: r.mode,
	}
	return true
}

// dragTo resizes to the size the pointer's current position implies.
//
// The delta is measured from the press position, not from the previous motion,
// so a drag that wanders and returns lands exactly where it started rather than
// accumulating rounding.
func (r *Resizable) dragTo(x, y int) bool {
	d := r.drag
	if d == nil {
		return false
	}
	w := d.beginSize.W + (x-d.originX)*d.handle.dx()
	h := d.beginSize.H + (y-d.originY)*d.handle.dy()
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

// cancelDrag restores the size and the mode captured at the start.
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
	r.requested, r.mode = d.beginSize, d.beginMode
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
	handle           Handle
	originX, originY int
	beginSize        tui.Size
	beginMode        SizeMode
}

// resizeHandle is one grip: a real component, so it is hit-tested, captured,
// styled and themed by the runtime rather than by hand-rolled coordinate
// arithmetic in its parent.
type resizeHandle struct {
	Base
	owner  *Resizable
	handle Handle
}

// AcceptsFocus is deliberately absent from this type's contract: a grip is not
// a tab stop, and adding nodes to the tree must not pollute traversal. It is
// stated as a method returning false rather than by omission so the intent is
// visible at the site.
func (g *resizeHandle) AcceptsFocus() bool { return false }

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
	x, y := g.toOwner(e.X, e.Y)
	switch e.Kind {
	case tui.MousePress:
		return ResizeBeginAction{Handle: g.handle, X: x, Y: y}, true
	case tui.MouseMotion:
		if g.owner.drag == nil {
			return nil, false
		}
		return ResizeDragAction{X: x, Y: y}, true
	case tui.MouseRelease:
		if g.owner.drag == nil {
			return nil, false
		}
		return ResizeEndAction{}, true
	}
	return nil, false
}

// toOwner converts grip-local coordinates into the wrapper's frame.
func (g *resizeHandle) toOwner(x, y int) (int, int) {
	r := gripRect(g.handle, g.owner.committed)
	return r.X + x, r.Y + y
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
