package widget_test

import (
	"strings"
	"testing"

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
