package vegalite

import (
	"context"
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
	p, err := newPlan(c)
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
		p, err := newPlan(c)
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
		p, err := newPlan(c)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(p.xs.domain, want) {
			t.Errorf("sort %s: %v, want %v", sort, p.xs.domain, want)
		}
	}
	c, _ := parse(`{"data":{"values":[{"a":"10","b":1},{"a":"9","b":1},{"a":"x","b":1}]},"mark":"bar","encoding":{"x":{"field":"a","type":"ordinal"},"y":{"field":"b","type":"quantitative"}}}`)
	p, _ := newPlan(c)
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
		"a params":        `{` + ok + `,"mark":"bar","params":[{"name":"p","select":"interval"}]}`,
		"a mark's orient": `{` + ok + `,"mark":{"type":"bar","orient":"horizontal"}}`,
		"a step width":    `{` + ok + `,"mark":"bar","width":{"step":30}}`,
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
	spec := `{"$schema":"x","config":{"view":{"stroke":null}},"background":"white","padding":5,"autosize":"fit",
		"title":{"text":"Sales","anchor":"start"},
		"data":{"values":[{"a":"x","b":1}]},"mark":{"type":"bar","tooltip":true,"cornerRadiusEnd":4},
		"encoding":{"x":{"field":"a","type":"nominal","axis":{"labelAngle":-45,"title":"Kind"},"scale":{"paddingInner":0.2}},
		"y":{"field":"b","type":"quantitative","axis":{"grid":false,"format":"d"}},"tooltip":[{"field":"b"}]}}`
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
