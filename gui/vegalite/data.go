package vegalite

import (
	"fmt"
	"math"
	"slices"
	"sort"
)

// THE DATA, before any pixel: which channel is the dimension (a bar's categories, a line's x) and
// which the measure, the rows read on them (aggregated when a channel aggregates), stacked when
// they pile, and each scale's domain.

// pos is a value on a position channel: num on a continuous scale, str on a discrete one.
type pos struct {
	num float64
	str string
}

// datum is a mark's data: its two positions, its series (the colour's value; "" for none), and the
// measure's base when it piles (v0 under v1).
type datum struct {
	x, y   pos
	series string
	v0, v1 float64 // the measure from v0 to v1: stacked, or from the baseline (v0 0) to the value
}

// plan is a chart's data on its channels: what layout draws.
type plan struct {
	c         *chart
	dimY      bool // the dimension is y: a horizontal bar
	data      []datum
	series    []string // the colour's domain, in order; nil: one series
	stacked   bool
	normalize bool
	xs, ys    axisPlan
}

// axisPlan is a position channel's scale: a discrete domain, or a continuous one.
type axisPlan struct {
	ch       *channel
	discrete bool
	band     bool // a band scale: a bar's categories; else a point scale when discrete
	domain   []string
	lo, hi   float64
	temporal bool
	nice     bool
}

// value is the measure channel: y, or x when the dimension is y.
func (p *plan) value() *channel {
	if p.dimY {
		return p.c.x
	}
	return p.c.y
}

// newPlan checks c's channels against its mark and reads its data on them.
func newPlan(c *chart) (*plan, error) {
	p := &plan{c: c}
	x, y := c.x, c.y
	if x == nil || y == nil || x.value != nil || y.value != nil {
		return nil, unsupported("a %s mark without both x and y fields", c.mark.kind)
	}
	if x.aggregate != "" && y.aggregate != "" {
		return nil, unsupported("x and y both aggregated")
	}
	if col := c.color; col != nil && col.value == nil && (col.typ != typeNominal || col.aggregate != "") {
		return nil, unsupported("a %v colour", col.typ)
	}
	switch c.mark.kind {
	case markBar:
		switch {
		case x.typ.discrete() && y.typ == typeQuantitative:
		case y.typ.discrete() && x.typ == typeQuantitative:
			p.dimY = true
		default:
			return nil, unsupported("a bar of %v x and %v y", x.typ, y.typ)
		}
	case markLine, markArea:
		if y.typ != typeQuantitative {
			return nil, unsupported("a %s with a %v y", c.mark.kind, y.typ)
		}
		if x.aggregate != "" {
			return nil, unsupported("a %s with x aggregated", c.mark.kind)
		}
	}
	if p.value().aggregate == "" && p.dim().aggregate != "" {
		return nil, unsupported("the dimension aggregated")
	}
	if err := p.read(); err != nil {
		return nil, err
	}
	if err := p.domains(); err != nil {
		return nil, err
	}
	return p, nil
}

// dim is the dimension channel: x, or y for a horizontal bar.
func (p *plan) dim() *channel {
	if p.dimY {
		return p.c.y
	}
	return p.c.x
}

// posOf reads a row's value on ch: false when a continuous channel's value is not one.
func posOf(ch *channel, row map[string]any) (pos, bool) {
	v := row[ch.field]
	switch ch.typ {
	case typeQuantitative:
		f, ok := numberOf(v)
		return pos{num: f}, ok
	case typeTemporal:
		f, ok := timeOf(v)
		return pos{num: f}, ok
	}
	return pos{str: textOf(v)}, true
}

// seriesOf is a row's series: its colour field's value, or "".
func (p *plan) seriesOf(row map[string]any) string {
	if col := p.c.color; col != nil && col.value == nil {
		return textOf(row[col.field])
	}
	return ""
}

