package vegalite

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
)

var testTheme = Theme{
	Background: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	Text:       color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff},
	Axis:       color.NRGBA{R: 0x88, G: 0x88, B: 0x88, A: 0xff},
	Grid:       color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff},
	Font:       gui.Font{Size: 11},
}

func lay(t *testing.T, src string, width float32) (*Laid, error) {
	t.Helper()
	m, release := gui.AcquireMeasurer(1)
	defer release()
	return Lay(context.Background(), src, width, testTheme, m)
}

func mustLay(t *testing.T, src string, width float32) *Laid {
	t.Helper()
	l, err := lay(t, src, width)
	if err != nil {
		t.Fatalf("Lay: %v", err)
	}
	return l
}

// of are the items of a kind, in paint order.
func (l *Laid) of(k itemKind) []item {
	var out []item
	for _, it := range l.items {
		if it.kind == k {
			out = append(out, it)
		}
	}
	return out
}

// texts are the labels drawn, in paint order.
func (l *Laid) texts() []string {
	var out []string
	for _, it := range l.of(itemText) {
		out = append(out, it.text)
	}
	return out
}

// bars are the filled rectangles in the palette's colours (not the legend's swatches, which come
// after the axes).
func (l *Laid) bars() []item {
	var out []item
	for _, it := range l.items {
		if it.kind == itemText {
			break // the marks are drawn before every axis label
		}
		if it.kind == itemRect {
			out = append(out, it)
		}
	}
	return out
}

const barSpec = `{
  "$schema": "https://vega.github.io/schema/vega-lite/v5.json",
  "description": "A simple bar chart.",
  "data": {"values": [
    {"a": "C", "b": 43}, {"a": "A", "b": 28}, {"a": "B", "b": 55}
  ]},
  "mark": {"type": "bar", "tooltip": true},
  "encoding": {
    "x": {"field": "a", "type": "nominal", "axis": {"labelAngle": 0}},
    "y": {"field": "b", "type": "quantitative"}
  }
}`

// A bar chart draws a bar a row, its categories in ascending order left to right, each bar's
// height in proportion to its value from a shared baseline, and its axes' labels and titles.
func TestABarChartIsLaidOut(t *testing.T) {
	l := mustLay(t, barSpec, 2000)
	bars := l.bars()
	if len(bars) != 3 {
		t.Fatalf("%d bars, want 3", len(bars))
	}
	byX := slices.Clone(bars)
	slices.SortFunc(byX, func(a, b item) int { return int(a.rect.X - b.rect.X) })
	// in the data's order C, A, B; drawn A, B, C: heights 28, 55, 43
	h := []float32{byX[0].rect.H, byX[1].rect.H, byX[2].rect.H}
	if !(h[0] < h[2] && h[2] < h[1]) {
		t.Errorf("heights left to right %v, want A (28) < C (43) < B (55)", h)
	}
	if r := h[1] / h[0]; math.Abs(float64(r-55.0/28)) > 0.01 {
		t.Errorf("B over A is %v, want 55/28", r)
	}
	base := bars[0].rect.Y + bars[0].rect.H
	for _, b := range bars {
		if got := b.rect.Y + b.rect.H; math.Abs(float64(got-base)) > 0.01 {
			t.Errorf("a bar ends at %v, the first at %v: no shared baseline", got, base)
		}
	}
	texts := strings.Join(l.texts(), "|")
	for _, want := range []string{"A", "B", "C", "a", "b", "0", "60"} {
		if !slices.Contains(l.texts(), want) {
			t.Errorf("label %q not drawn; drawn %s", want, texts)
		}
	}
	if s := l.Size(); s.W <= 0 || s.H <= 0 || l.fit != 1 {
		t.Errorf("size %+v fit %v", s, l.fit)
	}
}

// The y axis of a bar chart is niced: 0 to 60 by tens for values up to 55, its labels the ticks'.
func TestTheValueAxisIsNiced(t *testing.T) {
	c, err := parse(barSpec)
	if err != nil {
		t.Fatal(err)
	}
	p, err := newPlan(c.layers[0])
	if err != nil {
		t.Fatal(err)
	}
	s := &scale{a: &p.ys}
	s.setTicks(200, 1)
	if p.ys.lo != 0 || p.ys.hi != 60 || !slices.Equal(s.labels(), []string{"0", "10", "20", "30", "40", "50", "60"}) {
		t.Errorf("domain [%v, %v], labels %v; want [0, 60] by tens", p.ys.lo, p.ys.hi, s.labels())
	}
}

// An aggregated measure is one bar a category: sum, mean and count, as Vega-Lite groups them.
func TestAnAggregateGroupsByCategory(t *testing.T) {
	rows := `[{"k":"x","v":1},{"k":"x","v":3},{"k":"y","v":10},{"k":"y","v":null}]`
	for op, want := range map[string][]float64{"sum": {4, 10}, "mean": {2, 10}, "count": {2, 2}, "max": {3, 10}, "median": {2, 10}} {
		spec := `{"data":{"values":` + rows + `},"mark":"bar","encoding":{"x":{"field":"k","type":"nominal"},"y":{"aggregate":"` + op + `","field":"v","type":"quantitative"}}}`
		c, err := parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		p, err := newPlan(c.layers[0])
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		var got []float64
		for _, d := range p.data {
			got = append(got, d.v1)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", op, got, want)
		}
	}
}

const stackedSpec = `{
  "data": {"values": [
    {"m": "Jan", "kind": "b", "n": 2}, {"m": "Jan", "kind": "a", "n": 3},
    {"m": "Feb", "kind": "a", "n": 1}, {"m": "Feb", "kind": "b", "n": 4}
  ]},
  "mark": "bar",
  "encoding": {
    "x": {"field": "m", "type": "ordinal", "sort": null},
    "y": {"field": "n", "type": "quantitative"},
    "color": {"field": "kind", "type": "nominal"}
  }
}`

// A colour splits bars into series, piled in the colour's order, a's under b's, each its palette
// colour; the legend names each series with its swatch.
func TestABarsSeriesStack(t *testing.T) {
	l := mustLay(t, stackedSpec, 2000)
	bars := l.bars()
	if len(bars) != 4 {
		t.Fatalf("%d bars, want 4", len(bars))
	}
	// Jan is first (sort null: the data's order); a (blue) under b (orange) in each column
	cols := map[float32][]item{}
	for _, b := range bars {
		cols[b.rect.X] = append(cols[b.rect.X], b)
	}
	if len(cols) != 2 {
		t.Fatalf("bars in %d columns, want 2", len(cols))
	}
	for x, c := range cols {
		slices.SortFunc(c, func(a, b item) int { return int(b.rect.Y - a.rect.Y) }) // bottom first
		if c[0].color != tableau10[0] || c[1].color != tableau10[1] {
			t.Errorf("column at %v: bottom %v, top %v; want a's colour under b's", x, c[0].color, c[1].color)
		}
		if math.Abs(float64(c[0].rect.Y-(c[1].rect.Y+c[1].rect.H))) > 0.01 {
			t.Errorf("column at %v: b's bar does not sit on a's", x)
		}
	}
	for _, want := range []string{"kind", "a", "b", "Jan", "Feb"} {
		if !slices.Contains(l.texts(), want) {
			t.Errorf("%q not drawn: %v", want, l.texts())
		}
	}
	var swatches int
	for _, it := range l.of(itemRect) {
		if it.rect.W == it.rect.H && it.rect.W == testTheme.Font.Size*0.8 {
			swatches++
		}
	}
	if swatches != 2 {
		t.Errorf("%d legend swatches, want 2", swatches)
	}
	labels := l.texts()
	if i, j := slices.Index(labels, "Jan"), slices.Index(labels, "Feb"); i > j {
		t.Errorf("Feb labelled before Jan with sort null: %v", labels)
	}
}

// A normalized stack piles each category to 1, its axis in percent.
func TestANormalizedStackIsInPercent(t *testing.T) {
	spec := strings.Replace(stackedSpec, `"type": "quantitative"}`, `"type": "quantitative", "stack": "normalize"}`, 1)
	spec = strings.Replace(spec, `"kind": "b", "n": 4`, `"kind": "b", "n": 9`, 1) // totals 5 and 10: equal only when normalized
	l := mustLay(t, spec, 2000)
	if !slices.Contains(l.texts(), "100%") || !slices.Contains(l.texts(), "0%") {
		t.Errorf("labels %v, want 0%% to 100%%", l.texts())
	}
	tops := map[float32]float32{}
	for _, b := range l.bars() {
		if v, ok := tops[b.rect.X]; !ok || b.rect.Y < v {
			tops[b.rect.X] = b.rect.Y
		}
	}
	var first float32 = -1
	for _, y := range tops {
		if first >= 0 && math.Abs(float64(y-first)) > 0.01 {
			t.Errorf("normalized columns end at %v and %v, want the same height", first, y)
		}
		first = y
	}
}

