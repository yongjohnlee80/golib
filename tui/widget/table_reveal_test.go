package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// A table's reveal moves its first row just far enough to show the row, and keeps no blank below
// the last; its first field back to the field, or on until the field fits.
func TestATablesRevealMovesJustFarEnough(t *testing.T) {
	for _, tc := range []struct{ top, row, count, body, want int }{
		{0, 3, 20, 5, 0},  // in sight
		{0, 7, 20, 5, 3},  // below: the row last
		{10, 4, 20, 5, 4}, // above: the row first
		{18, 2, 6, 5, 1},  // rows gone: clamped to leave none blank, then the row shown
		{0, 0, 0, 5, 0},   // no rows
	} {
		if got := widget.TableRevealRow(tc.top, tc.row, tc.count, tc.body); got != tc.want {
			t.Errorf("TableRevealRow(%d, %d, %d, %d) = %d, want %d", tc.top, tc.row, tc.count, tc.body, got, tc.want)
		}
	}
	fitsTwo := func(from, to int) bool { return to-from < 2 } // two fields fit the view
	for _, tc := range []struct{ left, col, want int }{
		{0, 1, 0}, // fits already
		{0, 4, 3}, // on until 3..4 fits
		{5, 2, 2}, // back to the field
	} {
		if got := widget.TableRevealField(tc.left, tc.col, fitsTwo); got != tc.want {
			t.Errorf("TableRevealField(%d, %d) = %d, want %d", tc.left, tc.col, got, tc.want)
		}
	}
}
