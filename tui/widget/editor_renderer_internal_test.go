package widget

import "testing"

// A Rendered layout's caret and hit agree both ways: every source cell's hit is its cluster, and the
// caret of that cluster is the cell where it starts (a W-2 cluster's second cell gives its first). A
// decoration cell hits the nearest source cell; a concealed column's caret is the next shown cell;
// an Anchor row is its line's start; a line with no row is a fold, a run within its block.
func TestARenderedLayoutMapsBothWays(t *testing.T) {
	r := &renderedLayout{
		blocks:  []Block{{0, 2}, {2, 4}},
		blockOf: []int{0, 0, 1, 1},
		rows: []CellRow{
			{Anchor: -1, Cells: []RowCell{{Text: "•", W: 1, Line: -1}, {Text: "漢", W: 2, Line: 0, Col: 2}, {Text: "a", W: 1, Line: 0, Col: 3}}},
			{Anchor: 1},
			{Anchor: -1, Cells: []RowCell{{Text: "z", W: 1, Line: 3, Col: 0}}},
		},
		lineRows: [][]int{{0}, {1}, nil, {2}},
	}
	for x, want := range map[int][2]int{0: {0, 2}, 1: {0, 2}, 2: {0, 2}, 3: {0, 3}} {
		if ln, col := r.hit(0, x); ln != want[0] || col != want[1] {
			t.Errorf("hit(0, %d) = (%d, %d), want %v", x, ln, col, want)
		}
	}
	if row, x := r.caret(0, 2); row != 0 || x != 1 {
		t.Errorf("caret of the wide cluster (%d, %d), want (0, 1): its first cell", row, x)
	}
	if row, x := r.caret(0, 0); row != 0 || x != 1 {
		t.Errorf("caret of a concealed column (%d, %d), want the next shown cell (0, 1)", row, x)
	}
	if ln, col := r.hit(1, 5); ln != 1 || col != 0 {
		t.Errorf("hit on an Anchor row = (%d, %d), want (1, 0)", ln, col)
	}
	if from, to := r.sourcelessRun(2); from != 2 || to != 3 {
		t.Errorf("fold of line 2 = [%d, %d), want [2, 3)", from, to)
	}
	if got := r.firstRow(2); got != 2 {
		t.Errorf("a folded line's first row %d, want the next line's (2)", got)
	}
}
