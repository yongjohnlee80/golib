package widget

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// Natively, on a GPU, a shape is filled and stroked where its geometry says, the stroke over the
// fill: inside a 40x24 dp view at scale 2, with a red fill and a 2 dp blue stroke.
func TestAShapesPixelsAreItsGeometry(t *testing.T) {
	red, blue := color.NRGBA{R: 0xc0, A: 0xff}, color.NRGBA{B: 0xc0, A: 0xff}
	size := gui.Size{W: 40, H: 24}
	ground, err := gui.Snapshot(gui.ViewFunc(func(gui.Canvas) {}), size, 2)
	if errors.Is(err, gui.ErrNoGPU) {
		t.Skipf("no headless GPU here: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	bg := ground.RGBAAt(2, 2)
	type point struct {
		x, y float32 // dp
		want string  // "fill", "stroke" or "ground"
	}
	for _, c := range []struct {
		name   string
		s      *Shape
		points []point
	}{
		{"rect", NewShape(ShapeRect, WithFillColor(red), WithStroke(blue, 2)),
			[]point{{20, 12, "fill"}, {1, 12, "stroke"}, {0.75, 0.75, "stroke"}, {3.5, 3.5, "fill"}}},
		{"rounded", NewShape(ShapeRect, WithFillColor(red), WithStroke(blue, 2), WithRadius(8)),
			[]point{{20, 12, "fill"}, {1, 12, "stroke"}, {0.75, 0.75, "ground"}, {20, 23, "stroke"}}},
		{"ellipse", NewShape(ShapeEllipse, WithFillColor(red), WithStroke(blue, 2)),
			[]point{{20, 12, "fill"}, {1, 12, "stroke"}, {20, 1, "stroke"}, {3, 3, "ground"}, {37, 21, "ground"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			img, err := gui.Snapshot(c.s, size, 2)
			if err != nil {
				t.Fatal(err) // a GPU answered for the ground: any error here is a failure
			}
			if img.Bounds() != image.Rect(0, 0, 80, 48) {
				t.Fatalf("snapshot %v, want 80x48", img.Bounds())
			}
			for _, p := range c.points {
				px := img.RGBAAt(int(p.x*2), int(p.y*2))
				ok := false
				switch p.want {
				case "fill":
					ok = px.R >= 0xa0 && px.B <= 0x20
				case "stroke":
					ok = px.B >= 0xa0 && px.R <= 0x20 // over the fill, not blended with it
				case "ground":
					d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
					// not inked: within 16 levels, past the antialiasing a path's edge spills
					ok = d(px.R, bg.R) <= 16 && d(px.G, bg.G) <= 16 && d(px.B, bg.B) <= 16 && d(px.A, bg.A) <= 16
				}
				if !ok {
					t.Errorf("%s at (%v,%v) dp is %v; want the %s (ground %v)", c.name, p.x, p.y, px, p.want, bg)
				}
			}
		})
	}
}