// A bar with a nominal y is horizontal: its first category on top.
func TestAHorizontalBarPutsItsFirstCategoryOnTop(t *testing.T) {
	l := mustLay(t, `{"data":{"values":[{"c":"one","v":5},{"c":"two","v":9}]},"mark":"bar",
		"encoding":{"y":{"field":"c","type":"nominal","sort":null},"x":{"field":"v","type":"quantitative"}}}`, 2000)
	bars := l.bars()
	if len(bars) != 2 {
		t.Fatalf("%d bars", len(bars))
	}
	one, two := bars[0], bars[1]
	if one.rect.Y >= two.rect.Y || one.rect.W >= two.rect.W || one.rect.X != two.rect.X {
		t.Errorf("one %+v, two %+v: want one above, shorter, from the same x", one.rect, two.rect)
	}
	var top, bottom float32 // the y axis's line: the plot's height
	for _, it := range l.of(itemLine) {
		if it.color == testTheme.Axis && it.pts[0].X == it.pts[1].X && it.pts[1].Y-it.pts[0].Y > bottom-top {
			top, bottom = it.pts[0].Y, it.pts[1].Y
		}
	}
	for _, b := range bars {
		if b.rect.Y < top || b.rect.Y+b.rect.H > bottom {
			t.Errorf("a bar %+v is outside the plot, %v to %v", b.rect, top, bottom)
		}
	}
}

// Sorting a category axis: descending, by the measure ("-y"), or by a list.
func TestCategoriesSort(t *testing.T) {
	data := `{"values":[{"a":"B","b":1},{"a":"C","b":3},{"a":"A","b":2}]}`
	for sort, want := range map[string][]string{
		`"descending"`: {"C", "B", "A"},
		`"-y"`:         {"C", "A", "B"},
		`"y"`:          {"B", "A", "C"},
		`["C","A"]`:    {"C", "A", "B"},
		`null`:         {"B", "C", "A"},
	} {
		c, err := parse(`{"data":` + data + `,"mark":"bar","encoding":{"x":{"field":"a","type":"nominal","sort":` + sort + `},"y":{"field":"b","type":"quantitative"}}}`)
		if err != nil {
			t.Fatal(err)
		}
		p, err := newPlan(c.layers[0])
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(p.xs.domain, want) {
			t.Errorf("sort %s: %v, want %v", sort, p.xs.domain, want)
		}
	}
	c, _ := parse(`{"data":{"values":[{"a":"10","b":1},{"a":"9","b":1},{"a":"x","b":1}]},"mark":"bar","encoding":{"x":{"field":"a","type":"ordinal"},"y":{"field":"b","type":"quantitative"}}}`)
	p, _ := newPlan(c.layers[0])
	if !slices.Equal(p.xs.domain, []string{"9", "10", "x"}) {
		t.Errorf("ascending %v, want numbers as numbers, then text", p.xs.domain)
	}
}

const lineSpec = `{
  "data": {"values": [
    {"date": "2024-03-01", "price": 30, "sym": "B"},
    {"date": "2024-01-01", "price": 10, "sym": "A"},
    {"date": "2024-02-01", "price": 20, "sym": "A"},
    {"date": "2024-03-01", "price": 15, "sym": "A"},
    {"date": "2024-01-01", "price": 25, "sym": "B"}
  ]},
  "mark": {"type": "line", "point": true},
  "encoding": {
    "x": {"field": "date", "type": "temporal", "title": "Day"},
    "y": {"field": "price", "type": "quantitative"},
    "color": {"field": "sym", "type": "nominal"}
  }
}`

// A line is drawn through its series' points from left to right, whatever the data's order, with
// its points; its time axis is labelled on the calendar's boundaries.
func TestALineFollowsItsSeriesInXOrder(t *testing.T) {
	l := mustLay(t, lineSpec, 2000)
	var lines []item
	for _, it := range l.of(itemLine) {
		if it.color == tableau10[0] || it.color == tableau10[1] {
			lines = append(lines, it)
		}
	}
	// two series' lines, and the legend's two strokes
	if len(lines) != 4 {
		t.Fatalf("%d lines in the series' colours, want 4", len(lines))
	}
	a, b := lines[0], lines[1]
	if len(a.pts) != 3 || !(a.pts[0].X < a.pts[1].X && a.pts[1].X < a.pts[2].X) {
		t.Errorf("A's line through %v: want 3 points, left to right", a.pts)
	}
	if len(b.pts) != 2 || b.pts[0].X >= b.pts[1].X { // B's rows are March, then January
		t.Errorf("B's line through %v: want left to right", b.pts)
	}
	if !(a.pts[0].Y > a.pts[2].Y && a.pts[2].Y > a.pts[1].Y) { // y grows down: 20 highest, then 15, then 10
		t.Errorf("A goes 10, 20, 15? its line %v", a.pts)
	}
	if dots := l.of(itemDot); len(dots) != 5 {
		t.Errorf("%d points drawn, want 5", len(dots))
	}
	// two months over 200 pixels: d3's weekly ticks, on Sundays, their labels thinned to fit
	texts := l.texts()
	if !slices.Contains(texts, "Day") || !slices.Contains(texts, "Jan 07") || slices.Contains(texts, "Jan 14") {
		t.Errorf("labels %v: want the title Day and every other week from Jan 07", texts)
	}
	var below []gui.Rect // the x axis's labels: under the plot, upright never
	for _, it := range l.of(itemText) {
		if strings.HasPrefix(it.text, "Jan ") || strings.HasPrefix(it.text, "Feb ") {
			below = append(below, it.rect)
		}
	}
	for i := 1; i < len(below); i++ {
		if below[i].X < below[i-1].X+below[i-1].W {
			t.Errorf("x labels overlap: %+v then %+v", below[i-1], below[i])
		}
	}
}

// Labels that would overlap are thinned, every other one at a time, from the first.
func TestParityThinsOverlappingLabels(t *testing.T) {
	spans := [][2]float32{{0, 10}, {8, 18}, {16, 26}, {24, 34}, {32, 42}}
	if got := parity(spans, 0); !slices.Equal(got, []bool{true, false, true, false, true}) {
		t.Errorf("every other: %v", got)
	}
	spans = [][2]float32{{0, 10}, {4, 14}, {8, 18}, {12, 22}, {16, 26}}
	if got := parity(spans, 0); !slices.Equal(got, []bool{true, false, false, false, true}) {
		t.Errorf("every fourth: %v", got)
	}
	if got := parity([][2]float32{{0, 5}, {10, 15}}, 2); !slices.Equal(got, []bool{true, true}) {
		t.Errorf("apart: %v", got)
	}
}

// A stacked area's series pile: each one's polygon runs over its top and back over its base.
func TestAnAreaStacks(t *testing.T) {
	spec := strings.Replace(lineSpec, `{"type": "line", "point": true}`, `"area"`, 1)
	spec = strings.Replace(spec, `{"date": "2024-02-01", "price": 20, "sym": "A"},`, ``, 1)
	l := mustLay(t, spec, 2000)
	fills := l.of(itemFill)
	if len(fills) != 2 {
		t.Fatalf("%d areas, want 2", len(fills))
	}
	a, b := fills[0], fills[1]
	if len(a.pts) != 4 || len(b.pts) != 4 {
		t.Fatalf("polygons of %d and %d points, want 4 each (2 over, 2 back)", len(a.pts), len(b.pts))
	}
	// b's base is a's top
	if a.pts[0] != b.pts[3] || a.pts[1] != b.pts[2] {
		t.Errorf("b's base %v, %v is not a's top %v, %v", b.pts[3], b.pts[2], a.pts[0], a.pts[1])
	}
}

// A scatter plot of two quantitative fields draws rings, at Vega-Lite's 0.7 opacity; a circle mark
// fills them.
func TestPointsAreRingsOrDots(t *testing.T) {
	spec := `{"data":{"values":[{"x":1,"y":2},{"x":3,"y":-1},{"x":"bad","y":0}]},"mark":"point",
		"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}}`
	l := mustLay(t, spec, 2000)
	rings := l.of(itemRing)
	if len(rings) != 2 || len(l.of(itemDot)) != 0 {
		t.Fatalf("%d rings, %d dots; want 2 rings (the bad row left out)", len(rings), len(l.of(itemDot)))
	}
	if rings[0].color.A != uint8(255*0.7+0.5) {
		t.Errorf("a point's alpha %d, want 0.7's", rings[0].color.A)
	}
	if !slices.Contains(l.texts(), "−1.0") { // a step of 0.5: one decimal, as d3 formats it
		t.Errorf("labels %v: want negative ticks", l.texts())
	}
	l = mustLay(t, strings.Replace(spec, `"mark":"point"`, `"mark":"circle"`, 1), 2000)
	if len(l.of(itemDot)) != 2 {
		t.Errorf("a circle mark drew %d dots, want 2", len(l.of(itemDot)))
	}
}

