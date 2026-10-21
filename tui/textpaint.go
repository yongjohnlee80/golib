package tui

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TEXT ON A SURFACE — one painter and one wrapper for core and widgets alike.
//
// The problem: "paint this string" and "wrap this string" were each written several times (the
// too-small screen, the tooltip, the widget package), and the copies disagreed on what a cluster of
// zero width takes and where painting stops. A zero-width space took a column in one copy and none
// in the measurement that sized the box around it, so text painted past its card; painted, such a
// cluster is a width-0 cell, which the grid reads as a wide cell's continuation.
//
// The rule: a cluster takes exactly the columns the width policy gives it (Surface.StringWidth).
// One of width 0 paints nothing and takes no column, so painted text covers its measured width. A
// cluster that does not fit whole is not painted, nor anything after it: never half a wide cell.
// Wrapping measures with the caller's function (Surface.StringWidth in Render, the context's width
// elsewhere), so a box sized from the wrapped lines holds them.
//
// Rejected: clipping by the surface alone (a wide cluster at the edge is dropped, but the column
// count goes on as if it were painted), and giving a zero-width cluster one column (the defect).

// DrawText paints text on row y from column x, cluster by cluster, and returns the column after the
// last one painted. A cluster of zero width is skipped; a cluster that does not fit the surface's
// width whole ends the painting.
func DrawText(s Surface, x, y int, text string, st style.Style) int {
	w := s.Size().W
	for c := range grapheme.Clusters(text) {
		cw := s.StringWidth(c)
		if cw == 0 {
			continue
		}
		if x+cw > w {
			break
		}
		s.SetCell(x, y, c, st)
		x += cw
	}
	return x
}

// WrapText breaks text into lines of at most width columns, as measure counts them: at each of the
// text's own line breaks, at a space where a line can break (the space is dropped there), and inside
// a word too long for a line. A line with nothing in it is one empty line; a width below 1 gives
// each of the text's lines as one empty line.
func WrapText(text string, width int, measure func(string) int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		lines = append(lines, wrapLine(para, width, measure)...)
	}
	return lines
}

// wrapLine wraps one line (no line breaks) into rows of at most w columns.
func wrapLine(s string, w int, measure func(string) int) []string {
	if w <= 0 {
		return []string{""}
	}
	if measure(s) <= w {
		return []string{s}
	}
	var rows []string
	var row strings.Builder
	rowW := 0
	lastSpace := -1 // byte offset in row of the last space a row can break at
	lastSpaceW := 0 // the row's width before that space
	for c := range grapheme.Clusters(s) {
		cw := measure(c)
		if rowW+cw > w && rowW > 0 {
			if lastSpace >= 0 && lastSpace < row.Len() {
				full := row.String()
				rows = append(rows, full[:lastSpace])
				rest := strings.TrimPrefix(full[lastSpace:], " ")
				row.Reset()
				row.WriteString(rest)
				rowW = rowW - lastSpaceW - 1
				if rowW < 0 {
					rowW = measure(rest)
				}
			} else {
				rows = append(rows, row.String())
				row.Reset()
				rowW = 0
			}
			lastSpace = -1
			if c == " " && row.Len() == 0 {
				continue // a row never starts with the space it broke at
			}
		}
		if c == " " {
			lastSpace = row.Len()
			lastSpaceW = rowW
		}
		row.WriteString(c)
		rowW += cw
	}
	return append(rows, row.String())
}
