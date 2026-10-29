package gui

import (
	"cmp"
	"slices"

	"github.com/yongjohnlee80/gio/op"

	"github.com/yongjohnlee80/golib/tui"
)

var (
	_ tui.NativeHost   = (*Backend)(nil)
	_ tui.FaceHost     = (*Backend)(nil)
	_ tui.NativeStyler = (*Backend)(nil)
	_ tui.CellMetrics  = (*Backend)(nil)
	_ tui.SizedFlusher = (*Backend)(nil)
)

// Compositing Order and Layer Stacking Architecture:
//
//	+--------------------------------------------------------+
//	| Layer 0: Background cell grid and standard text glyphs |
//	+--------------------------------------------------------+
//	                           |
//	                           v
//	+--------------------------------------------------------+
//	| Layer N: Native vector views and Kitty PNG bitmaps     |
//	| (Sorted by Layer ID; native views precede image ties)  |
//	+--------------------------------------------------------+
//	                           |
//	                           v
//	+--------------------------------------------------------+
//	| Layer N+1: Covered cells pass (modal cards, dialogs,  |
//	|            popups placed over lower views/images)     |
//	+--------------------------------------------------------+
//
// PlaceNatives records the set of native vector views for the current frame.
// During the subsequent [Backend.Flush], these views are composited above background
// terminal cells at their designated cell coordinates.
func (b *Backend) PlaceNatives(ps []tui.NativePlacement) {
	b.natives = append(b.natives[:0], ps...)
}

// NativeFor queries the configured [Style] registry to check whether component c has an
// associated native vector [Painter]. If found, it executes the painter and returns the
// [View] interface, visual scope, and an acceptance flag.
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

// CellPixels reports the width and height of a single terminal monospace cell in logical
// device-independent pixels (dp), calculated from the window's latest metrics snapshot.
//
// A Panes window reports the admitted snapshot's cell: the one the tree is laid out, painted and
// (with pointer resolution at dispatch) routed with, so a component reconstructing pixels from an
// event's cells uses the same cell size they were counted in. It is read on
// the App goroutine, which owns the admitted snapshot.
func (b *Backend) CellPixels() tui.CellPixels {
	m := b.metrics.Load()
	if b.cfg.stage != nil && b.stage.admitted.layout != nil {
		m = &b.stage.admitted
	}
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
			countPaint()
			v.Paint(newGioCanvas(ops, r, g, m, CellRect{X: p.native.X, Y: p.native.Y, W: p.native.Cols, H: p.native.Rows}, p.native.Full, p.covered))
		case p.image != nil:
			r.drawImage(ops, *p.image)
		}
		for _, c := range p.covered {
			r.coveredCells(ops, g, CellRect{X: c.X, Y: c.Y, W: c.W, H: c.H})
		}
	}
}

// coveredRecording is the cells of one covered rect, recorded once: gens are its rows' rowGen
// when it was drawn, and used the last frame that replayed it.
type coveredRecording struct {
	gens []uint64
	call op.CallOp
	cost int
	used uint64
}

// coveredCells draws the cells in cr over a native view or an image, as paintCells does, from a
// recording kept while none of cr's rows was re-recorded. Every frame draws these cells again
// over the views beneath them, so a frame that changed nothing under them (a plugin's surface
// animating beside a dialog, say) replays the recording: golib draws no span again, and gio finds
// the same ops and paths. A row re-recorded since, or new metrics or a new layout (which re-record
// every row), make a new recording. Without frameParts's row gens (a renderer used directly), it
// draws the cells as paintCells does.
func (r *renderer) coveredCells(ops *op.Ops, g *grid, cr CellRect) {
	y0, y1 := max(cr.Y, 0), min(cr.Y+cr.H, g.h)
	if len(r.rowGen) != g.h || y0 >= y1 {
		r.paintCells(ops, g, cr, false)
		return
	}
	gens := r.rowGen[y0:y1]
	e := r.covered[cr]
	if e == nil || !slices.Equal(e.gens, gens) {
		rops := new(op.Ops)
		rec := op.Record(rops)
		start := walkNow()
		r.paintCells(rops, g, cr, false)
		e = &coveredRecording{gens: slices.Clone(gens), call: rec.Stop(), cost: walkNow() - start}
		if r.covered == nil {
			r.covered = map[CellRect]*coveredRecording{}
		}
		r.covered[cr] = e
	} else {
		countReplay(e.cost)
	}
	e.used = r.frameNo
	e.call.Add(ops)
}

// dropUnusedCovered forgets the covered recordings this frame didn't replay: their dialog moved or
// closed, and a recording kept for a rect no longer drawn would only hold its ops.
func (r *renderer) dropUnusedCovered() {
	for k, e := range r.covered {
		if e.used != r.frameNo {
			delete(r.covered, k)
		}
	}
}
