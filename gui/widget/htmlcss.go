package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
)

// CSS Cascading Style Resolution Architecture:
//
//	[ User Agent Stylesheet ] ---> [ Host Stylesheet ] ---> [ Page <style> & Inline style="" ]
//	                                                                   |
//	                                                                   v
//	[ CSS Rule Specificity Sorter ] ------------------------------> Resolved Properties
//	(ID # > Class . > Tag element, ordered by document position)       |
//	                                                                   v
//	[ Unit & Value Resolution (cssLen.px) ] ----------------------> Computed Box & Typography
//	(px, em, rem, ch, %, vw, vh, calc/min/max/clamp)
//
// THE CSS SUBSET — what HTMLView's pixel layout reads of a page's styles: the page's <style>
// elements, a host's stylesheet, and style="" attributes, over golib's own user-agent sheet.
//
// Selectors: type, .class, #id, :root, compounds of them (pre.mermaid), the descendant and child
// combinators, and lists (th,td); specificity as CSS orders it, then source order. Values: px, em,
// rem, ch, % and unitless line-heights; #rgb[a], #rrggbb[aa], rgb()/rgba() and named colours;
// custom properties and var() with a fallback. Properties: colour and font (with the font
// shorthand), line-height, text-align, text-decoration, white-space, display, margin, padding,
// border (solid, with its sides and its radius), background, width, max-width (margin: auto
// centres), border-collapse and overflow. Anything else is ignored, as a browser ignores an
// unknown property: never an error.
type cssLen struct {
	v    float32
	unit byte     // 'p' px, 'e' em, 'r' rem, 'c' ch, '%', 'w' vw, 'h' vh, 'a' auto, 'x' an expression, 0 unset
	expr *cssExpr // calc(), min(), max(), clamp() (htmlcss_values.go)
}

// set reports whether the CSS length value was explicitly assigned.
func (l cssLen) set() bool { return l.unit != 0 }

// auto reports whether the CSS length value is the 'auto' keyword.
func (l cssLen) auto() bool { return l.unit == 'a' }

// px is l in pixels: em and ch against font, rem against root, % against of, vw and vh against
// the view.
func (l cssLen) px(font, root, of float32, view gui.Size) float32 {
	switch l.unit {
	case 'x':
		return l.expr.eval(font, root, of, view)
	case 'w':
		return l.v / 100 * view.W
	case 'h':
		return l.v / 100 * view.H
	case 'p':
		return l.v
	case 'e':
		return l.v * font
	case 'r':
		return l.v * root
	case 'c':
		return l.v * font * 0.55
	case '%':
		return l.v / 100 * of
	}
	return 0
}

type cssBorder struct {
	w     cssLen
	color color.NRGBA
	set   bool // a colour was given; else the text's (currentColor)
	none  bool
}

// computed is an element's style after the cascade and inheritance.
type computed struct {
	// inherited
	color      color.NRGBA
	fontSize   float32 // px
	bold       bool
	italic     bool
	mono       bool
	family     string        // font-family as written, a CSS list; "" the window's typeface
	transform  textTransform // text-transform
	spacing    cssLen        // letter-spacing; unset is normal
	lineHeight float32       // a multiple of the font size
	align      flow.Align
	white      flow.WhiteSpace
	vars       map[string]string // custom properties, shared with the parent until one is set here
	ownVars    bool
	link       string // the enclosing <a>'s href

	// not inherited
	display        cssDisplay
	background     color.NRGBA
	underline      bool
	strike         bool
	margin         [4]cssLen // top, right, bottom, left
	padding        [4]cssLen
	border         [4]cssBorder
	radius         cssLen
	width          cssLen
	maxWidth       cssLen
	collapse       bool // border-collapse: collapse
	overflowScroll bool
	borderBox      bool        // box-sizing: border-box: width and height hold padding and border
	height         cssLen      // height; unset or auto: its content's
	minWidth       cssLen      // min-width
	minHeight      cssLen      // min-height
	maxHeight      cssLen      // max-height
	position       cssPosition // fixed is laid out as absolute
	inset          [4]cssLen   // top, right, bottom, left of a positioned box
	objectFit      objectFit   // an image's; fill is the default
	invisible      bool        // visibility: hidden; inherited, so a child can show again
	// flex and grid (htmlview_flex.go)
	flexDir    flexDirection // "" is row, the default
	flexWrap   bool          // flex-wrap: wrap
	gap        [2]cssLen     // row-gap, column-gap
	justify    flexAlign     // justify-content
	alignItems flexAlign     // align-items
	alignSelf  flexAlign     // align-self
	grow       float32       // flex-grow
	shrink     float32       // flex-shrink; 1 initially
	basis      cssLen        // flex-basis; unset or auto: the width, else the content's
	gridCols   string        // grid-template-columns, as written
}

