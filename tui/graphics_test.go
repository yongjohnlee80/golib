package tui

import (
	"bytes"
	"image"
	"image/png"
	"reflect"
	"testing"
	"time"
)

// imaged is a probe that reports an image while show is set.
type imaged struct {
	*probe
	img  Image
	show bool
}

func (c *imaged) Image() (Image, bool) { return c.img, c.show }

// waitImages waits, up to five seconds, for the backend's images to be want: a resize reaches the
// loop as input, on its own schedule, so a count of round trips is no bound on a slow machine.
func (h *harness) waitImages(want map[uint32]ImagePlacement) {
	h.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		h.sync()
		if got := h.tb.Images(); reflect.DeepEqual(got, want) {
			return
		}
	}
	h.t.Fatalf("images = %+v, want %+v", h.tb.Images(), want)
}

// The App places a visible reporter's image over its rect, places it again when its version or
// its cells change, and deletes it when the reporter shows none; an unchanged frame sends nothing.
func TestTheAppPlacesAndDeletesImages(t *testing.T) {
	c := &imaged{probe: &probe{name: "img"}, img: Image{ID: 77, PNG: []byte("png1"), Version: 1}, show: true}
	h := startApp(t, c, 20, 6)
	h.waitImages(map[uint32]ImagePlacement{77: {Image: c.img, X: 0, Y: 0, Cols: 20, Rows: 6}})

	// a repaint with nothing changed latches nothing
	h.onLoop(func() {
		c.ctx.MarkDirty()
	})
	h.sync()
	h.onLoop(func() {
		if n := len(h.tb.images.ops); n != 0 {
			t.Errorf("%d image commands latched for an unchanged image", n)
		}
	})

	h.onLoop(func() {
		c.img = Image{ID: 77, PNG: []byte("png2"), Version: 2}
		c.ctx.MarkDirty()
	})
	h.waitImages(map[uint32]ImagePlacement{77: {Image: Image{ID: 77, PNG: []byte("png2"), Version: 2}, Cols: 20, Rows: 6}})

	h.tb.InjectResize(12, 4) // its cells change
	h.waitImages(map[uint32]ImagePlacement{77: {Image: Image{ID: 77, PNG: []byte("png2"), Version: 2}, Cols: 12, Rows: 4}})

	h.onLoop(func() {
		c.show = false
		c.ctx.MarkDirty()
	})
	h.waitImages(map[uint32]ImagePlacement{})
}

// pictured is a probe that reports several images, each over cells of its own (ImagesReporter).
type pictured struct {
	*probe
	imgs []PlacedImage
}

func (c *pictured) PlacedImages(dst []PlacedImage) []PlacedImage { return append(dst, c.imgs...) }

// tinyPNG is a w × h PNG.
func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// An ImagesReporter's images are each placed over their own cells, from the component's origin:
// one wholly inside as it is, one running past the component's bottom cut to the cells that show
// and cropped to the same part of its picture, one wholly outside not at all; one the reporter
// stops reporting is deleted, the others kept.
func TestTheAppPlacesAReportersImagesEachOverItsOwnCells(t *testing.T) {
	pic := tinyPNG(t, 40, 40)
	c := &pictured{probe: &probe{name: "page"}, imgs: []PlacedImage{
		{Image: Image{ID: 81, PNG: pic, Version: 1}, At: Rect{X: 2, Y: 1, W: 4, H: 2}},
		{Image: Image{ID: 82, PNG: pic, Version: 1}, At: Rect{X: 0, Y: 4, W: 10, H: 4}}, // half below the bottom
		{Image: Image{ID: 83, PNG: pic, Version: 1}, At: Rect{X: 0, Y: 9, W: 10, H: 2}}, // wholly below it
	}}
	h := startApp(t, c, 20, 6)
	h.waitImages(map[uint32]ImagePlacement{
		81: {Image: c.imgs[0].Image, X: 2, Y: 1, Cols: 4, Rows: 2},
		82: {Image: Image{ID: 82, PNG: pic, Version: 1, Clip: Rect{X: 0, Y: 0, W: 40, H: 20}}, X: 0, Y: 4, Cols: 10, Rows: 2},
	})
	h.onLoop(func() {
		c.imgs = c.imgs[1:]
		c.ctx.MarkDirty()
	})
	h.waitImages(map[uint32]ImagePlacement{
		82: {Image: Image{ID: 82, PNG: pic, Version: 1, Clip: Rect{X: 0, Y: 0, W: 40, H: 20}}, X: 0, Y: 4, Cols: 10, Rows: 2},
	})
}
