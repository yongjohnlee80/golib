package gui

import (
	"image"

	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"

	"github.com/yongjohnlee80/golib/tui"
)

// The pointer's shape, over native views and cell regions.
//
// A native view that implements [PointerShapeView] is asked for its shape after it paints, each
// frame, as a [CaretView] is asked for its caret; one that implements [PointerRegionsView], for
// its regions, the same way. A cell-drawn component gives its regions as a
// [tui.PointerShaper], which the App cuts to what shows and hands over in PlacePointerShapes. The
// window shows a shape over the cells it is the last to have, in paint order, and the default over
// a cell painted over it (a dialog), so a covered view or region never shapes the pointer.

var _ tui.PointerShapeHost = (*Backend)(nil)

// gioCursors is each [tui.PointerShape]'s Gio cursor.
var gioCursors = [...]pointer.Cursor{
	tui.PointerDefault:    pointer.CursorDefault,
	tui.PointerText:       pointer.CursorText,
	tui.PointerHand:       pointer.CursorPointer,
	tui.PointerCrosshair:  pointer.CursorCrosshair,
	tui.PointerMove:       pointer.CursorAllScroll,
	tui.PointerResizeEW:   pointer.CursorEastWestResize,
	tui.PointerResizeNS:   pointer.CursorNorthSouthResize,
	tui.PointerNotAllowed: pointer.CursorNotAllowed,
	tui.PointerResizeNWSE: pointer.CursorNorthWestSouthEastResize,
	tui.PointerResizeNESW: pointer.CursorNorthEastSouthWestResize,
}

// gioCursor is s's Gio cursor; the default for a shape it doesn't know.
func gioCursor(s tui.PointerShape) pointer.Cursor {
	if int(s) < len(gioCursors) {
		return gioCursors[s]
	}
	return pointer.CursorDefault
}

// PointerShapeView is implemented by a [View] that wants a pointer shape over its rect.
type PointerShapeView interface {
	PointerShape() tui.PointerShape
}

// PointerRegionsView is implemented by a [View] that shapes the pointer over parts of itself:
// PointerRegions appends its regions in its own cells, from the origin it paints at (its Full
// rect's, when it shows in part). It is asked after it paints, each frame, as a [CaretView] is, so
// the regions are the geometry it just painted with (a gutter it measured in pixels, the links
// it laid out), never the frame before's. The cells it gives no region take the default. A view
// that implements both this and [PointerShapeView] is shaped by its regions alone: its
// PointerShape is not asked.
type PointerRegionsView interface {
	PointerRegions(dst []tui.PointerRegion) []tui.PointerRegion
}

// PlacePointerShapes records the frame's cell-region shapes, for the frame Flush draws.
func (b *Backend) PlacePointerShapes(ps []tui.PointerShapePlacement) {
	b.shapes = append(b.shapes[:0], ps...)
}

// cursorArea is a rect of the window, in device pixels, and the cursor over it.
type cursorArea struct {
	rect   image.Rectangle
	cursor pointer.Cursor
}

// shapedCell is one cell's shape, and what it is from (a native placement's index plus one, or a
// run's, past the natives; 0: none), so a run of cells is one source's.
type shapedCell struct {
	shape tui.PointerShape
	from  int
}

// viewCursors is the frame's cursor areas: one per row run of one source's shaped cells, as they
// show once the natives and the cell regions are laid in paint order, each native's covered cells
// and anything later over it taking the shape away. The areas never overlap a cell they don't
// shape: Gio reads the default as no cursor and takes the next area's beneath, so the default
// can't be put over a shape, only left out.
func viewCursors(dst []cursorArea, m metrics, natives []tui.NativePlacement, shapes []tui.PointerShapePlacement) []cursorArea {
	dst = dst[:0]
	views := make([]tui.PointerShape, len(natives))
	regions := make([][]tui.PointerRegion, len(natives)) // a PointerRegionsView's, asked once
	shapedAny := false
	for i, p := range natives {
		if rv, ok := p.View.(PointerRegionsView); ok {
			regions[i] = rv.PointerRegions(nil)
			for _, r := range regions[i] {
				shapedAny = shapedAny || gioCursor(r.Shape) != pointer.CursorDefault
			}
		} else if sv, ok := p.View.(PointerShapeView); ok && gioCursor(sv.PointerShape()) != pointer.CursorDefault {
			views[i], shapedAny = sv.PointerShape(), true
		}
	}
	for _, s := range shapes {
		shapedAny = shapedAny || gioCursor(s.Shape) != pointer.CursorDefault
	}
	w, h := m.grid.W, m.grid.H
	if !shapedAny || w <= 0 || h <= 0 {
		return dst
	}
	cells := make([]shapedCell, w*h)
	set := func(r tui.Rect, c shapedCell) {
		for y := max(r.Y, 0); y < min(r.Y+r.H, h); y++ {
			for x := max(r.X, 0); x < min(r.X+r.W, w); x++ {
				cells[y*w+x] = c
			}
		}
	}
	// both lists are in paint order: the earlier layer first, a native before a region of its node
	for i, j := 0, 0; i < len(natives) || j < len(shapes); {
		if i < len(natives) && (j == len(shapes) || natives[i].Layer <= shapes[j].Layer) {
			p := natives[i]
			shown := tui.Rect{X: p.X, Y: p.Y, W: p.Cols, H: p.Rows}
			set(shown, shapedCell{views[i], i + 1})
			ox, oy := p.X, p.Y
			if !p.Full.Empty() {
				ox, oy = p.Full.X, p.Full.Y // the view paints from its whole rect's origin
			}
			for _, r := range regions[i] {
				shape := r.Shape
				if gioCursor(shape) == pointer.CursorDefault {
					shape = tui.PointerDefault
				}
				set(r.Rect.Translate(ox, oy).Intersect(shown), shapedCell{shape, i + 1})
			}
			for _, c := range p.Covered {
				set(c, shapedCell{})
			}
			i++
			continue
		}
		s := shapes[j]
		shape := s.Shape
		if gioCursor(shape) == pointer.CursorDefault {
			shape = tui.PointerDefault
		}
		set(tui.Rect{X: s.X, Y: s.Y, W: s.W, H: 1}, shapedCell{shape, len(natives) + j + 1})
		j++
	}
	for y := range h {
		for x := 0; x < w; {
			c := cells[y*w+x]
			if c.shape == tui.PointerDefault {
				x++
				continue
			}
			x0 := x
			for x < w && cells[y*w+x] == c {
				x++
			}
			dst = appendCursorArea(dst, m, tui.Rect{X: x0, Y: y, W: x - x0, H: 1}, gioCursor(c.shape))
		}
	}
	return dst
}

// appendCursorArea appends r's cells, as they show in their pane, with cursor c.
func appendCursorArea(dst []cursorArea, m metrics, r tui.Rect, c pointer.Cursor) []cursorArea {
	dev, within, in := m.anchored(r)
	if !in {
		return dst
	}
	if dev = dev.Intersect(within); dev.Empty() {
		return dst
	}
	return append(dst, cursorArea{rect: dev, cursor: c})
}

// addCursors puts the frame's cursor areas over the window, after its input area: Gio takes the
// cursor of the topmost area under the pointer, and a pass-through area sits above the input
// area without taking its events. Under it, the input area's default would hide every one.
func addCursors(ops *op.Ops, areas []cursorArea) {
	for _, a := range areas {
		st := clip.Rect(a.rect).Push(ops)
		countClip()
		a.cursor.Add(ops)
		st.Pop()
	}
}
