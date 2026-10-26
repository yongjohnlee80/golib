package widget

import "github.com/yongjohnlee80/golib/tui"

// BeginFrameDrag begins the drag a plain primary press on the window's frame starts. e is in its
// owner's frame, and size is the owner's laid-out size. Row 0 moves the window. The left and
// right columns, the bottom row and the two bottom corners resize it. A second press on row 0
// toggles maximize where the window has it. It reports false for a press that isn't its own. The
// owner calls it only for a press nothing in it wanted (tui.UnclaimedPressHandler), so a control on
// the frame keeps its click. Only on a backend declaring Capabilities.FrameDrag: a terminal keeps
// the Alt chord alone.
func (c *WindowCore) BeginFrameDrag(e tui.MouseEvent, size tui.Size) bool {
	if e.Kind != tui.MousePress || e.Button != tui.MouseLeft || e.Mods.Chord() != 0 || !c.frameDrag() {
		return false
	}
	if e.Y == 0 && e.Count == 2 && c.State().Maximize {
		return c.ToggleMaximize() // before the drag: a maximized window refuses to begin one
	}
	op, h, ok := frameAt(e.X, e.Y, size)
	if !ok {
		return false
	}
	if op == WindowResize {
		if h, ok = c.frameSide(h, tui.Point{X: e.X, Y: e.Y}); !ok {
			return false
		}
	}
	return c.BeginPointerDrag(op, e, h) // the chord's own path: capture, focus, GestureBegan, completion
}

// frameDrag reports whether the frame takes plain drags: on a FrameDrag backend, mounted.
func (c *WindowCore) frameDrag() bool { return c.live() && c.ctx.Capabilities().FrameDrag }

// frameAt is what a press at (x, y) on a window of size does: move from row 0 (its corners
// included: the title runs edge to edge); resize from a side, the bottom, or a bottom corner.
func frameAt(x, y int, size tui.Size) (WindowOperation, Handle, bool) {
	switch {
	case size.W < 2 || size.H < 2:
		return 0, 0, false
	case y == 0:
		return WindowMove, HandleBottomRight, true // a move ignores the handle
	case y == size.H-1 && x == 0:
		return WindowResize, HandleBottomLeft, true
	case y == size.H-1 && x == size.W-1:
		return WindowResize, HandleBottomRight, true
	case y == size.H-1:
		return WindowResize, HandleBottom, true
	case x == 0:
		return WindowResize, HandleLeft, true
	case x == size.W-1:
		return WindowResize, HandleRight, true
	}
	return 0, 0, false
}

// frameSide is the handle a frame press at p resizes. A target that picks its own handle (a tile,
// WindowHandles) resizes only a side it shares, and HandleAt names the nearest one. An edge needs
// HandleAt to name that edge itself, or a press on a tile's screen-edge column would resize the
// border opposite it. A corner takes whichever of its two sides HandleAt names. Every other target
// resizes from h.
func (c *WindowCore) frameSide(h Handle, p tui.Point) (Handle, bool) {
	if c.handles == nil {
		return h, true
	}
	got, ok := c.handles.HandleAt(c.pointerPosition(p))
	return frameSideOf(h, got, ok)
}

// frameSideOf is frameSide's choice from what HandleAt named for the press: got, or nothing (!ok).
func frameSideOf(h, got Handle, ok bool) (Handle, bool) {
	switch {
	case !ok:
		return 0, false
	case h == HandleBottomLeft:
		return got, got == HandleBottom || got == HandleLeft
	case h == HandleBottomRight:
		return got, got == HandleBottom || got == HandleRight
	}
	return h, got == h
}

// FrameShapes appends the resize arrows over the frame cells BeginFrameDrag resizes from, in the
// owner's frame. They show during a drag too: the pointer keeps its arrow while it is held.
func (c *WindowCore) FrameShapes(dst []tui.PointerRegion, size tui.Size) []tui.PointerRegion {
	if !c.frameDrag() || !c.resize || c.minimized || c.State().Maximized || size.W < 2 || size.H < 2 {
		return dst
	}
	frame := [...]struct {
		h Handle
		r tui.Rect
	}{
		{HandleLeft, tui.Rect{X: 0, Y: 1, W: 1, H: size.H - 2}},
		{HandleRight, tui.Rect{X: size.W - 1, Y: 1, W: 1, H: size.H - 2}},
		{HandleBottom, tui.Rect{X: 1, Y: size.H - 1, W: size.W - 2, H: 1}},
		{HandleBottomLeft, tui.Rect{X: 0, Y: size.H - 1, W: 1, H: 1}},
		{HandleBottomRight, tui.Rect{X: size.W - 1, Y: size.H - 1, W: 1, H: 1}},
	}
	var hits [len(frame)]handleHit
	switch hs := c.handles.(type) {
	case nil:
		for i := range hits {
			hits[i] = handleHit{frame[i].h, true}
		}
	case windowHandlesAt: // one reading of the target's layout for all five
		var ps [len(frame)]tui.Point
		for i, f := range frame {
			ps[i] = c.pointerPosition(tui.Point{X: f.r.X, Y: f.r.Y})
		}
		hs.handlesAt(ps[:], hits[:])
		for i := range hits {
			hits[i].h, hits[i].ok = frameSideOf(frame[i].h, hits[i].h, hits[i].ok)
		}
	default:
		for i, f := range frame {
			hits[i].h, hits[i].ok = c.frameSide(f.h, tui.Point{X: f.r.X, Y: f.r.Y})
		}
	}
	for i, f := range frame {
		if !f.r.Empty() && hits[i].ok {
			dst = append(dst, tui.PointerRegion{Rect: f.r, Shape: hits[i].h.pointerShape()}) // a tile's corner shows its side's arrow
		}
	}
	return dst
}

// handleHit is what a target's HandleAt named for one point.
type handleHit struct {
	h  Handle
	ok bool
}

// windowHandlesAt is a WindowHandles that answers several points from one reading of its layout
// (a tile manager's target).
type windowHandlesAt interface {
	handlesAt(ps []tui.Point, out []handleHit)
}
