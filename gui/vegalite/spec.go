package vegalite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
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
	markRule // a line across the plot at each value of its one position channel
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
	sortByField // by another field's aggregate per category (sortField, sortOp)
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
	sortDesc  bool   // sortByOther: descending
	sortList  []any  // sortList's values
	sortField string // sortByField: the field whose aggregate orders the categories
	sortOp    string // sortByField: its aggregate; "": Vega-Lite's default (sum stacked, else min)
	zero      *bool
	domain    []any // an explicit scale domain
	nice      *bool
	stack     stackMode
	value     *color.NRGBA // color: {"value": "#hex"}: one colour for every mark
	datum     any          // a position channel's constant (isDatum): a number, a string
	isDatum   bool
	colors    []color.NRGBA // a colour scale's range: the series' colours in domain order, cycling
	shape     string        // shape: {"value": name}: one symbol for every point
	shapes    []string      // a shape scale's range: the symbols in domain order, cycling
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
	dash        []float32    // a line's or a rule's strokeDash: on and off lengths; nil: solid
}

// spec is a whole spec as read: its title and subtitle, its size, and its layers in the order they
// paint. A spec with no layer is one layer.
type spec struct {
	title         string
	subtitle      string
	width, height float32 // in Vega-Lite's pixels; 0: the default
	containerW    bool    // width: "container": the width asked
	layers        []*chart
}

// chart is one layer as read: one mark, its encoding and its data, before any layout.
type chart struct {
	mark        mark
	x, y, color *channel
	shape       *channel // a point's symbol by a field, or one; nil: circles
	opacity     *float32 // the opacity channel's value; nil: the mark's
	rows        []map[string]any
}

// param is a param as it stands before anyone touches the page: a variable at its initial value
// (its bind, a slider or an input, is the page's control, not what is drawn), or a selection with
// nothing selected yet.
type param struct {
	selection bool
	value     any
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
	passedTitle = names("text", "subtitle", "anchor", "align", "baseline", "color", "dx", "dy", "font", "fontSize", "fontStyle",
		"fontWeight", "frame", "limit", "lineHeight", "offset", "orient", "style", "zindex", "aria", "subtitleColor",
		"subtitleFont", "subtitleFontSize", "subtitleFontStyle", "subtitleFontWeight", "subtitleLineHeight", "subtitlePadding")
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
//
// A layered spec's layers inherit its data and its encoding, a layer's own channels over the
// spec's, as Vega-Lite merges them. The params are read first, every layer's: a datum's expr and a
// condition name them.
func parse(src string) (*spec, error) {
	var top map[string]json.RawMessage
	if err := decodeNumbers(strings.NewReader(src), &top); err != nil {
		return nil, fmt.Errorf("vegalite: %w", err)
	}
	if top == nil {
		return nil, unsupported("the spec is not an object")
	}
	sp := &spec{}
	params := map[string]param{}
	if err := readParams(top["params"], params); err != nil {
		return nil, err
	}
	var units []map[string]json.RawMessage
	var parentEnc json.RawMessage
	if raw, ok := top["layer"]; ok {
		if _, ok := top["mark"]; ok {
			return nil, unsupported("a mark beside a layer")
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
			return nil, unsupported("the layer is not a list of layers")
		}
		for _, it := range items {
			m, err := object(it, "a layer")
			if err != nil {
				return nil, err
			}
			for k := range m {
				switch k {
				case "mark", "encoding", "data", "params", "name", "description":
				default:
					return nil, unsupported("a layer's %q", k)
				}
			}
			if err := readParams(m["params"], params); err != nil {
				return nil, err
			}
			units = append(units, m)
		}
		parentEnc = top["encoding"]
	} else {
		units = []map[string]json.RawMessage{{"mark": top["mark"], "encoding": top["encoding"]}}
	}
	var rows []map[string]any
	for k, raw := range top {
		var err error
		switch {
		case passedTop[k], k == "mark", k == "encoding", k == "layer", k == "params":
		case k == "title":
			err = sp.readTitle(raw)
		case k == "config":
			err = readConfig(raw)
		case k == "width":
			sp.width, sp.containerW, err = readSize(raw, "width")
		case k == "height":
			sp.height, _, err = readSize(raw, "height") // "container": a note has no height to fill; the default
		case k == "data":
			rows, err = readData(raw)
		default:
			err = unsupported("%q", k)
		}
		if err != nil {
			return nil, err
		}
	}
	for _, u := range units {
		c := &chart{rows: rows}
		if raw, ok := u["data"]; ok {
			var err error
			if c.rows, err = readData(raw); err != nil {
				return nil, err
			}
		}
		if c.rows == nil {
			return nil, unsupported("no data")
		}
		if u["mark"] == nil {
			return nil, unsupported("no mark")
		}
		if err := c.readMark(u["mark"]); err != nil {
			return nil, err
		}
		enc, err := mergeEncoding(parentEnc, u["encoding"])
		if err != nil {
			return nil, err
		}
		if err := c.readEncoding(enc, params); err != nil {
			return nil, err
		}
		sp.layers = append(sp.layers, c)
	}
	return sp, nil
}

// mergeEncoding is a layer's encoding over its parent's, channel by channel.
func mergeEncoding(parent, own json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, raw := range []json.RawMessage{parent, own} {
		if raw == nil {
			continue
		}
		m, err := object(raw, "the encoding")
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			out[k] = v
		}
	}
	return out, nil
}

