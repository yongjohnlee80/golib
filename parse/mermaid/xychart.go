package mermaid

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// XYChart is the XY chart kind's value, declared here and registered in init (kind.go).
const XYChart Kind = 13

func init() {
	register(XYChart, "xychart", []string{"xychart-beta", "xychart"}, (*parser).xychart)
}

// XYChartDiagram is a parsed XY chart: bars and lines over one x axis, its categories or a numeric
// range, and one numeric y axis.
type XYChartDiagram struct {
	Title string
	// Horizontal is the header's horizontal: the x axis runs down the left, bars run across.
	Horizontal bool
	XAxis      XYAxis
	YAxis      XYAxis
	Series     []XYSeries
}

func (*XYChartDiagram) Kind() Kind { return XYChart }

// XYAxis is an axis: its title, and either its categories (the x axis only) or its range. With
// neither, the x axis's points are numbered from 1 and the y axis's range is the data's.
type XYAxis struct {
	Title      string
	Categories []string
	// Ranged is set when the chart gives the axis a range, Min --> Max.
	Ranged   bool
	Min, Max float64
}

// XYSeriesKind is what a series is drawn as.
type XYSeriesKind uint8

const (
	XYBar  XYSeriesKind = iota + 1 // bar
	XYLine                         // line
)

var xySeriesNames = [...]string{XYBar: "bar", XYLine: "line"}

