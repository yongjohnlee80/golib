package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// frame.go: the one border painter. Box, the modal card, a menu level, the command line's dropdown,
// a toast and the tile drop preview all frame themselves through it, so each honours the
// style.BorderStyle its style carries (a theme's ASCII, double or rounded border) rather than glyphs
// of its own.

// frameEdge is one edge of a frame: whether it is drawn, and its look.
type frameEdge struct {
	on bool
	st style.Style
}

// paintFrame paints bs around r: each edge drawn in its own look, and a corner where both of its
// edges are drawn, in the look of the top or bottom edge it closes. A glyph bs leaves empty is not
// drawn, and an edge with no room (a bottom or right edge on a rectangle one cell deep) is not.
func paintFrame(s tui.Surface, r tui.Rect, bs style.BorderStyle, top, right, bottom, left frameEdge) {
	if r.Empty() {
		return
	}
	x2, y2 := r.X+r.W-1, r.Y+r.H-1
	if top.on && bs.Top != "" {
		s.Fill(tui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, bs.Top, top.st)
	}
	if bottom.on && bs.Bottom != "" && r.H > 1 {
		s.Fill(tui.Rect{X: r.X, Y: y2, W: r.W, H: 1}, bs.Bottom, bottom.st)
	}
	if left.on && bs.Left != "" {
		s.Fill(tui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, bs.Left, left.st)
	}
	if right.on && bs.Right != "" && r.W > 1 {
		s.Fill(tui.Rect{X: x2, Y: r.Y, W: 1, H: r.H}, bs.Right, right.st)
	}
	if top.on && left.on && bs.TopLeft != "" {
		s.SetCell(r.X, r.Y, bs.TopLeft, top.st)
	}
	if top.on && right.on && bs.TopRight != "" {
		s.SetCell(x2, r.Y, bs.TopRight, top.st)
	}
	if bottom.on && left.on && bs.BottomLeft != "" {
		s.SetCell(r.X, y2, bs.BottomLeft, bottom.st)
	}
	if bottom.on && right.on && bs.BottomRight != "" {
		s.SetCell(x2, y2, bs.BottomRight, bottom.st)
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

// drawFrame paints a whole frame around r, every edge in st, in the border st carries (fallback
// when none). A rectangle under two cells either way has no room for a frame that does not overlap
// itself, and gets none.
func drawFrame(s tui.Surface, r tui.Rect, st style.Style, fallback style.BorderStyle) {
	if r.W < 2 || r.H < 2 {
		return
	}
	e := frameEdge{on: true, st: st}
	paintFrame(s, r, frameBorder(st, fallback), e, e, e, e)
}
