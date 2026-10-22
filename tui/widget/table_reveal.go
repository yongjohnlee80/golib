package widget

// A TABLE'S REVEAL — the scroll that brings a cell into sight, for any table view: DataTable's
// cells and a native table's pixels alike, each with its own measure of what fits.

// TableRevealRow is the first row a table body of body rows shows once row is in it: top kept
// within the count rows (rows gone leave no blank below the last), then moved just far enough, up
// or down, and never above row 0.
func TableRevealRow(top, row, count, body int) int {
	return revealRow(clampTop(top, count, body), row, body)
}

// TableRevealField is the first field shown once field col shows whole: left moved back to col when
// col is before it, or on, one field at a time, while fits(left, col) says the fields left..col do
// not fit the view. fits is the view's own measure: cells, or pixels.
func TableRevealField(left, col int, fits func(from, to int) bool) int {
	if col < left {
		left = col
	}
	for left < col && !fits(left, col) {
		left++
	}
	return left
}
