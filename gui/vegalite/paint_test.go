package vegalite

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	cv "github.com/yongjohnlee80/golib/chart/vegalite"
	"github.com/yongjohnlee80/golib/gui"
)

// The layout's own cells are chart/vegalite's; these are the painting's, over the window's shaper.

var testTheme = Theme{
	Background: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	Text:       color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff},
	Axis:       color.NRGBA{R: 0x88, G: 0x88, B: 0x88, A: 0xff},
	Grid:       color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff},
	Font:       gui.Font{Size: 11},
}

func mustLay(t *testing.T, src string, width float32) *Laid {
	t.Helper()
	m, release := gui.AcquireMeasurer(1)
	defer release()
	l, err := Lay(context.Background(), src, width, testTheme, m)
	if err != nil {
		t.Fatalf("Lay: %v", err)
	}
	return l
}

// of are the marks of a kind, in paint order.
func (l *Laid) of(k cv.MarkKind) []cv.Mark {
	var out []cv.Mark
	for _, it := range l.chart.Marks() {
		if it.Kind == k {
			out = append(out, it)
		}
	}
	return out
}

// texts are the labels drawn, in paint order.
func (l *Laid) texts() []string {
	var out []string
	for _, it := range l.of(cv.MarkText) {
		out = append(out, it.Text)
	}
	return out
}

// bars are the filled rectangles drawn before the first label: the marks, not the legend's
// swatches.
func (l *Laid) bars() []cv.Mark {
	var out []cv.Mark
	for _, it := range l.chart.Marks() {
		if it.Kind == cv.MarkText {
			break
		}
		if it.Kind == cv.MarkRect {
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

// Lay measures with the window's shaper: a label's box is as wide as the measurer says, so a
// label is placed by the font the window draws it in.
func TestLayMeasuresWithTheWindowsShaper(t *testing.T) {
	m, release := gui.AcquireMeasurer(1)
	defer release()
	l := mustLay(t, lineSpec, 2000)
	checked := 0
	for _, it := range l.of(cv.MarkText) {
		if it.Align == -1 && !it.Up { // a legend's title and labels: each box its measured width
			checked++
			if want := m.Measure(it.Text, gui.Font(it.Font)).Width(); it.Rect.W != want {
				t.Errorf("%q in a box %v wide, measured %v", it.Text, it.Rect.W, want)
			}
		}
	}
	if checked < 3 { // the title "sym", then A and B
		t.Fatalf("%d legend labels checked, want the title and both series", checked)
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
	if count["DrawText"] != len(l.of(cv.MarkText)) {
		t.Errorf("%d texts drawn, %d laid", count["DrawText"], len(l.of(cv.MarkText)))
	}
	up := 0
	for _, it := range l.of(cv.MarkText) {
		if it.Up {
			up++
		}
	}
	if up == 0 || count["PushTransform"] != up {
		t.Errorf("%d transforms pushed for %d upright labels", count["PushTransform"], up)
	}
	shapes := len(l.chart.Marks()) - len(l.of(cv.MarkText))
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
	if len(l.of(cv.MarkRing)) != 2000 || strokes > 10 {
		t.Errorf("%d rings stroked in %d calls, want a handful", len(l.of(cv.MarkRing)), strokes)
	}
	for _, it := range l.chart.Marks() {
		for _, v := range []float32{it.Rect.X, it.Rect.Y, it.Rect.W, it.Rect.H} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("an item at %+v", it)
			}
		}
	}
}