// readParams reads a list of params into params: a variable with its value, or a selection. A
// param computed by an expr, a selection with an initial value and a variable with none are
// unsupported: their starting state is not what the spec says alone.
func readParams(raw json.RawMessage, params map[string]param) error {
	if raw == nil {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return unsupported("the params are not a list")
	}
	for _, it := range items {
		m, err := object(it, "a param")
		if err != nil {
			return err
		}
		var name string
		if json.Unmarshal(m["name"], &name) != nil || name == "" {
			return unsupported("a param without a name")
		}
		if _, dup := params[name]; dup {
			return unsupported("the param %q twice", name)
		}
		var p param
		hasValue := false
		for k, v := range m {
			switch k {
			case "name", "bind", "description", "react", "views":
			case "value":
				if decodeNumbers(bytes.NewReader(v), &p.value) != nil {
					return unsupported("param %q's value", name)
				}
				hasValue = true
			case "select":
				if err := readSelect(v, name); err != nil {
					return err
				}
				p.selection = true
			default:
				return unsupported("param %q's %q", name, k)
			}
		}
		switch {
		case p.selection && hasValue:
			return unsupported("the selection %q with an initial value", name)
		case !p.selection && !hasValue:
			return unsupported("the param %q without a value", name)
		}
		if !p.selection {
			switch p.value.(type) {
			case json.Number, string, bool:
			default:
				return unsupported("param %q's value %v", name, p.value)
			}
		}
		params[name] = p
	}
	return nil
}

// passedSelect are a selection's keys that only say how the page picks: which events, which
// fields, how it clears.
var passedSelect = names("type", "fields", "encodings", "on", "clear", "nearest", "toggle", "resolve", "mark", "translate", "zoom")