// A chart wider than the width asked is scaled down to it; a "container" width fills it.
func TestTheChartFitsItsWidth(t *testing.T) {
	l := mustLay(t, strings.Replace(barSpec, `"mark"`, `"width": 900, "mark"`, 1), 300)
	if l.Size().W != 300 || l.fit >= 1 {
		t.Errorf("size %+v fit %v: want 300 wide, scaled down", l.Size(), l.fit)
	}
	l = mustLay(t, strings.Replace(barSpec, `"mark"`, `"width": "container", "mark"`, 1), 700)
	if w := l.Size().W; w < 650 || w > 700 || l.fit != 1 {
		t.Errorf("container width: %v (fit %v), want about 700 unscaled", w, l.fit)
	}
}

// Category labels too wide for their bands are drawn upright, and the axis is as deep as the
// widest.
func TestWideCategoryLabelsStandUp(t *testing.T) {
	l := mustLay(t, `{"data":{"values":[{"a":"a very long category name","b":1},{"a":"another long one","b":2}]},"mark":"bar",
		"encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative"}}}`, 2000)
	up := 0
	for _, it := range l.of(itemText) {
		if it.up && strings.Contains(it.text, "long") {
			up++
			if it.rect.H <= it.rect.W {
				t.Errorf("an upright label's box %+v is not tall", it.rect)
			}
		}
	}
	if up != 2 {
		t.Errorf("%d upright category labels, want 2", up)
	}
}

// The subset: everything outside it that changes what a chart says is ErrUnsupported, a spec over
// a limit ErrTooLarge, and malformed JSON neither (the caller shows the source).
func TestSpecsOutsideTheSubsetDecline(t *testing.T) {
	ok := `"data":{"values":[{"a":"x","b":1}]},"encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative"}}`
	for name, spec := range map[string]string{
		"transform":       `{` + ok + `,"mark":"bar","transform":[{"filter":"datum.b > 0"}]}`,
		"layer":           `{"layer":[],"data":{"values":[]},"mark":"bar"}`,
		"a url":           `{"data":{"url":"data.csv"},"mark":"bar"}`,
		"a tick mark":     `{` + ok + `,"mark":"tick"}`,
		"a log scale":     `{"data":{"values":[]},"mark":"bar","encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative","scale":{"type":"log"}}}}`,
		"a binned field":  `{"data":{"values":[]},"mark":"bar","encoding":{"x":{"field":"a","bin":true,"type":"quantitative"},"y":{"aggregate":"count"}}}`,
		"a time unit":     `{"data":{"values":[]},"mark":"line","encoding":{"x":{"field":"a","timeUnit":"month","type":"temporal"},"y":{"field":"b","type":"quantitative"}}}`,
		"a size channel":  `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"size":{"field":"b"},`, 1) + `,"mark":"point"}`,
		"a nested field":  `{"data":{"values":[]},"mark":"bar","encoding":{"x":{"field":"a.b","type":"nominal"},"y":{"field":"b","type":"quantitative"}}}`,
		"a scale colour":  `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"color":{"field":"b","type":"quantitative"},`, 1) + `,"mark":"bar"}`,
		"no mark":         `{"data":{"values":[]}}`,
		"a bar of 2 Qs":   `{"data":{"values":[]},"mark":"bar","encoding":{"x":{"field":"a","type":"quantitative"},"y":{"field":"b","type":"quantitative"}}}`,
		"one position":    `{"data":{"values":[]},"mark":"line","encoding":{"x":{"field":"a","type":"temporal"}}}`,
		"values of nums":  `{"data":{"values":[1,2,3]},"mark":"bar"}`,
		"a set selection": `{` + ok + `,"mark":"bar","params":[{"name":"p","select":"interval","value":{"x":[1,2]}}]}`,
		"a mark's orient": `{` + ok + `,"mark":{"type":"bar","orient":"horizontal"}}`,
		"a step width":    `{` + ok + `,"mark":"bar","width":{"step":30}}`,
		// what a label says, or which values are drawn, is not passed over
		"a label expr":     `{` + strings.Replace(ok, `"type":"quantitative"}`, `"type":"quantitative","axis":{"labelExpr":"datum.value * 100 + '%'"}}`, 1) + `,"mark":"bar"}`,
		"a label format":   `{` + strings.Replace(ok, `"type":"quantitative"}`, `"type":"quantitative","axis":{"format":"%"}}`, 1) + `,"mark":"bar"}`,
		"tick values":      `{` + strings.Replace(ok, `"type":"quantitative"}`, `"type":"quantitative","axis":{"values":[0,5]}}`, 1) + `,"mark":"bar"}`,
		"a legend format":  `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"color":{"field":"a","type":"nominal","legend":{"labelExpr":"'x'"}},`, 1) + `,"mark":"bar"}`,
		"a config scale":   `{` + ok + `,"mark":"bar","config":{"scale":{"zero":false}}}`,
		"a config format":  `{` + ok + `,"mark":"bar","config":{"numberFormat":".0%"}}`,
		"a config axis":    `{` + ok + `,"mark":"bar","config":{"axisY":{"labelExpr":"'x'"}}}`,
		"a config invalid": `{` + ok + `,"mark":"bar","config":{"mark":{"invalid":null}}}`,
		"a config step":    `{` + strings.Replace(ok, `"type":"nominal"`, `"type":"ordinal"`, 1) + `,"mark":"line","config":{"line":{"interpolate":"step-after"}}}`,
		"a config curve":   `{` + ok + `,"mark":"area","config":{"area":{"interpolate":"monotone"}}}`,
		"a mark curve":     `{` + ok + `,"mark":"line","config":{"mark":{"interpolate":"basis"}}}`,
		"a config tension": `{` + ok + `,"mark":"line","config":{"line":{"tension":0.5}}}`,
		"a mark invalid":   `{` + ok + `,"mark":{"type":"bar","invalid":null}}`,
		"a shape on a bar": `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"shape":{"field":"a","type":"nominal"},`, 1) + `,"mark":"bar"}`,
		"a scale shape":    `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"shape":{"field":"b","type":"quantitative"},`, 1) + `,"mark":"point"}`,
		"a summed shape":   `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"shape":{"field":"a","type":"nominal","aggregate":"count"},`, 1) + `,"mark":"point"}`,
		"a path shape":     `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"shape":{"value":"M0,0L1,1"}},`, 1) + `,"mark":"point"}`,
		"a range's shape":  `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"shape":{"field":"a","type":"nominal","scale":{"range":["circle","star"]}},`, 1) + `,"mark":"point"}`,
		"a range's colour": `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"color":{"field":"a","type":"nominal","scale":{"range":["#2e9e6a","nocolour"]}},`, 1) + `,"mark":"bar"}`,
		"a position range": `{` + strings.Replace(ok, `"type":"quantitative"}`, `"type":"quantitative","scale":{"range":[0,100]}}`, 1) + `,"mark":"bar"}`,
		"a sort op":        `{` + strings.Replace(ok, `"type":"nominal"}`, `"type":"nominal","sort":{"field":"b","op":"distinct"}}`, 1) + `,"mark":"bar"}`,
		"a sort no field":  `{` + strings.Replace(ok, `"type":"nominal"}`, `"type":"nominal","sort":{"op":"sum"}}`, 1) + `,"mark":"bar"}`,
		"a sort encoding":  `{` + strings.Replace(ok, `"type":"nominal"}`, `"type":"nominal","sort":{"encoding":"y"}}`, 1) + `,"mark":"bar"}`,
		"a sort order":     `{` + strings.Replace(ok, `"type":"nominal"}`, `"type":"nominal","sort":{"field":"b","order":"up"}}`, 1) + `,"mark":"bar"}`,
		"a legend sorted":  `{` + strings.Replace(ok, `"encoding":{`, `"encoding":{"color":{"field":"a","type":"nominal","sort":{"field":"b"}},`, 1) + `,"mark":"bar"}`,
		"a subtitle list":  `{` + ok + `,"mark":"bar","title":{"text":"t","subtitle":[1,2]}}`,
	} {
		if _, err := lay(t, spec, 600); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	var rows strings.Builder
	rows.WriteString(`{"mark":"point","data":{"values":[`)
	for i := range maxRows + 1 {
		if i > 0 {
			rows.WriteString(",")
		}
		rows.WriteString(`{"a":1}`)
	}
	rows.WriteString(`]}}`)
	if _, err := lay(t, rows.String(), 600); !errors.Is(err, ErrTooLarge) {
		t.Errorf("%d rows: %v, want ErrTooLarge", maxRows+1, err)
	}
	if _, err := lay(t, `{"mark": "bar",`, 600); err == nil || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrTooLarge) {
		t.Errorf("malformed JSON: %v, want the decoder's error", err)
	}
}

