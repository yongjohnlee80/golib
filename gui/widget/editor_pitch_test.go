package widget

import (
	"math"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// An editor in cell pitch is a pane of cells: no title bar takes a row, and the n-th line shown is
// on its n-th row, from the row's top, in all twelve of its rows; in the cells' font, a host's
// font size (WithFontSize) aside, so a line is one cell high.
func TestACellPitchEditorPutsEachLineOnItsRow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  []EditorOption
		cellH float32 // the window's cell height; 0: the harness's
	}{{"the window's size", nil, 0}, {"a font size of 30", []EditorOption{WithFontSize(30)}, 0},
		{"a cell 9 high, a small font's or a 50% zoom's", nil, 9}} {
		t.Run(tc.name, func(t *testing.T) {
			h := startEditor(t, 40, 12, append([]EditorOption{WithCore(editor.CoreInitialText(sixtyLines())), WithEditorCellPitch()}, tc.opts...)...)
			rowH := float32(cellH)
			if tc.cellH > 0 { // another cell size comes with another font, which lays the text out again
				h.cell.H, rowH = tc.cellH, tc.cellH
				h.onLoop(func() { h.e.layout.invalidate() })
			}
			h.paint()
			wantLast := int(math.Ceil(float64(12*cellH)/float64(rowH))) - 1 // the canvas is 12 rows of the harness's cells
			var rows, first, last int
			var lines []int
			h.onLoop(func() {
				rows = h.e.body.h
				first, last = h.e.layout.VisibleLines()
				for row := range 12 {
					ln, _ := h.e.layout.at(padX+1, float32(row)*rowH+1) // just below the row's top
					lines = append(lines, ln)
				}
			})
			if rows != 12 {
				t.Fatalf("the text has %d of the editor's 12 rows, want all: no title bar", rows)
			}
			if first != 0 || last != wantLast {
				t.Fatalf("lines %d-%d shown, want 0-%d: one a row", first, last, wantLast)
			}
			for row, ln := range lines {
				if ln != row {
					t.Fatalf("row %d holds line %d, want line %d: each line one cell high from the first row (%v)", row, ln, row, lines)
				}
			}
		})
	}
}

// An editor in cell pitch scrolls by whole lines: a part of a line's scroll goes to the nearest
// line, and SetTopLine puts its line on the first row.
func TestACellPitchEditorScrollsByWholeLines(t *testing.T) {
	h := startEditor(t, 40, 12, WithCore(editor.CoreInitialText(sixtyLines())), WithEditorCellPitch())
	h.paint()
	var scroll float32
	var top int
	read := func() {
		h.onLoop(func() { scroll, top = h.e.layout.scroll, h.e.TopLine() })
	}
	h.onLoop(func() { h.e.layout.scrollBy(cellH * 0.6) })
	read()
	if scroll != cellH || top != 1 {
		t.Fatalf("a scroll of 0.6 lines: at %vpx, top %d, want one line (%dpx), top 1", scroll, top, cellH)
	}
	h.onLoop(func() { h.e.layout.scrollBy(cellH * 0.4) })
	read()
	if scroll != cellH {
		t.Fatalf("0.4 lines more: at %vpx, want still one line (%dpx)", scroll, cellH)
	}
	h.onLoop(func() { h.e.SetTopLine(20) })
	h.paint()
	read()
	if scroll != 20*cellH || top != 20 {
		t.Fatalf("SetTopLine(20): at %vpx, top %d, want %dpx, top 20", scroll, top, 20*cellH)
	}
}
