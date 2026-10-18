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
	// layout is the Panes' layout (WithPanes); nil for a cell-root window, which maps as one pane
	// over the whole grid. Shared, never mutated: a new layout is a new pointer.
	layout *paneLayout
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

// pane is the pane whose band holds virtual cell (x, y); false for a hole or a cell outside the
// grid. Without a layout, one pane: the grid, at the grid's origin, clipped to the window.
func (m metrics) pane(x, y int) (paneMap, bool) {
	if m.layout == nil {
		return m.wholePane(), x >= 0 && y >= 0 && x < m.grid.W && y < m.grid.H
	}
	for _, p := range m.layout.panes {
		if b := p.band; x >= b.X && y >= b.Y && x < b.X+b.W && y < b.Y+b.H {
			return p, true
		}
	}
	return paneMap{}, false
}

// wholePane is a cell-root window's one pane.
func (m metrics) wholePane() paneMap {
	return paneMap{band: tui.Rect{W: m.grid.W, H: m.grid.H}, origin: m.origin, clip: image.Rectangle{Max: m.window}}
}

// panes is the panes cells are drawn in, in tree order: the layout's, or the one whole pane.
func (m metrics) panes() []paneMap {
	if m.layout == nil {
		return []paneMap{m.wholePane()}
	}
	return m.layout.panes
}

// paneAt is the pane device pixel (x, y) falls in; outside every pane (the padding, a pixel
// between panes), the nearest. Panes with an empty band are never chosen.
func (m metrics) paneAt(x, y float32) paneMap {
	if m.layout == nil {
		return m.wholePane()
	}
	best, bestD := paneMap{}, float32(math.Inf(1))
	for _, p := range m.layout.panes {
		if p.band.Empty() || p.clip.Empty() {
			continue
		}
		dx := max(float32(p.clip.Min.X)-x, 0, x-float32(p.clip.Max.X-1))
		dy := max(float32(p.clip.Min.Y)-y, 0, y-float32(p.clip.Max.Y-1))
		if d := dx*dx + dy*dy; d < bestD {
			best, bestD = p, d
		}
	}
	return best
}

// cellAt maps device pixel (x, y) to the cell of the pane it falls in, clamped to that pane's
// band, so a pointer past the grid's or a pane's edge keeps tracking the nearest cell.
func (m metrics) cellAt(x, y float32) (cx, cy int) {
	return m.cellIn(m.paneAt(x, y), x, y, false)
}

// cellIn maps (x, y) through pane p's band. grid clamps to the whole grid instead of to the
// band: a drag held by its press pane keeps counting cells past that pane's edge.
func (m metrics) cellIn(p paneMap, x, y float32, grid bool) (cx, cy int) {
	cx = p.band.X + int(math.Floor(float64(x-float32(p.origin.X))/float64(m.cell.X)))
	cy = p.band.Y + int(math.Floor(float64(y-float32(p.origin.Y))/float64(m.cell.Y)))
	lo, hi := tui.Point{X: p.band.X, Y: p.band.Y}, tui.Point{X: p.band.X + p.band.W - 1, Y: p.band.Y + p.band.H - 1}
	if grid {
		lo, hi = tui.Point{}, tui.Point{X: m.grid.W - 1, Y: m.grid.H - 1}
	}
	return min(max(cx, lo.X), hi.X), min(max(cy, lo.Y), hi.Y)
}

// subCell is where (x, y) falls inside cell (cx, cy), mapped by that cell's band, in [0, 1) on
// each axis.
func (m metrics) subCell(x, y float32, cx, cy int) (sx, sy float32) {
	p, ok := m.pane(cx, cy)
	if !ok {
		p = m.wholePane()
	}
	return m.subCellIn(p, x, y, cx, cy)
}

