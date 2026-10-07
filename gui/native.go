package gui

import (
	"cmp"
	"slices"

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

// placement is one thing composited over the rows: a native view or an image.
type placement struct {
	layer   uint32
	native  *tui.NativePlacement // one of native and image is set
	image   *placedImage
	covered []tui.Rect
}

// compositeOrder is the frame's native views and images in tui's paint order. Each has its
// Layer; on a tie (one node reporting both) the native view is first, as it is noted before the
// image. The sort is stable, so equal layers keep their order.
func compositeOrder(natives []tui.NativePlacement, images []placedImage) []placement {
	out := make([]placement, 0, len(natives)+len(images))
	for i := range natives {
		out = append(out, placement{layer: natives[i].Layer, native: &natives[i], covered: natives[i].Covered})
	}
	for i := range images {
		out = append(out, placement{layer: images[i].Layer, image: &images[i], covered: images[i].Covered})
	}
	slices.SortStableFunc(out, func(a, b placement) int {
		switch {
		case a.layer != b.layer:
			return cmp.Compare(a.layer, b.layer)
		case a.native != nil && b.native == nil:
			return -1
		case a.native == nil && b.native != nil:
			return 1
		}
		return 0
	})
	return out
}

// composite draws the frame's native views and images over the rows, in paint order. Each is
// drawn over its cells, then the cells written after it (its Covered) are drawn over it, so a
// dialog covers a native button or an image beneath it, and a later view or image covers an
// earlier one.
func (r *renderer) composite(ops *op.Ops, g *grid, m metrics, natives []tui.NativePlacement, images []placedImage) {
	for _, p := range compositeOrder(natives, images) {
		switch {
		case p.native != nil:
			v, ok := p.native.View.(View)
			if !ok || p.native.Cols <= 0 || p.native.Rows <= 0 {
				continue
			}
			v.Paint(newGioCanvas(ops, r, g, m, CellRect{X: p.native.X, Y: p.native.Y, W: p.native.Cols, H: p.native.Rows}))
		case p.image != nil:
			r.drawImage(ops, *p.image)
		}
		for _, c := range p.covered {
			r.paintCells(ops, g, CellRect{X: c.X, Y: c.Y, W: c.W, H: c.H}, false)
		}
	}
}
