package gui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/paint"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// fillRectCanvas is a canvas over a 20x2 grid of 10x22 cells at scale, on ops.
func fillRectCanvas(ops *op.Ops, scale float32) (*gioCanvas, metrics) {
	m := metrics{window: image.Pt(200, 44), cell: image.Pt(10, 22), ppem: 16, baseline: 16, scale: scale, grid: tui.Size{W: 20, H: 2}}
	g := &grid{}
	g.resize(20, 2)
	r := newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	r.m = m
	return newGioCanvas(ops, r, g, m, CellRect{W: 20, H: 2}, tui.Rect{}, nil), m
}

// TestARectOnWholePixelsTakesTheFastPath: at scale 1 an integral rect maps onto whole pixels; at
// 1.25 a rect at x=1 doesn't (1.25 px), and a rotation never does.
func TestARectOnWholePixelsTakesTheFastPath(t *testing.T) {
	c, _ := fillRectCanvas(new(op.Ops), 1)
	ox, oy := int(c.origin.X), int(c.origin.Y)
	if got, ok := c.pixelRect(Rect{X: 2, Y: 3, W: 10, H: 5}); !ok || got != image.Rect(ox+2, oy+3, ox+12, oy+8) {
		t.Fatalf("integral rect at scale 1: %v, %v; want %v", got, ok, image.Rect(ox+2, oy+3, ox+12, oy+8))
	}
	c125, _ := fillRectCanvas(new(op.Ops), 1.25)
	if _, ok := c125.pixelRect(Rect{X: 1, Y: 0, W: 4, H: 4}); ok {
		t.Fatal("a rect at 1.25 px took the fast path: its edge would lose its antialiasing")
	}
	c.xform = f32.Affine2D{}.Rotate(f32.Point{}, 0.3)
	if _, ok := c.pixelRect(Rect{X: 2, Y: 3, W: 10, H: 5}); ok {
		t.Fatal("a rotated rect took the fast path")
	}
	if _, ok := c.pixelRect(Rect{X: 2, Y: 3, W: 0, H: 5}); ok {
		t.Fatal("an empty rect took the fast path")
	}
}

// TestTheFastPathFillsThePathsPixels: a solid fill, a faded one and a gradient, on whole pixels,
// fill exactly the pixels the rectangle's path filled. Rendered on gio's headless GPU.
func TestTheFastPathFillsThePathsPixels(t *testing.T) {
	hw, err := headless.NewWindow(200, 44)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	shot := func(draw func(c *gioCanvas)) *image.RGBA {
		ops := new(op.Ops)
		paint.Fill(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		c, _ := fillRectCanvas(ops, 1)
		draw(c)
		if err := hw.Frame(ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 200, 44))
		if err := hw.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	r := Rect{X: 13, Y: 7, W: 61, H: 19}
	grad := &LinearGradient{From: Pt(13, 0), To: Pt(74, 0), FromColor: color.NRGBA{R: 0xd0, A: 0xff}, ToColor: color.NRGBA{B: 0xd0, A: 0xff}}
	for name, tc := range map[string]struct {
		b     Brush
		alpha float32
	}{
		"solid":    {Solid(color.NRGBA{G: 0x80, A: 0xff}), 1},
		"faded":    {Solid(color.NRGBA{R: 0x40, B: 0x90, A: 0xff}), 0.5},
		"gradient": {Brush{Gradient: grad}, 1},
	} {
		fast := shot(func(c *gioCanvas) { c.alpha = tc.alpha; c.FillRect(r, tc.b) })
		path := shot(func(c *gioCanvas) { c.alpha = tc.alpha; c.FillPath(rrectPath(r, 0), tc.b) })
		differ, worst, inked := 0, 0, 0
		for i := range fast.Pix {
			d := int(fast.Pix[i]) - int(path.Pix[i])
			if d != 0 {
				differ++
				worst = max(worst, int(math.Abs(float64(d))))
			}
			if path.Pix[i] != 0xff {
				inked++
			}
		}
		if inked == 0 {
			t.Fatalf("%s: the path drew nothing: the comparison proves nothing", name)
		}
		if differ != 0 {
			t.Errorf("%s: %d bytes differ from the path's, the worst by %d", name, differ, worst)
		}
	}
}
