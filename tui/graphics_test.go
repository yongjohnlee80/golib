package tui

import (
	"reflect"
	"testing"
)

// imaged is a probe that reports an image while show is set.
type imaged struct {
	*probe
	img  Image
	show bool
}

func (c *imaged) Image() (Image, bool) { return c.img, c.show }

// waitImages waits for the backend's images to be want.
func (h *harness) waitImages(want map[uint32]ImagePlacement) {
	h.t.Helper()
	for range 200 {
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
		if n := len(h.tb.imageOps); n != 0 {
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