// Keys that change only a chart's look, or its behaviour in a browser, are passed over.
func TestCosmeticKeysArePassedOver(t *testing.T) {
	spec := `{"$schema":"x","config":{"view":{"stroke":null},"axis":{"labelFont":"serif","gridColor":"#eee"},"bar":{"color":"teal"},"line":{"interpolate":"linear"},"legend":{"orient":"bottom"}},"background":"white","padding":5,"autosize":"fit",
		"title":{"text":"Sales","anchor":"start"},
		"data":{"values":[{"a":"x","b":1}]},"mark":{"type":"bar","tooltip":true,"cornerRadiusEnd":4},
		"encoding":{"x":{"field":"a","type":"nominal","axis":{"labelAngle":-45,"title":"Kind"},"scale":{"paddingInner":0.2}},
		"y":{"field":"b","type":"quantitative","axis":{"grid":false,"labelFontSize":12}},"tooltip":[{"field":"b"}]}}`
	l := mustLay(t, spec, 600)
	if !slices.Contains(l.texts(), "Sales") || !slices.Contains(l.texts(), "Kind") {
		t.Errorf("labels %v: want the title and the axis's", l.texts())
	}
	for _, it := range l.of(itemLine) {
		if it.color == testTheme.Grid {
			t.Error("a gridline drawn under grid: false")
			break
		}
	}
}

// Paint draws every label as text, an upright one turned, and every shape in a path: a run of
// one colour in one path, so a scatter of many points is one call, not one a point.
func TestPaintBatchesItsShapes(t *testing.T) {
	l := mustLay(t, lineSpec, 2000)
	rc := gui.NewRecordingCanvas(gui.Size{W: 2000, H: 1000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	count := map[string]int{}
	for _, c := range rc.Calls {
		count[c.Op]++
	}
	if count["DrawText"] != len(l.of(itemText)) {
		t.Errorf("%d texts drawn, %d laid", count["DrawText"], len(l.of(itemText)))
	}
	up := 0
	for _, it := range l.of(itemText) {
		if it.up {
			up++
		}
	}
	if up == 0 || count["PushTransform"] != up {
		t.Errorf("%d transforms pushed for %d upright labels", count["PushTransform"], up)
	}
	shapes := len(l.items) - len(l.of(itemText))
	if paths := count["StrokePath"] + count["FillPath"]; paths == 0 || paths >= shapes {
		t.Errorf("%d paths for %d shapes: not batched", paths, shapes)
	}
	var rows strings.Builder
	for i := range 2000 {
		if i > 0 {
			rows.WriteString(",")
		}
		fmt.Fprintf(&rows, `{"x":%d,"y":%d}`, i, (i*7919)%1000)
	}
	l = mustLay(t, `{"data":{"values":[`+rows.String()+`]},"mark":"point","encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}}`, 800)
	rc = gui.NewRecordingCanvas(gui.Size{W: 800, H: 800}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	strokes := 0
	for _, c := range rc.Calls {
		if c.Op == "StrokePath" {
			strokes++
		}
	}
	if len(l.of(itemRing)) != 2000 || strokes > 10 {
		t.Errorf("%d rings stroked in %d calls, want a handful", len(l.of(itemRing)), strokes)
	}
	for _, it := range l.items {
		for _, v := range []float32{it.rect.X, it.rect.Y, it.rect.W, it.rect.H} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("an item at %+v", it)
			}
		}
	}
}

// An empty chart, or one of a single value, still lays out: no NaN, no division by zero.
func TestDegenerateDataLaysOut(t *testing.T) {
	for _, spec := range []string{
		`{"data":{"values":[]},"mark":"bar","encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative"}}}`,
		`{"data":{"values":[{"t":"2024-01-01","v":3}]},"mark":"line","encoding":{"x":{"field":"t","type":"temporal"},"y":{"field":"v","type":"quantitative","scale":{"zero":false}}}}`,
	} {
		l := mustLay(t, spec, 600)
		for _, it := range l.items {
			for _, p := range append(it.pts, gui.Pt(it.rect.X, it.rect.Y), gui.Pt(it.rect.W, it.rect.H)) {
				if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) {
					t.Fatalf("%s: an item at %+v", spec, it)
				}
			}
		}
	}
}

// d3's ticks: 1, 2 or 5 times a power of ten; a nice domain out to them; labels grouped by
// thousands with the step's decimals.
func TestLinearTicks(t *testing.T) {
	for _, c := range []struct {
		lo, hi float64
		n      int
		step   float64
	}{{0, 55, 5, 10}, {0, 1, 5, 0.2}, {0, 1000, 10, 100}, {-3, 3, 6, 1}, {0, 0.07, 7, 0.01}} {
		if got := tickStep(c.lo, c.hi, c.n); math.Abs(got-c.step) > 1e-12 {
			t.Errorf("tickStep(%v, %v, %d) = %v, want %v", c.lo, c.hi, c.n, got, c.step)
		}
	}
	if lo, hi := niceDomain(3, 97, 5); lo != 0 || hi != 100 {
		t.Errorf("niceDomain(3, 97) = %v, %v", lo, hi)
	}
	ticks, step := linearTicks(0, 1, 5)
	if len(ticks) != 6 || formatNumber(ticks[3], step) != "0.6" {
		t.Errorf("ticks %v step %v", ticks, step)
	}
	for v, want := range map[float64]string{1234567: "1,234,567", -1500: "−1,500", 0: "0", 999: "999"} {
		if got := formatNumber(v, 100); got != want {
			t.Errorf("formatNumber(%v) = %q, want %q", v, got, want)
		}
	}
	if got := formatPercent(0.25, 0.05); got != "25%" {
		t.Errorf("formatPercent = %q", got)
	}
}

// A time scale's ticks fall on the calendar: months over a year, days over a week, and each is
// labelled by the coarsest boundary it is on.
func TestTimeTicks(t *testing.T) {
	ms := func(s string) float64 {
		v, ok := timeOf(s)
		if !ok {
			t.Fatalf("timeOf(%q)", s)
		}
		return v
	}
	var labels []string
	for _, v := range timeTicks(ms("2024-01-01"), ms("2024-12-31"), 5) {
		labels = append(labels, formatTime(v))
	}
	if !slices.Equal(labels, []string{"2024", "Apr", "Jul", "Oct"}) {
		t.Errorf("a year's ticks %v, want quarters", labels)
	}
	labels = nil
	for _, v := range timeTicks(ms("2024-03-04"), ms("2024-03-09"), 5) {
		labels = append(labels, formatTime(v))
	}
	if !slices.Equal(labels, []string{"Mar 04", "Mar 05", "Mar 06", "Mar 07", "Mar 08", "Mar 09"}) {
		t.Errorf("a week's ticks %v, want days", labels)
	}
	for in, want := range map[string]time.Time{
		"2024-02-03":           time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC),
		"2024-02-03T04:05:06Z": time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC),
		"2024":                 time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		"Feb 3, 2024":          time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC),
	} {
		if got := ms(in); got != float64(want.UnixMilli()) {
			t.Errorf("timeOf(%q) = %v, want %v", in, got, want)
		}
	}
	if v, ok := timeOf(float64(1700)); !ok || v != 1700 {
		t.Errorf("a number is milliseconds: %v %v", v, ok)
	}
}

// A line or point chart's scale with zero: false is its data's extent, niced: no zero baseline
// creeps in from an unstacked mark's base; a bar's measure keeps zero by default.
func TestZeroFalseIsTheDatasExtent(t *testing.T) {
	for _, mark := range []string{"line", "point"} {
		c, err := parse(`{"data":{"values":[{"x":1,"y":99},{"x":2,"y":100}]},"mark":"` + mark + `",
			"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative","scale":{"zero":false}}}}`)
		if err != nil {
			t.Fatal(err)
		}
		p, err := newPlan(c.layers[0])
		if err != nil {
			t.Fatal(err)
		}
		if p.ys.lo != 99 || p.ys.hi != 100 {
			t.Errorf("%s: y domain [%v, %v], want [99, 100]", mark, p.ys.lo, p.ys.hi)
		}
	}
	c, _ := parse(`{"data":{"values":[{"x":"a","y":99}]},"mark":"bar","encoding":{"x":{"field":"x","type":"nominal"},"y":{"field":"y","type":"quantitative"}}}`)
	if p, _ := newPlan(c.layers[0]); p.ys.lo != 0 {
		t.Errorf("a bar's measure from %v, want zero", p.ys.lo)
	}
}

// bundleSpec is xmd's bundle-size chart (its design document's): bars by part, coloured by when they
// load, faded by a legend selection, and a budget line a slider moves.
const bundleSpec = `{
  "$schema": "https://vega.github.io/schema/vega-lite/v6.json",
  "description": "Uncompressed size of each part of the web bundle, and when it loads.",
  "width": "container",
  "height": 220,
  "params": [{"name": "budget", "value": 500,
    "bind": {"input": "range", "min": 0, "max": 5500, "step": 50, "name": "Budget line (KB) "}}],
  "data": {"values": [
    {"part": "main.js", "kb": 62, "loads": "Always"},
    {"part": "main.css", "kb": 9, "loads": "Always"},
    {"part": "Fonts", "kb": 344, "loads": "Always"},
    {"part": "Vega + Vega-Lite", "kb": 662, "loads": "First chart"},
    {"part": "Mermaid + diagram types", "kb": 5165, "loads": "First diagram"}
  ]},
  "layer": [
    {"mark": {"type": "bar", "cornerRadiusEnd": 3},
     "encoding": {
       "y": {"field": "part", "type": "nominal", "sort": "-x", "title": null},
       "x": {"field": "kb", "type": "quantitative", "title": "KB (uncompressed)"},
       "color": {"field": "loads", "type": "nominal", "title": "Loads"},
       "opacity": {"condition": {"param": "pick", "value": 1}, "value": 0.25},
       "tooltip": [{"field": "part", "title": "Part"}, {"field": "kb", "title": "KB"}]},
     "params": [{"name": "pick", "select": {"type": "point", "fields": ["loads"]}, "bind": "legend"}]},
    {"mark": {"type": "rule", "strokeDash": [4, 3]},
     "encoding": {"x": {"datum": {"expr": "budget"}, "type": "quantitative"}}}
  ]
}`

