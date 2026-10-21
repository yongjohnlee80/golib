package tui

import "testing"

// The too-small screen ends its frame as a laid-out frame does: an image placed before the screen
// shrank below the minimum is deleted, since nothing of the tree shows, and it is placed again once
// the screen is large enough. Both frames present through the same steps.
func TestTheTooSmallScreenPresentsAsAFrameDoes(t *testing.T) {
	c := &imaged{probe: &probe{name: "img"}, img: Image{ID: 7, PNG: []byte("png"), Version: 1}, show: true}
	h := startApp(t, c, 40, 10, WithMinimumSize(Size{W: 40, H: 10}))
	placed := map[uint32]ImagePlacement{7: {Image: c.img, Cols: 40, Rows: 10}}
	h.waitImages(placed)

	h.tb.InjectResize(30, 8)
	waitFor(t, "the too-small screen", func() bool { return screenHas(h.tb, "Screen too small") })
	h.waitImages(map[uint32]ImagePlacement{})
	if _, _, visible := h.tb.CursorPos(); visible {
		t.Error("the too-small screen shows a cursor")
	}

	h.tb.InjectResize(40, 10)
	h.waitImages(placed)
}
