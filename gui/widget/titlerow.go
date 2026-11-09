package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
)

// titleRow is a title bar's native look, shared by Panel (a window inside the app) and TitleBar
// (the app's own window): the bar, the title, and round controls, right to left.
type titleRow struct {
	title            string
	titleX, titleEnd int  // the title's room, in cells
	fit              bool // the title shows only if it fits its room (trailing widgets follow it)
	controls         []control
	hover            control
}

// paint draws the row over the canvas's first cell row and reports the bar's colours; ok is false
// when the row is wholly covered, where the host paints the covering cells.
//
// The bar is a surface of the page, not the cells' row: a terminal marks the bar by reversing it,
// which in a window is a bright band across it. So the bar is the page colour around it tinted a
// little toward the text, with a hairline under it and the title muted. The text colour is whichever
// of the bar cells' colours stands off the page more, so a reversed or a plain bar style both give
// it.
func (r titleRow) paint(c gui.Canvas) (text, fill, page color.NRGBA, ok bool) {
	cell := c.CellSize()
	page = c.Backdrop()
	fg, bg, ok := openColors(c, 1)
	if !ok {
		return text, fill, page, false
	}
	text = fg
	if contrast(bg, page) > contrast(fg, page) {
		text = bg
	}
	fill = mix(text, page, 0.06)
	w := c.Size().W
	c.FillRect(gui.Rect{W: w, H: cell.H}, gui.Solid(fill))
	c.FillRect(gui.Rect{Y: cell.H - 1, W: w, H: 1}, gui.Solid(mix(text, page, 0.16)))
	if r.title != "" {
		t := c.Text().Layout(r.title, gui.Font{Size: c.TextSize() * 0.92}, 0)
		if !r.fit || t.Width <= float32(r.titleEnd-r.titleX)*cell.W {
			c.DrawText(t, gui.Pt(float32(r.titleX)*cell.W, (cell.H-t.Height)/2), gui.Solid(mix(text, page, 0.72)))
		}
	}
	rad := min(cell.H*0.32, cell.W*1.2)
	for i, ctl := range r.controls {
		cx := w - (float32(i)+0.5)*controlCells*cell.W
		col := [...]color.NRGBA{closeRed, maximizeGreen, minimizeAmber}[ctl]
		dot := gui.Rect{X: cx - rad, Y: cell.H/2 - rad, W: 2 * rad, H: 2 * rad}
		c.FillEllipse(dot, gui.Solid(col))
		if r.hover == ctl {
			c.StrokeRRect(dot, rad, 1, gui.Solid(color.NRGBA{A: 0x60}))
		}
	}
	return text, fill, page, true
}
