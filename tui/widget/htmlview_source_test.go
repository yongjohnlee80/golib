package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// SourceIndex is the one rule both HTML layouts answer "which block shows source byte b" with: the
// block holding it, else the first starting after it, else the last before it; blocks with no source
// are passed over, and none at all is -1.
func TestSourceIndexFindsTheBlockShowingAByte(t *testing.T) {
	blocks := [][2]int{{0, 10}, {-1, -1}, {20, 30}, {30, 40}}
	src := func(i int) [2]int { return blocks[i] }
	for b, want := range map[int]int{0: 0, 9: 0, 10: 2, 15: 2, 20: 2, 30: 3, 39: 3, 40: 3, 99: 3} {
		if got := widget.SourceIndex(len(blocks), src, b); got != want {
			t.Errorf("byte %d: block %d, want %d", b, got, want)
		}
	}
	if got := widget.SourceIndex(1, func(int) [2]int { return [2]int{-1, -1} }, 5); got != -1 {
		t.Errorf("no block with source: %d, want -1", got)
	}
}

// Document positions order by block, then span, then offset; OrderDocPos puts a selection's ends in
// document order, however it was dragged.
func TestDocPositionsOrderByBlockSpanOffset(t *testing.T) {
	a, b, c := widget.DocPos{Block: 1, Span: 2, Offset: 3}, widget.DocPos{Block: 1, Span: 2, Offset: 4}, widget.DocPos{Block: 2}
	if !a.Before(b) || !b.Before(c) || b.Before(a) || a.Before(a) {
		t.Error("Before does not order by block, span, offset")
	}
	if x, y := widget.OrderDocPos(c, a); x != a || y != c {
		t.Errorf("OrderDocPos(c, a) = %v, %v; want a, c", x, y)
	}
}
