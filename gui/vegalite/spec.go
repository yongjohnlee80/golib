package vegalite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"strings"

	"github.com/yongjohnlee80/golib/gui/internal/csscolor"
)

// THE SUBSET. A spec is read key by key, and every key is one of three kinds: one drawn, one
// passed over because it changes only how the chart looks or behaves in a browser (a tooltip, a
// config, an axis's label angle), or one that changes what the chart says. A key of the third kind
// that is not drawn (a transform, a layer, a log scale, a binned field, a size channel) makes the
// spec ErrUnsupported, so the caller falls back rather than drawing a chart that says something
// else. The passed-over keys are listed by name: a key on no list is unsupported.

var (
	// ErrUnsupported is a spec outside the subset drawn; the error names what. The caller falls
	// back, as for a Mermaid diagram type golib does not draw.
	ErrUnsupported = errors.New("vegalite: unsupported")
	// ErrTooLarge is a spec over a limit: rows, categories or series.
	ErrTooLarge = errors.New("vegalite: too large")
)

// The limits: the rows of data read, the categories of a discrete axis, the series of a colour.
const (
	maxRows   = 10000
	maxBands  = 200
	maxSeries = 50
)

// markKind is what a datum is drawn as.
type markKind uint8

const (
	markBar markKind = iota
	markLine
	markPoint
	markArea
)

// fieldType is a field's measurement type, which picks its scale.
type fieldType uint8

const (
	typeNominal fieldType = iota
	typeOrdinal
	typeQuantitative
	typeTemporal
)

// discrete is a nominal or ordinal type: a band or point scale.
func (t fieldType) discrete() bool { return t == typeNominal || t == typeOrdinal }

// stackMode is how a bar's or an area's values of one category pile up.
type stackMode uint8

const (
	stackAuto      stackMode = iota // Vega-Lite's default: stacked when a colour splits the series
	stackNone                       // overlaid
	stackZero                       // piled from zero
	stackNormalize                  // piled, each category's total 1
)

// sortKind orders a discrete domain.
type sortKind uint8

const (
	sortAscending sortKind = iota // Vega-Lite's default
	sortDescending
	sortNone    // the data's order
	sortList    // the values listed, then the rest in the data's order
	sortByOther // by the other position channel's value, summed per category
)

// channel is an encoding channel's definition: x, y or color.
type channel struct {
	field     string
	typ       fieldType
	aggregate string // "", count, sum, mean, median, min or max
	title     *string
	noTitle   bool // title: null
	noAxis    bool // axis: null, or legend: null for colour
	grid      *bool
	sort      sortKind
	sortDesc  bool  // sortByOther: descending
	sortList  []any // sortList's values
	zero      *bool
	domain    []any // an explicit scale domain
	nice      *bool
	stack     stackMode
	value     *color.NRGBA // color: {"value": "#hex"}: one colour for every mark
}

// mark is the mark's definition.
type mark struct {
	kind        markKind
	point       bool         // a line's points drawn too
	filled      bool         // a point filled, not a ring
	color       *color.NRGBA // every mark this colour
	opacity     float32      // 0: the default for the mark
	strokeWidth float32      // 0: the default
	size        float32      // a point's area in square pixels; 0: the default
}

// chart is a spec as read: what to draw, before any layout.
type chart struct {
	title         string
	width, height float32 // in Vega-Lite's pixels; 0: the default
	containerW    bool    // width: "container": the width asked
	mark          mark
	x, y, color   *channel
	rows          []map[string]any
}