// subCellIn is subCell mapped by pane p's band: a held drag's press pane.
func (m metrics) subCellIn(p paneMap, x, y float32, cx, cy int) (sx, sy float32) {
	in := func(v float32) float32 { return min(max(v, 0), math.Nextafter32(1, 0)) }
	sx = in((x-float32(p.origin.X))/float32(m.cell.X) - float32(cx-p.band.X))
	sy = in((y-float32(p.origin.Y))/float32(m.cell.Y) - float32(cy-p.band.Y))
	return sx, sy
}

// cellPoint is the device position of cell (x, y)'s top-left corner, mapped by its own band;
// false for a hole.
func (m metrics) cellPoint(x, y int) (image.Point, bool) {
	p, ok := m.pane(x, y)
	if !ok && m.layout != nil {
		return image.Point{}, false
	}
	return image.Pt((x-p.band.X)*m.cell.X, (y-p.band.Y)*m.cell.Y).Add(p.origin), true
}

// cellRect is the device rectangle of cells (x, y, w, h), mapped by (x, y)'s band and not
// clipped: one cell, a run or a rectangle inside one band. A rectangle that may cross bands goes
// through fragments; a whole placement through anchored.
func (m metrics) cellRect(x, y, w, h int) image.Rectangle {
	p, _ := m.pane(x, y)
	if m.layout == nil {
		p = m.wholePane() // cells past the grid map as today
	}
	x0, y0 := (x-p.band.X)*m.cell.X, (y-p.band.Y)*m.cell.Y
	return image.Rect(x0, y0, x0+w*m.cell.X, y0+h*m.cell.Y).Add(p.origin)
}

// fragment is the part of a cell rectangle inside one band: its cells, their device rectangle
// and the pane's clip.
type fragment struct {
	cells tui.Rect
	dev   image.Rectangle
	clip  image.Rectangle
}

// fragments splits cells r into its per-band parts, each mapped by its own band. Holes yield
// nothing. Without a layout, r itself.
func (m metrics) fragments(r tui.Rect) []fragment {
	var out []fragment
	for _, p := range m.panes() {
		c := intersect(r, p.band)
		if c.Empty() {
			continue
		}
		out = append(out, fragment{cells: c, dev: m.cellRect(c.X, c.Y, c.W, c.H), clip: p.clip})
	}
	return out
}

// anchored maps a whole placement (a native view, an image) through the band of its top-left
// cell (in a hole: the first band it overlaps, in tree order). Inside one band it is clipped to
// that pane; crossing bands it is an overlay, clipped to the window inside its padding. false:
// it overlaps no band.
func (m metrics) anchored(r tui.Rect) (dev, clip image.Rectangle, ok bool) {
	if m.layout == nil {
		return m.cellRect(r.X, r.Y, r.W, r.H), image.Rectangle{Max: m.window}, true
	}
	var anchor *paneMap
	if p, in := m.pane(r.X, r.Y); in {
		anchor = &p
	} else {
		for i := range m.layout.panes {
			if !intersect(r, m.layout.panes[i].band).Empty() {
				anchor = &m.layout.panes[i]
				break
			}
		}
	}
	if anchor == nil {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	x0, y0 := (r.X-anchor.band.X)*m.cell.X, (r.Y-anchor.band.Y)*m.cell.Y
	dev = image.Rect(x0, y0, x0+r.W*m.cell.X, y0+r.H*m.cell.Y).Add(anchor.origin)
	if intersect(r, anchor.band) == r {
		return dev, anchor.clip, true
	}
	return dev, m.layout.inner, true
}

func intersect(a, b tui.Rect) tui.Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return tui.Rect{}
	}
	return tui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// sameCells reports whether another snapshot draws the cells identically: the window's size
// aside, every field equal, and the layouts placing every pane alike (their generations aside).
func (m metrics) sameCells(o metrics) bool {
	m.window, o.window = image.Point{}, image.Point{}
	ml, ol := m.layout, o.layout
	m.layout, o.layout = nil, nil
	return m == o && ml.sameShape(ol)
}
