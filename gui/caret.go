package gui

import (
	"image"

	"github.com/yongjohnlee80/golib/tui"
)

// CaretView is a View that draws its own caret, as a text widget laying out in pixels does: its
// caret falls between cells, where tui's cell cursor cannot go. After the view paints, Caret is
// that caret, in the view's logical pixels, with its baseline below the rect's top; ok=false: none
// this frame. While no cell cursor shows, the backend tells the input method this caret and draws
// the text being composed at it, so a candidate window and a preedit follow the text.
type CaretView interface {
	View
	Caret() (r Rect, baseline float32, ok bool)
}

// viewCaret is the caret this frame's native views report, in device pixels with its baseline
// from its top: the last placement's that reports one.
func viewCaret(m metrics, natives []tui.NativePlacement) (image.Rectangle, int, bool) {
	var out image.Rectangle
	var base int
	found := false
	for _, p := range natives {
		cv, ok := p.View.(CaretView)
		if !ok {
			continue
		}
		r, b, ok := cv.Caret()
		if !ok {
			continue
		}
		o := m.cellRect(p.X, p.Y, 0, 0).Min
		s := m.scale
		out = image.Rect(o.X+int(r.X*s), o.Y+int(r.Y*s), o.X+int((r.X+max(r.W, 1))*s), o.Y+int((r.Y+r.H)*s))
		base, found = int(b*s), true
	}
	return out, base, found
}
