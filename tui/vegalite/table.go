package vegalite

import (
	"context"
	"math"
	"slices"
	"strings"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
)

// THE TABLE: the tier that takes what the others don't (layers of mixed marks, too many categories
// for bars): the chart's plotted values, after aggregate, stack and sort, a row a value of its
// dimension (x, or y for a horizontal bar) and a column a series of each mark layer:
//
//	Sales by region
//	region   2025   2026
//	───────  ─────  ─────
//	North       12     15
//	South        8   1.25
//
// A stacked value is its own part (V1 − V0), not its pile's top; a normalized one its percent. A
// rule is a line under the table, its values on its axis.

func init() { registerTier(3, "table", layTable) }

// layTable draws ch as its table. A chart with no mark layer (rules alone) has none.
func layTable(_ context.Context, ch *pv.Chart, m Measurer, _ int) (*Drawing, error) {
	var marks []pv.Layer
	for _, l := range ch.Layers {
		if l.Rule == nil {
			marks = append(marks, l)
		}
	}
	if len(marks) == 0 {
		return nil, ErrUnsupported
	}
	dimY := marks[0].Horizontal
	dimAxis, valAxis := ch.X, ch.Y
	if dimY {
		dimAxis, valAxis = ch.Y, ch.X
	}
	dimOf := func(d pv.Datum) pv.Pos {
		if dimY {
			return d.Y
		}
		return d.X
	}
	// the rows: the dimension's values, in its order
	type key struct {
		num float64
		str string
	}
	var rows []key
	if dimAxis.Discrete {
		for _, s := range dimAxis.Domain {
			rows = append(rows, key{str: s})
		}
	} else {
		seen := map[float64]bool{}
		for _, l := range marks {
			for _, d := range l.Data {
				if v := dimOf(d).Num; !seen[v] {
					seen[v] = true
					rows = append(rows, key{num: v})
				}
			}
		}
		slices.SortFunc(rows, func(a, b key) int { return cmpFloat(a.num, b.num) })
	}
	rowOf := func(d pv.Datum) key {
		if dimAxis.Discrete {
			return key{str: dimOf(d).Str}
		}
		return key{num: dimOf(d).Num}
	}
	// the columns: a series of each mark layer, in Series' order
	type column struct {
		head    string
		layer   int
		series  string
		percent bool
		vals    map[key][]float64
	}
	var cols []column
	series := ch.Series
	if len(series) == 0 {
		series = []string{""}
	}
	for li, l := range marks {
		for _, s := range series {
			c := column{layer: li, series: s, percent: l.Normalized, vals: map[key][]float64{}}
			for _, d := range l.Data {
				if d.Series == s {
					c.vals[rowOf(d)] = append(c.vals[rowOf(d)], d.V1-d.V0)
				}
			}
			if len(c.vals) == 0 {
				continue
			}
			c.head = s
			if s == "" {
				c.head = valAxis.Title
			}
			if len(marks) > 1 {
				c.head = l.Mark.String() + ": " + c.head
			}
			cols = append(cols, c)
		}
	}
	// the cells as text: the dimension's value, then each column's, a column's decimals its own
	text := make([][]string, len(rows))
	for i, r := range rows {
		text[i] = append(text[i], dimText(dimAxis, r.num, r.str))
	}
	for _, c := range cols {
		var all []float64
		for _, vs := range c.vals {
			all = append(all, vs...)
		}
		step := stepFor(all, c.percent)
		for i, r := range rows {
			var parts []string
			for _, v := range c.vals[r] {
				if c.percent {
					parts = append(parts, pv.FormatPercent(v, step))
				} else {
					parts = append(parts, pv.FormatNumber(v, step))
				}
			}
			text[i] = append(text[i], strings.Join(parts, ", "))
		}
	}
	heads := []string{dimAxis.Title}
	for _, c := range cols {
		heads = append(heads, c.head)
	}
	widths := make([]int, len(heads))
	for j, h := range heads {
		widths[j] = max(m.StringWidth(h), 1)
		for _, row := range text {
			widths[j] = max(widths[j], m.StringWidth(row[j]))
		}
	}
	const gap = 2
	w := -gap
	for _, cw := range widths {
		w += cw + gap
	}
	var rules []string
	for _, l := range ch.Layers {
		if l.Rule == nil {
			continue
		}
		a := ch.Y
		if l.Rule.OnX {
			a = ch.X
		}
		var at []string
		for _, v := range l.Rule.Values {
			at = append(at, dimText(a, v.Num, v.Str))
		}
		title := a.Title
		if title == "" {
			title = map[bool]string{true: "x", false: "y"}[l.Rule.OnX]
		}
		rules = append(rules, "rule: "+title+" = "+strings.Join(at, ", "))
	}
	var top []string
	if ch.Title != "" {
		top = append(top, ch.Title)
	}
	if ch.Subtitle != "" {
		top = append(top, ch.Subtitle)
	}
	for _, s := range slices.Concat(top, rules) {
		w = max(w, m.StringWidth(s))
	}
	h := len(top) + 2 + len(rows) + len(rules)
	d := newDrawing(max(w, 1), h)
	y := 0
	for i, s := range top {
		role := RoleText
		if i > 0 || ch.Title == "" {
			role = RoleMuted
		}
		d.say(0, y, s, role, m)
		y++
	}
	x := 0
	for j, hd := range heads { // the head row, each title over a rule its column wide
		at := x
		if j > 0 {
			at = x + widths[j] - m.StringWidth(hd) // a value's column: right-aligned, as its values
		}
		d.say(at, y, hd, RoleMuted, m)
		for k := range widths[j] {
			d.put(x+k, y+1, '─', RoleAxis, -1)
		}
		x += widths[j] + gap
	}
	y += 2
	for _, row := range text {
		x := 0
		for j, s := range row {
			at := x
			if j > 0 {
				at = x + widths[j] - m.StringWidth(s)
			}
			d.say(at, y, s, RoleText, m)
			x += widths[j] + gap
		}
		y++
	}
	for _, s := range rules {
		d.say(0, y, s, RoleMuted, m)
		y++
	}
	return d, nil
}

// dimText is a value on axis a as its table shows it: a category, a date, or a number.
func dimText(a pv.Axis, num float64, str string) string {
	switch {
	case a.Discrete:
		return str
	case a.Temporal:
		return pv.FormatTime(num)
	}
	return pv.FormatNumber(num, stepFor([]float64{num}, false))
}

// stepFor is the step that writes every value of vs as it is, to at most four decimals (a
// percent's to at most two): a column's values share their decimals.
func stepFor(vs []float64, percent bool) float64 {
	most := 4
	scale := 1.0
	if percent {
		most, scale = 2, 100
	}
	decimals := 0
	for _, v := range vs {
		v *= scale
		for decimals < most && math.Abs(v*math.Pow10(decimals)-math.Round(v*math.Pow10(decimals))) > 1e-6 {
			decimals++
		}
	}
	return math.Pow10(-decimals) / scale
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
