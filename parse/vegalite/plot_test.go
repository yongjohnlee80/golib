package vegalite

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func plot(t *testing.T, src string) *Chart {
	t.Helper()
	c, err := Plot(context.Background(), src)
	if err != nil {
		t.Fatalf("Plot: %v", err)
	}
	return c
}

// A stacked bar is plotted as a renderer reads it: the categories in the axis's order, each one's
// series in Series' order, piled V0 under V1, the measure's axis from zero over the piles, the
// titles Vega-Lite's, and the colour's legend.
func TestAStackedBarIsPlottedInDomainThenSeriesOrder(t *testing.T) {
	c := plot(t, `{"data":{"values":[
		{"c":"b","s":"y","v":2},{"c":"a","s":"y","v":1},{"c":"b","s":"x","v":3},{"c":"a","s":"x","v":4},{"c":"a","s":"x","v":1}]},
		"mark":"bar","encoding":{"x":{"field":"c","type":"nominal"},"y":{"aggregate":"sum","field":"v","type":"quantitative"},
		"color":{"field":"s","type":"nominal"}}}`)
	if !slices.Equal(c.X.Domain, []string{"a", "b"}) || !c.X.Discrete || !c.X.Band || !slices.Equal(c.Series, []string{"x", "y"}) {
		t.Fatalf("x %+v, series %v; want the categories a, b on a band and the series x, y", c.X, c.Series)
	}
	if len(c.Layers) != 1 || !c.Layers[0].Stacked || c.Layers[0].Mark != MarkBar {
		t.Fatalf("layers %+v; want one stacked bar", c.Layers)
	}
	type row struct {
		cat, series string
		v0, v1      float64
	}
	var got []row
	for _, d := range c.Layers[0].Data {
		got = append(got, row{d.X.Str, d.Series, d.V0, d.V1})
	}
	want := []row{{"a", "x", 0, 5}, {"a", "y", 5, 6}, {"b", "x", 0, 3}, {"b", "y", 3, 5}}
	if !slices.Equal(got, want) {
		t.Errorf("data %+v; want %+v (domain order, then series order, piled)", got, want)
	}
	if c.Y.Lo != 0 || c.Y.Hi != 6 || c.Y.Discrete {
		t.Errorf("y %v..%v; want 0..6, the tallest pile", c.Y.Lo, c.Y.Hi)
	}
	if c.Y.Title != "Sum of v" || c.X.Title != "c" || c.ColorLegend.Off || c.ColorLegend.Title != "s" || c.Layers[0].Title != "Sum of v" {
		t.Errorf("titles: x %q, y %q, legend %+v; want c, Sum of v, and the legend s", c.X.Title, c.Y.Title, c.ColorLegend)
	}
}

// A line's points are in x order within each series, whatever order the rows come in: a renderer
// joins consecutive points.
func TestALinesPointsAreInXOrderWithinEachSeries(t *testing.T) {
	c := plot(t, `{"data":{"values":[{"x":3,"y":1,"s":"b"},{"x":1,"y":2,"s":"a"},{"x":2,"y":3,"s":"b"},{"x":3,"y":4,"s":"a"},{"x":1,"y":5,"s":"b"}]},
		"mark":"line","encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"},"color":{"field":"s","type":"nominal"}}}`)
	xs := map[string][]float64{}
	for _, d := range c.Layers[0].Data {
		xs[d.Series] = append(xs[d.Series], d.X.Num)
	}
	if !slices.Equal(xs["a"], []float64{1, 3}) || !slices.Equal(xs["b"], []float64{1, 2, 3}) {
		t.Errorf("x by series %v; want each series in x order", xs)
	}
}

// A datum outside its axis's domain (the spec names the categories) is left out of the plot.
func TestADatumOutsideTheDomainIsLeftOut(t *testing.T) {
	c := plot(t, `{"data":{"values":[{"c":"a","v":1},{"c":"b","v":2},{"c":"z","v":9}]},
		"mark":"bar","encoding":{"x":{"field":"c","type":"nominal","scale":{"domain":["b","a"]}},"y":{"field":"v","type":"quantitative"}}}`)
	var cats []string
	for _, d := range c.Layers[0].Data {
		cats = append(cats, d.X.Str)
	}
	if !slices.Equal(c.X.Domain, []string{"b", "a"}) || !slices.Equal(cats, []string{"b", "a"}) {
		t.Errorf("domain %v, data %v; want b, a and nothing of z", c.X.Domain, cats)
	}
}

