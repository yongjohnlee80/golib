package widget

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/gui/internal/css"
)

// THE PROPERTIES — what each CSS property HTMLView reads sets on the computed style, one handler
// per property, registered by family below: adding a property is a registration, not an edit to
// apply. A property not registered is ignored, as a browser ignores an unknown one: never an error.

// cssValue is one declaration as a handler reads it: the property, its value as written with the
// custom properties substituted (v) and lowercased and trimmed (lv), and what relative units
// resolve against: the inherited style (its font size for em), the root's font size, and the view.
type cssValue struct {
	prop, v, lv string
	parent      *computed
	rootPx      float32
	view        gui.Size
}

// cssProp sets one property on c.
type cssProp func(c *computed, x cssValue)

// cssProps are the properties HTMLView reads, by name.
var cssProps = map[string]cssProp{}

// registerProps registers set under each of names: a property and its aliases, or a family of
// sides that set reads by x.prop.
func registerProps(set cssProp, names ...string) {
	for _, n := range names {
		cssProps[n] = set
	}
}

// apply sets one declaration on c; parent is the inherited style (its font size for em).
func (c *computed) apply(d cssDecl, parent *computed, rootPx float32, view gui.Size) {
	if strings.HasPrefix(d.prop, "--") {
		if !c.ownVars {
			nv := make(map[string]string, len(c.vars)+1)
			for k, v := range c.vars {
				nv[k] = v
			}
			c.vars, c.ownVars = nv, true
		}
		c.vars[d.prop] = d.value
		return
	}
	set, ok := cssProps[d.prop]
	if !ok {
		return
	}
	v := substituteVars(d.value, c.vars)
	set(c, cssValue{prop: d.prop, v: v, lv: strings.ToLower(strings.TrimSpace(v)), parent: parent, rootPx: rootPx, view: view})
}

// Text: colour, background, the font and its shorthand, and the text's own properties.
func init() {
	registerProps(func(c *computed, x cssValue) {
		if col, ok := parseColor(x.v, x.parent.color); ok {
			c.color = col
		}
	}, "color")
	registerProps(func(c *computed, x cssValue) {
		for _, f := range splitFields(x.v) {
			if col, ok := parseColor(f, c.color); ok {
				c.background = col
				break
			}
		}
		if x.lv == "none" {
			c.background = color.NRGBA{}
		}
	}, "background", "background-color")
	registerProps(func(c *computed, x cssValue) { c.fontSize = fontSize(x.lv, x.parent.fontSize, x.rootPx, x.view) }, "font-size")
	registerProps(func(c *computed, x cssValue) { c.bold = boldWeight(x.lv) }, "font-weight")
	registerProps(func(c *computed, x cssValue) { c.italic = x.lv == "italic" || x.lv == "oblique" }, "font-style")
	registerProps(func(c *computed, x cssValue) { c.mono, c.family = monoFamily(x.lv), familyList(x.v) }, "font-family")
	registerProps(func(c *computed, x cssValue) { c.font(x.lv, x.parent.fontSize, x.rootPx, x.view) }, "font")
	registerProps(func(c *computed, x cssValue) {
		switch t := textTransform(x.lv); t {
		case transformUppercase, transformLowercase, transformCapitalize:
			c.transform = t
		default:
			c.transform = transformNone
		}
	}, "text-transform")
	registerProps(func(c *computed, x cssValue) {
		if x.lv == "normal" {
			c.spacing = cssLen{}
		} else if l, ok := parseLen(x.lv); ok && !l.auto() && l.unit != '%' {
			c.spacing = l
		}
	}, "letter-spacing")
	registerProps(func(c *computed, x cssValue) { c.lineHeight = lineHeight(x.lv, c.fontSize) }, "line-height")
	registerProps(func(c *computed, x cssValue) {
		switch x.lv {
		case "center":
			c.align = flow.Center
		case "right", "end":
			c.align = flow.End
		default:
			c.align = flow.Start
		}
	}, "text-align")
	registerProps(func(c *computed, x cssValue) {
		c.underline = strings.Contains(x.lv, "underline")
		c.strike = strings.Contains(x.lv, "line-through")
	}, "text-decoration", "text-decoration-line")
	registerProps(func(c *computed, x cssValue) {
		switch x.lv {
		case "pre", "nowrap":
			c.white = flow.Pre
		case "pre-wrap", "pre-line", "break-spaces":
			c.white = flow.PreWrap
		default:
			c.white = flow.Normal
		}
	}, "white-space")
}