// readSelect reads a selection's select: point or interval.
func readSelect(raw json.RawMessage, name string) error {
	var t string
	if json.Unmarshal(raw, &t) != nil {
		m, err := object(raw, "selection "+name)
		if err != nil {
			return err
		}
		if json.Unmarshal(m["type"], &t) != nil {
			return unsupported("selection %q without a type", name)
		}
		for k := range m {
			if !passedSelect[k] {
				return unsupported("selection %q's %q", name, k)
			}
		}
	}
	if t != "point" && t != "interval" {
		return unsupported("selection %q of type %q", name, t)
	}
	return nil
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
		case k == "mark" || isMarkType(k): // the marks' shared config, and each type's
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
func (c *spec) readTitle(raw json.RawMessage) error {
	if json.Unmarshal(raw, &c.title) == nil {
		return nil
	}
	if err := passedOnly(raw, passedTitle, "the title"); err != nil {
		return err
	}
	var t struct {
		Text     json.RawMessage `json:"text"`
		Subtitle json.RawMessage `json:"subtitle"`
	}
	if json.Unmarshal(raw, &t) != nil {
		return unsupported("the title")
	}
	// text reads a string, or a list of lines (joined: one line is drawn)
	text := func(raw json.RawMessage, into *string, what string) error {
		if len(raw) == 0 || json.Unmarshal(raw, into) == nil {
			return nil
		}
		var lines []string
		if json.Unmarshal(raw, &lines) != nil {
			return unsupported("the title's %s", what)
		}
		*into = strings.Join(lines, " ")
		return nil
	}
	if err := text(t.Text, &c.title, "text"); err != nil {
		return err
	}
	return text(t.Subtitle, &c.subtitle, "subtitle")
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
func readData(raw json.RawMessage) ([]map[string]any, error) {
	m, err := object(raw, "data")
	if err != nil {
		return nil, err
	}
	for k := range m {
		if k != "values" && k != "name" {
			return nil, unsupported("data's %q", k)
		}
	}
	values, ok := m["values"]
	if !ok {
		return nil, unsupported("data without values")
	}
	var items []json.RawMessage
	if json.Unmarshal(values, &items) != nil {
		return nil, unsupported("data values that are not an array")
	}
	if len(items) > maxRows {
		return nil, fmt.Errorf("%w: %d rows (at most %d)", ErrTooLarge, len(items), maxRows)
	}
	rows := make([]map[string]any, 0, len(items))
	for _, it := range items {
		var row map[string]any
		if decodeNumbers(bytes.NewReader(it), &row) != nil || row == nil {
			return nil, unsupported("a data value that is not an object")
		}
		rows = append(rows, row)
	}
	return rows, nil
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
			col, ok := jsonColor(v)
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
		case k == "strokeDash":
			err = json.Unmarshal(v, &c.mark.dash)
			total := float32(0)
			for _, d := range c.mark.dash {
				if d < 0 {
					err = fmt.Errorf("a negative dash")
				}
				total += d
			}
			if total == 0 {
				c.mark.dash = nil // [] or all zeros: solid, as SVG draws it
			}
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

// markDef is what a mark type draws: its kind, and whether its points are filled.
type markDef struct {
	kind   markKind
	filled bool
}

// markTypes are the marks a chart draws, by their Vega-Lite type. The config has a section for each,
// so the two cannot disagree on which marks exist.
var markTypes = map[string]markDef{
	"bar":    {kind: markBar},
	"line":   {kind: markLine},
	"point":  {kind: markPoint},
	"circle": {kind: markPoint, filled: true}, // a filled point
	"area":   {kind: markArea},
	"rule":   {kind: markRule},
}

// isMarkType reports whether t is a mark type a chart draws.
func isMarkType(t string) bool {
	_, ok := markTypes[t]
	return ok
}

// markType sets the mark's type: one of markTypes.
func (c *chart) markType(t string) error {
	def, ok := markTypes[t]
	if !ok {
		return unsupported("the mark %q", t)
	}
	c.mark.kind = def.kind
	if def.filled {
		c.mark.filled = true
	}
	return nil
}

// readEncoding reads the encoding: x, y, color and opacity, each with any condition resolved
// for the page's starting state.
func (c *chart) readEncoding(m map[string]json.RawMessage, params map[string]param) error {
	for k, v := range m {
		if passedEncoding[k] {
			continue
		}
		def, err := resolve(v, k, params)
		if err != nil {
			return err
		}
		switch k {
		case "x":
			c.x, err = readChannel(def, "x", params)
		case "y":
			c.y, err = readChannel(def, "y", params)
		case "color", "fill":
			if c.color != nil {
				return unsupported("both color and fill")
			}
			c.color, err = readChannel(def, k, params)
		case "shape":
			c.shape, err = readChannel(def, "shape", params)
		case "opacity":
			var o float32
			if len(def) != 1 || def["value"] == nil || number(def["value"], &o, 0, 1) != nil {
				return unsupported("opacity other than one value")
			}
			c.opacity = &o
		default:
			return unsupported("the %q channel", k)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// resolve is a channel's definition as the page first draws it. A condition on a selection holds
// while the selection is empty, unless it says "empty": false: the condition's branch is drawn, or
// else the channel's own. A condition on a test, on a variable or on an unknown param is
// unsupported.
func resolve(raw json.RawMessage, name string, params map[string]param) (map[string]json.RawMessage, error) {
	m, err := object(raw, "the "+name+" channel")
	if err != nil {
		return nil, err
	}
	cond, ok := m["condition"]
	if !ok {
		return m, nil
	}
	cm, err := object(cond, name+"'s condition")
	if err != nil {
		return nil, err
	}
	var pn string
	if json.Unmarshal(cm["param"], &pn) != nil {
		return nil, unsupported("%s's condition without a param", name)
	}
	p, ok := params[pn]
	switch {
	case !ok:
		return nil, unsupported("%s's condition on the unknown param %q", name, pn)
	case !p.selection:
		return nil, unsupported("%s's condition on the variable %q", name, pn)
	}
	holds := true
	if e, ok := cm["empty"]; ok && json.Unmarshal(e, &holds) != nil {
		return nil, unsupported("%s's condition's empty %s", name, e)
	}
	out := map[string]json.RawMessage{}
	branch, skip := m, "condition"
	if holds {
		branch = cm
	}
	for k, v := range branch {
		if k != skip && k != "param" && k != "empty" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil, unsupported("%s's condition leaves nothing to draw", name)
	}
	return out, nil
}

// readDatum reads a datum: a number or a string, or {"expr": name} of a variable param, which is
// its value.
func readDatum(raw json.RawMessage, name string, params map[string]param) (any, error) {
	var e struct {
		Expr *string `json:"expr"`
	}
	if m, err := object(raw, name+"'s datum"); err == nil {
		if len(m) != 1 || json.Unmarshal(raw, &e) != nil || e.Expr == nil {
			return nil, unsupported("%s's datum %s", name, raw)
		}
		p, ok := params[strings.TrimSpace(*e.Expr)]
		if !ok || p.selection {
			return nil, unsupported("%s's datum expr %q: only a variable param's name is read", name, *e.Expr)
		}
		return p.value, nil
	}
	var v any
	if decodeNumbers(bytes.NewReader(raw), &v) != nil {
		return nil, unsupported("%s's datum %s", name, raw)
	}
	switch v.(type) {
	case json.Number, string:
		return v, nil
	}
	return nil, unsupported("%s's datum %s", name, raw)
}

// readChannel reads a channel's definition, its condition resolved.
func readChannel(m map[string]json.RawMessage, name string, params map[string]param) (*channel, error) {
	ch := &channel{}
	if v, ok := m["value"]; ok && name == "shape" {
		if len(m) != 1 || json.Unmarshal(v, &ch.shape) != nil || !symbols[ch.shape] {
			return nil, unsupported("shape %s", v)
		}
		return ch, nil
	}
	if v, ok := m["value"]; ok {
		col, ok := jsonColor(v)
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
		case k == "datum" && (name == "x" || name == "y"):
			ch.isDatum = true
			ch.datum, err = readDatum(v, name, params)
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
			a, ok := aggregateOp(v)
			if !ok {
				return nil, unsupported("%s's aggregate %s", name, v)
			}
			ch.aggregate = a
		case k == "title":
			if !ch.readTitle(v) {
				return nil, unsupported("%s's title %s", name, v)
			}
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
	if ch.isDatum {
		if ch.field != "" || ch.aggregate != "" {
			return nil, unsupported("%s with a datum and a field", name)
		}
		if !typed { // Vega-Lite's default for a datum: by its value
			ch.typ = typeNominal
			if _, num := ch.datum.(json.Number); num {
				ch.typ = typeQuantitative
			}
		}
		return ch, nil
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
			if !ch.readTitle(v) {
				return unsupported("%s's %s title", name, guide)
			}
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
			if decodeNumbers(bytes.NewReader(v), &ch.domain) != nil || len(ch.domain) == 0 {
				return unsupported("%s's scale domain %s", name, v)
			}
		case k == "range":
			if err := ch.readRange(v, name); err != nil {
				return err
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

// readRange reads a scale's range: a colour scale's list of colours, or a shape scale's list of
// symbols, each in domain order and cycling. The named range a channel draws by default (a
// nominal colour's "category", a shape's "symbol") is passed over. Any other range (another named
// one, a position's, a list of what is not drawn) is unsupported.
func (ch *channel) readRange(raw json.RawMessage, name string) error {
	var named string
	if json.Unmarshal(raw, &named) == nil {
		if (named == "category" && (name == "color" || name == "fill")) || (named == "symbol" && name == "shape") {
			return nil
		}
		return unsupported("%s's scale range %q", name, named)
	}
	var list []string
	if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
		return unsupported("%s's scale range %s", name, raw)
	}
	switch name {
	case "color", "fill":
		for _, s := range list {
			c, ok := cssColor(s)
			if !ok {
				return unsupported("%s's range colour %q", name, s)
			}
			ch.colors = append(ch.colors, c)
		}
	case "shape":
		for _, s := range list {
			if !symbols[s] {
				return unsupported("the shape %q", s)
			}
		}
		ch.shapes = list
	default:
		return unsupported("%s's scale range", name)
	}
	return nil
}

// readSort reads a sort: null, "ascending", "descending", a list of values, an encoding's name
// ("y", "-y"), or another field's aggregate ({"field", "op", "order"}).
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
	if m, err := object(raw, name+"'s sort"); err == nil {
		if name != "x" && name != "y" { // a legend's order by a field is not drawn
			return unsupported("%s sorted by a field", name)
		}
		return ch.readSortField(m, name)
	}
	if decodeNumbers(bytes.NewReader(raw), &ch.sortList) == nil {
		ch.sort = sortList
		return nil
	}
	return unsupported("%s's sort %s", name, raw)
}

// readSortField reads a sort by another field: its field, its aggregate (op) and its order
// (ascending, descending, or null: the data's order).
func (ch *channel) readSortField(m map[string]json.RawMessage, name string) error {
	ch.sort = sortByField
	for k, v := range m {
		switch k {
		case "field":
			if json.Unmarshal(v, &ch.sortField) != nil || ch.sortField == "" || strings.ContainsAny(ch.sortField, `.[]\`) {
				return unsupported("%s's sort field %s", name, v)
			}
		case "op":
			op, ok := aggregateOp(v)
			if !ok {
				return unsupported("%s's sort op %s", name, v)
			}
			ch.sortOp = op
		case "order":
			if isNull(v) {
				continue
			}
			var o string
			_ = json.Unmarshal(v, &o)
			switch o {
			case "ascending":
			case "descending":
				ch.sortDesc = true
			default:
				return unsupported("%s's sort order %s", name, v)
			}
		default:
			return unsupported("%s's sort %q", name, k)
		}
	}
	if ch.sortField == "" {
		return unsupported("%s's sort without a field", name)
	}
	if o, ok := m["order"]; ok && isNull(o) {
		ch.sort = sortNone // order null: the data's order, whatever the field
	}
	return nil
}

// symbols are the point shapes drawn: Vega's built-in symbols.
var symbols = names("circle", "square", "cross", "diamond", "triangle-up", "triangle-down", "triangle-right", "triangle-left")

// defaultShapes is Vega's symbol range: a nominal shape's symbols, in domain order.
var defaultShapes = []string{"circle", "square", "triangle-up", "cross", "diamond", "triangle-right", "triangle-down", "triangle-left"}

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

// jsonColor is a JSON string read as a CSS colour; false when it is not a string or not a colour.
func jsonColor(raw json.RawMessage) (color.NRGBA, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return color.NRGBA{}, false
	}
	return cssColor(s)
}

// aggregateOp is an aggregate operation, as an encoding's aggregate and a sort's op both name one:
// count, sum, mean (or its alias average), median, min or max.
func aggregateOp(raw json.RawMessage) (string, bool) {
	var a string
	_ = json.Unmarshal(raw, &a)
	switch a {
	case "count", "sum", "mean", "median", "min", "max":
		return a, true
	case "average":
		return "mean", true
	}
	return "", false
}

// readTitle reads a channel's or its guide's title: null hides it, a string sets it. false for
// anything else.
func (ch *channel) readTitle(raw json.RawMessage) bool {
	if isNull(raw) {
		ch.noTitle = true
		return true
	}
	var t string
	if json.Unmarshal(raw, &t) != nil {
		return false
	}
	ch.title = &t
	return true
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

// decodeNumbers decodes one JSON value from r into v, its numbers as json.Number: a spec keeps a
// number's text, so 0.1 and an integer past 2^53 read as written.
func decodeNumbers(r io.Reader, v any) error {
	d := json.NewDecoder(r)
	d.UseNumber()
	return d.Decode(v)
}
