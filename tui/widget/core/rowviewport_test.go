package core

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// TestRowViewportArithmetic: kit.RevealRow moves the window just far enough to show the cursor, and
// kit.ClampTop keeps the window full of rows and within them.
func TestRowViewportArithmetic(t *testing.T) {
	for _, c := range []struct{ top, cursor, rows, want int }{
		{5, 2, 4, 2},  // above: the cursor becomes the first row
		{0, 9, 4, 6},  // below: the cursor becomes the last row
		{3, 4, 4, 3},  // inside: unchanged
		{3, 4, 0, 3},  // no window: unchanged
		{-2, 0, 4, 0}, // never above row 0
	} {
		if got := kit.RevealRow(c.top, c.cursor, c.rows); got != c.want {
			t.Errorf("revealRow(%d, %d, %d) = %d, want %d", c.top, c.cursor, c.rows, got, c.want)
		}
	}
	for _, c := range []struct{ top, count, rows, want int }{
		{8, 10, 4, 6}, // no blank rows below the last
		{2, 10, 4, 2},
		{-1, 10, 4, 0},
		{3, 2, 4, 0}, // fewer rows than the window: the first
	} {
		if got := kit.ClampTop(c.top, c.count, c.rows); got != c.want {
			t.Errorf("clampTop(%d, %d, %d) = %d, want %d", c.top, c.count, c.rows, got, c.want)
		}
	}
}
