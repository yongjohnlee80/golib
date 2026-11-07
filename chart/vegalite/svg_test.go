package vegalite

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
)

// wellFormed fails t unless out parses as XML.
func wellFormed(t *testing.T, out []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(out))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("not well-formed (%v):\n%s", err, out)
		}
	}
}

// A chart writes itself as a standalone, well-formed SVG of its size: a <rect> a bar in the bar's
// colour, a <polyline> an axis, and every label a <text> saying it, escaped.
func TestAChartWritesItselfAsSVG(t *testing.T) {
	l := mustLay(t, strings.Replace(barSpec, `"C"`, `"C & <D>"`, 1), 2000)
	out := l.SVG()
	wellFormed(t, out)
	s := string(out)
	sz := l.Size()
	if head := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s">`,
		num(sz.W), num(sz.H), num(sz.W), num(sz.H)); !strings.HasPrefix(s, head) {
		t.Errorf("want it to open %s; it opens %.120s", head, s)
	}
	for _, b := range l.bars() {
		r := b.rect
		el := fmt.Sprintf(`<rect x="%s" y="%s" width="%s" height="%s" fill="#%02x%02x%02x"/>`, num(r.X), num(r.Y), num(r.W), num(r.H), b.color.R, b.color.G, b.color.B)
		if !strings.Contains(s, el) {
			t.Errorf("bar %+v is not written as %s", r, el)
		}
	}
	if !strings.Contains(s, "<polyline ") {
		t.Error("no axis written")
	}
	for _, label := range l.texts() {
		if !strings.Contains(s, ">"+textEscaper.Replace(label)+"</text>") {
			t.Errorf("label %q is not written:\n%s", label, s)
		}
	}
	if !strings.Contains(s, ">C &amp; &lt;D&gt;</text>") {
		t.Error("a category with & and < is not escaped")
	}
}

// A chart wider than the width asked is one group scaled by its fit, its marks in its own units;
// one that fits is not grouped.
func TestAFittedChartIsOneScaledGroup(t *testing.T) {
	l := mustLay(t, strings.Replace(barSpec, `"mark"`, `"width": 900, "mark"`, 1), 300)
	s := string(l.SVG())
	want := fmt.Sprintf(`<g transform="scale(%v)">`, l.Fit())
	if l.Fit() >= 1 || !strings.Contains(s, want) || !strings.HasSuffix(s, "</g></svg>") {
		t.Errorf("fit %v: want the marks in %s:\n%.300s", l.Fit(), want, s)
	}
	if s := string(mustLay(t, barSpec, 2000).SVG()); strings.Contains(s, "<g ") {
		t.Errorf("a chart that fits is grouped:\n%.300s", s)
	}
}

// A label sits where it was measured for: at its box's left, middle or right by its align, on its
// box's middle line; an upright one starts at its box's bottom middle and is turned a quarter
// left about that point.
func TestALabelIsPlacedByItsBox(t *testing.T) {
	l := mustLay(t, `{"data":{"values":[{"a":"a very long category name","b":1,"c":"x"},{"a":"another long one","b":2,"c":"y"}]},"mark":"bar",
		"encoding":{"x":{"field":"a","type":"nominal"},"y":{"field":"b","type":"quantitative"},"color":{"field":"c","type":"nominal"}}}`, 2000)
	seen := map[string]bool{}
	for _, it := range l.of(itemText) {
		var b bytes.Buffer
		writeText(&b, it)
		el, r := b.String(), it.rect
		var want string
		switch {
		case it.up:
			x, y := num(r.X+r.W/2), num(r.Y+r.H)
			want = fmt.Sprintf(`x="%s" y="%s" text-anchor="start" dominant-baseline="central" xml:space="preserve" transform="rotate(-90 %s %s)"`, x, y, x, y)
			seen["up"] = true
		case it.align == -1:
			want = fmt.Sprintf(`x="%s" y="%s" text-anchor="start"`, num(r.X), num(r.Y+r.H/2))
			seen["left"] = true
		case it.align == 1:
			want = fmt.Sprintf(`x="%s" y="%s" text-anchor="end"`, num(r.X+r.W), num(r.Y+r.H/2))
			seen["right"] = true
		default:
			want = fmt.Sprintf(`x="%s" y="%s" text-anchor="middle"`, num(r.X+r.W/2), num(r.Y+r.H/2))
			seen["middle"] = true
		}
		if !strings.Contains(el, want) {
			t.Errorf("%q in %+v (align %d, up %v): want %s, wrote %s", it.text, r, it.align, it.up, want, el)
		}
		if it.font.Bold != strings.Contains(el, `font-weight="bold"`) || !strings.Contains(el, `font-family="sans-serif"`) {
			t.Errorf("%q's font %+v is written %s", it.text, it.font, el)
		}
	}
	for _, k := range []string{"up", "left", "right", "middle"} {
		if !seen[k] {
			t.Errorf("no %s label in the chart: the cell checks nothing there", k)
		}
	}
}

// A colour's alpha is its opacity: a point's 0.7 is a stroke-opacity, an opaque fill has none.
func TestAColoursAlphaIsItsOpacity(t *testing.T) {
	if got := svgPaint("stroke", tableau10[0]); strings.Contains(got, "opacity") {
		t.Errorf("an opaque colour: %s", got)
	}
	c := tableau10[1]
	c.A = uint8(255*0.7 + 0.5)
	if got, want := svgPaint("stroke", c), ` stroke="#f58518" stroke-opacity="0.7"`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// Without a shaper, a label is measured by Helvetica's advances: a digit is 0.556 em, a narrow
// letter less, a wide East Asian character an em, a combining mark nothing, and bold a little
// wider. Nil measures so in Lay.
func TestApproxMeasureReadsHelvetica(t *testing.T) {
	f := Font{Size: 10}
	near := func(got, want float32) bool { return math.Abs(float64(got-want)) < 0.001 }
	for _, c := range []struct {
		s    string
		want float32
	}{
		{"", 0}, {"10", 11.12}, {"il", 4.44}, {"W", 9.44}, {"한글", 20}, {"漢", 10}, {"e\u0301", 5.56}, {"é", 5.56},
	} {
		if got := ApproxMeasure(c.s, f); !near(got, c.want) {
			t.Errorf("%q at 10 px: %v, want %v", c.s, got, c.want)
		}
	}
	if b := ApproxMeasure("10", Font{Size: 10, Bold: true}); !(b > 11.12) {
		t.Errorf("bold %v is not wider than regular 11.12", b)
	}
	a, err := Lay(t.Context(), barSpec, 2000, testTheme, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := mustLay(t, barSpec, 2000)
	if a.Size() != b.Size() || len(a.items) != len(b.items) {
		t.Errorf("Lay with no measure %+v (%d items), with ApproxMeasure %+v (%d)", a.Size(), len(a.items), b.Size(), len(b.items))
	}
}

// Marks are the items as a painter reads them, in paint order, nothing left out.
func TestMarksAreTheItems(t *testing.T) {
	l := mustLay(t, lineSpec, 2000)
	ms := l.Marks()
	if len(ms) != len(l.items) {
		t.Fatalf("%d marks for %d items", len(ms), len(l.items))
	}
	for i, it := range l.items {
		m := ms[i]
		if m.Kind != MarkKind(it.kind) || m.Rect != it.rect || len(m.Points) != len(it.pts) || m.Color != it.color ||
			m.Width != it.width || m.Text != it.text || m.Font != it.font || m.Align != it.align || m.Up != it.up {
			t.Errorf("mark %d %+v, item %+v", i, m, it)
		}
	}
}
