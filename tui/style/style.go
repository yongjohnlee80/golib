package style

import (
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// propKey identifies one settable property; a bit in Style.props.
type propKey uint64

const (
	propForeground propKey = 1 << iota
	propBackground
	propBold
	propItalic
	propUnderline
	propStrikethrough
	propReverse
	propBlink
	propFaint
	propPaddingTop
	propPaddingRight
	propPaddingBottom
	propPaddingLeft
	propMarginTop
	propMarginRight
	propMarginBottom
	propMarginLeft
	propWidth
	propHeight
	propMaxWidth
	propMaxHeight
	propAlignHorizontal
	propAlignVertical
	propBorderStyle
	propBorderTop
	propBorderRight
	propBorderBottom
	propBorderLeft
	propBorderTopForeground
	propBorderRightForeground
	propBorderBottomForeground
	propBorderLeftForeground
	// …future bits appended here; 64 slots, ~32 used in v1.
)

// propLast is the highest assigned bit; Inherit iterates [1, propLast].
const propLast = propBorderLeftForeground

// attrs bits: packed boolean attribute values. Set-ness lives in props.
const (
	attrBold uint16 = 1 << iota
	attrItalic
	attrUnderline
	attrStrikethrough
	attrReverse
	attrBlink
	attrFaint
)

// border-edge bits (Style.borderEdges); index order matches the [4] arrays:
// top, right, bottom, left.
const (
	edgeTop uint8 = 1 << iota
	edgeRight
	edgeBottom
	edgeLeft
)

// Align positions content within the rect layout gave a widget.
// AlignLeft/AlignCenter/AlignRight are horizontal; AlignTop/AlignMiddle/
// AlignBottom are vertical.
type Align uint8

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
	AlignTop
	AlignMiddle
	AlignBottom
)

// extState tracks how Ext must treat the extras map. It is extCOW (the zero
// value) on every Style at rest — Apply flips it on its private working copy
// only — so it never disturbs Style comparability.
type extState uint8

const (
	extCOW   extState = iota // clone-on-write on every Ext (any style at rest)
	extBatch                 // inside Apply, extras not yet cloned: clone once, then own
	extOwned                 // inside Apply, extras already cloned: mutate in place
)

// Style is an immutable style definition. The zero value is a valid empty
// style. Copy by assignment; every setter returns a new Style by value.
//
// Style is comparable (usable with == and as a map key) — a binding
// constraint: no map/slice/func fields live directly on
// Style; the extras map hides behind a pointer, which compares by identity.
// Two styles differing only in equal-but-distinct extras maps therefore
// compare unequal — documented and acceptable, since extras are the escape
// hatch, not the core.
type Style struct {
	props propKey // bitfield: which properties are explicitly set

	fg, bg                             Color
	attrs                              uint16 // bold/italic/… packed bools (values; set-ness lives in props)
	padding                            [4]int16
	margin                             [4]int16
	width, height, maxWidth, maxHeight int16
	alignH, alignV                     Align
	border                             BorderStyle
	borderEdges                        uint8    // which edges the border paints; values, set-ness in props
	borderFg                           [4]Color // per-edge border foreground

	extraMode extState    // Apply-scoped copy-on-write state; extCOW at rest
	extras    *extraProps // escape hatch; nil in the common case
}

// New returns an empty Style. Equivalent to the zero value; provided for the
// fluent idiom: style.New().Bold(true).Padding(1, 2).
func New() Style { return Style{} }

func (s Style) isSet(k propKey) bool { return s.props&k != 0 }
func (s *Style) set(k propKey)       { s.props |= k }  // internal; on the copy
func (s *Style) unset(k propKey)     { s.props &^= k } // internal; on the copy

// setAttr sets one packed boolean attribute (value + set bit) on the copy.
func (s Style) setAttr(k propKey, bit uint16, v bool) Style {
	if v {
		s.attrs |= bit
	} else {
		s.attrs &^= bit
	}
	s.set(k)
	return s
}

// Foreground sets the foreground color. It accepts a literal Color or a
// theme Token; both flatten to the internal Color representation at set time.
func (s Style) Foreground(c ColorSpec) Style {
	s.fg = c.spec()
	s.set(propForeground)
	return s
}

// Background sets the background color.
func (s Style) Background(c ColorSpec) Style {
	s.bg = c.spec()
	s.set(propBackground)
	return s
}

// Bold sets the bold attribute. Bold(false) is an explicit set — distinct
// from an untouched style (see GetBold's is-set flag).
func (s Style) Bold(v bool) Style { return s.setAttr(propBold, attrBold, v) }

// Italic sets the italic attribute.
func (s Style) Italic(v bool) Style { return s.setAttr(propItalic, attrItalic, v) }

// Underline sets the underline attribute.
func (s Style) Underline(v bool) Style { return s.setAttr(propUnderline, attrUnderline, v) }