// read reads the rows into data: a datum a row, or a datum a group when the measure aggregates
// (grouped by the dimension and the series).
func (p *plan) read() error {
	val, dim := p.value(), p.dim()
	set := func(d *datum, v pos, dimension pos) {
		if p.dimY {
			d.x, d.y = v, dimension
		} else {
			d.x, d.y = dimension, v
		}
	}
	if val.aggregate == "" {
		for _, row := range p.c.rows {
			xv, okx := posOf(p.c.x, row)
			yv, oky := posOf(p.c.y, row)
			if !okx || !oky {
				continue // Vega-Lite's invalid values: left out
			}
			p.data = append(p.data, datum{x: xv, y: yv, series: p.seriesOf(row)})
		}
		return nil
	}
	type key struct {
		dim    pos
		series string
	}
	var order []key
	groups := map[key][]float64{}
	counts := map[key]int{}
	for _, row := range p.c.rows {
		dv, ok := posOf(dim, row)
		if !ok {
			continue
		}
		k := key{dv, p.seriesOf(row)}
		if _, seen := counts[k]; !seen {
			order = append(order, k)
		}
		counts[k]++
		if val.aggregate != "count" {
			if f, ok := numberOf(row[val.field]); ok {
				groups[k] = append(groups[k], f)
			}
		}
	}
	for _, k := range order {
		v, ok := aggregate(val.aggregate, groups[k], counts[k])
		if !ok {
			continue
		}
		var d datum
		set(&d, pos{num: v}, k.dim)
		d.series = k.series
		p.data = append(p.data, d)
	}
	return nil
}

// aggregate is op over vs (count: the group's rows, n). ok is false for a group with no value an
// op other than count and sum can be of.
func aggregate(op string, vs []float64, n int) (float64, bool) {
	switch op {
	case "count":
		return float64(n), true
	case "sum":
		s := 0.0
		for _, v := range vs {
			s += v
		}
		return s, true
	}
	if len(vs) == 0 {
		return 0, false
	}
	switch op {
	case "mean":
		s := 0.0
		for _, v := range vs {
			s += v
		}
		return s / float64(len(vs)), true
	case "min":
		return slices.Min(vs), true
	case "max":
		return slices.Max(vs), true
	}
	s := slices.Clone(vs) // median
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2], true
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2, true
}

// domains orders the discrete domains and the series, piles a bar's or an area's values, and finds
// the continuous domains.
func (p *plan) domains() error {
	c := p.c
	p.xs = axisPlan{ch: c.x, discrete: c.x.typ.discrete(), temporal: c.x.typ == typeTemporal}
	p.ys = axisPlan{ch: c.y, discrete: c.y.typ.discrete(), temporal: c.y.typ == typeTemporal}
	if c.mark.kind == markBar {
		p.xs.band, p.ys.band = p.xs.discrete, p.ys.discrete
	}
	if col := c.color; col != nil && col.value == nil {
		var vals []string
		for _, d := range p.data {
			vals = append(vals, d.series)
		}
		p.series = order(vals, col, nil)
		if len(p.series) > maxSeries {
			return fmt.Errorf("%w: %d series (at most %d)", ErrTooLarge, len(p.series), maxSeries)
		}
		p.data = slices.DeleteFunc(p.data, func(d datum) bool { return !slices.Contains(p.series, d.series) })
	}
	for _, a := range []*axisPlan{&p.xs, &p.ys} {
		if !a.discrete {
			continue
		}
		isX := a == &p.xs
		var vals []string
		by := map[string]float64{}
		for _, d := range p.data {
			v, other := d.y, d.x
			if isX {
				v, other = d.x, d.y
			}
			vals = append(vals, v.str)
			by[v.str] += other.num
		}
		a.domain = order(vals, a.ch, by)
		if len(a.domain) > maxBands {
			return fmt.Errorf("%w: %d categories (at most %d)", ErrTooLarge, len(a.domain), maxBands)
		}
		p.data = slices.DeleteFunc(p.data, func(d datum) bool {
			v := d.y.str
			if isX {
				v = d.x.str
			}
			return !slices.Contains(a.domain, v)
		})
	}
	p.stack()
	return p.continuous()
}

// order is a discrete channel's domain from its values (in the data's order): sorted as ch says,
// or ch's explicit domain. by is each value's measure, for a sort by the other channel.
func order(vals []string, ch *channel, by map[string]float64) []string {
	if len(ch.domain) > 0 {
		out := make([]string, 0, len(ch.domain))
		for _, v := range ch.domain {
			out = append(out, textOf(v))
		}
		return out
	}
	var seen []string
	set := map[string]bool{}
	for _, v := range vals {
		if !set[v] {
			set[v] = true
			seen = append(seen, v)
		}
	}
	switch ch.sort {
	case sortNone:
	case sortList:
		var out []string
		listed := map[string]bool{}
		for _, v := range ch.sortList {
			s := textOf(v)
			if set[s] && !listed[s] {
				listed[s] = true
				out = append(out, s)
			}
		}
		for _, v := range seen {
			if !listed[v] {
				out = append(out, v)
			}
		}
		return out
	case sortByOther:
		sort.SliceStable(seen, func(i, j int) bool {
			if ch.sortDesc {
				return by[seen[i]] > by[seen[j]]
			}
			return by[seen[i]] < by[seen[j]]
		})
	default:
		sort.SliceStable(seen, func(i, j int) bool {
			if ch.sort == sortDescending {
				return naturalLess(seen[j], seen[i])
			}
			return naturalLess(seen[i], seen[j])
		})
	}
	return seen
}

