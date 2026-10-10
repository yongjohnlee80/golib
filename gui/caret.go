package gui

import (
	"image"
	"image/color"

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

// CaretColorsView is a CaretView that also reports the colours of the text at its caret, so the
// backend draws the text being composed there in them; a view without it gets the window
// theme's, which need not be its page's.
type CaretColorsView interface {
	CaretView
	CaretColors() (fg, bg color.NRGBA)
}

// caretAt is a caret in device pixels, with its baseline from its top, and the colours of the
// text at it when they are known (tinted).
type caretAt struct {
	rect   image.Rectangle
	base   int
	fg, bg color.NRGBA
	tinted bool
}

// viewCaret is the caret this frame's native views report: the last placement's that reports
// one, with its colours when that view reports them.
func viewCaret(m metrics, natives []tui.NativePlacement) (caretAt, bool) {
	var out caretAt
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
		out = caretAt{rect: image.Rect(o.X+int(r.X*s), o.Y+int(r.Y*s), o.X+int((r.X+max(r.W, 1))*s), o.Y+int((r.Y+r.H)*s)), base: int(b * s)}
		if ccv, ok := p.View.(CaretColorsView); ok {
			out.fg, out.bg = ccv.CaretColors()
			out.tinted = true
		}
		found = true
	}
	return out, found
}
