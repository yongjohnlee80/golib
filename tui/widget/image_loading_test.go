package widget_test

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// While it loads and has no PNG, an Image says so, a spinner turning by its own timer, instead of
// an empty pane; off, the words go.
func TestAnImageShowsThatItIsLoading(t *testing.T) {
	img := widget.NewImage()
	img.SetLoading(true) // before it is mounted: the spinner starts at Init
	img.SetLoadingText("rendering the page…")
	h := startApp(t, img, 40, 5)
	h.settle()
	h.wantContains("rendering the page…")
	first := h.row(2)
	turned := false
	for range 40 {
		h.settle()
		if h.row(2) != first {
			turned = true
			break
		}
	}
	if !turned {
		t.Errorf("the spinner never turned: %q", first)
	}
	h.onLoop(func() { img.SetLoading(false) })
	h.settle()
	if strings.Contains(h.grid(), "rendering") {
		t.Errorf("loading off, the words stay:\n%s", h.grid())
	}
}

// An Image that stops scrolling mid-drag lets the pointer go: the button beside it takes the next
// click, which a capture left behind would have kept from it.
func TestDisablingScrollMidDragReleasesThePointer(t *testing.T) {
	img := widget.NewImage()
	img.SetScrollable(true)
	img.SetPNG(bigPNG(t))
	var clicks atomic.Int32
	btn := widget.NewButton("OK", widget.WithOnActivate(func() { clicks.Add(1) }))
	root := tui.NewFlex(tui.Vertical)
	root.Add(btn, img) // the button first: the Image takes every row it is offered
	h := startApp(t, root, 30, 8)
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 3, Y: 3}) // a drag starts, on the Image
	h.settle()
	h.onLoop(func() { img.SetScrollable(false) })
	h.settle()
	x, y := cellOfLabel(t, h, "OK")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.settle()
	if clicks.Load() != 1 {
		t.Errorf("the button took %d clicks after the Image stopped scrolling mid-drag, want 1", clicks.Load())
	}
}

// bigPNG is a PNG larger than the Image shows, so it scrolls.
func bigPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2000, 2000))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
