package widget

// THE EDITOR'S ROW MAP — the one owner of every question about how the Editor's lines sit on the
// screen's rows: how many rows a line takes, where a (line, column) is drawn, which column a cell
// holds, which lines a window shows, and how far it scrolls. Paint, the gutter, a click, the
// caret, revealing it, scrolling and gj/gk all ask it, and none of them walks the wrap itself.
//
// It draws the Raw view: each line its wrapped rows (WrapSoft), or one row scrolled sideways by
// the window's left (WrapNone), by the soft-wrap geometry Editor and TextArea share (textbuffer.go).
// A Rendered view drawn by the Editor itself lays blocks here instead (ADR 1794046174 §4.3).

// rowMap is the Editor's lines in a view: the lines, and the view's size, wrap and measure.
type rowMap struct {
	lines []string
	v     wrapView
}

// rowMap is the map over the Editor's lines in its view, measuring as the Editor does.
func (e *Editor) rowMap() rowMap { return rowMap{lines: e.core.buf.textLines, v: e.view()} }

// measuring is m, measuring with measure: a painter measures with its surface.
func (m rowMap) measuring(measure func(string) int) rowMap {
	m.v.measure = measure
	return m
}

// settled is m with its wrap width worked out once, for a walk over many lines.
func (m rowMap) settled() rowMap {
	m.v = m.v.settled(m.lines)
	return m
}

// width is the width text wraps to: the view less its scroll indicator's column, when one shows.
func (m rowMap) width() int { return wrapUsableWidth(m.lines, m.v) }

// scrollable reports whether the lines are taller than the view: the scroll indicator shows.
func (m rowMap) scrollable() bool { return wrapScrollable(m.lines, m.v) }

// maxTop is the scroll range: the greatest first line that still shows the last line's end.
func (m rowMap) maxTop() int { return wrapMaxTop(m.lines, m.v) }

// rowCount is how many rows line ln takes: 1 unwrapped.
func (m rowMap) rowCount(ln int) int { return wrapRowsOfLine(m.lines, ln, m.v) }

// ranges are line ln's rows, each its clusters [from, to) of cs (ln's clusters): one row of them
// all unwrapped.
func (m rowMap) ranges(cs []string) [][2]int {
	if m.v.wrap == WrapNone {
		return [][2]int{{0, len(cs)}}
	}
	return wrapRanges(cs, m.v.usableWidth(m.lines), m.v.measure)
}

// wrapAt is a line's rows (cs its clusters) wrapped to w cells by m's measure: a painter's, at the
// width it paints.
func (m rowMap) wrapAt(cs []string, w int) [][2]int { return wrapRanges(cs, w, m.v.measure) }

// rowOf is (ln, col)'s row within its line and its cell across that row.
func (m rowMap) rowOf(ln, col int) (row, x int) {
	if m.v.wrap == WrapNone {
		return 0, cellsBefore(clusters(m.lines[ln]), col, m.v.measure)
	}
	return wrapPosOf(m.lines, ln, col, m.v)
}

// colAt is the column of line ln under cell x on its row `row` (clamped to the line's rows).
func (m rowMap) colAt(ln, row, x int) int {
	cs := clusters(m.lines[ln])
	rows := m.ranges(cs)
	return colInRow(cs, rows, max(0, min(row, len(rows)-1)), x, m.v.measure)
}

// visible is the lines from top to the last whose first row is in a view h rows tall: top always
// starts at the view's first row, so it is never partly off.
func (m rowMap) visible(top, h int) (first, last int) {
	m = m.settled()
	first = max(0, min(top, len(m.lines)-1))
	last, rows := first, 0
	for i := first; i < len(m.lines) && rows < h; i++ {
		last = i
		rows += wrapRowsOfLine(m.lines, i, m.v)
	}
	return first, last
}

// lowestTop is the first line that keeps line ln in a view h rows tall: top when it already does.
func (m rowMap) lowestTop(top, ln, h int) int { return lowestTop(m.lines, top, ln, h, m.v) }

// reveal is the window, from line top and cell left, moved just enough to show line ln at cell cx.
func (m rowMap) reveal(top, left, ln, cx int) (int, int) {
	return revealCaret(m.lines, m.v, top, left, ln, cx)
}

// caret is where (ln, col), at cell cx of its line, shows in the window from line top and cell left:
// its cell in the text's area, and false when the window doesn't show it.
func (m rowMap) caret(top, left, ln, col, cx int) (x, y int, ok bool) {
	return caretAt(m.lines, m.v, top, left, ln, col, cx)
}

// hit is the source under cell (x, y) of the window from line top and cell left: the exact inverse
// of caret. Past a line's end it lands on the line's last column; below the last line drawn, on the
// last line. A wide cluster is found by the cell it starts at (colAt walks measured widths), and a
// wrapped row's blank tail stays on its own row.
func (m rowMap) hit(top, left, x, y int, colAt func(cs []string, from, end, cells int) int, lastCol func(ln int) int) (ln, col int) {
	lastLn := len(m.lines) - 1
	if m.v.wrap == WrapNone {
		ln = min(top+max(y, 0), lastLn)
		cs := clusters(m.lines[ln])
		return ln, colAt(cs, 0, len(cs), left+max(x, 0))
	}
	remaining := max(y, 0)
	m = m.settled()
	for i := top; i <= lastLn; i++ {
		rows := wrapRowsOfLine(m.lines, i, m.v)
		if remaining < rows || i == lastLn {
			cs := clusters(m.lines[i])
			ranges := wrapRanges(cs, m.v.usable, m.v.measure)
			r := ranges[min(remaining, len(ranges)-1)]
			return i, colAt(cs, r[0], r[1], max(x, 0))
		}
		remaining -= rows
	}
	return lastLn, lastCol(lastLn)
}