// passed are the keys read and passed over, by exact name: they change only how a chart looks or
// behaves in a browser. A key that changes what a label says (labelExpr, format, an axis's
// values) or what is drawn (invalid, a config's scale) is on no list: unsupported.
var (
	passedTop      = names("$schema", "description", "name", "autosize", "padding", "background", "usermeta")
	passedMark     = names("tooltip", "clip", "cursor", "href", "cornerRadius", "cornerRadiusEnd", "stroke", "strokeOpacity", "fillOpacity", "strokeCap", "strokeJoin", "aria", "description", "style")
	passedChannel  = names("tooltip", "description")
	passedEncoding = names("tooltip")
	passedScale    = names("scheme", "padding", "paddingInner", "paddingOuter", "round")
	// a guide's (an axis's, a legend's) look: its labels', title's, ticks', domain's and grid's
	// fonts, colours and sizes, and where it is
	passedGuide = names("aria", "description", "style", "zindex", "offset", "orient", "translate", "minExtent", "maxExtent",
		"labelAngle", "labelAlign", "labelBaseline", "labelBound", "labelColor", "labelFlush", "labelFlushOffset", "labelFont",
		"labelFontSize", "labelFontStyle", "labelFontWeight", "labelLimit", "labelLineHeight", "labelOffset", "labelOpacity",
		"labelOverlap", "labelPadding", "labelSeparation", "labels",
		"titleAlign", "titleAnchor", "titleAngle", "titleBaseline", "titleColor", "titleFont", "titleFontSize", "titleFontStyle",
		"titleFontWeight", "titleLimit", "titleLineHeight", "titleOpacity", "titlePadding", "titleX", "titleY")
	passedAxis = union(passedGuide, names("tickCount", "ticks", "tickSize", "tickColor", "tickWidth", "tickOpacity", "tickDash",
		"tickCap", "tickBand", "tickOffset", "tickRound", "tickExtra", "domain", "domainColor", "domainWidth", "domainOpacity",
		"domainDash", "domainCap", "gridColor", "gridWidth", "gridOpacity", "gridDash", "gridCap", "bandPosition"))
	passedLegend = union(passedGuide, names("direction", "columns", "columnPadding", "rowPadding", "padding", "cornerRadius",
		"fillColor", "strokeColor", "symbolType", "symbolSize", "symbolStrokeWidth", "symbolFillColor", "symbolStrokeColor",
		"symbolOpacity", "symbolDash", "symbolOffset", "symbolLimit", "gradientLength", "gradientThickness", "type", "legendX", "legendY"))
	passedTitle = names("text", "anchor", "align", "baseline", "color", "dx", "dy", "font", "fontSize", "fontStyle", "fontWeight",
		"frame", "limit", "lineHeight", "offset", "orient", "style", "zindex", "aria")
	// a config's keys whose values only style: the view's frame, fonts, the palette, guides' looks
	passedConfig = names("view", "background", "padding", "font", "autosize", "range", "style", "title", "aria", "lineBreak")
	// a config's mark defaults: the mark's look. Its interpolate is read, as a mark's is: a step or
	// a curve draws something else than the lines between the points
	passedMarkConfig = union(passedMark, names("color", "fill", "opacity", "strokeWidth", "size", "filled", "point", "line",
		"shape", "binSpacing", "continuousBandSize", "discreteBandSize", "thickness", "width", "height"))
)

func names(s ...string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, n := range s {
		m[n] = true
	}
	return m
}

func union(a, b map[string]bool) map[string]bool {
	m := make(map[string]bool, len(a)+len(b))
	for k := range a {
		m[k] = true
	}
	for k := range b {
		m[k] = true
	}
	return m
}

func unsupported(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, fmt.Sprintf(format, a...))
}

// object reads raw as a JSON object, its keys in a map.
func object(raw json.RawMessage, what string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, unsupported("%s is not an object", what)
	}
	return m, nil
}

// isNull reports whether raw is JSON's null.
func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// parse reads src as a Vega-Lite spec in the subset drawn. Malformed JSON is the decoder's error;
// a spec outside the subset is ErrUnsupported, one over a limit ErrTooLarge.
func parse(src string) (*chart, error) {
	var top map[string]json.RawMessage
	d := json.NewDecoder(strings.NewReader(src))
	d.UseNumber()
	if err := d.Decode(&top); err != nil {
		return nil, fmt.Errorf("vegalite: %w", err)
	}
	if top == nil {
		return nil, unsupported("the spec is not an object")
	}
	c := &chart{}
	for k, raw := range top {
		var err error
		switch {
		case passedTop[k]:
		case k == "title":
			err = c.readTitle(raw)
		case k == "config":
			err = readConfig(raw)
		case k == "width":
			c.width, c.containerW, err = readSize(raw, "width")
		case k == "height":
			c.height, _, err = readSize(raw, "height") // "container": a note has no height to fill; the default
		case k == "data":
			err = c.readData(raw)
		case k == "mark":
			err = c.readMark(raw)
		case k == "encoding":
			err = c.readEncoding(raw)
		default:
			err = unsupported("%q", k)
		}
		if err != nil {
			return nil, err
		}
	}
	if _, ok := top["mark"]; !ok {
		return nil, unsupported("no mark")
	}
	if _, ok := top["data"]; !ok {
		return nil, unsupported("no data")
	}
	return c, nil
}

