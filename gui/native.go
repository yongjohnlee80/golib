package gui

import (
	"gioui.org/op"

	"github.com/yongjohnlee80/golib/tui"
)

var (
	_ tui.NativeHost   = (*Backend)(nil)
	_ tui.NativeStyler = (*Backend)(nil)
	_ tui.CellMetrics  = (*Backend)(nil)
)

// PlaceNatives takes the frame's native views, drawn by the Flush that follows, over the cells.
func (b *Backend) PlaceNatives(ps []tui.NativePlacement) {
	b.natives = append(b.natives[:0], ps...)
}

// NativeFor offers c to the style.
func (b *Backend) NativeFor(c tui.Component) (any, tui.NativeScope, bool) {
	p := b.cfg.style.painterFor(c)
	if p == nil {
		return nil, 0, false
	}
	v, scope, ok := p(c)
	if !ok || v == nil {
		return nil, 0, false
	}
	return v, scope, true
}

// CellPixels is one cell in logical pixels, from the window's latest metrics.
func (b *Backend) CellPixels() tui.CellPixels {
	m := b.metrics.Load()
	if m == nil || m.scale <= 0 {
		return tui.CellPixels{}
	}
	return tui.CellPixels{W: float32(m.cell.X) / m.scale, H: float32(m.cell.Y) / m.scale}
}

// drawNatives composites the frame's native views over the rows, in paint order. Each view is
// drawn over its cells, then the cells written after it (its Covered) are drawn over the view,
// so a dialog opened over a native button covers it, and a later view covers an earlier one.
func (r *renderer) drawNatives(ops *op.Ops, g *grid, m metrics, ps []tui.NativePlacement) {
	for _, p := range ps {
		v, ok := p.View.(View)
		if !ok || p.Cols <= 0 || p.Rows <= 0 {
			continue
		}
		v.Paint(newGioCanvas(ops, r, g, m, CellRect{X: p.X, Y: p.Y, W: p.Cols, H: p.Rows}))
		for _, c := range p.Covered {
			r.paintCells(ops, g, CellRect{X: c.X, Y: c.Y, W: c.W, H: c.H}, false)
		}
	}
}
