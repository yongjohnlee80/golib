package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// An editor in cell pitch is a pane of cells: no title bar takes a row, and the n-th line shown is
// on its n-th row, from the row's top, in all twelve of its rows; in the cells' font, a host's
// font size (WithFontSize) aside, so a line is one cell high.
func TestACellPitchEditorPutsEachLineOnItsRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []EditorOption
	}{{"the window's size", nil}, {"a font size of 30", []EditorOption{WithFontSize(30)}}} {
		t.Run(tc.name, func(t *testing.T) {
			h := startEditor(t, 40, 12, append([]EditorOption{WithCore(editor.CoreInitialText(sixtyLines())), WithEditorCellPitch()}, tc.opts...)...)
			h.paint()
			var rows, first, last int
			var lines []int
			h.onLoop(func() {
				rows = h.e.body.h
				first, last = h.e.layout.VisibleLines()
				for row := range 12 {
					ln, _ := h.e.layout.at(padX+1, float32(row*cellH)+1) // just below the row's top
					lines = append(lines, ln)
				}
			})
			if rows != 12 {
				t.Fatalf("the text has %d of the editor's 12 rows, want all: no title bar", rows)
			}
			if first != 0 || last != 11 {
				t.Fatalf("lines %d-%d shown, want 0-11: one a row", first, last)
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
