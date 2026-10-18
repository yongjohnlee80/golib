package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/gio/op/paint"
)

const testW, testH = 192, 120

// cpuPicture is the CPU producer's picture at t, as its frame's pixels.
func cpuPicture(t *testing.T, at float64) *image.RGBA {
	t.Helper()
	p := newCPUProducer()
	f, err := p.render(at, image.Pt(testW, testH))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Drop()
	img := image.NewRGBA(image.Rect(0, 0, testW, testH))
	copy(img.Pix, f.Desc().Planes[0].Data)
	return img
}

// The CPU producer draws the scene: sky at the top, the crate's wood in the middle, the floor at
// the bottom, as the camera frames them.
func TestTheCPUProducerDrawsTheScene(t *testing.T) {
	img := cpuPicture(t, 0)
	savePNG(t, "cpu", img)
	sky := img.RGBAAt(4, 4)
	if sky != clearColour {
		t.Errorf("top-left %v; want the sky %v", sky, clearColour)
	}
	if c := img.RGBAAt(testW/2, testH/2); !(c.R > c.G && c.G > c.B && c.R > 0x50) {
		t.Errorf("the middle %v; want the crate's wood (red over green over blue)", c)
	}
	if c := img.RGBAAt(testW/2, testH-3); c == clearColour || (c.R > c.G && c.G > c.B) {
		t.Errorf("the bottom middle %v; want the floor, neither sky nor wood", c)
	}
}

// The scene moves: two times are two pictures.
func TestTheSceneTurns(t *testing.T) {
	a, b := cpuPicture(t, 0), cpuPicture(t, 0.5)
	if diff, _ := compare(a, b); diff < 0.05 {
		t.Fatalf("t=0 and t=0.5 differ in %.1f%% of their pixels; the crate does not turn", diff*100)
	}
}

// A GPU producer's frame, drawn by a window, is the CPU producer's picture: the same scene, the
// right way up, in the right colours. Rasterization and sampling rules differ at edges, so a few
// pixels may.
func TestTheGPUProducerDrawsWhatTheCPUDraws(t *testing.T) {
	hw, err := headless.NewWindow(testW, testH)
	if err != nil {
		t.Skipf("no headless window here: %v", err)
	}
	defer hw.Release()
	prod, err := newGPUProducer(hw.ExternalCaps(), false)
	if err != nil {
		t.Skipf("no %s producer for this window (%s): %v", gpuProducerName, hw.ExternalCaps().Driver, err)
	}
	defer prod.close()
	t.Logf("%s producer, frames %s, window driver %s", prod.name(), prod.path(), hw.ExternalCaps().Driver)

	ref, err := newCPUProducer().render(0.3, image.Pt(testW, testH))
	if err != nil {
		t.Fatal(err)
	}
	want := drawThrough(t, hw, ref)
	got, err := prod.render(0.3, image.Pt(testW, testH))
	if err != nil {
		t.Fatal(err)
	}
	have := drawThrough(t, hw, got)
	savePNG(t, "cpu-through-window", want)
	savePNG(t, prod.name()+"-through-window", have)
	diff, worst := compare(want, have)
	t.Logf("%.2f%% of pixels differ by more than the tolerance; worst at %v", diff*100, worst)
	if diff > 0.03 {
		t.Fatalf("the %s producer's picture differs from the CPU's in %.1f%% of its pixels (first at %v); want under 3%%", prod.name(), diff*100, worst)
	}
}

// drawThrough draws f in w and returns the window's pixels, dropping f.
func drawThrough(t *testing.T, w *headless.Window, f *extimage.Frame) *image.RGBA {
	t.Helper()
	defer f.Drop()
	var reported error
	var ops op.Ops
	extimage.NewImageOp(f, func(err error) { reported = err }).Add(&ops)
	paint.PaintOp{}.Add(&ops)
	if err := w.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	if reported != nil {
		t.Fatalf("the window did not draw the frame: %v", reported)
	}
	img := image.NewRGBA(image.Rect(0, 0, testW, testH))
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

// compare is the share of pixels of a and b that differ by more than 24 in a channel, and the
// first such pixel.
func compare(a, b *image.RGBA) (float64, image.Point) {
	n := 0
	first := image.Pt(-1, -1)
	for y := range a.Rect.Dy() {
		for x := range a.Rect.Dx() {
			if !close24(a.RGBAAt(x, y), b.RGBAAt(x, y)) {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return float64(n) / float64(a.Rect.Dx()*a.Rect.Dy()), first
}

func close24(a, b color.RGBA) bool {
	d := func(x, y uint8) bool { return int(x)-int(y) > 24 || int(y)-int(x) > 24 }
	return !d(a.R, b.R) && !d(a.G, b.G) && !d(a.B, b.B)
}

// savePNG writes img to $SCENE3D_PNG/<name>.png when the variable names a directory, to look at.
func savePNG(t *testing.T, name string, img image.Image) {
	dir := os.Getenv("SCENE3D_PNG")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Error(err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Error(err)
	}
}
