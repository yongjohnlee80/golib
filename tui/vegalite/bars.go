package vegalite

import (
	"math"
	"slices"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
	"github.com/yongjohnlee80/golib/tui"
)

// BARS IN CELLS: a bar layer's marks drawn in eighths of a cell, inside the plot the frame gives.
// A horizontal bar runs from its datum's V0 to its V1 along the row of its category's band, a
// vertical one up the column of its band, each end cut to the nearest eighth: ▏▎▍▌▋▊▉█ across,
// ▁▂▃▄▅▆▇█ up. Stacked series pile in the chart's series order from zero, each in its own colour;
// where two meet inside a cell, the lower one's eighths are drawn in its colour over the upper
// one's as the cell's background (▍ in a's colour on b's), so a stack reads exact to an eighth.
// Series not stacked stand side by side within the band, in series order. A negative value runs
// back from zero the same way.
//
//	north  │██████▊          12.5
//	south  │███▍▓▓▓▓          7.9     (a stack: ▍ in the first series' colour on the second's)

// barPage is the occupant of a cell's part no bar fills: the page.
const barPage = -1

// barPlot is where a bar layer draws: the frame's plot rect, each category's band (its first cell
// and its length, in rows for horizontal bars, in columns for vertical ones, from the rect's
// origin), and the measure's position (from the rect's left for horizontal bars, from its bottom for
// vertical ones), in cells, fractional.
type barPlot struct {
	rect tui.Rect
	band func(category string) (first, n int, ok bool)
	at   func(v float64) float64
}

// barCell is one cell a bar layer draws: its glyph, its colour (a series' index, or barPage) and
// its background (the same).
type barCell struct {
	x, y   int
	glyph  string
	fg, bg int
}

// eighths are a cell's eighths, the slot nearest the measure's origin first (left, or bottom), each
// the occupant there.
type eighths [8]int

// barsAcross and barsUp are the partial glyphs: n eighths filled from the left, or from the bottom.
var (
	barsAcross = [9]string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}
	barsUp     = [9]string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
)

// layBars draws ly's bars into p: a cell for every cell any bar reaches, sorted by row then column.
func layBars(ly *pv.Layer, series []string, p barPlot) []barCell {
	rank := map[string]int{}
	for i, s := range series {
		rank[s] = i
	}
	n := max(len(series), 1)
	occ := map[[2]int]*eighths{} // by (band cell, measure cell)
	for _, d := range ly.Data {
		cat := d.X.Str
		if ly.Horizontal {
			cat = d.Y.Str
		}
		first, size, ok := p.band(cat)
		if !ok {
			continue
		}
		s := rank[d.Series]
		lo, hi := barThickness(first, size, s, n, ly.Stacked || len(series) <= 1)
		e0, e1 := int(math.Round(p.at(d.V0)*8)), int(math.Round(p.at(d.V1)*8))
		if e0 > e1 {
			e0, e1 = e1, e0
		}
		for b := lo; b < hi; b++ {
			for e := e0; e < e1; e++ {
				k := [2]int{b, floorDiv(e, 8)}
				o := occ[k]
				if o == nil {
					o = &eighths{barPage, barPage, barPage, barPage, barPage, barPage, barPage, barPage}
					occ[k] = o
				}
				o[e-8*floorDiv(e, 8)] = s
			}
		}
	}
	out := make([]barCell, 0, len(occ))
	for k, o := range occ {
		glyph, fg, bg := o.cell(ly.Horizontal)
		c := barCell{glyph: glyph, fg: fg, bg: bg}
		if ly.Horizontal {
			c.x, c.y = p.rect.X+k[1], p.rect.Y+k[0]
		} else {
			c.x, c.y = p.rect.X+k[0], p.rect.Y+p.rect.H-1-k[1]
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b barCell) int {
		if a.y != b.y {
			return a.y - b.y
		}
		return a.x - b.x
	})
	return out
}

// barThickness is the cells across a band that series s's bar takes, [lo, hi): the band's middle
// with a cell's gap from the next band when the band is three cells or more; for series not stacked,
// the series' own share of that, side by side in series order, at least a cell each.
func barThickness(first, size, s, n int, whole bool) (lo, hi int) {
	gap := 0
	if size >= 3 {
		gap = 1
	}
	lo, hi = first, first+size-gap
	if whole || n <= 1 {
		return lo, hi
	}
	w := max((hi-lo)/n, 1)
	lo += s * w
	return lo, lo + w
}

// cell is how a cell of these eighths draws: the run from the origin's side in its occupant's
// colour, the rest (its larger occupant, when more than one shares it) as the background. A cell
// one occupant fills draws whole (█) or blank.
func (o *eighths) cell(across bool) (glyph string, fg, bg int) {
	glyphs := barsUp
	if across {
		glyphs = barsAcross
	}
	a := o[0]
	run := 1
	for run < 8 && o[run] == a {
		run++
	}
	if run == 8 {
		if a == barPage {
			return " ", barPage, barPage
		}
		return glyphs[8], a, a
	}
	// the rest: its most frequent occupant, the nearest one on a tie
	count := map[int]int{}
	b, best := o[run], 0
	for _, x := range o[run:] {
		count[x]++
	}
	for _, x := range o[run:] {
		if count[x] > best {
			b, best = x, count[x]
		}
	}
	return glyphs[run], a, b
}

// floorDiv is a/b rounded down (b > 0), for a measure behind the origin.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// barValue is a value label in the gutter right of horizontal bars: its row in the plot (from the
// plot's top) and its text.
type barValue struct {
	row int
	s   string
}

// barValues are the labels for ly's horizontal bars, by row: a stack's total on its band's row,
// each series' own value on its own row when not stacked; none for a normalized stack (every total
// is the whole). A value shows as many decimals as it has, up to two, its thousands grouped.
func barValues(ly *pv.Layer, series []string, band func(string) (int, int, bool)) []barValue {
	if !ly.Horizontal || ly.Normalized {
		return nil
	}
	rank := map[string]int{}
	for i, s := range series {
		rank[s] = i
	}
	n := max(len(series), 1)
	whole := ly.Stacked || len(series) <= 1
	sum := map[int]float64{}
	var rows []int
	for _, d := range ly.Data {
		first, size, ok := band(d.Y.Str)
		if !ok {
			continue
		}
		lo, _ := barThickness(first, size, rank[d.Series], n, whole)
		if _, seen := sum[lo]; !seen {
			rows = append(rows, lo)
		}
		sum[lo] += d.V1 - d.V0
	}
	slices.Sort(rows)
	out := make([]barValue, 0, len(rows))
	for _, r := range rows {
		out = append(out, barValue{r, barNumber(sum[r])})
	}
	return out
}

// barNumber is v with as many decimals as it has, up to two, as an axis groups its thousands.
func barNumber(v float64) string {
	step := 1.0
	for range 2 {
		if math.Abs(v/step-math.Round(v/step)) < 1e-9 {
			break
		}
		step /= 10
	}
	return pv.FormatNumber(math.Round(v/step)*step, step)
}
