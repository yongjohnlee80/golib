package widget_test

import (
	"strings"
	"testing"

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