// The chart is drawn as the page first shows it: five bars, largest on top, at full opacity (the
// legend selection starts empty, so its condition holds), three series in the legend, and the
// budget rule at the slider's initial 500 KB, dashed, across the plot.
func TestALayeredChartIsDrawnAsItsFirstState(t *testing.T) {
	l := mustLay(t, bundleSpec, 900)
	bars := l.bars()
	if len(bars) != 5 {
		t.Fatalf("%d bars, want 5", len(bars))
	}
	for _, b := range bars {
		if b.color.A != 0xff {
			t.Errorf("a bar at alpha %d: the selection's condition should hold, opacity 1", b.color.A)
		}
	}
	top := bars[0]
	for _, b := range bars {
		if b.rect.Y < top.rect.Y {
			top = b
		}
	}
	if top.rect.W < bars[0].rect.W || top.rect.W < bars[4].rect.W {
		t.Errorf("the top bar is not the widest (sort -x): %+v", top.rect)
	}
	for _, want := range []string{"Loads", "Always", "First chart", "First diagram", "KB (uncompressed)"} {
		if !slices.Contains(l.texts(), want) {
			t.Errorf("%q not drawn: %v", want, l.texts())
		}
	}
	// the rule: dashes along one x, from the plot's top to its bottom
	var dashes []item
	for _, it := range l.of(itemLine) {
		if it.color == tableau10[0] && len(it.pts) >= 2 && it.pts[0].X == it.pts[len(it.pts)-1].X {
			dashes = append(dashes, it)
		}
	}
	if len(dashes) < 10 {
		t.Fatalf("%d rule dashes, want a dashed line", len(dashes))
	}
	x := dashes[0].pts[0].X
	sp, _ := parse(bundleSpec)
	lp, err := newLayered(sp)
	if err != nil {
		t.Fatal(err)
	}
	if lp.xs.lo != 0 || lp.xs.hi < 5165 {
		t.Errorf("x domain [%v, %v], want 0 to past 5165", lp.xs.lo, lp.xs.hi)
	}
	var widest item // the 5165 KB bar spans 0 to 5165 on x: 500 is at about a tenth of it
	for _, b := range bars {
		if b.rect.W > widest.rect.W {
			widest = b
		}
	}
	at := widest.rect.X + widest.rect.W*500/5165
	if math.Abs(float64(x-at)) > 1 {
		t.Errorf("the budget rule at x %v, want about %v (500 KB)", x, at)
	}
	for _, d := range dashes {
		if d.pts[0].X != x {
			t.Errorf("a dash at x %v, the rule at %v", d.pts[0].X, x)
		}
	}
}

// A condition with "empty": false does not hold while the selection is empty: the channel's own
// value is drawn.
func TestAConditionThatNeedsASelectionStartsUnmet(t *testing.T) {
	spec := strings.Replace(bundleSpec, `{"param": "pick", "value": 1}`, `{"param": "pick", "value": 1, "empty": false}`, 1)
	for _, b := range mustLay(t, spec, 900).bars() {
		if b.color.A != 64 { // 255 × 0.25, rounded
			t.Fatalf("a bar at alpha %d, want 0.25's", b.color.A)
		}
	}
}

// Layers share their scales: a line over bars of the same categories is drawn at the bands'
// centres, and the y domain covers both layers' values.
func TestLayersShareTheirScales(t *testing.T) {
	spec := `{"data":{"values":[{"m":"a","n":2,"t":9},{"m":"b","n":4,"t":12}]},
		"encoding":{"x":{"field":"m","type":"nominal"}},
		"layer":[
			{"mark":"bar","encoding":{"y":{"field":"n","type":"quantitative"}}},
			{"mark":"line","encoding":{"y":{"field":"t","type":"quantitative"}}}]}`
	sp, err := parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	lp, err := newLayered(sp)
	if err != nil {
		t.Fatal(err)
	}
	if lp.ys.hi < 12 || !slices.Equal(lp.xs.domain, []string{"a", "b"}) {
		t.Fatalf("y up to %v, x %v: want both layers' values", lp.ys.hi, lp.xs.domain)
	}
	l := mustLay(t, spec, 900)
	if !slices.Contains(l.texts(), "n, t") {
		t.Errorf("the shared y axis's title: %v, want both layers' fields, \"n, t\"", l.texts())
	}
	bars := l.bars()
	var line item
	for _, it := range l.of(itemLine) {
		if it.color == tableau10[0] && len(it.pts) == 2 && it.pts[0].X != it.pts[1].X {
			line = it
		}
	}
	if len(bars) != 2 || len(line.pts) != 2 {
		t.Fatalf("%d bars and a line of %d points", len(bars), len(line.pts))
	}
	for i, b := range bars {
		if c := b.rect.X + b.rect.W/2; math.Abs(float64(c-line.pts[i].X)) > 0.01 {
			t.Errorf("the line's point %d at x %v, its band's centre %v", i, line.pts[i].X, c)
		}
		if line.pts[i].Y >= b.rect.Y { // t is above n: higher on screen
			t.Errorf("the line's point %d is not above its bar", i)
		}
	}
}

// A rule by a field is a line at each distinct value; a rule's datum on y is one line across.
func TestRulesByFieldAndByDatum(t *testing.T) {
	spec := `{"data":{"values":[{"x":1,"y":1,"at":2},{"x":3,"y":2,"at":2},{"x":5,"y":3,"at":4}]},
		"layer":[{"mark":"point","encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}},
		{"mark":{"type":"rule","color":"red"},"encoding":{"x":{"field":"at","type":"quantitative"}}},
		{"mark":{"type":"rule","color":"green"},"encoding":{"y":{"datum":2.5}}}]}`
	l := mustLay(t, spec, 900)
	red, green := 0, 0
	for _, it := range l.of(itemLine) {
		switch it.color {
		case color.NRGBA{R: 255, A: 255}:
			red++
			if it.pts[0].X != it.pts[1].X {
				t.Errorf("an x rule is not vertical: %v", it.pts)
			}
		case color.NRGBA{G: 128, A: 255}:
			green++
			if it.pts[0].Y != it.pts[1].Y {
				t.Errorf("a y rule is not horizontal: %v", it.pts)
			}
		}
	}
	if red != 2 || green != 1 {
		t.Errorf("%d x rules (want 2: at 2 and 4) and %d y rules (want 1)", red, green)
	}
	// a rule beyond the data widens the shared domain to reach it
	sp, err := parse(strings.Replace(spec, `"datum":2.5`, `"datum":20`, 1))
	if err != nil {
		t.Fatal(err)
	}
	lp, err := newLayered(sp)
	if err != nil {
		t.Fatal(err)
	}
	if lp.ys.hi < 20 {
		t.Errorf("y domain up to %v, want the rule at 20 within it", lp.ys.hi)
	}
}

// A rule of an aggregate is one line at it over every row: the mean line over bars.
func TestAMeanRule(t *testing.T) {
	spec := `{"data":{"values":[{"m":"a","n":2},{"m":"b","n":4},{"m":"c","n":9}]},
		"layer":[{"mark":"bar","encoding":{"x":{"field":"m","type":"nominal"},"y":{"field":"n","type":"quantitative"}}},
		{"mark":{"type":"rule","color":"red"},"encoding":{"y":{"aggregate":"mean","field":"n"}}}]}`
	sp, err := parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	lp, err := newLayered(sp)
	if err != nil {
		t.Fatal(err)
	}
	r := lp.order[1].(*rulePlan)
	if len(r.vals) != 1 || r.vals[0].num != 5 {
		t.Fatalf("the mean rule's values %v, want one at 5", r.vals)
	}
	l := mustLay(t, spec, 900)
	var rule []item
	for _, it := range l.of(itemLine) {
		if it.color == (color.NRGBA{R: 255, A: 255}) {
			rule = append(rule, it)
		}
	}
	if len(rule) != 1 || rule[0].pts[0].Y != rule[0].pts[1].Y {
		t.Errorf("the mean rule: %v, want one horizontal line", rule)
	}
}

