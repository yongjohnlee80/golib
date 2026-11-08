package text

import "github.com/yongjohnlee80/golib/tui"

// Clusters splits s into grapheme clusters (UAX #29 via tui.Graphemes).
func Clusters(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for c := range tui.Graphemes(s) {
		out = append(out, c)
	}
	return out
}

// CellsBefore is the display width of cs[:i] under the caller's measure
// (Surface.StringWidth in Render, Base.Measure elsewhere).
func CellsBefore(cs []string, i int, measure func(string) int) int {
	w := 0
	for j := 0; j < i && j < len(cs); j++ {
		w += measure(cs[j])
	}
	return w
}

// WrapRanges wraps a cluster slice into rows of at most w cells, breaking
// at spaces when possible and mid-word otherwise. It returns [start, end)
// cluster index ranges per row; the single break space between two
// word-wrapped rows belongs to no row. Empty input yields one empty row.
func WrapRanges(cs []string, w int, measure func(string) int) [][2]int {
	if len(cs) == 0 || w <= 0 {
		return [][2]int{{0, len(cs)}}
	}
	var rows [][2]int
	start, rowW, lastSpace := 0, 0, -1
	i := 0
	for i < len(cs) {
		cw := measure(cs[i])
		if rowW+cw > w && rowW > 0 {
			if lastSpace >= start {
				rows = append(rows, [2]int{start, lastSpace})
				start = lastSpace + 1 // the break space belongs to no row
			} else {
				rows = append(rows, [2]int{start, i})
				start = i
			}
			lastSpace = -1
			rowW = 0
			for j := start; j < i; j++ {
				rowW += measure(cs[j])
			}
			continue
		}
		if cs[i] == " " {
			lastSpace = i
		}
		rowW += cw
		i++
	}
	rows = append(rows, [2]int{start, len(cs)})
	return rows
}