// naturalLess orders two discrete values as Vega-Lite's ascending sort does: as numbers when both
// are, else as text.
func naturalLess(a, b string) bool {
	fa, oka := numberOf(a)
	fb, okb := numberOf(b)
	if oka && okb {
		return fa < fb
	}
	return a < b
}

// stack sets each datum's v0 and v1: piled per category in the series' order (negative values
// down from zero), or from zero alone. A bar or an area piles, by default, when a colour splits
// it into series.
func (p *plan) stack() {
	val := p.value()
	mode := val.stack
	if mode == stackAuto {
		mode = stackNone
		if (p.c.mark.kind == markBar || p.c.mark.kind == markArea) && p.series != nil {
			mode = stackZero
		}
	}
	if p.c.mark.kind != markBar && p.c.mark.kind != markArea {
		mode = stackNone
	}
	measure := func(d *datum) float64 {
		if p.dimY {
			return d.x.num
		}
		return d.y.num
	}
	dimKey := func(d *datum) pos {
		if p.dimY {
			return d.y
		}
		return d.x
	}
	for i := range p.data {
		p.data[i].v1 = measure(&p.data[i])
	}
	if mode == stackNone {
		return
	}
	p.stacked, p.normalize = true, mode == stackNormalize
	rank := map[string]int{}
	for i, s := range p.series {
		rank[s] = i
	}
	idx := make([]int, len(p.data))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return rank[p.data[idx[a]].series] < rank[p.data[idx[b]].series] })
	up, down, total := map[pos]float64{}, map[pos]float64{}, map[pos]float64{}
	for _, i := range idx {
		d := &p.data[i]
		k := dimKey(d)
		v := d.v1
		total[k] += math.Abs(v)
		if v >= 0 {
			d.v0, d.v1 = up[k], up[k]+v
			up[k] = d.v1
		} else {
			d.v0, d.v1 = down[k], down[k]+v
			down[k] = d.v1
		}
	}
	if p.normalize {
		for i := range p.data {
			d := &p.data[i]
			if t := total[dimKey(d)]; t > 0 {
				d.v0, d.v1 = d.v0/t, d.v1/t
			}
		}
	}
}

// continuous finds each continuous scale's domain: its data's extent (a pile's ends for the
// measure), with zero for a quantitative one unless the scale says not, or the scale's explicit
// domain.
func (p *plan) continuous() error {
	for _, a := range []*axisPlan{&p.xs, &p.ys} {
		if a.discrete {
			continue
		}
		isX := a == &p.xs
		isValue := p.dimY == isX
		ch := a.ch
		if len(ch.domain) > 0 {
			if len(ch.domain) != 2 {
				return unsupported("a continuous domain of %d values", len(ch.domain))
			}
			read := numberOf
			if a.temporal {
				read = timeOf
			}
			lo, ok1 := read(ch.domain[0])
			hi, ok2 := read(ch.domain[1])
			if !ok1 || !ok2 {
				return unsupported("the domain %v", ch.domain)
			}
			a.lo, a.hi = lo, hi
			a.nice = ch.nice != nil && *ch.nice
		} else {
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, d := range p.data {
				vs := []float64{d.y.num}
				if isX {
					vs[0] = d.x.num
				}
				if isValue {
					vs = []float64{d.v0, d.v1}
				}
				for _, v := range vs {
					lo, hi = math.Min(lo, v), math.Max(hi, v)
				}
			}
			if lo > hi { // no data
				lo, hi = 0, 1
			}
			if !a.temporal && (ch.zero == nil || *ch.zero) { // Vega-Lite's default for a quantitative x or y
				lo, hi = math.Min(lo, 0), math.Max(hi, 0)
			}
			if p.normalize && isValue {
				lo, hi = math.Min(lo, 0), math.Max(hi, 1)
			}
			a.lo, a.hi = lo, hi
			a.nice = !a.temporal && (ch.nice == nil || *ch.nice)
		}
		if a.lo == a.hi {
			if a.temporal {
				a.lo, a.hi = a.lo-msDay, a.hi+msDay
			} else {
				a.lo, a.hi = a.lo-1, a.hi+1
			}
		}
	}
	return nil
}

func (k markKind) String() string {
	return [...]string{"bar", "line", "point", "area"}[k]
}
