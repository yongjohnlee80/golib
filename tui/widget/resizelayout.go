package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// THE SIZING ALGORITHM.
//
// ONE helper computes the child's constraints, used by BOTH modes, so they
// cannot diverge. And the constraints are computed BEFORE the child is measured,
// which is the part an earlier design got wrong: measuring under the parent's
// constraints and clamping afterwards lets the child choose outside the
// configured min/max, and it is then PLACED into a rect it was never laid out
// under — breaking constraints-down/sizes-up and desynchronising render, hit
// geometry and every descendant's placement.

const resizeCommitKey tui.CommitKey = "widget.resizable"

// childConstraints is the box the child is measured in: the parent's ceiling
// intersected with the configured bounds, less any cells the handle reserves.
func (r *Resizable) childConstraints(c tui.Constraints) tui.Constraints {
	out := tui.Constraints{
		MinW: max(c.MinW, r.min.W),
		MaxW: min(c.MaxW, r.max.W),
		MinH: max(c.MinH, r.min.H),
		MaxH: min(c.MaxH, r.max.H),
	}
	// A configured minimum can never exceed what the parent allows: the parent
	// wins, and the wrapper reports the smaller truth rather than returning a
	// size its own parent will not accept.
	if out.MinW > out.MaxW {
		out.MinW = out.MaxW
	}
	if out.MinH > out.MaxH {
		out.MinH = out.MaxH
	}
	hw, hh := r.handleCells()
	out.MaxW = max(out.MaxW-hw, 0)
	out.MaxH = max(out.MaxH-hh, 0)
	out.MinW = min(out.MinW, out.MaxW)
	out.MinH = min(out.MinH, out.MaxH)
	return out
}

// Layout measures the child once, inside constraints derived from the parent's
// before measurement, so the child is never placed at a size it was not laid
// out under.
//
// THE RETURNED SIZE IS THE PARENT'S TRUTH, and the final Constrain is not
// belt-and-braces. Child + handle equals the wrapper in the ordinary case, but
// not when the parent's MINIMUM exceeds the configured MAXIMUM — a wrapper with
// a 30-cell cap inside a rect tightly fixed at 40. The child must not be forced
// past its cap, so it is measured at 30; the wrapper must not return a size its
// parent forbids, so it reports 40 and carries ten cells of slack. An earlier
// version returned 30 and the runtime flagged the constraint violation, which
// is the right answer arriving in the wrong place: a component that returns an
// illegal size has already broken the layout contract by the time anyone checks.
//
// It stores NOTHING and publishes NOTHING. The commit record does both — that
// is what keeps this function pure and the size honest.
func (r *Resizable) Layout(c tui.Constraints) tui.Size {
	ctx := r.Context()
	if ctx == nil {
		return c.Constrain(tui.Size{})
	}
	inner := r.childConstraints(c)
	if r.mode == SizeExplicit {
		inner = tightenTo(inner, r.requested, r)
	}
	got := ctx.LayoutChild(r.child, inner)

	hw, hh := r.handleCells()
	eff := c.Constrain(tui.Size{W: got.W + hw, H: got.H + hh})
	ctx.PlaceChild(r.child, tui.Rect{X: 0, Y: 0, W: got.W, H: got.H})
	r.placeGrips(ctx, eff)

	ctx.AfterLayout(resizeCommitKey, func() { r.commit(eff) })
	return eff
}

// tightenTo pins the constraints to the requested size, within what the parent
// and the configuration already allow.
//
// The request is a REQUEST: a tight parent wins, and the wrapper reports the
// size it actually reached rather than the one it asked for. That is what makes
// a Resizable inside a fixed-rect Float truthful instead of a special case.
func tightenTo(c tui.Constraints, want tui.Size, r *Resizable) tui.Constraints {
	hw, hh := r.handleCells()
	w := clampInt(want.W-hw, c.MinW, c.MaxW)
	h := clampInt(want.H-hh, c.MinH, c.MaxH)
	return tui.Constraints{MinW: w, MaxW: w, MinH: h, MaxH: h}
}

// clampInt bounds v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// placeGrips positions each handle against the wrapper's own edges.
//
// A grip that will not fit is DROPPED for the frame rather than driving the
// child below its minimum: the wrapper never renders an affordance it cannot
// fit, and a one-cell box with a grip over its only cell shows nothing at all.
func (r *Resizable) placeGrips(ctx *tui.Context, eff tui.Size) {
	for _, g := range r.grips {
		rect := gripRect(g.handle, eff)
		if rect.W <= 0 || rect.H <= 0 || eff.W <= 1 || eff.H <= 1 {
			ctx.LayoutChild(g, tui.Tight(tui.Size{}))
			ctx.PlaceChild(g, tui.Rect{})
			continue
		}
		ctx.LayoutChild(g, tui.Tight(tui.Size{W: rect.W, H: rect.H}))
		ctx.PlaceChild(g, rect)
	}
}

// gripRect is where one handle sits within a box of eff.
func gripRect(h Handle, eff tui.Size) tui.Rect {
	right, bottom := eff.W-1, eff.H-1
	switch h {
	case HandleBottomRight:
		return tui.Rect{X: right, Y: bottom, W: 1, H: 1}
	case HandleBottomLeft:
		return tui.Rect{X: 0, Y: bottom, W: 1, H: 1}
	case HandleTopRight:
		return tui.Rect{X: right, Y: 0, W: 1, H: 1}
	case HandleTopLeft:
		return tui.Rect{X: 0, Y: 0, W: 1, H: 1}
	case HandleRight:
		return tui.Rect{X: right, Y: 0, W: 1, H: eff.H}
	case HandleBottom:
		return tui.Rect{X: 0, Y: bottom, W: eff.W, H: 1}
	}
	return tui.Rect{}
}

// commit stores the effective size and publishes the change, in the commit
// phase where geometry-derived side effects are legal.
//
// No event on the FIRST layout: there was no previous size for it to differ
// from, and a listener counting resizes should not see one for the box simply
// appearing. No event when nothing changed: a drag that moves no cells has not
// resized anything, however much the pointer moved.
func (r *Resizable) commit(eff tui.Size) {
	if r.sized && r.committed == eff {
		return
	}
	first := !r.sized
	r.committed, r.sized = eff, true
	if first {
		return
	}
	if ctx := r.Context(); ctx != nil {
		ctx.Bus().Publish(ResizedEvent{Owner: r.NodeID(), Size: eff})
	}
}

// Render paints nothing: the wrapper is geometry, the child paints itself, and
// each grip paints its own cell.
func (r *Resizable) Render(tui.Surface) {}