// A temporal axis is in Unix milliseconds: its extent and every datum's position on it.
func TestATemporalAxisIsInUnixMilliseconds(t *testing.T) {
	c := plot(t, `{"data":{"values":[{"d":"2026-01-01","v":1},{"d":"2026-01-03","v":2}]},
		"mark":"line","encoding":{"x":{"field":"d","type":"temporal"},"y":{"field":"v","type":"quantitative"}}}`)
	first := float64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	if !c.X.Temporal || c.X.Lo != first || c.Layers[0].Data[0].X.Num != first {
		t.Errorf("x %+v, first datum %v; want temporal from %v (Unix ms)", c.X, c.Layers[0].Data[0].X.Num, first)
	}
}

// A rule over points is a layer of its own: its values on its axis, no data.
func TestARuleIsALayerOfItsValues(t *testing.T) {
	c := plot(t, `{"data":{"values":[{"x":1,"y":1,"at":2},{"x":3,"y":2,"at":2},{"x":5,"y":3,"at":4}]},
		"layer":[{"mark":"point","encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}},
		{"mark":"rule","encoding":{"x":{"field":"at","type":"quantitative"}}}]}`)
	if len(c.Layers) != 2 || c.Layers[1].Rule == nil || !c.Layers[1].Rule.OnX || c.Layers[1].Data != nil || c.Layers[1].Title != "at" {
		t.Fatalf("layers %+v; want a point layer, then a rule on x", c.Layers)
	}
	var at []float64
	for _, v := range c.Layers[1].Rule.Values {
		at = append(at, v.Num)
	}
	if !slices.Equal(at, []float64{2, 4}) || !c.Layers[0].Faded {
		t.Errorf("rule at %v, points faded %v; want 2 and 4, and points one per row faded", at, c.Layers[0].Faded)
	}
}

// A spec outside the subset is ErrUnsupported, one over a limit ErrTooLarge: a renderer's notice.
func TestPlotRefusesWhatItDoesNotDraw(t *testing.T) {
	if _, err := Plot(context.Background(), `{"data":{"values":[]},"transform":[{"filter":"true"}],"mark":"bar"}`); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a transform: %v; want ErrUnsupported", err)
	}
	if _, err := Plot(context.Background(), `{"mark":"bar"`); err == nil || errors.Is(err, ErrUnsupported) {
		t.Errorf("malformed JSON: %v; want the decoder's error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Plot(ctx, `{"data":{"values":[{"x":1,"y":1}]},"mark":"point","encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}}`); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled context: %v; want context.Canceled", err)
	}
}

// A horizontal bar's dimension is y: its data in the y axis's order (the first category on top),
// each category's series in Series' order.
func TestAHorizontalBarsDataIsInItsYAxissOrder(t *testing.T) {
	c := plot(t, `{"data":{"values":[{"c":"b","s":"x","v":1},{"c":"a","s":"y","v":2},{"c":"a","s":"x","v":3},{"c":"b","s":"y","v":4}]},
		"mark":"bar","encoding":{"y":{"field":"c","type":"nominal"},"x":{"field":"v","type":"quantitative"},"color":{"field":"s","type":"nominal"}}}`)
	l := c.Layers[0]
	var got []string
	for _, d := range l.Data {
		got = append(got, d.Y.Str+d.Series)
	}
	if !l.Horizontal || !slices.Equal(c.Y.Domain, []string{"a", "b"}) || !slices.Equal(got, []string{"ax", "ay", "bx", "by"}) {
		t.Errorf("horizontal %v, y %v, data %v; want a horizontal bar, a then b, each x then y", l.Horizontal, c.Y.Domain, got)
	}
}

// Plot leaves a continuous axis's extent as the data's: nicing is the renderer's, to its own
// ticks (NiceDomain), so a window and a terminal can round it differently.
func TestPlotLeavesNicingToTheRenderer(t *testing.T) {
	c := plot(t, `{"data":{"values":[{"x":3,"y":37},{"x":9,"y":12}]},
		"mark":"point","encoding":{"x":{"field":"x","type":"quantitative","scale":{"zero":false}},"y":{"field":"y","type":"quantitative"}}}`)
	if c.Y.Lo != 0 || c.Y.Hi != 37 || !c.Y.Nice || c.X.Lo != 3 || c.X.Hi != 9 {
		t.Errorf("y %v..%v (nice %v), x %v..%v; want y 0..37 to be niced by its renderer, x the data's 3..9", c.Y.Lo, c.Y.Hi, c.Y.Nice, c.X.Lo, c.X.Hi)
	}
	if lo, hi := NiceDomain(c.Y.Lo, c.Y.Hi, 5); lo != 0 || hi != 40 {
		t.Errorf("NiceDomain(0, 37, 5) = %v..%v; want 0..40", lo, hi)
	}
}
