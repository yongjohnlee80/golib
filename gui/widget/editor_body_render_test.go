package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// rowRecorder is a surface that keeps the first row's cells: each one's content and whether it
// was drawn reversed. Its width policy draws "§" (East-Asian ambiguous) two cells wide, as a
// surface whose policy takes ambiguous characters as wide does.
type rowRecorder struct {
	tui.Surface
	w, h     int
	text     map[int]string
	reversed map[int]bool
}

func newRowRecorder(w, h int) *rowRecorder {
	return &rowRecorder{w: w, h: h, text: map[int]string{}, reversed: map[int]bool{}}
}

func (r *rowRecorder) Size() tui.Size                     { return tui.Size{W: r.w, H: r.h} }
func (r *rowRecorder) Theme() *style.Theme                { return nil }
func (r *rowRecorder) Fill(tui.Rect, string, style.Style) {}
func (r *rowRecorder) StringWidth(s string) int {
	if s == "§" {
		return 2
	}
	return tui.StringWidth(s)
}
func (r *rowRecorder) SetCell(x, y int, content string, st style.Style) {
	if y == 0 {
		r.text[x], r.reversed[x] = content, st.Attrs()&style.AttrReverse != 0
	}
}

// renderSelected renders line with clusters [from, to) selected, the editor focused, and returns
// the first row's reversed cells' contents.
func renderSelected(t *testing.T, line string, from, to int) []string {
	t.Helper()
	e := NewEditor()
	e.core.SetValue(line)
	e.core.Select(tuiwidget.TextRange{From: tuiwidget.TextPos{Col: from}, To: tuiwidget.TextPos{Col: to}})
	e.body.focused = true
	r := newRowRecorder(20, 3)
	e.body.Render(r)
	var got []string
	for x := range r.w {
		if r.reversed[x] {
			got = append(got, r.text[x])
		}
	}
	return got
}

// TestTheCellBodyHighlightsTheSelectedClusters: the clusters a selection covers, and only they,
// are drawn reversed.
func TestTheCellBodyHighlightsTheSelectedClusters(t *testing.T) {
	got := renderSelected(t, "abcdefgh", 2, 5)
	if len(got) != 3 || got[0] != "c" || got[1] != "d" || got[2] != "e" {
		t.Fatalf("reversed cells %q, want [c d e]", got)
	}
}

// TestTheHighlightFollowsTheClusterDrawnWhereWidthsDisagree: on a surface that draws "§" two cells
// wide, selecting "a" in "§ab" reverses "a". Finding the cluster again from its column with the
// package-level width (which takes "§" as one cell) reversed "b" instead.
func TestTheHighlightFollowsTheClusterDrawnWhereWidthsDisagree(t *testing.T) {
	got := renderSelected(t, "§ab", 1, 2)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("reversed cells %q, want [a]", got)
	}
}
