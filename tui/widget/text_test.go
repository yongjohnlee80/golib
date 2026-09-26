package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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

func TestTextColorOverridesOnlyThePaletteForegroundAcrossRestyles(t *testing.T) {
	label := widget.NewText("first", widget.WithTextStyle(style.New().Foreground(style.ANSI(4)).Reverse(true)))
	h := startApp(t, label, 16, 2)
	h.settle()
	h.onLoop(func() { label.SetColor(style.ANSI(1)) })
	h.settle()
	cell := h.tb.Snapshot()[0][0]
	if cell.Attrs.FG.Kind != tui.CellColorANSI || cell.Attrs.FG.Index != 1 || cell.Attrs.Mask&tui.AttrReverse == 0 {
		t.Fatalf("Text.color replaced the rest of the style: %+v", cell.Attrs)
	}
	h.onLoop(func() {
		label.WithStyle(style.New().Foreground(style.ANSI(3)).Reverse(false))
		label.SetText("second")
		label.SetColor(style.ANSI(2))
	})
	h.settle()
	cell = h.tb.Snapshot()[0][0]
	if !strings.Contains(h.grid(), "second") || cell.Attrs.FG.Index != 2 || cell.Attrs.Mask&tui.AttrReverse != 0 {
		t.Fatalf("Text.color did not follow the new palette/text and complete mask: %+v", cell.Attrs)
	}
	// Idempotent updates do not alter a mounted label or its chosen color.
	h.onLoop(func() { label.SetColor(style.ANSI(2)); label.SetText("second") })
	h.settle()
	if got := h.tb.Snapshot()[0][0].Attrs; got != cell.Attrs {
		t.Fatalf("same value changed the label style: %+v to %+v", cell.Attrs, got)
	}
}

func TestWrappedTextUsesItsExplicitColorOnEveryLine(t *testing.T) {
	label := widget.NewText("top\nbottom", widget.WithWrapMode(widget.Wrap))
	h := startApp(t, label, 12, 2)
	h.onLoop(func() { label.SetColor(style.ANSI(5)) })
	h.settle()
	for y := 0; y < 2; y++ {
		if got := h.tb.Snapshot()[y][0].Attrs.FG; got.Kind != tui.CellColorANSI || got.Index != 5 {
			t.Errorf("wrapped row %d lost Text.color: %+v", y, got)
		}
	}
}
