package tui

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// DrawText gives each cluster the columns its width is: a zero-width cluster paints nothing and
// takes no column, and a cluster that does not fit whole is not painted, nor anything after it. It
// returns the column after the last cluster painted.
func TestDrawTextTakesTheMeasuredColumns(t *testing.T) {
	s, buf := newTestSurface(6, 1, WidthPolicyDefault)
	if end := DrawText(s, 0, 0, "a\u200bb\u200bc", style.New()); end != 3 {
		t.Errorf("DrawText(a ZWSP b ZWSP c) ended at column %d, want 3", end)
	}
	for x, want := range []string{"a", "b", "c"} {
		if c := cellAt(buf, x, 0); c.Content != want || c.Width != 1 {
			t.Errorf("column %d holds %+v, want %q one wide", x, c, want)
		}
	}

	w, wbuf := newTestSurface(4, 1, WidthPolicyDefault)
	if end := DrawText(w, 1, 0, "ab漢x", style.New()); end != 3 {
		t.Errorf("a wide cluster at the edge: DrawText ended at column %d, want 3, before it", end)
	}
	if c := cellAt(wbuf, 3, 0); c.Content != "" && c.Content != " " {
		t.Errorf("the last column holds %+v: the wide cluster, or what follows it, was painted", c)
	}
}

// WrapText breaks at the text's own line breaks, at a space (dropped there), and inside a word too
// long for a line; an empty line is one empty line, and a width below 1 gives each line empty.
func TestWrapTextBreaksAtLinesSpacesAndLongWords(t *testing.T) {
	width := StringWidth
	for _, c := range []struct {
		in   string
		w    int
		want []string
	}{
		{"the prose font: the Rendered view", 12, []string{"the prose", "font: the", "Rendered", "view"}},
		{"abcdefghij xy", 4, []string{"abcd", "efgh", "ij", "xy"}},
		{"one\ntwo three", 5, []string{"one", "two", "three"}},
		{"a\n\nb", 5, []string{"a", "", "b"}},
		{"fits", 10, []string{"fits"}},
		{"x\ny", 0, []string{"", ""}},
	} {
		if got := WrapText(c.in, c.w, width); !slices.Equal(got, c.want) {
			t.Errorf("WrapText(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

// A tooltip's text paints inside its card: a zero-width cluster takes no column, as the width that
// sized the card counted it, so nothing is painted past the card's right edge.
func TestToolTip_ZeroWidthClustersStayInsideTheCard(t *testing.T) {
	a, b := newTipProbe("a", "first"), newTipProbe("b", "ab\u200bcd\u200bef")
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 1}, {X: 0, Y: 2, W: 6, H: 1}}}
	root.Add(a, b)
	h := startApp(t, root, 40, 6)
	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	h.inject(KeyEvent{Kind: KeyPress, Code: KeyTab})
	waitFor(t, "b's card", func() bool { return tipShown(h) == b.nodeID() })
	h.sync()
	var card Rect
	h.onLoop(func() { card = h.app.tip.rect })
	if card.W != 8 {
		t.Fatalf("the card is %d wide, want 8: the text's six measured columns and a column each side", card.W)
	}
	grid := h.tb.Snapshot()
	row := grid[card.Y]
	if got := row[card.X+6].Content; got != "f" {
		t.Errorf("the card's last text column holds %q, want f:\n%s", got, h.tb.String())
	}
	if got := row[card.X+card.W].Content; got == "f" || got == "e" {
		t.Errorf("text painted past the card's right edge (%q at column %d):\n%s", got, card.X+card.W, h.tb.String())
	}
}
