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

// Style is an immutable, value-semantic style definition that encapsulates ANSI text
// attributes, foreground and background colors, box-model framing (padding, margin, border),
// dimensional constraints, and layout alignment.
//
// # What Style Solves
//
// Traditional TUI styling approaches suffer from several recurring pitfalls:
//  1. Mutable Pointer Aliasing: Sharing a style pointer across widgets allows one component
//     to inadvertently mutate another component's appearance.
//  2. Unbounded Heap Allocations: Returning new heap-allocated style structs per fluent call
//     overwhelms the Go garbage collector during 60 FPS animation loops.
//  3. Loss of "Set" State: Standard structs cannot distinguish between an attribute that was
//     never specified versus one explicitly set to its zero value (e.g. bold=false vs unset).
//  4. Non-Comparable Types: Embedding slices, maps, or closures in style definitions prevents
//     using == or map caches, destroying render pipeline memoization.
//
// Style resolves all four issues:
//   - Immutable Value Semantics: Setters receive s by value, mutate the local copy, and return
//     it. Assignment is a deep copy, eliminatng defensive copying.
//   - Zero Allocations: Modifying properties executes in ~18ns with zero heap allocations.
//   - Explicit Set-Bitfield: A uint64 props bitfield tracks whether each property has been
//     explicitly configured, powering selective [Style.Inherit] and clean [Style.Unset] behavior.
//   - Comparable: Style is a flat struct (no slices or maps directly). It is strictly comparable
//     with == and functions natively as a map key for render-time attribute caches.
//
// # The TUI Box Model Hierarchy
//
// Style implements the classical CSS box model, calculating frame dimensions from the
// inside out:
//
//	┌────────────────────────────────────────────────────────┐
//	│ Margin (outer transparent spacing)                     │
//	│  ┌──────────────────────────────────────────────────┐  │
//	│  │ Border (box-drawing perimeter)                   │  │
//	│  │  ┌────────────────────────────────────────────┐  │  │
//	│  │  │ Padding (interior whitespace clearance)    │  │  │
//	│  │  │  ┌──────────────────────────────────────┐  │  │  │
//	│  │  │  │ Content Area (rendered text/widgets) │  │  │  │
//	│  │  │  │ w = Width, h = Height                │  │  │  │
//	│  │  │  └──────────────────────────────────────┘  │  │  │
//	│  │  └────────────────────────────────────────────┘  │  │
//	│  └──────────────────────────────────────────────────┘  │
//	└────────────────────────────────────────────────────────┘
//
// Total horizontal frame size = Margin(L+R) + Border(L+R) + Padding(L+R)
// Total vertical frame size   = Margin(T+B) + Border(T+B) + Padding(T+B)
//
// # Usage Examples
//
// 1. Creating a prominent notification modal card:
//
//	modalStyle := style.New().
//		Foreground(style.TokenTextOnPrimary).
//		Background(style.TokenSurface).
//		Bold(true).
//		Padding(1, 2).                   // 1 vertical cell, 2 horizontal cells
//		Border(style.BorderRounded).     // all four edges
//		BorderForeground(style.TokenPrimary).
//		Margin(1).                       // outer spacing
//		Align(style.AlignCenter)
//
// 2. Creating an interactive button with state variants:
//
//	normalBtn := style.New().
//		Foreground(style.TokenPrimary).
//		Background(style.TokenSurface).
//		Padding(0, 1).
//		Border(style.BorderNormal)
//
//	// Focused button inherits layout, updates colors & weight:
//	focusedBtn := normalBtn.
//		Bold(true).
//		BorderForeground(style.TokenBorderFocused).
//		Foreground(style.TokenAccent)
//
// 3. Status bar pill with tight padding:
//
//	statusPill := style.New().
//		Foreground(style.TokenTextOnSuccess).
//		Background(style.TokenSuccess).
//		Bold(true).
//		Padding(0, 1)
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

// Padding sets interior padding using standard CSS shorthand rules:
//   - 1 argument:  all 4 sides (top = right = bottom = left = sides[0])
//   - 2 arguments: vertical, horizontal (top/bottom = sides[0], left/right = sides[1])
//   - 3 arguments: top, horizontal, bottom (top = sides[0], left/right = sides[1], bottom = sides[2])
//   - 4 arguments: top, right, bottom, left (clockwise: sides[0], sides[1], sides[2], sides[3])
//
// Passing 0 or more than 4 arguments panics at construction (fail-loud convention).
//
// Usage:
//
//	st.Padding(1)       // 1 cell on all sides
//	st.Padding(1, 2)    // 1 cell top/bottom, 2 cells left/right
//	st.Padding(1, 2, 3) // 1 top, 2 left/right, 3 bottom
//	st.Padding(1, 2, 3, 4) // 1 top, 2 right, 3 bottom, 4 left
func (s Style) Padding(sides ...int) Style {
	s.padding = expandSides("Padding", sides)
	s.set(propPaddingTop | propPaddingRight | propPaddingBottom | propPaddingLeft)
	return s
}

