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

// insets is the cells each side of the wrapper reserves for grips.
//
// DIRECTIONAL, computed from the configured handles. A single width/height bit
// could not say WHICH side the cells came off, so the child was always placed at
// (0,0) and a reserved TOP-LEFT grip landed on the child's first cell —
// "reserve" behaving exactly like "overlay", which is the one thing it exists
// not to do. Opposing handles reserve both sides.
type insets struct{ left, right, top, bottom int }

func (i insets) w() int { return i.left + i.right }
func (i insets) h() int { return i.top + i.bottom }

// reserved is the inset set for this wrapper, or the zero value under overlay
// placement, where a grip costs the child nothing by design.
func (r *Resizable) reserved() insets {
	var in insets
	if r.placement != PlacementReserve {
		return in
	}
	cells := r.glyphCells
	for _, h := range r.handles {
		switch {
		case h.dx() < 0:
			in.left = cells
		case h.dx() > 0:
			in.right = cells
		}
		switch {
		case h.dy() < 0:
			in.top = 1
		case h.dy() > 0:
			in.bottom = 1
		}
	}
	return in
}

// childConstraints is the box the child is measured in: the parent's ceiling
// intersected with the configured bounds, less any cells the handles reserve.
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
	// BOTH bounds lose the reserved cells, not just the maximum. The configured
	// min/max describe the WRAPPER, so a child measured against the wrapper's
	// minimum would be one band too large and the outer contract would no
	// longer be true: a wrapper with a 10-cell minimum and a reserved edge owes
	// its parent 10 cells, of which the child may have 9.
	in := r.reserved()
	out.MaxW = max(out.MaxW-in.w(), 0)
	out.MaxH = max(out.MaxH-in.h(), 0)
	out.MinW = min(max(out.MinW-in.w(), 0), out.MaxW)
	out.MinH = min(max(out.MinH-in.h(), 0), out.MaxH)
	return out
}

// Layout measures the child once, inside constraints derived from the parent's
// before measurement, so the child is never placed at a size it was not laid
// out under.
//
// THE RETURNED SIZE IS THE PARENT'S TRUTH, and the final Constrain is not
// belt-and-braces. Child + reserved bands equals the wrapper in the ordinary
// case, but not when the parent's MINIMUM exceeds the configured MAXIMUM — a
// wrapper with a 30-cell cap inside a rect tightly fixed at 40. The child must
// not be forced past its cap, so it is measured at 30; the wrapper must not
// return a size its parent forbids, so it reports 40 and carries ten cells of
// slack. An earlier version returned 30 and the runtime flagged the constraint
// violation, which is the right answer arriving in the wrong place: a component
// that returns an illegal size has already broken the layout contract by the
// time anyone checks.
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

	in := r.reserved()
	eff := c.Constrain(tui.Size{W: got.W + in.w(), H: got.H + in.h()})
	// OFFSET BY THE LEADING BANDS. Placing at (0,0) under reserve put the child
	// underneath a top or left grip, which is the occlusion reserve exists to
	// prevent.
	ctx.PlaceChild(r.child, tui.Rect{X: in.left, Y: in.top, W: got.W, H: got.H})
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
	in := r.reserved()
	w := clampInt(want.W-in.w(), c.MinW, c.MaxW)
	h := clampInt(want.H-in.h(), c.MinH, c.MaxH)
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

// placeGrips positions each handle in its reserved band, or over the child's
// edge under overlay placement.
//
// A grip is DROPPED when the rect it actually needs does not fit — measured
// against its own width, not a blanket "the box is one cell". A wide glyph
// needs two columns, and dropping it only at one-cell boxes let it be placed in
// a space too narrow to show it, where it rendered nothing at all: an
// affordance that is present, hit-testable and invisible.
func (r *Resizable) placeGrips(ctx *tui.Context, eff tui.Size) {
	for _, g := range r.grips {
		rect := r.gripRect(g.handle, eff)
		if rect.W <= 0 || rect.H <= 0 || rect.X < 0 || rect.Y < 0 ||
			rect.X+rect.W > eff.W || rect.Y+rect.H > eff.H {
			ctx.LayoutChild(g, tui.Tight(tui.Size{}))
			ctx.PlaceChild(g, tui.Rect{})
			continue
		}
		ctx.LayoutChild(g, tui.Tight(tui.Size{W: rect.W, H: rect.H}))
		ctx.PlaceChild(g, rect)
	}
}

// gripRect is where one handle sits within a box of eff.
//
// A method rather than a free function because the answer depends on the
// wrapper's glyph width: a two-column glyph occupies two columns, and a rect
// that said otherwise would clip the affordance in half.
func (r *Resizable) gripRect(h Handle, eff tui.Size) tui.Rect {
	w := r.glyphCells
	right, bottom := eff.W-w, eff.H-1
	switch h {
	case HandleBottomRight:
		return tui.Rect{X: right, Y: bottom, W: w, H: 1}
	case HandleBottomLeft:
		return tui.Rect{X: 0, Y: bottom, W: w, H: 1}
	case HandleTopRight:
		return tui.Rect{X: right, Y: 0, W: w, H: 1}
	case HandleTopLeft:
		return tui.Rect{X: 0, Y: 0, W: w, H: 1}
	case HandleRight:
		return tui.Rect{X: right, Y: 0, W: w, H: eff.H}
	case HandleLeft:
		return tui.Rect{X: 0, Y: 0, W: w, H: eff.H}
	case HandleBottom:
		return tui.Rect{X: 0, Y: bottom, W: eff.W, H: 1}
	case HandleTop:
		return tui.Rect{X: 0, Y: 0, W: eff.W, H: 1}
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
