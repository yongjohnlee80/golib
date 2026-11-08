package kit

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// FrameEdge is one edge of a frame: whether it is drawn, and its look.
type FrameEdge struct {
	On bool
	St style.Style
}

// PaintFrame paints bs around r: each edge drawn in its own look, and a corner where both of its
// edges are drawn, in the look of the top or bottom edge it closes. A glyph bs leaves empty is not
// drawn, and an edge with no room (a bottom or right edge on a rectangle one cell deep) is not.
func PaintFrame(s tui.Surface, r tui.Rect, bs style.BorderStyle, top, right, bottom, left FrameEdge) {
	if r.Empty() {
		return
	}
	x2, y2 := r.X+r.W-1, r.Y+r.H-1
	if top.On && bs.Top != "" {
		s.Fill(tui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, bs.Top, top.St)
	}
	if bottom.On && bs.Bottom != "" && r.H > 1 {
		s.Fill(tui.Rect{X: r.X, Y: y2, W: r.W, H: 1}, bs.Bottom, bottom.St)
	}
	if left.On && bs.Left != "" {
		s.Fill(tui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, bs.Left, left.St)
	}
	if right.On && bs.Right != "" && r.W > 1 {
		s.Fill(tui.Rect{X: x2, Y: r.Y, W: 1, H: r.H}, bs.Right, right.St)
	}
	if top.On && left.On && bs.TopLeft != "" {
		s.SetCell(r.X, r.Y, bs.TopLeft, top.St)
	}
	if top.On && right.On && bs.TopRight != "" {
		s.SetCell(x2, r.Y, bs.TopRight, top.St)
	}
	if bottom.On && left.On && bs.BottomLeft != "" {
		s.SetCell(r.X, y2, bs.BottomLeft, bottom.St)
	}
	if bottom.On && right.On && bs.BottomRight != "" {
		s.SetCell(x2, y2, bs.BottomRight, bottom.St)
	}
}

// frameBorder is the border st carries, or fallback when it carries none: the shape a widget draws
// unless its theme says otherwise.
func frameBorder(st style.Style, fallback style.BorderStyle) style.BorderStyle {
	if bs, ok := st.GetBorder(); ok {
		return bs
	}
	return fallback
}

// DrawFrame paints a whole frame around r, every edge in st, in the border st carries (fallback
// when none). A rectangle under two cells either way has no room for a frame that does not overlap
// itself, and gets none.
func DrawFrame(s tui.Surface, r tui.Rect, st style.Style, fallback style.BorderStyle) {
	if r.W < 2 || r.H < 2 {
		return
	}
	e := FrameEdge{On: true, St: st}
	PaintFrame(s, r, frameBorder(st, fallback), e, e, e, e)
}
