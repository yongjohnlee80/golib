package vegalite

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/yongjohnlee80/golib/parse/csscolor"
)

// THE DATA, before any pixel: which channel is the dimension (a bar's categories, a line's x) and
// which the measure, the rows read on them (aggregated when a channel aggregates), stacked when
// they pile, and each scale's domain.

// Pos is a value on a position channel: Num on a continuous scale (Unix milliseconds on a temporal
// one), Str on a discrete one.
type Pos struct {
	Num float64
	Str string
}

// Datum is a mark's data: its two positions, its series (the colour's value; "" for none), and the
// measure from V0 to V1: stacked, or from the baseline (V0 0) to the value.
type Datum struct {
	X, Y   Pos
	Series string
	Shape  string // the shape field's value; "" without one
	V0, V1 float64
}

// Plan is a chart's data on its channels: what layout draws.
type Plan struct {
	Layer     *LayerSpec
	DimY      bool // the dimension is y: a horizontal bar
	Data      []Datum
	Series    []string // the colour's domain, in order; nil: one series
	shapes    []string // the shape field's domain, in order; nil: none
	Stacked   bool
	Normalize bool
	Xs, Ys    AxisPlan
}

// AxisPlan is a position channel's scale: a discrete domain, or a continuous one.
type AxisPlan struct {
	Channel  *Channel
	Discrete bool
	Band     bool // a band scale: a bar's categories; else a point scale when discrete
	Domain   []string
	Lo, Hi   float64
	Temporal bool
	Nice     bool
}

// Value is the measure channel: y, or x when the dimension is y.
func (p *Plan) Value() *Channel {
	if p.DimY {
		return p.Layer.X
	}
	return p.Layer.Y
}

