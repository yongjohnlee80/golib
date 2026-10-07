package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
)

// A highlightCache works over any text it is given: a line is highlighted again only when it or
// the state before it changed, and a screen past the frame budget is drawn provisionally.
func TestHighlightCacheStandsAlone(t *testing.T) {
	calls := 0
	c := highlightCache{hl: highlight.HighlighterFunc(func(line string, in highlight.State) ([]highlight.Span, highlight.State) {
		calls++
		return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Keyword}}, in
	})}
	text := []string{"a", "b", "c"}
	unchanged := func() int { return noChange }
	f, behind := c.beginFrame(text, 0, unchanged)
	for ln := range text {
		c.highlighted(text, 0, ln, f)
	}
	if behind || calls != 3 {
		t.Fatalf("first frame: behind %v, %d highlights; want 3", behind, calls)
	}
	text[1] = "B" // an edit to line 1, reported as the first line changed
	f, _ = c.beginFrame(text, 0, func() int { return 1 })
	for ln := range text {
		c.highlighted(text, 0, ln, f)
	}
	if calls != 4 {
		t.Fatalf("after an edit to one line: %d highlights; want 4 (that line only)", calls)
	}

	long := strings.Split(strings.Repeat("x\n", hlFrameBudget+10), "\n")
	var d highlightCache
	d.hl = c.hl
	if _, behind := d.beginFrame(long, hlFrameBudget+5, unchanged); !behind {
		t.Fatal("a screen past the frame budget was not provisional")
	}
}
