package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/op"

	"github.com/yongjohnlee80/golib/tui"
)

// shapeView is a view that shapes the pointer.
type shapeView struct {
	ViewFunc
	shape tui.PointerShape
}

func (v shapeView) PointerShape() tui.PointerShape { return v.shape }

func shaped(s tui.PointerShape) shapeView { return shapeView{ViewFunc: func(Canvas) {}, shape: s} }

// centre is cell (x, y)'s middle, in device pixels.
func centre(m metrics, x, y int) f32.Point {
	r := m.cellRect(x, y, 1, 1)
	return f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
}

// A native view's pointer shape shows over the part of it that shows, and the default over a cell
// painted over it and off it; the shapes sit over the window's input area, which still takes the
// pointer's events.
func TestAViewShapesThePointerOverItsCells(t *testing.T) {
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), scale: 1, grid: tui.Size{W: 8, H: 4}}
	b := NewBackend()
	natives := []tui.NativePlacement{{View: shaped(tui.PointerText), Cols: 4, Rows: 2, Covered: []tui.Rect{{X: 1, Y: 0, W: 1, H: 1}}}}
	b.latest = &frame{m: m, cursors: viewCursors(nil, m, natives, nil)}
	router := new(input.Router)
	b.readInput(router.Source(), &b.gio, m) // the input area's filters
	b.present(app.FrameEvent{Size: m.window, Source: router.Source(), Frame: func(o *op.Ops) { router.Frame(o) }}, &b.gio, m, b.font.load())
	over := func(x, y int) pointer.Cursor {
		router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: centre(m, x, y)})
		b.readInput(router.Source(), &b.gio, m) // the window takes the move, as its frame loop does
		return router.Cursor()
	}
	for _, c := range []struct {
		x, y int
		want pointer.Cursor
	}{{0, 0, pointer.CursorText}, {3, 1, pointer.CursorText}, {1, 0, pointer.CursorDefault}, {5, 3, pointer.CursorDefault}, {2, 1, pointer.CursorText}} {
		if got := over(c.x, c.y); got != c.want {
			t.Errorf("over cell (%d, %d): %v, want %v", c.x, c.y, got, c.want)
		}
	}
	if n := len(queuedMouse(b)); n == 0 {
		t.Error("the input area took none of the pointer's moves under the shapes")
	}
}

// The frame's cursor areas follow paint order: a view with no shape over a shaped one takes the
// shape from its cells, a lone view with none adds nothing, a shape gui doesn't know is none, and
// each row of a view's shaped cells is one area.
func TestAViewsCursorAreasFollowPaintOrder(t *testing.T) {
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), scale: 1, grid: tui.Size{W: 8, H: 4}}
	plain := ViewFunc(func(Canvas) {})
	if got := viewCursors(nil, m, []tui.NativePlacement{{View: plain, Cols: 4, Rows: 2}, {View: shaped(tui.PointerShape(99)), X: 6, Cols: 1, Rows: 1}}, nil); len(got) != 0 {
		t.Errorf("views with no shape gui knows: %+v", got)
	}
	got := viewCursors(nil, m, []tui.NativePlacement{
		{View: shaped(tui.PointerCrosshair), Cols: 4, Rows: 2},
		{View: plain, X: 2, Cols: 2, Rows: 1},
		{View: shaped(tui.PointerHand), X: 6, Cols: 2, Rows: 1},
	}, nil)
	want := []cursorArea{
		{m.cellRect(0, 0, 2, 1), pointer.CursorCrosshair},
		{m.cellRect(6, 0, 2, 1), pointer.CursorPointer},
		{m.cellRect(0, 1, 4, 1), pointer.CursorCrosshair},
	}
	if len(got) != len(want) {
		t.Fatalf("areas %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("area %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	for s := tui.PointerDefault; s <= tui.PointerResizeNESW; s++ {
		if s != tui.PointerDefault && gioCursor(s) == pointer.CursorDefault {
			t.Errorf("shape %d has no cursor of its own", s)
		}
	}
}

// Cell regions and native views shape the pointer in paint order: a region over a view's cells
// takes them, a view painted over a region takes the region's, and a region alone is its runs.
func TestCellRegionsAndViewsShapeThePointerInPaintOrder(t *testing.T) {
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), scale: 1, grid: tui.Size{W: 8, H: 4}}
	got := viewCursors(nil, m,
		[]tui.NativePlacement{{View: shaped(tui.PointerCrosshair), Cols: 4, Rows: 1, Layer: 2}, {View: ViewFunc(func(Canvas) {}), X: 4, Y: 2, Cols: 2, Rows: 1, Layer: 5}},
		[]tui.PointerShapePlacement{
			{X: 1, Y: 0, W: 2, Shape: tui.PointerText, Layer: 3}, // over the crosshair view
			{X: 3, Y: 2, W: 4, Shape: tui.PointerHand, Layer: 4}, // under the plain view at 4..6
			{X: 0, Y: 3, W: 3, Shape: tui.PointerResizeEW, Layer: 1},
		})
	want := []cursorArea{
		{m.cellRect(0, 0, 1, 1), pointer.CursorCrosshair},
		{m.cellRect(1, 0, 2, 1), pointer.CursorText},
		{m.cellRect(3, 0, 1, 1), pointer.CursorCrosshair},
		{m.cellRect(3, 2, 1, 1), pointer.CursorPointer},
		{m.cellRect(6, 2, 1, 1), pointer.CursorPointer},
		{m.cellRect(0, 3, 3, 1), pointer.CursorEastWestResize},
	}
	if len(got) != len(want) {
		t.Fatalf("areas %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("area %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}
