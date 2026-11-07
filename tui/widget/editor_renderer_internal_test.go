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

// A decoration hits the nearest source cell by measured distance, the left one only on a true tie:
// [a, 界 (a W-2 decoration), b] at the decoration's right cell is b (one cell away; a is two), at
// its left cell a; [a, •, b] at the bullet is a tie, so a; a leading decoration hits the first
// source cell, a trailing one the last; past the row's end, the last source cell.
func TestADecorationHitsTheNearestSourceCellByCells(t *testing.T) {
	src := func(s string, col int) RowCell { return RowCell{Text: s, W: 1, Line: 0, Col: col} }
	deco := func(s string, w int) RowCell { return RowCell{Text: s, W: w, Line: -1} }
	for _, c := range []struct {
		name  string
		cells []RowCell
		x     int
		col   int
	}{
		{"wide decoration, its right cell", []RowCell{src("a", 0), deco("界", 2), src("b", 2)}, 2, 2},
		{"wide decoration, its left cell", []RowCell{src("a", 0), deco("界", 2), src("b", 2)}, 1, 0},
		{"a true tie takes the left", []RowCell{src("a", 0), deco("•", 1), src("b", 2)}, 1, 0},
		{"a leading decoration", []RowCell{deco("•", 1), deco(" ", 1), src("a", 2), src("b", 3)}, 0, 2},
		{"a trailing decoration", []RowCell{src("a", 0), src("b", 1), deco("│", 1)}, 2, 1},
		{"past the row's end", []RowCell{src("a", 0), src("b", 1), deco("│", 1)}, 9, 1},
	} {
		r := &renderedLayout{rows: []CellRow{{Anchor: -1, Cells: c.cells}}, lineRows: [][]int{{0}}}
		if ln, col := r.hit(0, c.x); ln != 0 || col != c.col {
			t.Errorf("%s: hit at x %d = (%d, %d), want col %d", c.name, c.x, ln, col, c.col)
		}
	}
}

// The view's top on a block's opening row (a table's top rule) is the block's first line, none of
// it above the view: not the line before, a row of it above.
func TestABlocksOpeningRowStartsItsFirstLine(t *testing.T) {
	deco := []RowCell{{Text: "┌", W: 1, Line: -1}}
	r := &renderedLayout{
		blocks:  []Block{{0, 1}, {1, 2}, {2, 5}},
		blockOf: []int{0, 1, 2, 2, 2},
		rows: []CellRow{
			{Anchor: -1, Cells: []RowCell{{Text: "b", W: 1, Line: 0, Col: 0}}},
			{Anchor: 1},
			{Anchor: -1, Cells: deco, Unnumbered: true},
			{Anchor: -1, Cells: []RowCell{{Text: "a", W: 1, Line: 2, Col: 2}}},
			{Anchor: 3, Cells: deco},
			{Anchor: -1, Cells: []RowCell{{Text: "c", W: 1, Line: 4, Col: 2}}},
			{Anchor: -1, Cells: deco, Unnumbered: true},
		},
		lineRows: [][]int{{0}, {1}, {3}, {4}, {5}},
		rowBlock: []int{0, 1, 2, 2, 2, 2, 2},
	}
	if top, in := r.atRow(2); top != 2 || in != 0 {
		t.Errorf("atRow on the table's top rule = (%d, %d), want (2, 0): its first line, from its top", top, in)
	}
	if top, in := r.atRow(3); top != 2 || in != 1 {
		t.Errorf("atRow on the header = (%d, %d), want (2, 1)", top, in)
	}
	if top, in := r.atRow(1); top != 1 || in != 0 {
		t.Errorf("atRow on the blank line = (%d, %d), want (1, 0)", top, in)
	}
}
