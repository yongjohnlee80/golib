package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// The Rendered view lays the core's overlay over its cells, as the text's paint does: a find marks
// every match, not only the current one, and the host's own marks (SetHighlightOverlay) show too.
func TestTheRenderedViewMarksEveryMatch(t *testing.T) {
	e := NewEditor(WithCore(CoreInitialText("# Head\n\nalpha beta alpha")))
	var st SyntaxStyles
	st[highlight.Alert] = style.New().Foreground(style.RGB(200, 0, 0))
	st[highlight.Keyword] = style.New().Foreground(style.RGB(0, 0, 200))
	e.core.SetSyntaxStyles(st)
	e.SetRenderer(&MarkdownCellRenderer{MarkdownBlocks: MarkdownBlocks{HideMetadata: true}})
	e.SetView(ViewRendered)
	ih := widgettest.StartApp(t, e, 40, 6)
	t.Cleanup(ih.Stop)
	ih.Sync()
	// the looks of the row's three words, by their first cells
	words := func() (a1, b, a2 tui.CellAttrs, ok bool) {
		for _, row := range ih.TB.Snapshot() {
			var text strings.Builder
			for _, c := range row {
				text.WriteString(c.Content)
			}
			i := strings.Index(text.String(), "alpha beta alpha")
			if i < 0 {
				continue
			}
			return row[i].Attrs, row[i+6].Attrs, row[i+11].Attrs, true
		}
		return
	}
	a1, b, a2, ok := words()
	if !ok || a1 != b || a2 != b {
		t.Fatalf("with no find, the words look alike (%v): %+v %+v %+v\n%s", ok, a1, b, a2, ih.TB.String())
	}
	plain := b
	ih.OnLoop(func() { e.core.SetFind("alpha", FindOptions{}) })
	waitGrid(t, ih, "both matches marked", func(string) bool {
		a1, b, a2, ok := words()
		return ok && a1 != plain && a2 == a1 && b == plain
	})
	ih.OnLoop(func() {
		e.core.SetHighlightOverlay(func(line string, spans []highlight.Span) []highlight.Span {
			if i := strings.Index(line, "beta"); i >= 0 {
				spans = append(spans, highlight.Span{Start: i, End: i + len("beta"), Style: highlight.Keyword})
			}
			return spans
		})
	})
	waitGrid(t, ih, "the host's mark on beta", func(string) bool {
		a1, b, a2, ok := words()
		return ok && b != plain && b != a1 && a1 == a2 && a1 != plain
	})
}
