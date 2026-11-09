package gui

import (
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/gpu/headless"
)

// A gradient's stops are in dp, as every Canvas coordinate is: at scale 2 it spans twice the
// device pixels, and in a view placed off the window's origin it moves with the view, so each
// logical point of the view has the same colour. Checked on every fill route: a rectangle on
// whole pixels (FillRect's clip), a path (FillRRect), and text (DrawText, whose glyphs sit under
// transforms of their own), at scale 1 and 2, with the view at the window's origin and 2 cells in.
func TestAGradientsStopsAreInDpAtEveryScale(t *testing.T) {
	g := &LinearGradient{From: Pt(8, 0), To: Pt(72, 0), FromColor: color.NRGBA{R: 0xff, A: 0xff}, ToColor: color.NRGBA{B: 0xff, A: 0xff}}
	rect := func(c Canvas) { c.FillRect(Rect{X: 8, Y: 8, W: 64, H: 48}, Brush{Gradient: g}) }
	path := func(c Canvas) { c.FillRRect(Rect{X: 8, Y: 8, W: 64, H: 48}, 4, Brush{Gradient: g}) }
	text := func(c Canvas) {
		c.DrawText(c.Text().Layout("MMMMMM", Font{Family: CellFamily, Size: 22, Bold: true}, 0), Pt(4, 12), Brush{Gradient: g})
	}
	type key struct{ s, col int }
	shots := map[string]map[key]*image.RGBA{"rect": {}, "path": {}, "text": {}}
	for _, s := range []int{1, 2} {
		hw, err := headless.NewWindow(96*s, 64*s)
		if err != nil {
			t.Skipf("no headless GPU here: %v", err)
		}
		for _, col := range []int{0, 2} {
			shots["rect"][key{s, col}] = goldenShotAt(t, hw, rect, s, col)
			shots["path"][key{s, col}] = goldenShotAt(t, hw, path, s, col)
			shots["text"][key{s, col}] = goldenShotAt(t, hw, text, s, col)
		}
		hw.Release()
	}
	// red is the red byte at the view's logical point (x, 32) in shot k (a view 2 cells in starts
	// 16 dp right).
	red := func(img *image.RGBA, k key, x int) uint8 { return img.RGBAAt((x+8*k.col)*k.s, 32*k.s).R }
	ref := key{1, 0}
	for _, route := range []string{"rect", "path"} {
		for k, img := range shots[route] {
			for _, x := range []int{20, 40, 60} {
				want, got := red(shots[route][ref], ref, x), red(img, k, x)
				if d := int(got) - int(want); d < -4 || d > 4 {
					t.Errorf("%s at %dx, view %d cells in: red %d at logical x %d; want %d, as at 1x at the origin", route, k.s, k.col, got, x, want)
				}
			}
		}
	}
	// Text: every glyph pixel wholly covered (its green 0: the gradient has none, the ground is
	// white) has the rectangle's colour at that pixel of the same shot.
	for k, img := range shots["text"] {
		under, n := shots["rect"][k], 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				p, q := img.RGBAAt(x, y), under.RGBAAt(x, y)
				if p.G != 0 || q.G != 0 {
					continue
				}
				n++
				if d := int(p.R) - int(q.R); d < -4 || d > 4 {
					t.Errorf("text at %dx, view %d cells in: (%d,%d) is %v; want the gradient's %v there", k.s, k.col, x, y, p, q)
					return
				}
			}
		}
		if n < 50 {
			t.Errorf("text at %dx, view %d cells in: %d wholly covered glyph pixels; want enough to compare", k.s, k.col, n)
		}
	}
}

// Under an uneven scale or a shear a gradient keeps each point's colour: the point's image has the
// colour the point has without the transform. A 40x40 dp gradient, red at (0,0) to blue at (40,40),
// sampled at the image of (30,10) under scale (2,1) and under a shear, against no transform.
func TestAGradientKeepsItsColoursUnderAnUnevenScaleAndAShear(t *testing.T) {
	g := &LinearGradient{From: Pt(0, 0), To: Pt(40, 40), FromColor: color.NRGBA{R: 0xff, A: 0xff}, ToColor: color.NRGBA{B: 0xff, A: 0xff}}
	hw, err := headless.NewWindow(96, 64)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	shot := func(m Affine) *image.RGBA {
		return goldenShot(t, hw, func(c Canvas) {
			c.PushTransform(m)
			c.FillPath(new(Path).MoveTo(Pt(0, 0)).LineTo(Pt(40, 0)).LineTo(Pt(40, 40)).LineTo(Pt(0, 40)).Close(), Brush{Gradient: g})
			c.Pop()
		}, 1)
	}
	p := Pt(30, 10)
	base := shot(Affine{}).RGBAAt(30, 10)
	for _, c := range []struct {
		name string
		m    Affine
	}{
		{"scale (2,1)", Affine{}.Scale(f32.Point{}, f32.Pt(2, 1))},
		{"shear", f32.NewAffine2D(1, 0.5, 0, 0, 1, 0)},
		{"scale (1,1.5) and a rotation", Affine{}.Scale(f32.Point{}, f32.Pt(1, 1.5)).Rotate(f32.Pt(20, 20), 0.3)},
	} {
		q := c.m.Transform(f32.Pt(p.X, p.Y))
		got := shot(c.m).RGBAAt(int(q.X), int(q.Y))
		d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
		if d(got.R, base.R) > 4 || d(got.B, base.B) > 4 {
			t.Errorf("%s: (30,10) maps to (%.0f,%.0f), coloured %v; want the untransformed %v", c.name, q.X, q.Y, got, base)
		}
	}
}
