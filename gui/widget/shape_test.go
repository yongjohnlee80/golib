package widget

import (
	"context"
	"image/color"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
)

// shapeScreen runs s as the root of a w x h terminal TestBackend and returns the screen once it
// equals want, or what it last showed.
func shapeScreen(t *testing.T, s *Shape, w, h int, want string) string {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(s, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)
	var got string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if got = tb.String(); tb.Flushes() > 0 && got == want {
			return got
		}
	}
	return got
}

// On a terminal a shape is its outline: square corners for a plain rectangle, rounded ones for a
// rounded rectangle and an ellipse; a shape a row or a column thin is only its fill.
func TestAShapeOnATerminalIsItsOutline(t *testing.T) {
	for _, c := range []struct {
		name string
		s    *Shape
		w, h int
		want string
	}{
		{"rect", NewShape(ShapeRect), 6, 3, "┌────┐\n│    │\n└────┘"},
		{"rounded", NewShape(ShapeRect, WithRadius(6)), 6, 3, "╭────╮\n│    │\n╰────╯"},
		{"ellipse", NewShape(ShapeEllipse), 5, 4, "╭───╮\n│   │\n│   │\n╰───╯"},
		{"one row", NewShape(ShapeRect), 4, 1, "    "},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := shapeScreen(t, c.s, c.w, c.h, c.want); got != c.want {
				t.Fatalf("the screen is\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// Natively a shape fills and strokes its rectangle, the stroke inside it; a transparent fill or a
// zero-width stroke draws nothing.
func TestAShapePaintsItsFillAndStroke(t *testing.T) {
	red, blue := color.NRGBA{R: 0xc0, A: 0xff}, color.NRGBA{B: 0xc0, A: 0xff}
	full, inset := gui.Rect{W: 80, H: 48}, gui.Rect{X: 1, Y: 1, W: 78, H: 46}
	type call struct {
		op     string
		rect   gui.Rect
		radius float32
		width  float32
		color  color.NRGBA
	}
	for _, c := range []struct {
		name string
		s    *Shape
		want []call
	}{
		{"fill", NewShape(ShapeRect, WithFillColor(red)), []call{{"FillRect", full, 0, 0, red}}},
		{"rounded fill and stroke", NewShape(ShapeRect, WithFillColor(red), WithRadius(6), WithStroke(blue, 2)),
			[]call{{"FillRRect", inset, 6, 0, red}, {"StrokeRRect", inset, 6, 2, blue}}},
		{"ellipse", NewShape(ShapeEllipse, WithFillColor(red), WithStroke(blue, 2)),
			[]call{{"FillEllipse", inset, 0, 0, red}, {"StrokePath", inset, 0, 2, blue}}},
		{"nothing", NewShape(ShapeRect, WithStroke(blue, 0)), nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			rc := gui.NewRecordingCanvas(gui.Size{W: 80, H: 48}, gui.Size{W: 8, H: 16})
			c.s.Paint(rc)
			if len(rc.Calls) != len(c.want) {
				t.Fatalf("painted %+v; want %+v", rc.Calls, c.want)
			}
			for i, w := range c.want {
				g := rc.Calls[i]
				if g.Op != w.op || g.Rect != w.rect || g.Radius != w.radius || g.Width != w.width || g.Brush.Color != w.color {
					t.Errorf("call %d: %s %+v r%v w%v %v; want %+v", i, g.Op, g.Rect, g.Radius, g.Width, g.Brush.Color, w)
				}
			}
		})
	}
}

// A shape asks for its size, or takes all the room it is given.
func TestAShapeTakesItsSizeOrTheRoom(t *testing.T) {
	sized := NewShape(ShapeRect, WithShapeSize(4, 2))
	if got := sized.Layout(tui.Constraints{MaxW: 20, MaxH: 10}); got != (tui.Size{W: 4, H: 2}) {
		t.Errorf("sized shape laid out %v, want 4x2", got)
	}
	if got := NewShape(ShapeRect).Layout(tui.Constraints{MaxW: 20, MaxH: 10}); got != (tui.Size{W: 20, H: 10}) {
		t.Errorf("unsized shape laid out %v, want the room 20x10", got)
	}
	if got := NewShape(ShapeRect).Layout(tui.Constraints{MaxW: tui.Unbounded, MaxH: 3}); got != (tui.Size{W: 0, H: 3}) {
		t.Errorf("unsized shape on an unbounded axis laid out %v, want 0x3", got)
	}
}
