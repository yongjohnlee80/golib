package style

// Package style's Unset methods provide granular reversion of individual style properties
// back to their untouched, un-configured state.
//
// # Why Unset Exists
//
// In component-based TUI architectures, widgets frequently derive their appearance by inheriting
// from shared themes or parent container styles (e.g., a standard modal card, a highlighted
// table row). However, child widgets often need to selectively opt out of specific attributes—such
// as stripping borders in nested panels, dropping margins in compact modes, or resetting text
// attributes when rendering unselected items.
//
// # Structural Equality Invariant: The Dual-Clear Mechanism
//
// Every Unset* method performs two coordinated operations on the copied [Style]:
//  1. Clears the property's bit in the uint64 props bitfield (marking it as untouched).
//  2. Zeroes the corresponding value field (e.g., zeroing Color, unmasking packed uint16 attrs,
//     clearing int16 dimensions).
//
// Why both? Go compares structs field-by-field. If an Unset method only cleared the bitfield
// flag but left residual bits in the value field, a style modified and then unset would NOT
// be structurally equal (==) to a fresh style:
//
//	st := style.New().Bold(true).UnsetBold()
//	st == style.New() // Evaluates to TRUE because attrs and props are both zeroed.
//
// This reflexivity invariant ensures that the TUI resolver's internal attribute cache
// (which uses [Style] directly as a map key) never misses cache hits due to "ghost" attribute values.
//
// # Interaction with Inherit
//
// [Style.Inherit] copies properties from another style ONLY if the receiver does not already
// have them set (!s.isSet(k)). Calling an Unset method clears the set bit, effectively
// re-admitting that property to participate in subsequent inheritance:
//
//	base := style.New().Foreground(style.TokenPrimary).Bold(true)
//	custom := base.UnsetBold() // custom now has Bold untouched
//
//	fallback := style.New().Bold(false).Italic(true)
//	result := custom.Inherit(fallback) // result inherits Italic, and re-inherits Bold(false)
//
// # Usage Examples
//
// 1. Reverting text styling for an unselected menu item:
//
//	// Derived from an active item style
//	itemStyle := activeItemStyle.UnsetBold().UnsetUnderline().Foreground(style.TokenTextMuted)
//
// 2. Stripping borders and padding for a compact inner container:
//
//	// Remove framing so inner content fits snugly inside a parent card
//	innerStyle := cardStyle.UnsetBorder().UnsetPadding().UnsetMargin()
//
// 3. Selectively unsetting edge-specific border colors:
//
//	// Retain top/bottom/right border colors, but reset left accent border to default
//	neutralized := calloutStyle.UnsetBorderLeftForeground()

// UnsetForeground clears the foreground color.
func (s Style) UnsetForeground() Style {
	s.fg = Color{}
	s.unset(propForeground)
	return s
}

// UnsetBackground clears the background color.
func (s Style) UnsetBackground() Style {
	s.bg = Color{}
	s.unset(propBackground)
	return s
}

// unsetAttr clears one packed boolean attribute (value + set bit).
func (s Style) unsetAttr(k propKey, bit uint16) Style {
	s.attrs &^= bit
	s.unset(k)
	return s
}

// UnsetBold clears the bold attribute.
func (s Style) UnsetBold() Style { return s.unsetAttr(propBold, attrBold) }

// UnsetItalic clears the italic attribute.
func (s Style) UnsetItalic() Style { return s.unsetAttr(propItalic, attrItalic) }

// UnsetUnderline clears the underline attribute.
func (s Style) UnsetUnderline() Style { return s.unsetAttr(propUnderline, attrUnderline) }

// UnsetStrikethrough clears the strikethrough attribute.
func (s Style) UnsetStrikethrough() Style { return s.unsetAttr(propStrikethrough, attrStrikethrough) }

// UnsetReverse clears the reverse-video attribute.
func (s Style) UnsetReverse() Style { return s.unsetAttr(propReverse, attrReverse) }

// UnsetBlink clears the blink attribute.
func (s Style) UnsetBlink() Style { return s.unsetAttr(propBlink, attrBlink) }

// UnsetFaint clears the faint attribute.
func (s Style) UnsetFaint() Style { return s.unsetAttr(propFaint, attrFaint) }

// UnsetPadding clears all four padding sides.
func (s Style) UnsetPadding() Style {
	s.padding = [4]int16{}
	s.unset(propPaddingTop | propPaddingRight | propPaddingBottom | propPaddingLeft)
	return s
}

// UnsetMargin clears all four margin sides.
func (s Style) UnsetMargin() Style {
	s.margin = [4]int16{}
	s.unset(propMarginTop | propMarginRight | propMarginBottom | propMarginLeft)
	return s
}

// UnsetWidth clears the fixed width.
func (s Style) UnsetWidth() Style {
	s.width = 0
	s.unset(propWidth)
	return s
}

// UnsetHeight clears the fixed height.
func (s Style) UnsetHeight() Style {
	s.height = 0
	s.unset(propHeight)
	return s
}

// UnsetMaxWidth clears the width cap.
func (s Style) UnsetMaxWidth() Style {
	s.maxWidth = 0
	s.unset(propMaxWidth)
	return s
}

// UnsetMaxHeight clears the height cap.
func (s Style) UnsetMaxHeight() Style {
	s.maxHeight = 0
	s.unset(propMaxHeight)
	return s
}

// UnsetAlign clears both the horizontal and vertical alignment.
func (s Style) UnsetAlign() Style {
	s.alignH, s.alignV = 0, 0
	s.unset(propAlignHorizontal | propAlignVertical)
	return s
}

// UnsetBorder clears the border style and all edge switches. Border
// foreground colors are separate properties; clear them with
// UnsetBorderForeground.
func (s Style) UnsetBorder() Style {
	s.border = BorderStyle{}
	s.borderEdges = 0
	s.unset(propBorderStyle | propBorderTop | propBorderRight | propBorderBottom | propBorderLeft)
	return s
}

// UnsetBorderForeground clears the border foreground color on all four edges.
func (s Style) UnsetBorderForeground() Style {
	s.borderFg = [4]Color{}
	s.unset(propBorderTopForeground | propBorderRightForeground | propBorderBottomForeground | propBorderLeftForeground)
	return s
}

// UnsetBorderTopForeground clears the top border foreground color.
func (s Style) UnsetBorderTopForeground() Style {
	s.borderFg[0] = Color{}
	s.unset(propBorderTopForeground)
	return s
}

// UnsetBorderRightForeground clears the right border foreground color.
func (s Style) UnsetBorderRightForeground() Style {
	s.borderFg[1] = Color{}
	s.unset(propBorderRightForeground)
	return s
}

// UnsetBorderBottomForeground clears the bottom border foreground color.
func (s Style) UnsetBorderBottomForeground() Style {
	s.borderFg[2] = Color{}
	s.unset(propBorderBottomForeground)
	return s
}

// UnsetBorderLeftForeground clears the left border foreground color.
func (s Style) UnsetBorderLeftForeground() Style {
	s.borderFg[3] = Color{}
	s.unset(propBorderLeftForeground)
	return s
}
