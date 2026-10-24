package gui

import (
	"image"

	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"

	"github.com/yongjohnlee80/golib/tui"
)

// The pointer's shape over native views.
//
// A native view that implements [PointerShapeView] is asked for its shape after it paints, each
// frame, as a [CaretView] is asked for its caret. The window shows that shape while the pointer is
// over the part of the view that shows, and the default over a cell painted over it (a dialog) or a
// later view over it, so a covered view never shapes the pointer.

// PointerShape is a shape the window's pointer takes.
type PointerShape uint8

const (
	PointerDefault    PointerShape = iota // the platform's arrow
	PointerText                           // an I-beam, over text to select or type
	PointerHand                           // a pointing hand, over a link
	PointerCrosshair                      // a crosshair, for a precise point
	PointerMove                           // arrows in four directions, for something to move
	PointerResizeEW                       // a left-right arrow, for a width
	PointerResizeNS                       // an up-down arrow, for a height
	PointerNotAllowed                     // a struck circle, for what can't be done here
)

// gioCursors is each PointerShape's Gio cursor.
var gioCursors = [...]pointer.Cursor{
	PointerDefault:    pointer.CursorDefault,
	PointerText:       pointer.CursorText,
	PointerHand:       pointer.CursorPointer,
	PointerCrosshair:  pointer.CursorCrosshair,
	PointerMove:       pointer.CursorAllScroll,
	PointerResizeEW:   pointer.CursorEastWestResize,
	PointerResizeNS:   pointer.CursorNorthSouthResize,
	PointerNotAllowed: pointer.CursorNotAllowed,
}

// cursor is s's Gio cursor; the default for a shape it doesn't know.
func (s PointerShape) cursor() pointer.Cursor {
	if int(s) < len(gioCursors) {
		return gioCursors[s]
	}
	return pointer.CursorDefault
}

// PointerShapeView is implemented by a [View] that wants a pointer shape over its rect.
type PointerShapeView interface {
	PointerShape() PointerShape
}

// cursorArea is a rect of the window, in device pixels, and the cursor over it.
type cursorArea struct {
	rect   image.Rectangle
	cursor pointer.Cursor
}

// shapedCell is one cell's shape, and the placement it is from (its index plus one; 0: none).
type shapedCell struct {
	shape PointerShape
	from  int
}

// viewCursors is the frame's cursor areas: one per row run of a view's shaped cells, as they show
// after the views paint in order, each view's covered cells and a later view over it taking the
// shape away. The areas never overlap a cell they don't shape: Gio reads the default as no cursor
// and takes the next area's beneath, so the default can't be put over a shape, only left out.
func viewCursors(dst []cursorArea, m metrics, natives []tui.NativePlacement) []cursorArea {
	dst = dst[:0]
	shapes := make([]PointerShape, len(natives))
	shapedAny := false
	for i, p := range natives {
		if sv, ok := p.View.(PointerShapeView); ok && sv.PointerShape().cursor() != pointer.CursorDefault {
			shapes[i], shapedAny = sv.PointerShape(), true
		}
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
	for i, p := range natives {
		set(tui.Rect{X: p.X, Y: p.Y, W: p.Cols, H: p.Rows}, shapedCell{shapes[i], i + 1})
		for _, c := range p.Covered {
			set(c, shapedCell{})
		}
	}
	for y := range h {
		for x := 0; x < w; {
			c := cells[y*w+x]
			if c.shape == PointerDefault {
				x++
				continue
			}
			x0 := x
			for x < w && cells[y*w+x] == c {
				x++
			}
			dst = appendCursorArea(dst, m, tui.Rect{X: x0, Y: y, W: x - x0, H: 1}, c.shape.cursor())
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
