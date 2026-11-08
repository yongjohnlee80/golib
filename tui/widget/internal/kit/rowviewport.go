package kit

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// The rows one wheel notch scrolls, per widget kind: one in a list or a tree, whose rows are what
// the user is choosing among, three in a data table or a scroll view, as terminal pagers scroll. A
// difference between them is named here, never a literal at the call.
const (
	ListWheelRows   = 1
	TableWheelRows  = 3
	ScrollWheelRows = 3
)

// RevealRow is the first row a window of rows rows shows once cursor is in it: top moved just far
// enough, up or down, and never above row 0.
func RevealRow(top, cursor, rows int) int {
	if rows <= 0 {
		return top
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+rows {
		top = cursor - rows + 1
	}
	return max(top, 0)
}

// ClampTop is top kept within the rows: no further down than leaves a window of rows rows full of
// the count rows (no blank rows below the last), and never above row 0.
func ClampTop(top, count, rows int) int {
	return max(0, min(top, count-rows))
}

// PaintRows paints the window of rows on s: the rows from top that fit its height, of count, each
// in the style styleOf gives it and drawn by draw within w cells (the width left of a scroll
// indicator, which the caller paints where its frame puts it). A row its style MARKS (a background
// or a reversal) fills its width first: a reversed cursor that covered only its letters read as a
// highlighted word, not as the row the cursor is on.
func PaintRows(s tui.Surface, top, count, w int, styleOf func(i int) style.Style, draw func(s tui.Surface, i, y, w int, st style.Style)) {
	for y := 0; y < s.Size().H && top+y < count; y++ {
		i := top + y
		st := styleOf(i)
		_, bg := st.GetBackground()
		if reversed, _ := st.GetReverse(); bg || reversed {
			s.Fill(tui.Rect{X: 0, Y: y, W: w, H: 1}, " ", st)
		}
		draw(s, i, y, w, st)
	}
}

// RowsWidth is the width a window h rows tall gives count rows within w cells: one less while they
// overflow it, for the scroll indicator in the last column, and whether they do.
func RowsWidth(w, h, count int) (rows int, scrollable bool) {
	if count > h {
		return w - 1, true
	}
	return w, false
}
