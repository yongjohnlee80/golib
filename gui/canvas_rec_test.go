package gui

import (
	"image/color"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
)

// A recording keeps each call's Rect as the view gave it, and records where a window draws it
// (Drawn: the pushed transform, then the sub-canvas offset) and at what opacity (Alpha); a path
// fill records the path's bounds, as a stroke does.
func TestARecordingRecordsWhereAWindowDraws(t *testing.T) {
	rc := NewRecordingCanvas(Size{W: 200, H: 100}, Size{W: 8, H: 16})
	sub := rc.Sub(Rect{X: 10, Y: 20, W: 100, H: 50})
	sub.PushOpacity(0.5)
	sub.PushTransform(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(2, 2)))
	sub.FillRect(Rect{X: 1, Y: 2, W: 3, H: 4}, Solid(color.NRGBA{A: 0xff}))
	sub.FillPath(new(Path).Polyline([]Point{{X: 0, Y: 0}, {X: 5, Y: 5}}, false), Solid(color.NRGBA{A: 0xff}))
	sub.Pop()
	sub.FillRect(Rect{X: 1, Y: 2, W: 3, H: 4}, Solid(color.NRGBA{A: 0xff}))
	sub.Pop()
	sub.FillRect(Rect{X: 1, Y: 2, W: 3, H: 4}, Solid(color.NRGBA{A: 0xff}))

	var fills []PaintCall
	for _, c := range rc.Calls {
		if c.Op == "FillRect" || c.Op == "FillPath" {
			fills = append(fills, c)
		}
	}
	if len(fills) != 4 {
		t.Fatalf("%d fills recorded, want 4: %+v", len(fills), rc.Calls)
	}
	want := []struct {
		rect, drawn Rect
		alpha       float32
	}{
		{Rect{X: 11, Y: 22, W: 3, H: 4}, Rect{X: 12, Y: 24, W: 6, H: 8}, 0.5},   // scaled 2x, then offset
		{Rect{X: 10, Y: 20, W: 5, H: 5}, Rect{X: 10, Y: 20, W: 10, H: 10}, 0.5}, // the path's bounds
		{Rect{X: 11, Y: 22, W: 3, H: 4}, Rect{X: 11, Y: 22, W: 3, H: 4}, 0.5},   // the scale popped
		{Rect{X: 11, Y: 22, W: 3, H: 4}, Rect{X: 11, Y: 22, W: 3, H: 4}, 1},     // the opacity popped
	}
	for i, w := range want {
		if f := fills[i]; f.Rect != w.rect || f.Drawn != w.drawn || f.Alpha != w.alpha {
			t.Errorf("fill %d: Rect %+v Drawn %+v Alpha %v; want %+v %+v %v", i, f.Rect, f.Drawn, f.Alpha, w.rect, w.drawn, w.alpha)
		}
	}
}
