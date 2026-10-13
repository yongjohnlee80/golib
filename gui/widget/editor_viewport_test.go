package widget

import (
	"fmt"
	"strings"
	"testing"

	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// lineTop is where line ln's top is painted: its caret at column 0, else its block's top.
func lineTop(l *pixelLayout, ln int) float32 {
	if r, ok := l.caretAt(ln, 0); ok {
		return r.Y
	}
	return l.topOf(l.blockOf(ln))
}

// checkVisible holds VisibleLines to its contract against the painted geometry: a line is in the
// range exactly when its top is in the view.
func checkVisible(t *testing.T, h *edHarness, what string) (first, last int) {
	t.Helper()
	var errs []string
	h.onLoop(func() {
		l := h.e.layout
		first, last = l.VisibleLines()
		bottom := l.scroll + l.height - 2*padY
		for ln := range h.e.core.LineCount() {
			top := lineTop(l, ln)
			in := top >= l.scroll && top < bottom
			if in != (ln >= first && ln <= last) {
				errs = append(errs, fmt.Sprintf("line %d top %.1f in [%.1f, %.1f) is %v, range [%d %d]", ln, top, l.scroll, bottom, in, first, last))
			}
		}
	})
	if len(errs) > 0 {
		t.Fatalf("%s:\n%s", what, strings.Join(errs, "\n"))
	}
	return first, last
}

func numbered(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String() + "end"
}

// In the Raw view the visible lines are those whose tops are in the view: one scrolled partly off
// the top is out, one cut off at the bottom is in.
func TestPixelVisibleLinesRaw(t *testing.T) {
	h := startEditor(t, 40, 10, WithCore(tuiwidget.CoreInitialText(numbered(40))))
	h.paint()
	if first, last := checkVisible(t, h, "at the top"); first != 0 || last < 3 {
		t.Fatalf("at the top: [%d %d]", first, last)
	}
	h.onLoop(func() { h.e.layout.scroll = 2.5 * h.e.layout.lineHeight(h.e.layout.monoFont()) })
	h.paint()
	if first, _ := checkVisible(t, h, "two and a half lines down"); first != 3 {
		t.Fatalf("two and a half lines down: first %d, want 3 (line 2 is partly off)", first)
	}
}

// ScrollLines brings the line delta lines from the first visible one to the top, within the text.
func TestPixelScrollLines(t *testing.T) {
	h := startEditor(t, 40, 10, WithCore(tuiwidget.CoreInitialText(numbered(40))))
	h.paint()
	var got, want float32
	h.onLoop(func() {
		l := h.e.layout
		l.ScrollLines(5)
		got, want = l.scroll, lineTop(l, 5)
	})
	if got != want || got <= 0 {
		t.Fatalf("down 5: scroll %.1f, want line 5's top %.1f", got, want)
	}
	h.onLoop(func() {
		l := h.e.layout
		l.ScrollLines(-2)
		got, want = l.scroll, lineTop(l, 3)
	})
	if got != want {
		t.Fatalf("up 2: scroll %.1f, want line 3's top %.1f", got, want)
	}
	h.onLoop(func() {
		l := h.e.layout
		l.ScrollLines(1000)
		got, want = l.scroll, l.total()-(l.height-2*padY)
	})
	if got != want {
		t.Fatalf("down 1000: scroll %.1f, want the end's %.1f", got, want)
	}
	h.onLoop(func() {
		l := h.e.layout
		l.ScrollLines(-1000)
		got = l.scroll
		l.ScrollLines(-1)
		want = l.scroll
	})
	if got != 0 || want != 0 {
		t.Fatalf("up 1000, then 1: scroll %.1f, %.1f", got, want)
	}
}

// In the Rendered view blocks are not a line a row: the range still follows the painted tops.
func TestPixelVisibleLinesRendered(t *testing.T) {
	text := "# Title\n\npara one\n\n```go\nx := 1\ny := 2\nz := 3\n```\n\n## Next\n\n" + numbered(30)
	h := startEditor(t, 40, 10, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(text)))
	h.paint()
	checkVisible(t, h, "rendered, at the top")
	for _, ln := range []int{5, 10, 14} {
		h.onLoop(func() { h.e.layout.scroll = lineTop(h.e.layout, ln) + 1 })
		h.paint()
		checkVisible(t, h, fmt.Sprintf("rendered, just past line %d's top", ln))
	}
}

// A line taller than the view, scrolled into, is the range alone.
func TestPixelVisibleLinesInsideATallLine(t *testing.T) {
	text := "short\n" + strings.Repeat("word ", 400) + "\nshort"
	h := startEditor(t, 30, 8, WithWrap(tuiwidget.WrapSoft), WithCore(tuiwidget.CoreInitialText(text)))
	h.paint()
	h.onLoop(func() {
		l := h.e.layout
		l.scroll = lineTop(l, 1) + l.lineHeight(l.monoFont())
	})
	h.paint()
	var first, last int
	var tall bool
	h.onLoop(func() {
		l := h.e.layout
		tall = lineTop(l, 2) > l.scroll+l.height-2*padY
		first, last = l.VisibleLines()
	})
	if !tall {
		t.Fatal("the fixture's long line is not taller than the view")
	}
	if first != 1 || last != 1 {
		t.Fatalf("inside the tall line: [%d %d], want [1 1]", first, last)
	}
}

// pixelLayout is a tui EditorViewport: H M L and Ctrl-d Ctrl-u find it by its methods. The
// assertion is spelled out here because gui builds against a released golib that may predate it.
var _ interface {
	VisibleLines() (int, int)
	ScrollLines(int)
} = (*pixelLayout)(nil)