// NewPlan checks c's channels against its mark and reads its data on them.
func NewPlan(c *LayerSpec) (*Plan, error) {
	p := &Plan{Layer: c}
	x, y := c.X, c.Y
	if x == nil || y == nil || x.Value != nil || y.Value != nil {
		return nil, Unsupported("a %s mark without both x and y fields", c.Mark.Kind)
	}
	if x.isDatum || y.isDatum {
		return nil, Unsupported("a datum in a %s mark", c.Mark.Kind)
	}
	if x.Aggregate != AggNone && y.Aggregate != AggNone {
		return nil, Unsupported("x and y both aggregated")
	}
	if col := c.Color; col != nil && col.Value == nil && (col.Type != typeNominal || col.Aggregate != AggNone) {
		return nil, Unsupported("a %v colour", col.Type)
	}
	def := marks[c.Mark.Kind]
	if def.check != nil {
		if err := def.check(p); err != nil {
			return nil, err
		}
	}
	if c.Shape != nil && !def.shapes {
		return nil, Unsupported("a shape on a %s mark", c.Mark.Kind)
	}
	if sh := c.Shape; sh != nil && sh.Shape == "" && (!sh.Type.Discrete() || sh.Aggregate != AggNone) {
		return nil, Unsupported("a %v shape", sh.Type)
	}
	if p.Value().Aggregate == AggNone && p.dim().Aggregate != AggNone {
		return nil, Unsupported("the dimension aggregated")
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
func (p *Plan) dim() *Channel {
	if p.DimY {
		return p.Layer.Y
	}
	return p.Layer.X
}

// posOf reads a row's value on ch: false when a continuous channel's value is not one.
func posOf(ch *Channel, row map[string]any) (Pos, bool) {
	return posOfValue(ch, row[ch.Field])
}

// posOfValue reads v as a value on ch.
func posOfValue(ch *Channel, v any) (Pos, bool) {
	switch ch.Type {
	case TypeQuantitative:
		f, ok := numberOf(v)
		return Pos{Num: f}, ok
	case TypeTemporal:
		f, ok := TimeOf(v)
		return Pos{Num: f}, ok
	}
	return Pos{Str: textOf(v)}, true
}

// shapeOf is a row's shape: its shape field's value, or "".
func (p *Plan) shapeOf(row map[string]any) string {
	if sh := p.Layer.Shape; sh != nil && sh.Shape == "" {
		return textOf(row[sh.Field])
	}
	return ""
}

// seriesOf is a row's series: its colour field's value, or "".
func (p *Plan) seriesOf(row map[string]any) string {
	if col := p.Layer.Color; col != nil && col.Value == nil {
		return textOf(row[col.Field])
	}
	return ""
}

// read reads the rows into data: a datum a row, or a datum a group when the measure aggregates
// (grouped by the dimension and the series).
func (p *Plan) read() error {
	val, dim := p.Value(), p.dim()
	set := func(d *Datum, v Pos, dimension Pos) {
		if p.DimY {
			d.X, d.Y = v, dimension
		} else {
			d.X, d.Y = dimension, v
		}
	}
	if val.Aggregate == AggNone {
		for _, row := range p.Layer.rows {
			xv, okx := posOf(p.Layer.X, row)
			yv, oky := posOf(p.Layer.Y, row)
			if !okx || !oky {
				continue // Vega-Lite's invalid values: left out
			}
			p.Data = append(p.Data, Datum{X: xv, Y: yv, Series: p.seriesOf(row), Shape: p.shapeOf(row)})
		}
		return nil
	}
	type key struct {
		dim           Pos
		series, shape string
	}
	var order []key
	groups := map[key][]float64{}
	counts := map[key]int{}
	for _, row := range p.Layer.rows {
		dv, ok := posOf(dim, row)
		if !ok {
			continue
		}
		k := key{dv, p.seriesOf(row), p.shapeOf(row)}
		if _, seen := counts[k]; !seen {
			order = append(order, k)
		}
		counts[k]++
		if val.Aggregate != AggCount {
			if f, ok := numberOf(row[val.Field]); ok {
				groups[k] = append(groups[k], f)
			}
		}
	}
	for _, k := range order {
		v, ok := aggregate(val.Aggregate, groups[k], counts[k])
		if !ok {
			continue
		}
		var d Datum
		set(&d, Pos{Num: v}, k.dim)
		d.Series, d.Shape = k.series, k.shape
		p.Data = append(p.Data, d)
	}
	return nil
}

// aggregate is op over vs (count: the group's rows, n). ok is false for a group with no value an
// op other than count and sum can be of.
func aggregate(op Aggregation, vs []float64, n int) (float64, bool) {
	switch op {
	case AggCount:
		return float64(n), true
	case AggSum:
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
	case AggMean:
		s := 0.0
		for _, v := range vs {
			s += v
		}
		return s / float64(len(vs)), true
	case AggMin:
		return slices.Min(vs), true
	case AggMax:
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
func (p *Plan) domains() error {
	c := p.Layer
	p.Xs = AxisPlan{Channel: c.X, Discrete: c.X.Type.Discrete(), Temporal: c.X.Type == TypeTemporal}
	p.Ys = AxisPlan{Channel: c.Y, Discrete: c.Y.Type.Discrete(), Temporal: c.Y.Type == TypeTemporal}
	if marks[c.Mark.Kind].band {
		p.Xs.Band, p.Ys.Band = p.Xs.Discrete, p.Ys.Discrete
	}
	var err error
	if col := c.Color; col != nil && col.Value == nil {
		if p.Series, err = p.discreteDomain(col, func(d Datum) string { return d.Series }, nil, MaxSeries, "series"); err != nil {
			return err
		}
	}
	if sh := c.Shape; sh != nil && sh.Shape == "" {
		if p.shapes, err = p.discreteDomain(sh, func(d Datum) string { return d.Shape }, nil, MaxSeries, "shapes"); err != nil {
			return err
		}
	}
	for _, a := range []*AxisPlan{&p.Xs, &p.Ys} {
		if !a.Discrete {
			continue
		}
		at, other := func(d Datum) Pos { return d.Y }, func(d Datum) Pos { return d.X }
		if a == &p.Xs {
			at, other = other, at
		}
		by := map[string]float64{}
		for _, d := range p.Data {
			by[at(d).Str] += other(d).Num
		}
		if a.Channel.sort == sortByField {
			by = p.sortKeys(a.Channel)
		}
		if a.Domain, err = p.discreteDomain(a.Channel, func(d Datum) string { return at(d).Str }, by, MaxBands, "categories"); err != nil {
			return err
		}
	}
	p.stack()
	return p.continuous()
}

// discreteDomain is ch's domain over the data's values (of each datum), ordered as ch says (by:
// each value's measure, for a sort by the other channel), refused past limit; the data outside it
// is dropped. what names the values in the refusal.
func (p *Plan) discreteDomain(ch *Channel, of func(Datum) string, by map[string]float64, limit int, what string) ([]string, error) {
	vals := make([]string, 0, len(p.Data))
	for _, d := range p.Data {
		vals = append(vals, of(d))
	}
	dom := order(vals, ch, by)
	if len(dom) > limit {
		return nil, fmt.Errorf("%w: %d %s (at most %d)", ErrTooLarge, len(dom), what, limit)
	}
	p.Data = slices.DeleteFunc(p.Data, func(d Datum) bool { return !slices.Contains(dom, of(d)) })
	return dom, nil
}

// order is a discrete channel's domain from its values (in the data's order): sorted as ch says,
// or ch's explicit domain. by is each value's measure, for a sort by the other channel.
func order(vals []string, ch *Channel, by map[string]float64) []string {
	if len(ch.Domain) > 0 {
		out := make([]string, 0, len(ch.Domain))
		for _, v := range ch.Domain {
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
	case sortByOther, sortByField:
		// a category with no key (its sort field has no value it can be aggregated from) goes last
		sort.SliceStable(seen, func(i, j int) bool {
			a, okA := by[seen[i]]
			b, okB := by[seen[j]]
			switch {
			case okA != okB:
				return okA
			case ch.sortDesc:
				return a > b
			}
			return a < b
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

// sortKeys are ch's categories' keys for a sort by another field: that field's aggregate over each
// category's rows, by the sort's op or Vega-Lite's default, sum in a stacked plot and min otherwise.
func (p *Plan) sortKeys(ch *Channel) map[string]float64 {
	op := ch.sortOp
	if op == AggNone {
		op = AggMin
		if p.stacking() != stackNone {
			op = AggSum
		}
	}
	vs := map[string][]float64{}
	n := map[string]int{}
	for _, row := range p.Layer.rows {
		v, ok := posOf(ch, row)
		if !ok {
			continue
		}
		n[v.Str]++
		if f, ok := numberOf(row[ch.sortField]); ok {
			vs[v.Str] = append(vs[v.Str], f)
		}
	}
	out := map[string]float64{}
	for cat, count := range n {
		if k, ok := aggregate(op, vs[cat], count); ok {
			out[cat] = k
		}
	}
	return out
}

// stacking is how the measure piles: the value channel's stack, or Vega-Lite's default (a mark
// that stacks, a bar or an area, split into series piles from zero); other marks never pile.
func (p *Plan) stacking() stackMode {
	if !marks[p.Layer.Mark.Kind].stacks {
		return stackNone
	}
	mode := p.Value().stack
	if mode == stackAuto {
		mode = stackNone
		if p.Layer.Color != nil && p.Layer.Color.Value == nil {
			mode = stackZero
		}
	}
	return mode
}

// stack sets each datum's v0 and v1: piled per category in the series' order (negative values
// down from zero), or from zero alone. A bar or an area piles, by default, when a colour splits
// it into series.
func (p *Plan) stack() {
	mode := p.stacking()
	measure := func(d *Datum) float64 {
		if p.DimY {
			return d.X.Num
		}
		return d.Y.Num
	}
	dimKey := func(d *Datum) Pos {
		if p.DimY {
			return d.Y
		}
		return d.X
	}
	for i := range p.Data {
		p.Data[i].V1 = measure(&p.Data[i])
	}
	if mode == stackNone {
		return
	}
	p.Stacked, p.Normalize = true, mode == stackNormalize
	rank := map[string]int{}
	for i, s := range p.Series {
		rank[s] = i
	}
	idx := make([]int, len(p.Data))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return rank[p.Data[idx[a]].Series] < rank[p.Data[idx[b]].Series] })
	up, down, total := map[Pos]float64{}, map[Pos]float64{}, map[Pos]float64{}
	for _, i := range idx {
		d := &p.Data[i]
		k := dimKey(d)
		v := d.V1
		total[k] += math.Abs(v)
		if v >= 0 {
			d.V0, d.V1 = up[k], up[k]+v
			up[k] = d.V1
		} else {
			d.V0, d.V1 = down[k], down[k]+v
			down[k] = d.V1
		}
	}
	if p.Normalize {
		for i := range p.Data {
			d := &p.Data[i]
			if t := total[dimKey(d)]; t > 0 {
				d.V0, d.V1 = d.V0/t, d.V1/t
			}
		}
	}
}

// continuous finds each continuous scale's domain: its data's extent (a pile's ends for the
// measure), with zero for a quantitative one unless the scale says not, or the scale's explicit
// domain.
func (p *Plan) continuous() error {
	for _, a := range []*AxisPlan{&p.Xs, &p.Ys} {
		if a.Discrete {
			continue
		}
		isX := a == &p.Xs
		isValue := p.DimY == isX
		ch := a.Channel
		if len(ch.Domain) > 0 {
			if len(ch.Domain) != 2 {
				return Unsupported("a continuous domain of %d values", len(ch.Domain))
			}
			read := numberOf
			if a.Temporal {
				read = TimeOf
			}
			lo, ok1 := read(ch.Domain[0])
			hi, ok2 := read(ch.Domain[1])
			if !ok1 || !ok2 {
				return Unsupported("the domain %v", ch.Domain)
			}
			a.Lo, a.Hi = lo, hi
			a.Nice = ch.nice != nil && *ch.nice
		} else {
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, d := range p.Data {
				vs := []float64{d.Y.Num}
				if isX {
					vs[0] = d.X.Num
				}
				if isValue && p.Stacked { // a pile's base; unstacked, the base is the scale's zero, if any
					vs = []float64{d.V0, d.V1}
				}
				for _, v := range vs {
					lo, hi = math.Min(lo, v), math.Max(hi, v)
				}
			}
			if lo > hi { // no data
				lo, hi = 0, 1
			}
			if !a.Temporal && (ch.zero == nil || *ch.zero) { // Vega-Lite's default for a quantitative x or y
				lo, hi = math.Min(lo, 0), math.Max(hi, 0)
			}
			if p.Normalize && isValue {
				lo, hi = math.Min(lo, 0), math.Max(hi, 1)
			}
			a.Lo, a.Hi = lo, hi
			a.Nice = !a.Temporal && (ch.nice == nil || *ch.nice)
		}
		if a.Lo == a.Hi {
			if a.Temporal {
				a.Lo, a.Hi = a.Lo-msDay, a.Hi+msDay
			} else {
				a.Lo, a.Hi = a.Lo-1, a.Hi+1
			}
		}
	}
	return nil
}

func (k MarkKind) String() string {
	return [...]string{"bar", "line", "point", "area", "rule"}[k]
}

// RulePlan is a rule layer: a line across the plot at each of its values on one axis, a datum's
// one value or its field's distinct ones.
type RulePlan struct {
	Layer   *LayerSpec
	OnX     bool // lines at x values, top to bottom; else at y values, left to right
	Channel *Channel
	Vals    []Pos
}

// NewRule checks a rule layer's channels and reads its values: an aggregated channel is one value,
// over every row (a mean line). A rule of x and y both (a segment per row, x2 or y2's, or one per
// band) and one coloured by a field are unsupported.
func NewRule(c *LayerSpec) (*RulePlan, error) {
	if (c.X == nil) == (c.Y == nil) {
		return nil, Unsupported("a rule with both or neither of x and y")
	}
	r := &RulePlan{Layer: c, OnX: c.X != nil, Channel: c.Y}
	if r.OnX {
		r.Channel = c.X
	}
	switch {
	case r.Channel.Value != nil:
		return nil, Unsupported("a rule at a value")
	case c.Color != nil && c.Color.Value == nil:
		return nil, Unsupported("a rule coloured by a field")
	}
	if r.Channel.isDatum {
		v, ok := posOfValue(r.Channel, r.Channel.datum)
		if !ok {
			return nil, Unsupported("the datum %v as %v", r.Channel.datum, r.Channel.Type)
		}
		r.Vals = []Pos{v}
		return r, nil
	}
	if r.Channel.Aggregate != AggNone {
		var vs []float64
		for _, row := range c.rows {
			if f, ok := numberOf(row[r.Channel.Field]); ok {
				vs = append(vs, f)
			}
		}
		if v, ok := aggregate(r.Channel.Aggregate, vs, len(c.rows)); ok {
			r.Vals = []Pos{{Num: v}}
		}
		return r, nil
	}
	seen := map[Pos]bool{}
	for _, row := range c.rows {
		if v, ok := posOf(r.Channel, row); ok && !seen[v] {
			seen[v] = true
			r.Vals = append(r.Vals, v)
		}
	}
	return r, nil
}

// Layered is a spec's layers on shared scales: one x domain and one y domain over every layer, and
// one colour domain, as Vega-Lite resolves a layer's scales by default.
type Layered struct {
	Spec    *Spec
	Order   []LayerPlan // in paint order
	Base    *Plan       // the first mark layer: the axes' titles and guides
	Xs, Ys  AxisPlan
	Series  []string
	Shapes  []string
	Colors  []csscolor.Color // the colour scale's range, the first layer's that has one; nil: the theme's
	Symbols []string         // the shape scale's range; nil: Vega's
}

// NewLayered reads every layer and merges their scales. Layers whose axes differ in type, and a
// spec of rules alone, are unsupported.
func NewLayered(sp *Spec) (*Layered, error) {
	l := &Layered{Spec: sp}
	var plans []*Plan
	for _, c := range sp.Layers {
		if read := marks[c.Mark.Kind].layer; read != nil {
			r, err := read(c)
			if err != nil {
				return nil, err
			}
			l.Order = append(l.Order, r)
			continue
		}
		p, err := NewPlan(c)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
		l.Order = append(l.Order, p)
	}
	if len(plans) == 0 {
		return nil, Unsupported("a chart of rules alone")
	}
	l.Base = plans[0]
	l.Xs, l.Ys = l.Base.Xs, l.Base.Ys
	l.Xs.Domain, l.Ys.Domain = slices.Clone(l.Xs.Domain), slices.Clone(l.Ys.Domain)
	for _, p := range plans {
		if err := mergeAxis(&l.Xs, &p.Xs, "x"); err != nil {
			return nil, err
		}
		if err := mergeAxis(&l.Ys, &p.Ys, "y"); err != nil {
			return nil, err
		}
		for _, s := range p.Series {
			if !slices.Contains(l.Series, s) {
				l.Series = append(l.Series, s)
			}
		}
		for _, s := range p.shapes {
			if !slices.Contains(l.Shapes, s) {
				l.Shapes = append(l.Shapes, s)
			}
		}
		if col := p.Layer.Color; col != nil && len(col.colors) > 0 && l.Colors == nil {
			l.Colors = col.colors
		}
		if sh := p.Layer.Shape; sh != nil && len(sh.shapes) > 0 && l.Symbols == nil {
			l.Symbols = sh.shapes
		}
	}
	for _, o := range l.Order {
		if err := o.Widen(l); err != nil {
			return nil, err
		}
	}
	if len(l.Xs.Domain) > MaxBands || len(l.Ys.Domain) > MaxBands {
		return nil, fmt.Errorf("%w: more than %d categories", ErrTooLarge, MaxBands)
	}
	if len(l.Series) > MaxSeries {
		return nil, fmt.Errorf("%w: %d series (at most %d)", ErrTooLarge, len(l.Series), MaxSeries)
	}
	return l, nil
}

// mergeAxis widens dst, a shared scale, by src, a layer's: its categories after dst's, or its
// extent.
func mergeAxis(dst, src *AxisPlan, name string) error {
	if dst.Discrete != src.Discrete || dst.Temporal != src.Temporal {
		return Unsupported("layers with %s axes of different types", name)
	}
	dst.Band = dst.Band || src.Band
	if dst.Discrete {
		for _, v := range src.Domain {
			if !slices.Contains(dst.Domain, v) {
				dst.Domain = append(dst.Domain, v)
			}
		}
		return nil
	}
	if len(dst.Channel.Domain) == 0 {
		dst.Lo, dst.Hi = math.Min(dst.Lo, src.Lo), math.Max(dst.Hi, src.Hi)
	}
	dst.Nice = dst.Nice || src.Nice
	return nil
}