// The box: display, its sides (margin, padding, border), its size, its position, and how it shows.
func init() {
	registerProps(func(c *computed, x cssValue) { c.display = cssDisplay(x.lv) }, "display")
	registerProps(func(c *computed, x cssValue) { boxSides(x.lv, &c.margin) }, "margin")
	registerProps(func(c *computed, x cssValue) { boxSides(x.lv, &c.padding) }, "padding")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			c.margin[side(x.prop)] = l
		}
	}, "margin-top", "margin-right", "margin-bottom", "margin-left")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			c.padding[side(x.prop)] = l
		}
	}, "padding-top", "padding-right", "padding-bottom", "padding-left")
	registerProps(func(c *computed, x cssValue) {
		b := parseBorder(x.lv, c.color)
		c.border = [4]cssBorder{b, b, b, b}
	}, "border")
	registerProps(func(c *computed, x cssValue) { c.border[side(x.prop)] = parseBorder(x.lv, c.color) },
		"border-top", "border-right", "border-bottom", "border-left")
	registerProps(func(c *computed, x cssValue) {
		if col, ok := parseColor(x.lv, c.color); ok {
			for i := range c.border {
				c.border[i].color, c.border[i].set = col, true
			}
		}
	}, "border-color")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			for i := range c.border {
				c.border[i].w = l
			}
		}
	}, "border-width")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(splitFields(x.lv + " 0")[0]); ok {
			c.radius = l
		}
	}, "border-radius")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			c.width = l
		}
	}, "width")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			c.maxWidth = l
		}
	}, "max-width")
	registerProps(func(c *computed, x cssValue) { c.collapse = x.lv == "collapse" }, "border-collapse")
	registerProps(func(c *computed, x cssValue) { c.borderBox = x.lv == "border-box" }, "box-sizing")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			switch x.prop {
			case "height":
				c.height = l
			case "min-width":
				c.minWidth = l
			case "min-height":
				c.minHeight = l
			default:
				c.maxHeight = l
			}
		} else if x.lv == "none" && x.prop == "max-height" {
			c.maxHeight = cssLen{}
		}
	}, "height", "min-width", "min-height", "max-height")
	registerProps(func(c *computed, x cssValue) {
		switch p := cssPosition(x.lv); p {
		case positionRelative, positionAbsolute:
			c.position = p
		case "fixed": // laid out as absolute
			c.position = positionAbsolute
		default:
			c.position = positionStatic
		}
	}, "position")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			c.inset[side("-"+x.prop)] = l
		}
	}, "top", "right", "bottom", "left")
	registerProps(func(c *computed, x cssValue) { boxSides(x.lv, &c.inset) }, "inset")
	registerProps(func(c *computed, x cssValue) { c.invisible = x.lv == "hidden" || x.lv == "collapse" }, "visibility")
	registerProps(func(c *computed, x cssValue) { c.objectFit = objectFit(x.lv) }, "object-fit")
	registerProps(func(c *computed, x cssValue) { c.overflowScroll = x.lv == "auto" || x.lv == "scroll" }, "overflow", "overflow-x")
}

// Flex and grid: the container's direction, wrap, gaps and alignment, and an item's own.
func init() {
	registerProps(func(c *computed, x cssValue) {
		c.flexDir = flexRow
		if strings.HasPrefix(x.lv, "column") {
			c.flexDir = flexColumn
		}
	}, "flex-direction")
	registerProps(func(c *computed, x cssValue) { c.flexWrap = strings.HasPrefix(x.lv, "wrap") }, "flex-wrap")
	registerProps(func(c *computed, x cssValue) {
		for _, f := range strings.Fields(x.lv) {
			switch {
			case strings.HasPrefix(f, "column"):
				c.flexDir = flexColumn
			case strings.HasPrefix(f, "row"):
				c.flexDir = flexRow
			case strings.HasPrefix(f, "wrap"):
				c.flexWrap = true
			case f == "nowrap":
				c.flexWrap = false
			}
		}
	}, "flex-flow")
	registerProps(func(c *computed, x cssValue) {
		f := css.SplitTop(x.lv, ' ')
		var vals []cssLen
		for _, s := range f {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			if l, ok := parseLen(s); ok && !l.auto() {
				vals = append(vals, l)
			}
		}
		switch len(vals) {
		case 1:
			c.gap = [2]cssLen{vals[0], vals[0]}
		case 2:
			c.gap = [2]cssLen{vals[0], vals[1]}
		}
	}, "gap", "grid-gap")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok && !l.auto() {
			c.gap[map[bool]int{true: 0, false: 1}[strings.Contains(x.prop, "row")]] = l
		}
	}, "row-gap", "grid-row-gap", "column-gap", "grid-column-gap")
	registerProps(func(c *computed, x cssValue) { c.justify = flexAlign(x.lv) }, "justify-content")
	registerProps(func(c *computed, x cssValue) { c.alignItems = flexAlign(x.lv) }, "align-items")
	registerProps(func(c *computed, x cssValue) { c.alignSelf = flexAlign(x.lv) }, "align-self")
	registerProps(func(c *computed, x cssValue) {
		if f, err := strconv.ParseFloat(x.lv, 32); err == nil && f >= 0 {
			if x.prop == "flex-grow" {
				c.grow = float32(f)
			} else {
				c.shrink = float32(f)
			}
		}
	}, "flex-grow", "flex-shrink")
	registerProps(func(c *computed, x cssValue) {
		if l, ok := parseLen(x.lv); ok {
			c.basis = l
		}
	}, "flex-basis")
	registerProps(func(c *computed, x cssValue) { c.flex(x.lv) }, "flex")
	registerProps(func(c *computed, x cssValue) { c.gridCols = x.lv }, "grid-template-columns")
}