// inherit is a child's starting style: what CSS inherits, the rest at its initial value.
func (c *computed) inherit() *computed {
	return &computed{
		color: c.color, fontSize: c.fontSize, bold: c.bold, italic: c.italic, mono: c.mono,
		family: c.family, transform: c.transform, spacing: c.spacing, invisible: c.invisible,
		lineHeight: c.lineHeight, align: c.align, white: c.white, vars: c.vars, link: c.link,
		display: "inline", shrink: 1,
		// text-decoration propagates to inline descendants as drawn decoration
		underline: c.underline, strike: c.strike,
	}
}

type cssDecl struct {
	prop, value string
	important   bool
}

type cssRule struct {
	sels  []cssSelector
	decls []cssDecl
	order int
}

// THE KEYWORD SETS — the values of the properties the layout reads, as the computed style holds
// them. A property handler reads the CSS text into one (htmlcss_props.go); the layout names the
// constants. A value outside a set is kept as written and read as the property's default.

// cssDisplay is a display value.
type cssDisplay string

const (
	displayDefault       cssDisplay = "" // as unset: inline
	displayNone          cssDisplay = "none"
	displayBlock         cssDisplay = "block"
	displayInline        cssDisplay = "inline"
	displayInlineBlock   cssDisplay = "inline-block"
	displayListItem      cssDisplay = "list-item"
	displayFlex          cssDisplay = "flex"
	displayInlineFlex    cssDisplay = "inline-flex"
	displayGrid          cssDisplay = "grid"
	displayInlineGrid    cssDisplay = "inline-grid"
	displayTable         cssDisplay = "table"
	displayTableRowGroup cssDisplay = "table-row-group"
	displayTableRow      cssDisplay = "table-row"
	displayTableCell     cssDisplay = "table-cell"
)

// cssPosition is a position value; fixed is read as absolute.
type cssPosition string

const (
	positionStatic   cssPosition = ""
	positionRelative cssPosition = "relative"
	positionAbsolute cssPosition = "absolute"
)

// flexDirection is a flex container's main axis.
type flexDirection string

const (
	flexRow    flexDirection = "row"
	flexColumn flexDirection = "column"
)

// flexAlign is a justify-content, align-items or align-self value.
type flexAlign string

const (
	alignUnset        flexAlign = ""
	alignNormal       flexAlign = "normal"
	alignAuto         flexAlign = "auto"
	alignStretch      flexAlign = "stretch"
	alignCenter       flexAlign = "center"
	alignEnd          flexAlign = "end"
	alignFlexEnd      flexAlign = "flex-end"
	alignRight        flexAlign = "right"
	alignSpaceBetween flexAlign = "space-between"
	alignSpaceAround  flexAlign = "space-around"
	alignSpaceEvenly  flexAlign = "space-evenly"
)

// objectFit is an image's object-fit.
type objectFit string

const (
	fitFill      objectFit = "fill"
	fitContain   objectFit = "contain"
	fitScaleDown objectFit = "scale-down"
	fitCover     objectFit = "cover"
	fitNone      objectFit = "none"
)

// textTransform is a text-transform value; transformNone is none.
type textTransform string

const (
	transformNone       textTransform = ""
	transformUppercase  textTransform = "uppercase"
	transformLowercase  textTransform = "lowercase"
	transformCapitalize textTransform = "capitalize"
)