// Strikethrough sets the strikethrough attribute.
func (s Style) Strikethrough(v bool) Style {
	return s.setAttr(propStrikethrough, attrStrikethrough, v)
}

// Reverse sets the reverse-video attribute.
func (s Style) Reverse(v bool) Style { return s.setAttr(propReverse, attrReverse, v) }

// Blink sets the blink attribute.
func (s Style) Blink(v bool) Style { return s.setAttr(propBlink, attrBlink, v) }

// Faint sets the faint (dim) attribute.
func (s Style) Faint(v bool) Style { return s.setAttr(propFaint, attrFaint, v) }

// expandSides applies the CSS shorthand rule to 1-4 side values:
// 1 arg = all, 2 = vertical/horizontal, 3 = top/horizontal/bottom,
// 4 = top/right/bottom/left. Anything else panics — misconfiguration fails
// loud at construction (golib convention). Result order: top, right, bottom,
// left.
func expandSides(fn string, sides []int) [4]int16 {
	switch len(sides) {
	case 1:
		a := int16(sides[0])
		return [4]int16{a, a, a, a}
	case 2:
		v, h := int16(sides[0]), int16(sides[1])
		return [4]int16{v, h, v, h}
	case 3:
		t, h, b := int16(sides[0]), int16(sides[1]), int16(sides[2])
		return [4]int16{t, h, b, h}
	case 4:
		return [4]int16{int16(sides[0]), int16(sides[1]), int16(sides[2]), int16(sides[3])}
	}
	panic(fmt.Sprintf("style.%s: %d side arguments (CSS shorthand takes 1-4)", fn, len(sides)))
}

// Padding sets padding using CSS shorthand: 1 arg = all sides, 2 = v/h,
// 3 = t/h/b, 4 = t/r/b/l. 0 or >4 arguments panic.
func (s Style) Padding(sides ...int) Style {
	s.padding = expandSides("Padding", sides)
	s.set(propPaddingTop | propPaddingRight | propPaddingBottom | propPaddingLeft)
	return s
}

// Margin sets margins using the same CSS shorthand as Padding.
func (s Style) Margin(sides ...int) Style {
	s.margin = expandSides("Margin", sides)
	s.set(propMarginTop | propMarginRight | propMarginBottom | propMarginLeft)
	return s
}

// Width sets the fixed content width.
func (s Style) Width(w int) Style {
	s.width = int16(w)
	s.set(propWidth)
	return s
}

// Height sets the fixed content height.
func (s Style) Height(h int) Style {
	s.height = int16(h)
	s.set(propHeight)
	return s
}

// MaxWidth caps the rendered width.
func (s Style) MaxWidth(w int) Style {
	s.maxWidth = int16(w)
	s.set(propMaxWidth)
	return s
}

// MaxHeight caps the rendered height.
func (s Style) MaxHeight(h int) Style {
	s.maxHeight = int16(h)
	s.set(propMaxHeight)
	return s
}

// Align sets the horizontal alignment, and optionally the vertical one:
// Align(h) or Align(h, v). h must be AlignLeft/AlignCenter/AlignRight and v
// AlignTop/AlignMiddle/AlignBottom; anything else panics.
func (s Style) Align(h Align, v ...Align) Style {
	if h > AlignRight {
		panic(fmt.Sprintf("style.Align: horizontal alignment %d is not AlignLeft/AlignCenter/AlignRight", h))
	}
	if len(v) > 1 {
		panic(fmt.Sprintf("style.Align: %d vertical alignment arguments (want at most 1)", len(v)))
	}
	s.alignH = h
	s.set(propAlignHorizontal)
	if len(v) == 1 {
		if v[0] < AlignTop || v[0] > AlignBottom {
			panic(fmt.Sprintf("style.Align: vertical alignment %d is not AlignTop/AlignMiddle/AlignBottom", v[0]))
		}
		s.alignV = v[0]
		s.set(propAlignVertical)
	}
	return s
}

// expandEdges applies the CSS shorthand rule to border edge switches; no
// arguments means all edges on. Result order: top, right, bottom, left.
func expandEdges(fn string, edges []bool) uint8 {
	var t, r, b, l bool
	switch len(edges) {
	case 0:
		t, r, b, l = true, true, true, true
	case 1:
		t, r, b, l = edges[0], edges[0], edges[0], edges[0]
	case 2:
		t, r, b, l = edges[0], edges[1], edges[0], edges[1]
	case 3:
		t, r, b, l = edges[0], edges[1], edges[2], edges[1]
	case 4:
		t, r, b, l = edges[0], edges[1], edges[2], edges[3]
	default:
		panic(fmt.Sprintf("style.%s: %d edge arguments (CSS shorthand takes 0-4)", fn, len(edges)))
	}
	var e uint8
	if t {
		e |= edgeTop
	}
	if r {
		e |= edgeRight
	}
	if b {
		e |= edgeBottom
	}
	if l {
		e |= edgeLeft
	}
	return e
}

