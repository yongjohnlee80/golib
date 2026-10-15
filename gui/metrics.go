package gui

import (
	"image"
	"math"

	"github.com/yongjohnlee80/gio/unit"
	"github.com/yongjohnlee80/golib/tui"
)

// Package gui provides high-performance graphical terminal rendering, layout,
// typography, and event handling layered on top of vector graphics and GPU surfaces.

// Coordinate Systems and Metric Layout:
//
//	+-------------------------------------------------------------+
//	| Window Bounds (Device Pixels: win.X * win.Y)                |
//	|                                                             |
//	|   origin.X                                                  |
//	|   |<----->|                                                 |
//	|   +-------+---------------------------------------+         |
//	|   | pad   | Top Margin (p + spare.Y/2)            |         |
//	|   +-------+---------------------------------------+         |
//	|   | Left  | Grid Cell (0, 0)      ... Grid (W-1, 0)| Right  |
//	|   | Margin| +-------------+           +---------+ | Margin  |
//	|   |       | | cell.X      |           |         | |         |
//	|   |       | |*----*       |           |         | |         |
//	|   |       | |cell.Y       |           |         | |         |
//	|   |       | +-------------+           +---------+ |         |
//	|   |       |                                       |         |
//	|   |       | Grid (0, H-1)         ... Grid(W-1,H-1)|        |
//	|   +-------+---------------------------------------+         |
//	|   |       | Bottom Margin                         |         |
//	|   +-------+---------------------------------------+         |
//	+-------------------------------------------------------------+
//
// fontMetrics captures monospace font dimension ratios per em unit (1px font size).
// Glyph dimensions scale linearly with font size, so base ratios are measured once
// and scaled across device displays.
type fontMetrics struct {
	advance float32 // Horizontal cell advance width per em unit
	ascent  float32 // Distance above baseline per em unit
	descent float32 // Distance below baseline per em unit
}

// metrics represents ONE immutable snapshot of everything that turns cells into pixels.
//
// Whenever the host window size, display scale factor (DPI), or font configuration changes,
// the Gio goroutine publishes a new snapshot. The layout, paint, and pointer translation routines
// each read a snapshot and use it consistently throughout, so a frame is never composed from mixed
// geometric configurations.
//
// It is a comparable struct: two snapshots are identical (==) when every field matches,
// including window dimensions and origin offsets. When comparing solely cell rendering behavior
// (sameCells), only window size differences are ignored.
type metrics struct {
	window   image.Point // Total window dimension in physical device pixels
	origin   image.Point // Top-left pixel coordinate of grid cell (0, 0), including margins
	scale    float32     // Physical device pixels per device-independent pixel (DPI scale)
	ppem     float32     // Rendered font size in physical device pixels (pixels per em)
	cell     image.Point // Width and height of an individual cell in physical device pixels
	baseline int         // Distance in device pixels from the cell top edge to the font baseline
	grid     tui.Size    // Number of columns and rows that fit into the available window grid
	fontGen  uint64      // Generation counter of the underlying font state (invalidates on change)
}

// measure computes a geometric snapshot for a window of win physical device pixels,
// scaling according to display metric m, and reserving pad margin on each edge.
//
// Terminals require padding around their cells to prevent character glyphs from touching
// window border decorations. If the remaining inner window area is not an integer multiple
// of cell dimensions, the remaining spare pixels are evenly divided between opposite sides
// so that the character grid remains centered.
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

// cellAt maps a physical device-pixel coordinate (x, y) to the corresponding grid cell
// indices (cx, cy). Coordinates falling outside the grid area (such as during window drags
// beyond edge boundaries) are clamped to the nearest edge cell.
func (m metrics) cellAt(x, y float32) (cx, cy int) {
	cx = int(math.Floor(float64(x-float32(m.origin.X)) / float64(m.cell.X)))
	cy = int(math.Floor(float64(y-float32(m.origin.Y)) / float64(m.cell.Y)))
	return min(max(cx, 0), m.grid.W-1), min(max(cy, 0), m.grid.H-1)
}

// subCell calculates the normalized sub-cell relative offset [0, 1) where physical coordinate
// (x, y) falls inside the cell indexed by (cx, cy). This sub-cell precision enables smooth
// high-resolution pointer tracking, fine selection highlights, and fluid scroll gestures.
func (m metrics) subCell(x, y float32, cx, cy int) (sx, sy float32) {
	in := func(v float32) float32 { return min(max(v, 0), math.Nextafter32(1, 0)) }
	sx = in((x-float32(m.origin.X))/float32(m.cell.X) - float32(cx))
	sy = in((y-float32(m.origin.Y))/float32(m.cell.Y) - float32(cy))
	return sx, sy
}

// cellRect converts a rectangular block of cells spanning (x, y) with size (w, h)
// into an absolute bounding rectangle expressed in physical device pixels.
func (m metrics) cellRect(x, y, w, h int) image.Rectangle {
	return image.Rect(x*m.cell.X, y*m.cell.Y, (x+w)*m.cell.X, (y+h)*m.cell.Y).Add(m.origin)
}

// sameCells reports whether another metrics instance produces identical cell rendering.
// Changes solely to total window size merely alter surrounding margins and do not invalidate
// cached glyph rasterizations or internal cell geometry.
func (m metrics) sameCells(o metrics) bool {
	m.window, o.window = image.Point{}, image.Point{}
	return m == o
}