// readConfig reads a config: its styling keys passed over, its guides' too; anything else (a
// scale's defaults, a label format) is unsupported.
func readConfig(raw json.RawMessage) error {
	m, err := object(raw, "the config")
	if err != nil {
		return err
	}
	for k, v := range m {
		switch {
		case passedConfig[k]:
		case k == "mark" || k == "bar" || k == "line" || k == "point" || k == "area" || k == "circle":
			mc, err := object(v, "config's "+k)
			if err != nil {
				return err
			}
			for mk, mv := range mc {
				switch {
				case passedMarkConfig[mk]:
				case mk == "interpolate":
					if err := linear(mv); err != nil {
						return err
					}
				default:
					return unsupported("config's %s's %q", k, mk)
				}
			}
		case strings.HasPrefix(k, "axis"): // axis, axisX, axisY, axisBand, axisQuantitative…
			if err := passedOnly(v, passedAxis, "config's "+k); err != nil {
				return err
			}
		case k == "legend":
			if err := passedOnly(v, passedLegend, "config's legend"); err != nil {
				return err
			}
		default:
			return unsupported("config's %q", k)
		}
	}
	return nil
}

// passedOnly checks an object holds only keys passed over.
func passedOnly(raw json.RawMessage, passed map[string]bool, what string) error {
	m, err := object(raw, what)
	if err != nil {
		return err
	}
	for k := range m {
		if !passed[k] {
			return unsupported("%s's %q", what, k)
		}
	}
	return nil
}

// readTitle reads a title: a string, or an object with its text (its look passed over).
func (c *chart) readTitle(raw json.RawMessage) error {
	if json.Unmarshal(raw, &c.title) == nil {
		return nil
	}
	if err := passedOnly(raw, passedTitle, "the title"); err != nil {
		return err
	}
	var t struct {
		Text json.RawMessage `json:"text"`
	}
	if json.Unmarshal(raw, &t) != nil {
		return unsupported("the title")
	}
	if len(t.Text) > 0 && json.Unmarshal(t.Text, &c.title) != nil {
		var lines []string
		if json.Unmarshal(t.Text, &lines) != nil {
			return unsupported("the title's text")
		}
		c.title = strings.Join(lines, " ")
	}
	return nil
}

// readSize reads a width or height: a number, or "container" (container true, size 0).
func readSize(raw json.RawMessage, what string) (float32, bool, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "container" {
			return 0, true, nil
		}
		return 0, false, unsupported("%s %q", what, s)
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		var step map[string]json.RawMessage
		if json.Unmarshal(raw, &step) == nil {
			return 0, false, unsupported("%s as a step", what)
		}
		return 0, false, unsupported("%s", what)
	}
	if n <= 0 {
		return 0, false, unsupported("%s %v", what, n)
	}
	return float32(n), false, nil
}

// readData reads data: inline values, an array of objects.
func (c *chart) readData(raw json.RawMessage) error {
	m, err := object(raw, "data")
	if err != nil {
		return err
	}
	for k := range m {
		if k != "values" && k != "name" {
			return unsupported("data's %q", k)
		}
	}
	values, ok := m["values"]
	if !ok {
		return unsupported("data without values")
	}
	var items []json.RawMessage
	if json.Unmarshal(values, &items) != nil {
		return unsupported("data values that are not an array")
	}
	if len(items) > maxRows {
		return fmt.Errorf("%w: %d rows (at most %d)", ErrTooLarge, len(items), maxRows)
	}
	c.rows = make([]map[string]any, 0, len(items))
	for _, it := range items {
		d := json.NewDecoder(bytes.NewReader(it))
		d.UseNumber()
		var row map[string]any
		if d.Decode(&row) != nil || row == nil {
			return unsupported("a data value that is not an object")
		}
		c.rows = append(c.rows, row)
	}
	return nil
}

// readMark reads the mark: its type, or an object of its type and properties.
func (c *chart) readMark(raw json.RawMessage) error {
	var typ string
	if json.Unmarshal(raw, &typ) == nil {
		return c.markType(typ)
	}
	m, err := object(raw, "the mark")
	if err != nil {
		return err
	}
	if t, ok := m["type"]; !ok || json.Unmarshal(t, &typ) != nil {
		return unsupported("a mark without a type")
	}
	if err := c.markType(typ); err != nil {
		return err
	}
	for k, v := range m {
		var err error
		switch {
		case k == "type" || passedMark[k]:
		case k == "point":
			var b bool
			if json.Unmarshal(v, &b) != nil {
				return unsupported("the mark's point as an object")
			}
			c.mark.point = b
		case k == "filled":
			err = json.Unmarshal(v, &c.mark.filled)
		case k == "color", k == "fill":
			var s string
			col, ok := color.NRGBA{}, false
			if json.Unmarshal(v, &s) == nil {
				col, ok = cssColor(s)
			}
			if !ok {
				return unsupported("the mark's colour %s", v)
			}
			c.mark.color = &col
		case k == "opacity":
			err = number(v, &c.mark.opacity, 0, 1)
		case k == "strokeWidth":
			err = number(v, &c.mark.strokeWidth, 0, 100)
		case k == "size":
			err = number(v, &c.mark.size, 0, 1e5)
		case k == "interpolate":
			err = linear(v)
		default:
			return unsupported("the mark's %q", k)
		}
		if err != nil {
			return unsupported("the mark's %q", k)
		}
	}
	return nil
}