// Dashes follow their pattern along a polyline, round its corners, and an odd pattern repeats.
func TestDashes(t *testing.T) {
	runs := dashed([]gui.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}, []float32{2, 3}, 1)
	if len(runs) != 2 || runs[0][0].X != 0 || runs[0][1].X != 2 || runs[1][0].X != 5 || runs[1][1].X != 7 {
		t.Errorf("2 on 3 off over 10: %v", runs)
	}
	runs = dashed([]gui.Point{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 4}}, []float32{3, 1}, 1)
	if len(runs) == 0 || len(runs[0]) != 3 || runs[0][1] != (gui.Point{X: 2, Y: 0}) || runs[0][2] != (gui.Point{X: 2, Y: 1}) {
		t.Errorf("a dash round a corner: %v", runs)
	}
	if runs = dashed([]gui.Point{{X: 0, Y: 0}, {X: 6, Y: 0}}, []float32{1}, 2); len(runs) != 2 {
		t.Errorf("an odd pattern [1] at unit 2: %v, want 2 on 2 off twice", runs)
	}
}

// A pattern too fine to see, or one that would cut a line into too many dashes, is drawn solid,
// and the walk ends: a sub-pixel dash once stalled it (float32 stopped advancing) and a thousandth
// of a pixel made 300,000 dashes. A zero-length dash draws nothing.
func TestDashesStayBounded(t *testing.T) {
	line := []gui.Point{{X: 0, Y: 0}, {X: 0, Y: 600}}
	for _, pat := range [][]float32{{1e-6, 1e-6}, {1e-3, 1e-3}, {0.4, 0.4}} {
		done := make(chan [][]gui.Point, 1)
		go func() { done <- dashed(line, pat, 1) }()
		select {
		case runs := <-done:
			if len(runs) != 1 || len(runs[0]) != 2 {
				t.Errorf("%v over 600 px: %d runs, want the line solid", pat, len(runs))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%v over 600 px: still cutting after 5 s", pat)
		}
	}
	if runs := dashed(line, []float32{1e-6, 2}, 1); len(runs) > 301 {
		t.Errorf("dashes of a millionth of a pixel, 2 px apart: %d runs, want at most one a period", len(runs))
	}
	if runs := dashed(line, []float32{0, 3}, 1); len(runs) != 0 {
		t.Errorf("zero-length dashes: %d runs drawn, want none", len(runs))
	}
	if runs := dashed([]gui.Point{{X: 0, Y: 0}, {X: 0, Y: 1e5}}, []float32{0.5, 0.5}, 2); len(runs) != 1 {
		t.Errorf("a 2-pixel period over 100,000 px (50,000 dashes): %d runs, want solid", len(runs))
	}
}

// The still's limits: what the page's first state does not settle is declined.
func TestParamsOutsideTheStillDecline(t *testing.T) {
	base := strings.Replace(bundleSpec, "\n", "", -1)
	for name, c := range map[string][2]string{
		"a variable without a value": {`"name": "budget", "value": 500,`, `"name": "budget",`},
		"a computed param":           {`"name": "budget", "value": 500,`, `"name": "budget", "expr": "250 * 2",`},
		"an expression datum":        {`{"expr": "budget"}`, `{"expr": "budget * 2"}`},
		"a selection as a datum":     {`{"expr": "budget"}`, `{"expr": "pick"}`},
		"a condition test":           {`{"param": "pick", "value": 1}`, `{"test": "datum.kb > 100", "value": 1}`},
		"a condition on a variable":  {`{"param": "pick", "value": 1}`, `{"param": "budget", "value": 1}`},
		"a condition on nothing":     {`{"param": "pick", "value": 1}`, `{"param": "nobody", "value": 1}`},
		"conditions as a list":       {`{"param": "pick", "value": 1}`, `[{"param": "pick", "value": 1}]`},
		"a field opacity":            {`"opacity": {"condition": {"param": "pick", "value": 1}, "value": 0.25}`, `"opacity": {"field": "kb", "type": "quantitative"}`},
		"a rule of x and y":          {`{"x": {"datum": {"expr": "budget"}, "type": "quantitative"}}`, `{"x": {"datum": 500}, "y": {"datum": 1}}`},
		"a rule on a nominal x":      {`{"x": {"datum": {"expr": "budget"}, "type": "quantitative"}}`, `{"x": {"datum": "main.js"}}`},
		"a mark beside the layer":    {`"width": "container",`, `"width": "container", "mark": "bar",`},
		"a nested layer":             {`{"mark": {"type": "rule", "strokeDash": [4, 3]},`, `{"layer": [], "mark": {"type": "rule", "strokeDash": [4, 3]},`},
		"a selection set at first":   {`"select": {"type": "point", "fields": ["loads"]}`, `"select": {"type": "point", "fields": ["loads"]}, "value": [{"loads": "Always"}]`},
		"a third selection type":     {`"type": "point", "fields"`, `"type": "region", "fields"`},
	} {
		spec := strings.Replace(base, c[0], c[1], 1)
		if spec == base {
			t.Fatalf("%s: the replacement found nothing", name)
		}
		if _, err := lay(t, spec, 900); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	rules := `{"data":{"values":[{"a":1}]},"mark":"rule","encoding":{"x":{"field":"a","type":"quantitative"}}}`
	if _, err := lay(t, rules, 900); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a chart of rules alone: %v, want ErrUnsupported", err)
	}
	if _, err := lay(t, bundleSpec, 900); err != nil {
		t.Fatalf("the base spec: %v", err)
	}
}

// Through Lay, a chart whose line is dashed a hundred-millionth of a pixel at a time lays out
// promptly (the line drawn solid); it once never returned, whatever its context said. A cancelled
// layout stops cutting dashes, and Lay answers the context's error.
func TestATinyDashThroughLayEnds(t *testing.T) {
	spec := `{"data":{"values":[{"x":0,"y":0},{"x":1,"y":1}]},"mark":{"type":"line","strokeDash":[1e-8,1e-8]},
		"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}}`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		m, release := gui.AcquireMeasurer(1)
		defer release()
		_, err := Lay(ctx, spec, 600, testTheme, m)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Lay: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Lay still laying out a [1e-8, 1e-8] dash after 5 s")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	b := &builder{ctx: cancelled, unit: 1}
	b.stroke(testTheme.Text, 1, []float32{4, 3}, gui.Pt(0, 0), gui.Pt(0, 600))
	if len(b.items) != 0 {
		t.Errorf("a cancelled layout cut %d dashes", len(b.items))
	}
}

// a subtitled chart's text items: the title's and the subtitle's
func titled(t *testing.T, l *Laid, title, subtitle string) (ti, su item) {
	t.Helper()
	for _, it := range l.of(itemText) {
		switch it.text {
		case title:
			ti = it
		case subtitle:
			su = it
		}
	}
	if su.text == "" || (title != "" && ti.text == "") {
		t.Fatalf("labels %v: want %q and %q", l.texts(), title, subtitle)
	}
	return ti, su
}

// A subtitle is drawn under the title in the axes' quieter colour, a list of lines as one, and
// the plot moves down to make room for it, with or without a title.
func TestASubtitleIsDrawnUnderItsTitle(t *testing.T) {
	spec := `{"title":%s,"data":{"values":[{"a":"x","b":4}]},"mark":"bar",
		"encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative"}}}`
	plain := mustLay(t, fmt.Sprintf(spec, `"Commits"`), 2000)
	l := mustLay(t, fmt.Sprintf(spec, `{"text":"Commits","subtitle":["lines added","and removed"]}`), 2000)
	ti, su := titled(t, l, "Commits", "lines added and removed")
	if su.rect.Y < ti.rect.Y+ti.rect.H {
		t.Errorf("the subtitle at %v overlaps the title at %v", su.rect, ti.rect)
	}
	if su.color != testTheme.Axis || ti.color != testTheme.Text {
		t.Errorf("the subtitle in %v, the title in %v: want the axes' colour under the text's", su.color, ti.color)
	}
	bar, plainBar := l.of(itemRect)[0], plain.of(itemRect)[0] // the one bar; the title is drawn before it
	if bar.rect.H != plainBar.rect.H || bar.rect.Y <= plainBar.rect.Y || bar.rect.Y < su.rect.Y+su.rect.H {
		t.Errorf("the bar at %v (%v without the subtitle): want the plot moved under the subtitle at %v", bar.rect, plainBar.rect, su.rect)
	}
	if l.Size().H <= plain.Size().H {
		t.Errorf("height %v, %v without the subtitle: want room for it", l.Size().H, plain.Size().H)
	}
	l = mustLay(t, fmt.Sprintf(spec, `{"subtitle":"alone"}`), 2000)
	_, su = titled(t, l, "", "alone")
	if bar := l.of(itemRect)[0]; bar.rect.Y < su.rect.Y+su.rect.H {
		t.Errorf("a subtitle alone at %v over the bar at %v", su.rect, bar.rect)
	}
}

// A discrete axis sorted by another field orders its categories by that field's aggregate: the
// op asked, or Vega-Lite's default (min, or sum when the bars stack), ascending or descending; a
// category whose aggregate has no value (a min of nothing; a sum of nothing is 0) goes last, and
// an order of null keeps the data's.
func TestCategoriesSortByAField(t *testing.T) {
	// A's orders are 1 and 10 (min 1, sum 11), B's 5, C has none
	rows := `{"values":[{"a":"C","b":1,"s":"p"},{"a":"A","b":2,"o":1,"s":"p"},{"a":"B","b":3,"o":5,"s":"q"},{"a":"A","b":4,"o":10,"s":"q"}]}`
	for _, c := range []struct {
		sort, color string
		want        []string
	}{
		{`{"field":"o"}`, "", []string{"A", "B", "C"}}, // min: A 1, B 5
		{`{"field":"o","order":"descending"}`, "", []string{"B", "A", "C"}},
		{`{"field":"o","op":"sum"}`, "", []string{"C", "B", "A"}},                             // a sum of nothing is 0: C first; A 11
		{`{"field":"o","op":"average","order":"ascending"}`, "", []string{"B", "A", "C"}},     // A 5.5
		{`{"field":"o","op":"count"}`, "", []string{"C", "B", "A"}},                           // rows: C 1, B 1, A 2
		{`{"field":"o"}`, `,"color":{"field":"s","type":"nominal"}`, []string{"C", "B", "A"}}, // stacked: sum
		{`{"field":"o","order":null}`, "", []string{"C", "A", "B"}},
	} {
		sp, err := parse(`{"data":` + rows + `,"mark":"bar","encoding":{"x":{"field":"a","type":"ordinal","sort":` + c.sort + `},"y":{"field":"b","type":"quantitative"}` + c.color + `}}`)
		if err != nil {
			t.Fatal(err)
		}
		p, err := newPlan(sp.layers[0])
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(p.xs.domain, c.want) {
			t.Errorf("sort %s%s: %v, want %v", c.sort, c.color, p.xs.domain, c.want)
		}
	}
}

// commitsSpec is a chart like a pull request's, commit by commit: lines added up and removed down,
// the commits in the order a field gives, not their names', the layers coloured from a domain.
const commitsSpec = `{
  "title": {"text": "A PR, commit by commit", "subtitle": "lines added (up) and removed (down)"},
  "width": 300, "height": 120,
  "data": {"values": [
    {"sha": "f00", "order": 3, "layer": "Tests", "lines": 40},
    {"sha": "f00", "order": 3, "layer": "Tests", "lines": -5},
    {"sha": "b12", "order": 1, "layer": "ADR", "lines": 120},
    {"sha": "b12", "order": 1, "layer": "ADR", "lines": -10},
    {"sha": "a9e", "order": 2, "layer": "DAO", "lines": 80},
    {"sha": "a9e", "order": 2, "layer": "DAO", "lines": -30}
  ]},
  "params": [{"name": "layer", "select": {"type": "point", "fields": ["layer"]}, "bind": "legend"}],
  "mark": {"type": "bar"},
  "encoding": {
    "x": {"field": "sha", "type": "ordinal", "sort": {"field": "order"}, "title": "commit", "axis": {"labelAngle": -40}},
    "y": {"field": "lines", "type": "quantitative"},
    "color": {"field": "layer", "type": "nominal", "scale": {"domain": ["ADR", "DAO", "Tests"]}},
    "opacity": {"condition": {"param": "layer", "value": 1}, "value": 0.2}
  }
}`

// The commits chart draws its commits in the field's order, under its subtitle.
func TestACommitChartSortsByItsOrderField(t *testing.T) {
	l := mustLay(t, commitsSpec, 2000)
	var shas []string
	for _, s := range l.texts() {
		if s == "b12" || s == "a9e" || s == "f00" {
			shas = append(shas, s)
		}
	}
	if !slices.Equal(shas, []string{"b12", "a9e", "f00"}) {
		t.Errorf("commits labelled %v, want b12 a9e f00 (by order)", shas)
	}
	titled(t, l, "A PR, commit by commit", "lines added (up) and removed (down)")
}

// A colour scale's range colours its series in the domain's order, the legend's swatches too,
// cycling when the series outnumber it.
func TestAColourRangeColoursTheSeries(t *testing.T) {
	green, red := color.NRGBA{0x2e, 0x9e, 0x6a, 0xff}, color.NRGBA{0xd6, 0x45, 0x45, 0xff}
	spec := `{"data":{"values":[{"s":"failed","n":1},{"s":"passed","n":5},{"s":"skipped","n":2}]},"mark":"bar",
		"encoding":{"x":{"field":"s","type":"nominal"},"y":{"field":"n","type":"quantitative"},
		"color":{"field":"s","type":"nominal","scale":{"domain":["passed","failed","skipped"],"range":["#2e9e6a","#d64545"]}}}}`
	l := mustLay(t, spec, 2000)
	got := map[string]color.NRGBA{}
	bars := l.bars()
	for i, s := range []string{"failed", "passed", "skipped"} { // the x axis's ascending order
		got[s] = bars[i].color
	}
	if got["passed"] != green || got["failed"] != red || got["skipped"] != green {
		t.Errorf("bars %v: want passed green, failed red, skipped green (the range cycling)", got)
	}
	var swatches []color.NRGBA
	for _, it := range l.of(itemRect)[len(bars):] { // the legend's, after the bars
		swatches = append(swatches, it.color)
	}
	if !slices.Equal(swatches, []color.NRGBA{green, red, green}) {
		t.Errorf("legend swatches %v, want the range in the domain's order", swatches)
	}
	l = mustLay(t, strings.Replace(spec, `"range":["#2e9e6a","#d64545"]`, `"range":"category"`, 1), 2000)
	if c := l.bars()[1].color; c != tableau10[0] {
		t.Errorf("a named range drew passed in %v, want the palette's first", c)
	}
}

// symbolsOf are a chart's symbols, in paint order, named by their shape: a dot or ring is a
// circle, and a polygon is named by its corners.
func symbolsOf(l *Laid, c color.NRGBA) []string {
	var out []string
	for _, it := range l.items {
		if it.color != c {
			continue
		}
		switch {
		case it.kind == itemDot || it.kind == itemRing:
			out = append(out, "circle")
		case it.kind == itemFill || (it.kind == itemLine && len(it.pts) > 1 && it.pts[0] == it.pts[len(it.pts)-1]):
			n := len(it.pts)
			if it.kind == itemLine {
				n--
			}
			out = append(out, map[int]string{3: "triangle", 4: "quad", 12: "cross"}[n])
		}
	}
	return out
}

// A point's shape field gives each value one of Vega's symbols, in the domain's order (Vega's
// range, or the scale's, cycling); a shape value gives every point one; an unfilled point is its
// outline.
func TestPointsTakeVegasSymbols(t *testing.T) {
	spec := `{"data":{"values":[{"x":1,"y":1,"k":"c"},{"x":2,"y":2,"k":"a"},{"x":3,"y":3,"k":"b"},{"x":4,"y":4,"k":"d"}]},
		"mark":{"type":"point","filled":true},
		"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"},%s}}`
	ink := color.NRGBA{0x4c, 0x78, 0xa8, uint8(255*0.7 + 0.5)} // the palette's first at a point's opacity
	for enc, want := range map[string][]string{
		`"shape":{"field":"k","type":"nominal","legend":null}`:                                       {"triangle", "circle", "quad", "cross"},
		`"shape":{"field":"k","type":"nominal","legend":null,"scale":{"range":["diamond","cross"]}}`: {"quad", "quad", "cross", "cross"},
		`"shape":{"value":"triangle-down"}`:                                                          {"triangle", "triangle", "triangle", "triangle"},
	} {
		l := mustLay(t, fmt.Sprintf(spec, enc), 2000)
		if got := symbolsOf(l, ink)[:4]; !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", enc, got, want)
		}
	}
	// the default range's third is triangle-up: one corner, its apex, on top
	l := mustLay(t, fmt.Sprintf(spec, `"shape":{"field":"k","type":"nominal","legend":null}`), 2000)
	tri := l.of(itemFill)[0].pts
	top := slices.MinFunc(tri, func(a, b gui.Point) int { return cmp.Compare(a.Y, b.Y) })
	if n := len(slices.DeleteFunc(slices.Clone(tri), func(p gui.Point) bool { return p.Y != top.Y })); n != 1 {
		t.Errorf("triangle-up %v: %d corners on top, want its apex alone", tri, n)
	}
	l = mustLay(t, strings.Replace(fmt.Sprintf(spec, `"shape":{"value":"square"}`), `"filled":true`, `"filled":false`, 1), 2000)
	if got := symbolsOf(l, ink)[:4]; !slices.Equal(got, []string{"quad", "quad", "quad", "quad"}) || len(l.of(itemFill)) != 0 {
		t.Errorf("unfilled squares: %v and %d fills, want four outlines", got, len(l.of(itemFill)))
	}
}

