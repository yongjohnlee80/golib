package gui

import (
	"image"
	"math"

	"gioui.org/unit"
	"github.com/yongjohnlee80/golib/tui"
)

// fontMetrics is the monospace font's cell, per em: what one cell is at a font size of 1px.
// Glyph metrics scale linearly with the font size, so it is measured once and scaled.
type fontMetrics struct {
	advance, ascent, descent float32
}

// metrics is ONE snapshot of everything that turns cells into pixels. The Gio goroutine
// publishes a new one when the window or its scale changes; Size, Flush and pointer translation
// each read one and use it throughout, so a frame is never composed from two.
//
// It is a comparable value: two snapshots are equal exactly when nothing a cell needs changed.
type metrics struct {
	window   image.Point // the window, in device pixels
	scale    float32     // device pixels per device-independent pixel
	ppem     float32     // the font size, in device pixels
	cell     image.Point // one cell, in device pixels: whole pixels, so the grid has no seams
	baseline int         // device pixels from a cell's top to the glyphs' baseline
	grid     tui.Size    // whole cells that fit the window; at least 1×1
}

// measure computes the snapshot for a window of win device pixels at metric m.
func measure(fm fontMetrics, size unit.Sp, win image.Point, m unit.Metric) metrics {
	ppem := float32(m.Sp(size))
	if ppem <= 0 {
		ppem = float32(size)
	}
	asc, desc := fm.ascent*ppem, fm.descent*ppem
	cell := image.Point{
		X: max(1, int(math.Round(float64(fm.advance*ppem)))),
		Y: max(1, int(math.Ceil(float64(asc+desc)))),
	}
	scale := m.PxPerDp
	if scale <= 0 {
		scale = 1
	}
	return metrics{
		window: win,
		scale:  scale,
		ppem:   ppem,
		cell:   cell,
		// The line box is a whole pixel taller than the glyphs; centre them in it.
		baseline: int(math.Round(float64(asc + (float32(cell.Y)-(asc+desc))/2))),
		grid:     tui.Size{W: max(1, win.X/cell.X), H: max(1, win.Y/cell.Y)},
	}
}

// cellAt is the cell under the device-pixel point (x, y), clamped to the grid: a captured drag
// outside the window reports the edge cell, as a terminal does.
func (m metrics) cellAt(x, y float32) (cx, cy int) {
	cx = int(math.Floor(float64(x) / float64(m.cell.X)))
	cy = int(math.Floor(float64(y) / float64(m.cell.Y)))
	return min(max(cx, 0), m.grid.W-1), min(max(cy, 0), m.grid.H-1)
}

// cellRect is the device-pixel rectangle of cells (x, y) through (x+w, y+h).
func (m metrics) cellRect(x, y, w, h int) image.Rectangle {
	return image.Rect(x*m.cell.X, y*m.cell.Y, (x+w)*m.cell.X, (y+h)*m.cell.Y)
}

// sameCells reports whether o draws cells exactly as m does: everything but the window's size,
// which only moves the margin past the last whole cell.
func (m metrics) sameCells(o metrics) bool {
	m.window, o.window = image.Point{}, image.Point{}
	return m == o
}
