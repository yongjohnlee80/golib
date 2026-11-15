package gui

import (
	"errors"
	"image"
	"image/color"
	"testing"
)

// bakeOrSkip is pb's bake, the test skipped where there is no headless GPU.
func bakeOrSkip(t *testing.T, pb *PictureBaker, v View, size Size, scale float32) Baked {
	t.Helper()
	b, err := pb.Bake(v, size, scale)
	if errors.Is(err, ErrNoGPU) {
		t.Skipf("no headless GPU here: %v", err)
	}
	if err != nil {
		t.Fatal(err) // a GPU here, and the drawing or its read-back failed
	}
	return b
}

// pixels is a bake's image as RGBA.
func pixels(t *testing.T, b Baked) *image.RGBA {
	t.Helper()
	if b.RGBA == nil || b.Image.src != image.Image(b.RGBA) {
		t.Fatalf("a bake's RGBA is not its image's pixels")
	}
	return b.RGBA
}

// A bake is the view's pixels at the scale asked, each side rounded up, transparent where the view
// paints nothing: at 1.5, a 20 x 10.2 dp view is 30 x 16 pixels.
func TestABakeIsTheViewsPixels(t *testing.T) {
	pb := NewBackend().NewPictureBaker()
	defer pb.Release()
	v := ViewFunc(func(c Canvas) { c.FillRect(Rect{W: 10, H: 5}, Solid(color.NRGBA{R: 0xc0, A: 0xff})) })
	b := bakeOrSkip(t, pb, v, Size{W: 20, H: 10.2}, 1.5)
	if b.Pixels != image.Pt(30, 16) || b.Scale != 1.5 {
		t.Fatalf("a bake of 20 x 10.2 dp at 1.5: %v pixels at %v, want 30 x 16 at 1.5", b.Pixels, b.Scale)
	}
	img := pixels(t, b)
	if img.Bounds() != image.Rect(0, 0, 30, 16) {
		t.Fatalf("its image is %v", img.Bounds())
	}
	if c := img.RGBAAt(4, 3); c.R < 0xa0 || c.A != 0xff {
		t.Errorf("(4,3) is %v, want the red the view painted", c)
	}
	if c := img.RGBAAt(4, 12); c.A != 0 {
		t.Errorf("(4,12) is %v, want transparent: the view paints nothing there", c)
	}
	if c := img.RGBAAt(20, 3); c.A != 0 {
		t.Errorf("(20,3) is %v, want transparent: the red ends at 15 pixels", c)
	}
}

// DrawBaked draws a bake pixel for pixel, its corner on the nearest whole device pixel: a bake of
// a view that draws another bake at (0.3, 0.2) dp is that bake, and at (1, 0) dp, at 1.5, it is
// that bake two pixels to the right.
func TestDrawBakedDrawsPixelForPixel(t *testing.T) {
	pb := NewBackend().NewPictureBaker()
	defer pb.Release()
	size := Size{W: 40, H: 20}
	scene := ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: 40, H: 20}, Solid(color.NRGBA{R: 0xe6, G: 0xd9, B: 0xb9, A: 0xff}))
		p := new(Path).MoveTo(Pt(2, 18)).LineTo(Pt(38, 2))
		c.StrokePath(p, 1, Solid(color.NRGBA{R: 0x30, G: 0x40, B: 0xa0, A: 0xff}))
		c.FillEllipse(Rect{X: 5, Y: 3, W: 9, H: 7}, Solid(color.NRGBA{G: 0x80, A: 0xc0}))
	})
	first := bakeOrSkip(t, pb, scene, size, 1.5)
	want := pixels(t, first)
	if c := want.RGBAAt(1, 1); c != (color.RGBA{R: 0xe6, G: 0xd9, B: 0xb9, A: 0xff}) {
		t.Fatalf("the bake's (1,1) is %v, want the scene's page: a bake that drew nothing compares as nothing", c)
	}
	for _, tc := range []struct {
		at    Point
		shift int
	}{{Pt(0.3, 0.2), 0}, {Pt(1, 0), 2}} {
		again := bakeOrSkip(t, pb, ViewFunc(func(c Canvas) { DrawBaked(c, first, tc.at) }), size, 1.5)
		got := pixels(t, again)
		for y := range want.Bounds().Dy() {
			for x := range want.Bounds().Dx() - tc.shift {
				if g, w := got.RGBAAt(x+tc.shift, y), want.RGBAAt(x, y); g != w {
					t.Fatalf("drawn at %v: pixel (%d,%d) is %v, want the bake's (%d,%d) %v", tc.at, x+tc.shift, y, g, x, y, w)
				}
			}
		}
	}
}

// Off a window there is no baker, and a bake is drawn into its size as DrawImage draws; a released
// baker bakes nothing.
func TestABakeOffAWindowAndAfterRelease(t *testing.T) {
	rc := NewRecordingCanvas(Size{W: 40, H: 20}, Size{W: 1, H: 1})
	if BakerOf(rc) != nil {
		t.Fatal("a recording has a baker")
	}
	img := image.NewRGBA(image.Rect(0, 0, 30, 15))
	DrawBaked(rc, Baked{Image: NewImage(img), Pixels: image.Pt(30, 15), Scale: 1.5}, Pt(2, 3))
	found := false
	for _, call := range rc.Calls {
		if call.Op == "DrawImage" && call.Rect == (Rect{X: 2, Y: 3, W: 20, H: 10}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("a bake on a recording is not drawn into its 20 x 10 dp at (2,3): %+v", rc.Calls)
	}
	pb := NewBackend().NewPictureBaker()
	pb.Release()
	if _, err := pb.Bake(ViewFunc(func(Canvas) {}), Size{W: 4, H: 4}, 1); !errors.Is(err, ErrBakerClosed) {
		t.Fatalf("a bake after Release: %v", err)
	}
}
