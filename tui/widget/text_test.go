package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestTextWrapAndTruncate(t *testing.T) {
	trunc := widget.NewText("a very long label that overflows")
	h := startApp(t, trunc, 12, 1)
	h.settle()
	if got := h.row(0); !strings.HasSuffix(strings.TrimRight(got, " "), "…") {
		t.Fatalf("Truncate mode did not ellipsize: %q", got)
	}

	wrap := widget.NewText("alpha beta gamma", widget.WithWrapMode(widget.Wrap))
	h2 := startApp(t, wrap, 6, 4)
	h2.settle()
	h2.wantContains("alpha")
	h2.wantContains("beta")
	h2.wantContains("gamma")
	if strings.Contains(h2.row(0), "beta") {
		t.Fatalf("Wrap mode did not wrap: %q", h2.row(0))
	}
}

// TestAWrappedTextIsAsWideAsItsWidestLine: it wraps at the width it is offered
// and reports no more than it draws in, so a card sized to its content is not
// stretched to the screen by three short lines.
func TestAWrappedTextIsAsWideAsItsWidestLine(t *testing.T) {
	for _, c := range []struct {
		text  string
		maxW  int
		wantW int
		wantH int
	}{
		{"short\nlonger line", 40, 11, 2}, // fits: the widest line
		{"alpha beta gamma", 6, 6, 3},     // wraps: the offer
		{"a\n\nb", 40, 1, 3},              // blank lines kept
	} {
		txt := widget.NewText(c.text, widget.WithWrapMode(widget.Wrap))
		sz := txt.Layout(tui.Loose(tui.Size{W: c.maxW, H: 10}))
		if sz.W != c.wantW || sz.H != c.wantH {
			t.Errorf("%q in %d columns: %dx%d, want %dx%d", c.text, c.maxW, sz.W, sz.H, c.wantW, c.wantH)
		}
	}
}