// A symbol is Vega's: its size the area of its bounding box (a square's side the size's root, a
// circle's diameter too), every polygon wound one way, so overlapping symbols of one colour in
// one path fill rather than cancel.
func TestSymbolsAreVegasGeometry(t *testing.T) {
	spec := `{"data":{"values":[{"x":1,"y":1}]},"mark":{"type":"point","filled":true,"size":100%s},
		"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}%s}}`
	if d := mustLay(t, fmt.Sprintf(spec, "", ""), 2000).of(itemDot)[0]; math.Abs(float64(d.rect.W-10)) > 1e-4 {
		t.Errorf("a size-100 circle %v wide, want 10", d.rect.W)
	}
	sq := mustLay(t, fmt.Sprintf(spec, "", `,"shape":{"value":"square"}`), 2000).of(itemFill)[0]
	if w := sq.pts[1].X - sq.pts[0].X; math.Abs(math.Abs(float64(w))-10) > 1e-4 {
		t.Errorf("a size-100 square %v wide, want 10", w)
	}
	const r = 5
	h := float32(math.Sqrt(3) / 2 * r)
	for name, box := range map[string][2]float32{
		"square": {2 * r, 2 * r}, "diamond": {2 * r, 2 * r}, "cross": {2 * r, 2 * r},
		"triangle-up": {2 * r, 2 * h}, "triangle-down": {2 * r, 2 * h},
		"triangle-left": {2 * h, 2 * r}, "triangle-right": {2 * h, 2 * r},
	} {
		pts := symbolPoints(name, r)
		var area, minX, maxX, minY, maxY float32
		for i, p := range pts {
			q := pts[(i+1)%len(pts)]
			area += p.X*q.Y - q.X*p.Y
			minX, maxX, minY, maxY = min(minX, p.X), max(maxX, p.X), min(minY, p.Y), max(maxY, p.Y)
		}
		if area <= 0 {
			t.Errorf("%s wound anticlockwise", name)
		}
		if math.Abs(float64(maxX-minX-box[0])) > 1e-4 || math.Abs(float64(maxY-minY-box[1])) > 1e-4 {
			t.Errorf("%s %vx%v, want %vx%v", name, maxX-minX, maxY-minY, box[0], box[1])
		}
	}
	if symbolPoints("circle", r) != nil {
		t.Error("a circle as a polygon, want the dot's")
	}
}

