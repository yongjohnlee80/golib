package gui

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"

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

// paintedRegion is a view whose region is decided as it paints: an I-beam two cells wide from the
// column next holds when Paint runs.
type paintedRegion struct{ next, painted *int }

func (v paintedRegion) Paint(Canvas) { *v.painted = *v.next }

func (v paintedRegion) PointerRegions(dst []tui.PointerRegion) []tui.PointerRegion {
	if *v.painted < 0 {
		return dst
	}
	return append(dst, tui.PointerRegion{Rect: tui.Rect{X: *v.painted, W: 2, H: 1}, Shape: tui.PointerText})
}

// A native view's regions are the frame's own: asked after the frame paints it, through the real
// publish, on the first frame and on the one after its geometry moves, never the paint before's.
func TestANativeViewsRegionsAreTheFrameItPainted(t *testing.T) {
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	b.grid.resize(8, 4)
	next, painted := 3, -1
	b.PlaceNatives([]tui.NativePlacement{{View: paintedRegion{&next, &painted}, Cols: 8, Rows: 4}})
	frame := func() []cursorArea {
		b.publishFrame(m)
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.latest.cursors
	}
	if got := frame(); len(got) != 1 || got[0].rect != m.cellRect(3, 0, 2, 1) || got[0].cursor != pointer.CursorText {
		t.Fatalf("the first frame: %+v, want the I-beam from column 3, as it painted", got)
	}
	next = 5
	if got := frame(); len(got) != 1 || got[0].rect != m.cellRect(5, 0, 2, 1) {
		t.Fatalf("the frame after: %+v, want column 5, its own paint's, not the one before's 3", got)
	}
}

// While recording, a pointer-shape report has what it needs: each new frame's cursor areas, once for
// the frame however often it is presented, and each pointer event with its cell. The replay knows
// both, skips them, and the recording still validates.
func TestARecordingHoldsEachFramesCursorAreasAndThePointer(t *testing.T) {
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), scale: 1, grid: tui.Size{W: 8, H: 4}}
	ring := newRingRecorder(64)
	b := NewBackend(WithRecorder(ring))
	natives := []tui.NativePlacement{{View: shaped(tui.PointerText), Cols: 4, Rows: 2}}
	b.latest = &frame{m: m, cursors: viewCursors(nil, m, natives, nil)}
	router := new(input.Router)
	frameEvent := func() { // as the window's loop runs each frame: its input, then the newest frame
		b.readInput(router.Source(), &b.gio, m)
		b.present(app.FrameEvent{Size: m.window, Source: router.Source(), Frame: func(o *op.Ops) { router.Frame(o) }}, &b.gio, m, b.font.load())
	}
	frameEvent()
	frameEvent() // the same frame again: nothing new to record
	router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: centre(m, 1, 1)})
	b.readInput(router.Source(), &b.gio, m)

	recs := ring.records()
	var cursors, pointers []Record
	for _, r := range recs {
		switch {
		case r.Stage == StagePointer && r.Kind == "cursors":
			cursors = append(cursors, r)
		case r.Stage == StagePointer && r.Kind == "pointer" && r.Data["kind"] == pointer.Move.String():
			pointers = append(pointers, r)
		}
	}
	if len(cursors) != 1 {
		t.Fatalf("%d cursors records for one frame presented twice, want 1", len(cursors))
	}
	areas, _ := cursors[0].Data["areas"].([]map[string]any)
	want := []map[string]any{
		{"x0": 0, "y0": 0, "x1": 32, "y1": 16, "cursor": pointer.CursorText.String()},
		{"x0": 0, "y0": 16, "x1": 32, "y1": 32, "cursor": pointer.CursorText.String()},
	}
	if fmt.Sprint(areas) != fmt.Sprint(want) {
		t.Errorf("the frame's areas recorded as %v, want %v", areas, want)
	}
	if len(pointers) != 1 || pointers[0].Data["kind"] != pointer.Move.String() || pointers[0].Data["col"] != 1 || pointers[0].Data["row"] != 1 {
		t.Fatalf("the moves recorded as %v, want one, over cell (1, 1)", pointers)
	}
	var jsonl strings.Builder
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		jsonl.Write(append(line, '\n'))
	}
	decoded, err := decodeRecording([]byte(jsonl.String()))
	if err != nil {
		t.Fatalf("the recording does not decode: %v", err)
	}
	if err := validateTrace(decoded); err != nil {
		t.Fatalf("the recording does not validate: %v", err)
	}
}