// Margin sets exterior margins using the same CSS shorthand rules as [Style.Padding].
// Margins define transparent clearance outside the border perimeter.
//
// Note: Margins represent external layout placement and are NEVER copied by [Style.Inherit].
//
// Usage:
//
//	st.Margin(1)    // 1 cell margin around the entire widget
//	st.Margin(1, 0) // 1 cell vertical margin, 0 horizontal
func (s Style) Margin(sides ...int) Style {
	s.margin = expandSides("Margin", sides)
	s.set(propMarginTop | propMarginRight | propMarginBottom | propMarginLeft)
	return s
}

// Width sets the fixed content width in monospace terminal cells.
func (s Style) Width(w int) Style {
	s.width = int16(w)
	s.set(propWidth)
	return s
}

// Height sets the fixed content height in terminal lines.
func (s Style) Height(h int) Style {
	s.height = int16(h)
	s.set(propHeight)
	return s
}

// MaxWidth caps the rendered content width in monospace cells.
func (s Style) MaxWidth(w int) Style {
	s.maxWidth = int16(w)
	s.set(propMaxWidth)
	return s
}

// MaxHeight caps the rendered content height in terminal lines.
func (s Style) MaxHeight(h int) Style {
	s.maxHeight = int16(h)
	s.set(propMaxHeight)
	return s
}

// Align sets the horizontal alignment and, optionally, the vertical alignment of content
// within the widget's allocated rectangle:
//   - Horizontal: [AlignLeft], [AlignCenter], [AlignRight]
//   - Vertical (optional): [AlignTop], [AlignMiddle], [AlignBottom]
//
// Passing an invalid alignment constant or more than 1 vertical alignment argument panics.
//
// Usage:
//
//	st.Align(style.AlignCenter)                    // Center horizontally
//	st.Align(style.AlignRight, style.AlignBottom)  // Bottom-right corner
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

// Border sets the border style and selectively enables which perimeter edges to paint.
// The variadic bools follow CSS shorthand clockwise rules:
//   - 0 arguments: all 4 edges enabled (default)
//   - 1 argument:  all 4 edges set to edges[0]
//   - 2 arguments: vertical (top/bottom), horizontal (left/right)
//   - 3 arguments: top, horizontal (left/right), bottom
//   - 4 arguments: top, right, bottom, left (clockwise)
//
// Passing more than 4 arguments panics.
//
// Usage:
//
//	st.Border(style.BorderRounded)                     // Full box border
//	st.Border(style.BorderNormal, true, false)         // Top & bottom horizontal rules
//	st.Border(style.BorderThick, false, false, false, true) // Left accent bar only
func (s Style) Border(b BorderStyle, edges ...bool) Style {
	s.border = b
	s.borderEdges = expandEdges("Border", edges)
	s.set(propBorderStyle | propBorderTop | propBorderRight | propBorderBottom | propBorderLeft)
	return s
}

// BorderForeground sets the border foreground color across all four edges simultaneously.
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

// Inherit copies from other ONLY the properties that are NOT already set on s.
//
// # Appearance vs. Placement Rationale
//
// Margins and padding are NEVER copied during inheritance. In user interface architectures,
// padding and margins define local spatial layout (placement relative to neighboring elements),
// whereas colors, font weights (bold, italic), borders, and alignment define visual styling
// (appearance). Automatically inheriting margins or padding would corrupt child layout geometry.
//
// Third-party extra properties (attached via [Style.Ext]) are also excluded from inheritance.
//
// Usage:
//
//	themeBase := style.New().
//		Foreground(style.TokenForeground).
//		Background(style.TokenSurface).
//		Bold(true)
//
//	// Custom button sets its own foreground and padding:
//	btn := style.New().
//		Foreground(style.TokenPrimary).
//		Padding(1, 2)
//
//	// inheritedBtn receives Background and Bold from themeBase,
//	// keeps its own Foreground, and retains its own Padding (unaffected):
//	inheritedBtn := btn.Inherit(themeBase)
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
