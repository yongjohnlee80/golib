package widget

import "slices"

// A table's columns at a width: the HTML page's and the Markdown cell renderer's tables narrow their
// columns by one rule.

// tableMinColumn is the narrowest a column of a table wider than its room is narrowed to: a column
// narrower than that by nature keeps its width; below it a cell's text breaks into slivers, and the
// table is drawn at its own width and pans instead.
const tableMinColumn = 4

// FitColumns narrows a table's natural column widths to fit avail cells: the widest are capped at one
// width, as wide as avail allows, the narrower keeping theirs, and the cells the cap leaves over go
// to the capped columns, one each from the left. nil when the columns can't narrow to
// tableMinColumn.
func FitColumns(natural []int, avail int) []int {
	if len(natural) == 0 {
		return nil
	}
	capped := func(limit int) int {
		sum := 0
		for _, w := range natural {
			sum += min(w, limit)
		}
		return sum
	}
	lo, hi := tableMinColumn, max(slices.Max(natural), tableMinColumn)
	if capped(lo) > avail {
		return nil
	}
	for lo < hi { // the widest cap that fits
		mid := (lo + hi + 1) / 2
		if capped(mid) <= avail {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	out, left := make([]int, len(natural)), avail-capped(lo)
	for i, w := range natural {
		out[i] = min(w, lo)
		if w > lo && left > 0 {
			out[i]++
			left--
		}
	}
	return out
}