// linear checks an interpolate is "linear", the lines between the points that are drawn.
func linear(raw json.RawMessage) error {
	var s string
	if json.Unmarshal(raw, &s) != nil || s != "linear" {
		return unsupported("interpolate %s", raw)
	}
	return nil
}

// number reads a number between lo and hi into f.
func number(raw json.RawMessage, f *float32, lo, hi float64) error {
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	if n < lo || n > hi {
		return fmt.Errorf("%v out of range", n)
	}
	*f = float32(n)
	return nil
}

// markType sets the mark's type: bar, line, point, area, or circle (a filled point).
func (c *chart) markType(t string) error {
	switch t {
	case "bar":
		c.mark.kind = markBar
	case "line":
		c.mark.kind = markLine
	case "point":
		c.mark.kind = markPoint
	case "circle":
		c.mark.kind, c.mark.filled = markPoint, true
	case "area":
		c.mark.kind = markArea
	default:
		return unsupported("the mark %q", t)
	}
	return nil
}

// readEncoding reads the encoding: x, y and color.
func (c *chart) readEncoding(raw json.RawMessage) error {
	m, err := object(raw, "the encoding")
	if err != nil {
		return err
	}
	for k, v := range m {
		var err error
		switch {
		case passedEncoding[k]:
		case k == "x":
			c.x, err = readChannel(v, "x")
		case k == "y":
			c.y, err = readChannel(v, "y")
		case k == "color", k == "fill":
			if c.color != nil {
				return unsupported("both color and fill")
			}
			c.color, err = readChannel(v, k)
		default:
			return unsupported("the %q channel", k)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// readChannel reads a channel's definition.
func readChannel(raw json.RawMessage, name string) (*channel, error) {
	m, err := object(raw, "the "+name+" channel")
	if err != nil {
		return nil, err
	}
	ch := &channel{}
	if v, ok := m["value"]; ok {
		var s string
		col, ok := color.NRGBA{}, false
		if json.Unmarshal(v, &s) == nil {
			col, ok = cssColor(s)
		}
		if (name != "color" && name != "fill") || !ok || len(m) != 1 {
			return nil, unsupported("%s as a value", name)
		}
		ch.value = &col
		return ch, nil
	}
	typed := false
	for k, v := range m {
		var err error
		switch {
		case passedChannel[k]:
		case k == "field":
			if json.Unmarshal(v, &ch.field) != nil || ch.field == "" {
				return nil, unsupported("%s's field %s", name, v)
			}
			if strings.ContainsAny(ch.field, `.[]\`) { // a nested field, or an escape: a path, not a key
				return nil, unsupported("%s's field %q", name, ch.field)
			}
		case k == "type":
			var t string
			_ = json.Unmarshal(v, &t)
			switch t {
			case "nominal":
				ch.typ = typeNominal
			case "ordinal":
				ch.typ = typeOrdinal
			case "quantitative":
				ch.typ = typeQuantitative
			case "temporal":
				ch.typ = typeTemporal
			default:
				return nil, unsupported("%s's type %s", name, v)
			}
			typed = true
		case k == "aggregate":
			var a string
			_ = json.Unmarshal(v, &a)
			switch a {
			case "count", "sum", "mean", "median", "min", "max":
				ch.aggregate = a
			case "average":
				ch.aggregate = "mean"
			default:
				return nil, unsupported("%s's aggregate %s", name, v)
			}
		case k == "title":
			if isNull(v) {
				ch.noTitle = true
				continue
			}
			var t string
			if json.Unmarshal(v, &t) != nil {
				return nil, unsupported("%s's title %s", name, v)
			}
			ch.title = &t
		case k == "axis" || k == "legend":
			err = ch.readGuide(v, name, k)
		case k == "scale":
			err = ch.readScale(v, name)
		case k == "sort":
			err = ch.readSort(v, name)
		case k == "stack":
			err = ch.readStack(v, name)
		default:
			return nil, unsupported("%s's %q", name, k)
		}
		if err != nil {
			return nil, err
		}
	}
	if ch.field == "" && ch.aggregate != "count" {
		return nil, unsupported("%s without a field", name)
	}
	if !typed {
		ch.typ = typeNominal // Vega-Lite's default for a field, and quantitative for an aggregate
		if ch.aggregate != "" {
			ch.typ = typeQuantitative
		}
	}
	if ch.aggregate != "" && ch.typ != typeQuantitative {
		return nil, unsupported("%s aggregated as %v", name, ch.typ)
	}
	return ch, nil
}

// readGuide reads an axis or a legend: null hides it; its title and grid are read, and the keys
// of how it looks (passedAxis, passedLegend) passed over.
func (ch *channel) readGuide(raw json.RawMessage, name, guide string) error {
	if isNull(raw) {
		ch.noAxis = true
		return nil
	}
	m, err := object(raw, name+"'s "+guide)
	if err != nil {
		return err
	}
	for k, v := range m {
		switch {
		case k == "title":
			if isNull(v) {
				ch.noTitle = true
				continue
			}
			var t string
			if json.Unmarshal(v, &t) != nil {
				return unsupported("%s's %s title", name, guide)
			}
			ch.title = &t
		case k == "grid" && guide == "axis":
			var g bool
			if json.Unmarshal(v, &g) != nil {
				return unsupported("%s's grid", name)
			}
			ch.grid = &g
		case guide == "axis" && passedAxis[k], guide == "legend" && passedLegend[k]:
		default:
			return unsupported("%s's %s %q", name, guide, k)
		}
	}
	return nil
}

// readScale reads a scale: zero, an explicit domain, nice, and a type that is the default's; a
// log, power or other scale is unsupported.
func (ch *channel) readScale(raw json.RawMessage, name string) error {
	if isNull(raw) {
		return nil
	}
	m, err := object(raw, name+"'s scale")
	if err != nil {
		return err
	}
	for k, v := range m {
		switch {
		case passedScale[k]:
		case k == "zero":
			var z bool
			if json.Unmarshal(v, &z) != nil {
				return unsupported("%s's scale zero", name)
			}
			ch.zero = &z
		case k == "nice":
			var n bool
			if json.Unmarshal(v, &n) != nil {
				return unsupported("%s's scale nice %s", name, v)
			}
			ch.nice = &n
		case k == "domain":
			d := json.NewDecoder(bytes.NewReader(v))
			d.UseNumber()
			if d.Decode(&ch.domain) != nil || len(ch.domain) == 0 {
				return unsupported("%s's scale domain %s", name, v)
			}
		case k == "type":
			var t string
			_ = json.Unmarshal(v, &t)
			switch t {
			case "linear", "time", "utc", "band", "point", "ordinal":
			default:
				return unsupported("%s's scale type %s", name, v)
			}
		default:
			return unsupported("%s's scale %q", name, k)
		}
	}
	return nil
}

// readSort reads a sort: null, "ascending", "descending", a list of values, or an encoding's name
// ("y", "-y").
func (ch *channel) readSort(raw json.RawMessage, name string) error {
	if isNull(raw) {
		ch.sort = sortNone
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch s {
		case "ascending":
			ch.sort = sortAscending
		case "descending":
			ch.sort = sortDescending
		case "x", "y", "-x", "-y":
			if strings.TrimPrefix(s, "-") == name {
				return unsupported("%s sorted by itself", name)
			}
			ch.sort, ch.sortDesc = sortByOther, strings.HasPrefix(s, "-")
		default:
			return unsupported("%s's sort %q", name, s)
		}
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&ch.sortList) == nil {
		ch.sort = sortList
		return nil
	}
	return unsupported("%s's sort %s", name, raw)
}

// readStack reads a stack: null or false overlays, true or "zero" piles, "normalize" piles to 1.
func (ch *channel) readStack(raw json.RawMessage, name string) error {
	if isNull(raw) {
		ch.stack = stackNone
		return nil
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		ch.stack = stackNone
		if b {
			ch.stack = stackZero
		}
		return nil
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	switch s {
	case "zero":
		ch.stack = stackZero
	case "normalize":
		ch.stack = stackNormalize
	default:
		return unsupported("%s's stack %s", name, raw)
	}
	return nil
}

// cssColor reads a CSS colour, as Vega-Lite takes one; currentColor is not one.
func cssColor(s string) (color.NRGBA, bool) {
	if strings.EqualFold(strings.TrimSpace(s), "currentcolor") {
		return color.NRGBA{}, false
	}
	return csscolor.Parse(s, color.NRGBA{})
}

func (t fieldType) String() string {
	return [...]string{"nominal", "ordinal", "quantitative", "temporal"}[t]
}