// Border sets the border style and which edges it paints. The variadic bools
// follow the CSS shorthand rule (no args = all edges, 1 = all, 2 = v/h,
// 3 = t/h/b, 4 = t/r/b/l); >4 panics.
func (s Style) Border(b BorderStyle, edges ...bool) Style {
	s.border = b
	s.borderEdges = expandEdges("Border", edges)
	s.set(propBorderStyle | propBorderTop | propBorderRight | propBorderBottom | propBorderLeft)
	return s
}

// BorderForeground sets the border foreground color on all four edges.
func (s Style) BorderForeground(c ColorSpec) Style {
	col := c.spec()
	s.borderFg = [4]Color{col, col, col, col}
	s.set(propBorderTopForeground | propBorderRightForeground | propBorderBottomForeground | propBorderLeftForeground)
	return s
}

// BorderTopForeground sets the top border foreground color.
func (s Style) BorderTopForeground(c ColorSpec) Style {
	s.borderFg[0] = c.spec()
	s.set(propBorderTopForeground)
	return s
}

// BorderRightForeground sets the right border foreground color.
func (s Style) BorderRightForeground(c ColorSpec) Style {
	s.borderFg[1] = c.spec()
	s.set(propBorderRightForeground)
	return s
}

// BorderBottomForeground sets the bottom border foreground color.
func (s Style) BorderBottomForeground(c ColorSpec) Style {
	s.borderFg[2] = c.spec()
	s.set(propBorderBottomForeground)
	return s
}

// BorderLeftForeground sets the left border foreground color.
func (s Style) BorderLeftForeground(c ColorSpec) Style {
	s.borderFg[3] = c.spec()
	s.set(propBorderLeftForeground)
	return s
}

// Inherit copies from other ONLY the properties not already set on s.
// Margins and padding are NEVER inherited (they are placement, not
// appearance) — the lipgloss rule, kept verbatim. Extras are not inherited.
func (s Style) Inherit(other Style) Style {
	for k := propKey(1); k <= propLast; k <<= 1 {
		switch k {
		case propPaddingTop, propPaddingRight, propPaddingBottom, propPaddingLeft,
			propMarginTop, propMarginRight, propMarginBottom, propMarginLeft:
			continue // placement, never inherited
		}
		if !other.isSet(k) || s.isSet(k) {
			continue
		}
		s.copyProp(other, k)
	}
	return s
}

// copyProp copies one property's value from other and marks it set.
// Padding and margin bits are handled by their callers (Inherit skips them).
func (s *Style) copyProp(other Style, k propKey) {
	switch k {
	case propForeground:
		s.fg = other.fg
	case propBackground:
		s.bg = other.bg
	case propBold, propItalic, propUnderline, propStrikethrough, propReverse, propBlink, propFaint:
		bit := attrBit(k)
		s.attrs = s.attrs&^bit | other.attrs&bit
	case propWidth:
		s.width = other.width
	case propHeight:
		s.height = other.height
	case propMaxWidth:
		s.maxWidth = other.maxWidth
	case propMaxHeight:
		s.maxHeight = other.maxHeight
	case propAlignHorizontal:
		s.alignH = other.alignH
	case propAlignVertical:
		s.alignV = other.alignV
	case propBorderStyle:
		s.border = other.border
	case propBorderTop, propBorderRight, propBorderBottom, propBorderLeft:
		bit := edgeBit(k)
		s.borderEdges = s.borderEdges&^bit | other.borderEdges&bit
	case propBorderTopForeground:
		s.borderFg[0] = other.borderFg[0]
	case propBorderRightForeground:
		s.borderFg[1] = other.borderFg[1]
	case propBorderBottomForeground:
		s.borderFg[2] = other.borderFg[2]
	case propBorderLeftForeground:
		s.borderFg[3] = other.borderFg[3]
	default:
		panic(errs.Fatal{Op: "style: copyProp", Rule: fmt.Sprintf("unhandled property bit %#x", uint64(k))})
	}
	s.set(k)
}

// attrBit maps an attribute propKey to its packed value bit.
func attrBit(k propKey) uint16 {
	switch k {
	case propBold:
		return attrBold
	case propItalic:
		return attrItalic
	case propUnderline:
		return attrUnderline
	case propStrikethrough:
		return attrStrikethrough
	case propReverse:
		return attrReverse
	case propBlink:
		return attrBlink
	case propFaint:
		return attrFaint
	}
	return 0
}

// edgeBit maps a border-edge propKey to its packed value bit.
func edgeBit(k propKey) uint8 {
	switch k {
	case propBorderTop:
		return edgeTop
	case propBorderRight:
		return edgeRight
	case propBorderBottom:
		return edgeBottom
	case propBorderLeft:
		return edgeLeft
	}
	return 0
}