// boundarySpec is a chart like a filter's window: two dashed rules at its edges and a point for
// each case, coloured in or out with no colour legend, shaped by its case with a shape legend.
const boundarySpec = `{
  "title": {"text": "The 2019 window is half-open", "subtitle": "min in, before out"},
  "width": 400, "height": 60,
  "layer": [
    {"data": {"values": [{"t": "2019-01-01T00:00:00Z"}, {"t": "2020-01-01T00:00:00Z"}]},
     "mark": {"type": "rule", "strokeDash": [4, 3], "strokeWidth": 2},
     "encoding": {"x": {"field": "t", "type": "temporal"}, "color": {"value": "#7a7a7a"}}},
    {"data": {"values": [
       {"case": "just before", "d": "2018-12-31T23:59:59Z", "in": "no"},
       {"case": "first instant", "d": "2019-01-01T00:00:00Z", "in": "yes"},
       {"case": "mid-year", "d": "2019-07-01T12:00:00Z", "in": "yes"},
       {"case": "after", "d": "2020-01-01T00:00:00Z", "in": "no"}]},
     "mark": {"type": "point", "filled": true, "size": 220},
     "encoding": {
       "x": {"field": "d", "type": "temporal", "scale": {"domain": ["2018-11-01", "2020-03-01"]}},
       "y": {"field": "in", "type": "nominal", "sort": ["yes", "no"]},
       "color": {"field": "in", "type": "nominal", "scale": {"domain": ["yes", "no"], "range": ["#2e9e6a", "#d64545"]}, "legend": null},
       "shape": {"field": "case", "type": "nominal", "title": "case"}}}
  ]
}`

// The window chart draws its rules, its points coloured by the range and shaped by their case,
// and a shape legend, in the axes' colour, that fits in the chart however short the plot.
func TestAShapeLegendNamesEachShape(t *testing.T) {
	l := mustLay(t, boundarySpec, 2000)
	green, red := color.NRGBA{0x2e, 0x9e, 0x6a, 0xff}, color.NRGBA{0xd6, 0x45, 0x45, 0xff}
	fade := func(c color.NRGBA) color.NRGBA { c.A = uint8(255*0.7 + 0.5); return c }
	if got := append(symbolsOf(l, fade(red)), symbolsOf(l, fade(green))...); !slices.Equal(got, []string{"triangle", "circle", "quad", "cross"}) {
		t.Errorf("points %v, want out: triangle-up, circle; in: square, cross (the cases ascending: after, first instant, just before, mid-year)", got)
	}
	if got := symbolsOf(l, testTheme.Axis); !slices.Equal(got, []string{"circle", "quad", "triangle", "cross"}) {
		t.Errorf("legend symbols %v, want circle, square, triangle-up, cross", got)
	}
	var legend []item
	for _, it := range l.of(itemText) {
		if slices.Contains([]string{"case", "just before", "first instant", "mid-year", "after"}, it.text) {
			legend = append(legend, it)
		}
	}
	if want := []string{"case", "after", "first instant", "just before", "mid-year"}; len(legend) != 5 || !slices.EqualFunc(legend, want, func(it item, s string) bool { return it.text == s }) {
		t.Fatalf("legend labels %v, want the title, then each case", legend)
	}
	titled(t, l, "The 2019 window is half-open", "min in, before out")
	// a plot shorter than its legend: the chart grows to hold the legend
	l = mustLay(t, strings.Replace(boundarySpec, `"height": 60`, `"height": 10`, 1), 2000)
	for _, it := range l.of(itemText) {
		if it.text == "mid-year" && it.rect.Y+it.rect.H > l.Size().H {
			t.Errorf("the legend's last label at %v, below the chart's %v", it.rect, l.Size().H)
		}
	}
}

// A shape by a field of its own has a legend of its own, under the colour's.
func TestTwoLegendsStack(t *testing.T) {
	spec := `{"data":{"values":[{"x":1,"y":1,"g":"g1","k":"k1"},{"x":2,"y":2,"g":"g2","k":"k2"}]},"mark":"point",
		"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"},
		"color":{"field":"g","type":"nominal"},"shape":{"field":"k","type":"nominal"}}}`
	at := map[string]gui.Rect{}
	for _, it := range mustLay(t, spec, 2000).of(itemText) {
		at[it.text] = it.rect
	}
	if g, k := at["g2"], at["k"]; k.H == 0 || k.Y < g.Y+g.H {
		t.Errorf("the shape legend's title at %v, the colour legend's last entry at %v: want it below", k, g)
	}
}

// A shape by the colour's field shares the colour's legend, as Vega-Lite merges them: one entry a
// value, its swatch the value's symbol in its colour.
func TestAShapeByTheColoursFieldSharesItsLegend(t *testing.T) {
	spec := `{"data":{"values":[{"x":1,"y":1,"k":"a"},{"x":2,"y":2,"k":"b"}]},"mark":{"type":"point","filled":true},
		"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"},
		"color":{"field":"k","type":"nominal"},"shape":{"field":"k","type":"nominal"}}}`
	l := mustLay(t, spec, 2000)
	n := 0
	for _, s := range l.texts() {
		if s == "b" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%q labelled %d times, want one legend", "b", n)
	}
	if got := symbolsOf(l, testTheme.Axis); len(got) != 0 {
		t.Errorf("a second legend's symbols %v", got)
	}
	if got := symbolsOf(l, tableau10[1]); !slices.Equal(got, []string{"quad"}) {
		t.Errorf("b's swatch %v, want its square", got)
	}
}

// vm43Spec is a chart like a test gate's: stacked outcome bars, and a text layer filtered to the
// failures. The text mark and the transform stay outside the subset, so it declines whole.
const vm43Spec = `{
  "title": {"text": "The gate", "subtitle": "one failure"},
  "data": {"values": [{"suite": "dao", "outcome": "passed", "tests": 9}, {"suite": "root", "outcome": "failed", "tests": 1}]},
  "layer": [
    {"mark": "bar", "encoding": {"y": {"field": "suite", "type": "nominal"}, "x": {"field": "tests", "type": "quantitative", "stack": "zero"},
      "color": {"field": "outcome", "type": "nominal", "scale": {"domain": ["passed", "failed"], "range": ["#2e9e6a", "#d64545"]}}}},
    {"transform": [{"filter": "datum.outcome == 'failed'"}], "mark": {"type": "text", "dx": 6},
      "encoding": {"y": {"field": "suite", "type": "nominal"}, "x": {"field": "tests", "type": "quantitative", "aggregate": "sum"}, "text": {"value": "1 failure"}}}
  ]
}`

func TestAFilteredTextLayerStillDeclines(t *testing.T) {
	if _, err := lay(t, vm43Spec, 600); !errors.Is(err, ErrUnsupported) {
		t.Errorf("%v, want ErrUnsupported", err)
	}
	// its bars alone draw: the subtitle and the range are in the subset
	var sp map[string]any
	_ = json.Unmarshal([]byte(vm43Spec), &sp)
	sp["layer"] = sp["layer"].([]any)[:1]
	src, _ := json.Marshal(sp)
	mustLay(t, string(src), 600)
}
