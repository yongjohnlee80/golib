package widget

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
)

// sceneColour reads a scene's pixel at (x, y) dp at scale 2 as one of the scene's known colours:
// "red", "blue", "green", "ground", or "" for none of them.
func sceneColour(img *image.RGBA, x, y float32, ground color.RGBA) string {
	px := img.RGBAAt(int(x*2), int(y*2))
	d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	switch {
	case px.R >= 0xa0 && px.G <= 0x20 && px.B <= 0x20:
		return "red"
	case px.B >= 0xa0 && px.R <= 0x20 && px.G <= 0x20:
		return "blue"
	case px.G >= 0xa0 && px.R <= 0x20 && px.B <= 0x20:
		return "green"
	case d(px.R, ground.R) <= 16 && d(px.G, ground.G) <= 16 && d(px.B, ground.B) <= 16:
		return "ground"
	}
	return ""
}

// In a window's frame, overlapping shapes paint in their order: a later one over an earlier one
// where they overlap, each whole where it doesn't; a shape over another widget's view covers it
// where it lies; and cells painted after a shape (its Covered) show over it. On a GPU, in pixels,
// through the window's own compositing (Snapshotter.SnapScene).
func TestOverlappingShapesPaintInTheirOrder(t *testing.T) {
	red, blue := color.NRGBA{R: 0xc0, A: 0xff}, color.NRGBA{B: 0xc0, A: 0xff}
	green := color.NRGBA{G: 0xc0, A: 0xff}
	snap, err := gui.NewSnapshotter(gui.Size{W: 40, H: 24}, 2)
	if errors.Is(err, gui.ErrNoGPU) {
		t.Skipf("no headless GPU here (gui.ErrNoGPU): the pixels are not checked on this machine: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Release()
	empty, err := snap.SnapScene()
	if err != nil {
		t.Fatal(err)
	}
	ground := empty.RGBAAt(2, 2)
	a := func() *Shape { return NewShape(ShapeRect, WithFillColor(red)) }
	b := func() *Shape { return NewShape(ShapeRect, WithFillColor(blue)) }
	widget := gui.ViewFunc(func(c gui.Canvas) { // another widget's view: a plain green fill
		sz := c.Size()
		c.FillRect(gui.Rect{W: sz.W, H: sz.H}, gui.Solid(green))
	})
	type point struct {
		x, y float32
		want string
	}
	for _, c := range []struct {
		name   string
		scene  []gui.Placed
		points []point
	}{
		{"red, then blue over it", []gui.Placed{
			{View: a(), X: 0, Y: 0, Cols: 24, Rows: 16},
			{View: b(), X: 16, Y: 8, Cols: 24, Rows: 16},
		}, []point{{20, 12, "blue"}, {4, 4, "red"}, {36, 20, "blue"}, {36, 4, "ground"}}},
		{"blue, then red over it", []gui.Placed{
			{View: b(), X: 16, Y: 8, Cols: 24, Rows: 16},
			{View: a(), X: 0, Y: 0, Cols: 24, Rows: 16},
		}, []point{{20, 12, "red"}, {4, 4, "red"}, {36, 20, "blue"}}},
		{"a shape over another widget", []gui.Placed{
			{View: widget, X: 0, Y: 0, Cols: 40, Rows: 24},
			{View: a(), X: 8, Y: 4, Cols: 16, Rows: 12},
		}, []point{{12, 8, "red"}, {2, 2, "green"}, {36, 20, "green"}}},
		{"cells painted after a shape cover it", []gui.Placed{
			{View: a(), X: 0, Y: 0, Cols: 40, Rows: 24, Covered: []tui.Rect{{X: 20, Y: 0, W: 20, H: 24}}},
		}, []point{{10, 12, "red"}, {30, 12, "ground"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			img, err := snap.SnapScene(c.scene...)
			if err != nil {
				t.Fatal(err) // the window drew the empty scene: any error here is a failure
			}
			for _, p := range c.points {
				if got := sceneColour(img, p.x, p.y, ground); got != p.want {
					t.Errorf("at (%v,%v) dp: %q (%v), want %s", p.x, p.y, got, img.RGBAAt(int(p.x*2), int(p.y*2)), p.want)
				}
			}
		})
	}
}
