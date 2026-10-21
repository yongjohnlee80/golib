package widget

// rowviewport.go: the arithmetic of a column of rows seen through a window of them — List, Tree,
// the Select popup and DataTable's rows all scroll by it, so a fix to scrolling is made once.
// What a widget counts as its rows and its window height is its own (a header, borders, a filter
// line); these functions take both.

// The rows one wheel notch scrolls, per widget kind: one in a list or a tree, whose rows are what
// the user is choosing among, three in a data table or a scroll view, as terminal pagers scroll. A
// difference between them is named here, never a literal at the call.
const (
	listWheelRows   = 1
	tableWheelRows  = 3
	scrollWheelRows = 3
)

// revealRow is the first row a window of rows rows shows once cursor is in it: top moved just far
// enough, up or down, and never above row 0.
func revealRow(top, cursor, rows int) int {
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

// clampTop is top kept within the rows: no further down than leaves a window of rows rows full of
// the count rows (no blank rows below the last), and never above row 0.
func clampTop(top, count, rows int) int {
	return max(0, min(top, count-rows))
}