func (k XYSeriesKind) String() string               { return enumName(xySeriesNames[:], int(k)) }
func (k XYSeriesKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// XYSeries is one series: a bar or a line, its name ("" when it has none), and its values, one a
// point of the x axis, in order.
type XYSeries struct {
	Kind   XYSeriesKind
	Name   string
	Values []float64
	Span   [2]int // its statement
}

// Points is how many points the chart's x axis has: its categories, else its longest series.
func (d *XYChartDiagram) Points() int {
	if n := len(d.XAxis.Categories); n > 0 {
		return n
	}
	n := 0
	for _, s := range d.Series {
		n = max(n, len(s.Values))
	}
	return n
}

// ValueRange is the value axis's range: the chart's own, else its data's, from 0 when every value
// is above it, one wide when every value is one; 0 to 1 for a chart of no values.
func (d *XYChartDiagram) ValueRange() (lo, hi float64) {
	if d.YAxis.Ranged {
		return d.YAxis.Min, d.YAxis.Max
	}
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, s := range d.Series {
		for _, v := range s.Values {
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	if math.IsInf(lo, 1) {
		return 0, 1
	}
	lo = min(lo, 0)
	if hi <= lo {
		hi = lo + 1
	}
	return lo, hi
}

// xyParser is the parse of one XY chart.
type xyParser struct {
	p *parser
	d *XYChartDiagram
}

// xychart parses an XY chart: rest is its header after the keyword, `horizontal` or `vertical` or
// nothing. Each line is a title, an axis, a bar or a line, or an accessibility line. A series
// longer than the x axis's categories is a syntax error: it has points no category names.
func (p *parser) xychart(rest string, restOff int) (Diagram, error) {
	t := &xyParser{p: p, d: &XYChartDiagram{}}
	switch s := strings.TrimSpace(rest); s {
	case "", "vertical":
	case "horizontal":
		t.d.Horizontal = true
	default:
		bad, _ := firstWord(s)
		p.fail(restOff+strings.Index(rest, s), "unexpected %q after xychart: horizontal or vertical", bad)
	}
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		t.statement(st)
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	if n := len(t.d.XAxis.Categories); n > 0 {
		for _, s := range t.d.Series {
			if len(s.Values) > n {
				p.fail(s.Span[0], "a %s of %d values is longer than the x axis's %d categories", s.Kind, len(s.Values), n)
				break
			}
		}
	}
	return p.done(t.d)
}

// statement is one line.
func (t *xyParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "title":
		if s, err := t.p.label(rest, restOff); err == nil {
			t.d.Title = s
		}
	case "x-axis":
		t.axis(&t.d.XAxis, rest, restOff, true)
	case "y-axis":
		t.axis(&t.d.YAxis, rest, restOff, false)
	case "bar":
		t.series(XYBar, rest, restOff, st)
	case "line":
		t.series(XYLine, rest, restOff, st)
	case "accTitle:", "accDescr:":
		// accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if !strings.HasPrefix(rest, ":") {
			t.p.unsupport(st.off, w+" block")
		}
	default:
		t.p.fail(st.off, "unexpected %q: an XY chart's line is a title, an axis, a bar or a line", w)
	}
}

// axis reads an axis: an optional title (quoted, or one word), then categories `[a, "b c", …]`
// (the x axis only) or a range `min --> max`, or neither.
func (t *xyParser) axis(a *XYAxis, rest string, off int, categories bool) {
	*a = XYAxis{}
	title, tail, toff := xyTitle(rest)
	if title != "" {
		lbl, err := t.p.label(title, off)
		if err != nil {
			return
		}
		a.Title = lbl
	}
	tail = strings.TrimSpace(tail)
	toff = off + toff
	switch {
	case tail == "":
	case strings.HasPrefix(tail, "["):
		if !categories {
			t.p.fail(toff, "the y axis takes a range, min --> max, not categories")
			return
		}
		items, ok := t.list(tail, toff)
		if !ok {
			return
		}
		for i, it := range items {
			lbl, err := t.p.label(it, toff)
			if err != nil {
				return
			}
			if strings.TrimSpace(lbl) == "" {
				t.p.fail(toff, "the x axis's category %d is empty", i+1)
				return
			}
			a.Categories = append(a.Categories, lbl)
		}
	default:
		lo, hi, ok := strings.Cut(tail, "-->")
		if !ok {
			t.p.fail(toff, "unexpected %q: an axis wants [categories] or min --> max after its title", tail)
			return
		}
		from, okFrom := t.number(lo, toff, "an axis's min")
		to, okTo := t.number(hi, toff, "an axis's max")
		if !okFrom || !okTo {
			return
		}
		if from >= to {
			t.p.fail(toff, "an axis's min %s is not below its max %s", strings.TrimSpace(lo), strings.TrimSpace(hi))
			return
		}
		a.Ranged, a.Min, a.Max = true, from, to
	}
}

// xyTitle splits an axis's text into its title (quoted, or the first word when what follows it is
// a list or a range) and the rest, with the rest's offset in s.
func xyTitle(s string) (title, rest string, restOff int) {
	t := strings.TrimLeft(s, " \t")
	lead := len(s) - len(t)
	if strings.HasPrefix(t, `"`) {
		if end := strings.IndexByte(t[1:], '"'); end >= 0 {
			return t[:end+2], t[end+2:], lead + end + 2
		}
		return t, "", len(s)
	}
	if strings.HasPrefix(t, "[") || xyStartsNumber(t) {
		return "", t, lead
	}
	w, after := firstWord(t)
	return w, after, len(s) - len(after)
}

// xyStartsNumber reports whether s starts as a number does: a digit, a sign or a point.
func xyStartsNumber(s string) bool {
	return s != "" && (s[0] >= '0' && s[0] <= '9' || s[0] == '-' || s[0] == '+' || s[0] == '.')
}

// series reads `bar "name" [v, …]` or `line [v, …]`: an optional quoted name, then its values.
func (t *xyParser) series(k XYSeriesKind, rest string, off int, st stmt) {
	s := XYSeries{Kind: k, Span: [2]int{st.off, st.off + len(st.text)}}
	body := strings.TrimSpace(rest)
	if strings.HasPrefix(body, `"`) {
		end := strings.IndexByte(body[1:], '"')
		if end < 0 {
			t.p.fail(off, "a %s's name is not closed", k)
			return
		}
		name, err := t.p.label(body[:end+2], off)
		if err != nil {
			return
		}
		s.Name, body = name, strings.TrimSpace(body[end+2:])
	}
	if !strings.HasPrefix(body, "[") {
		t.p.fail(off, "a %s wants its values in [ ]", k)
		return
	}
	items, ok := t.list(body, off)
	if !ok {
		return
	}
	for i, it := range items {
		v, ok := t.number(it, off, "a "+k.String()+"'s value "+strconv.Itoa(i+1))
		if !ok {
			return
		}
		s.Values = append(s.Values, v)
	}
	if len(s.Values) == 0 {
		t.p.fail(off, "a %s has no values", k)
		return
	}
	t.d.Series = append(t.d.Series, s)
}

// list reads `[a, b, …]`, the whole of s, into its items, a quoted item's commas its own.
func (t *xyParser) list(s string, off int) ([]string, bool) {
	if !strings.HasSuffix(s, "]") {
		t.p.fail(off, "a [ ] list is not closed")
		return nil, false
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return nil, true
	}
	var out []string
	quoted, start := false, 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	if quoted {
		t.p.fail(off, "a quoted item in a [ ] list is not closed")
		return nil, false
	}
	return append(out, strings.TrimSpace(inner[start:])), true
}

// number reads one finite number; what names it in an error.
func (t *xyParser) number(s string, off int, what string) (float64, bool) {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	switch {
	case err != nil:
		t.p.fail(off, "%s %q is not a number", what, s)
		return 0, false
	case math.IsNaN(v) || math.IsInf(v, 0):
		t.p.fail(off, "%s %q is not finite", what, s)
		return 0, false
	}
	return v, true
}
