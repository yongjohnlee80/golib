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
	origin   image.Point // the top-left of cell (0, 0): the padding plus half the leftover, device pixels
	scale    float32     // device pixels per device-independent pixel
	ppem     float32     // the font size, in device pixels
	cell     image.Point // one cell, in device pixels: whole pixels, so the grid has no seams
	baseline int         // device pixels from a cell's top to the glyphs' baseline
	grid     tui.Size    // whole cells that fit the window inside its padding; at least 1×1
}

// measure computes the snapshot for a window of win device pixels at metric m, with pad of
// padding on every side, as terminals keep their cells off the window's edge.
func measure(fm fontMetrics, size unit.Sp, pad unit.Dp, win image.Point, m unit.Metric) metrics {
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
	p := max(m.Dp(pad), 0)
	inner := image.Pt(max(win.X-2*p, 0), max(win.Y-2*p, 0))
	grid := tui.Size{W: max(1, inner.X/cell.X), H: max(1, inner.Y/cell.Y)}
	// The window is rarely a whole number of cells: the remainder is split between opposite
	// sides, so the grid sits centred and the margins match left and right, top and bottom.
	spare := image.Pt(max(inner.X-grid.W*cell.X, 0), max(inner.Y-grid.H*cell.Y, 0))
	return metrics{
		window: win,
		origin: image.Pt(p+spare.X/2, p+spare.Y/2),
		scale:  scale,
		ppem:   ppem,
		cell:   cell,
		// The line box is a whole pixel taller than the glyphs; centre them in it.
		baseline: int(math.Round(float64(asc + (float32(cell.Y)-(asc+desc))/2))),
		grid:     grid,
	}
}

// cellAt is the cell under the device-pixel point (x, y), clamped to the grid: a captured drag
// outside the window reports the edge cell, as a terminal does.
func (m metrics) cellAt(x, y float32) (cx, cy int) {
	cx = int(math.Floor(float64(x-float32(m.origin.X)) / float64(m.cell.X)))
	cy = int(math.Floor(float64(y-float32(m.origin.Y)) / float64(m.cell.Y)))
	return min(max(cx, 0), m.grid.W-1), min(max(cy, 0), m.grid.H-1)
}

// subCell is where the device-pixel point (x, y) falls inside cell (cx, cy), each in [0, 1). A
// point outside the cell (a drag past the window's edge, clamped by cellAt) reports the edge.
func (m metrics) subCell(x, y float32, cx, cy int) (sx, sy float32) {
	in := func(v float32) float32 { return min(max(v, 0), math.Nextafter32(1, 0)) }
	sx = in((x-float32(m.origin.X))/float32(m.cell.X) - float32(cx))
	sy = in((y-float32(m.origin.Y))/float32(m.cell.Y) - float32(cy))
	return sx, sy
}

// cellRect is the device-pixel rectangle, in the window, of cells (x, y) through (x+w, y+h).
func (m metrics) cellRect(x, y, w, h int) image.Rectangle {
	return image.Rect(x*m.cell.X, y*m.cell.Y, (x+w)*m.cell.X, (y+h)*m.cell.Y).Add(m.origin)
}

// sameCells reports whether o draws cells exactly as m does: everything but the window's size,
// which only moves the margins around the grid.
func (m metrics) sameCells(o metrics) bool {
	m.window, o.window = image.Point{}, image.Point{}
	return m == o
}
