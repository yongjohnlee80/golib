package style

// Unset* clears a property's props bit AND zeroes its value field, so an
// unset-then-compared style is == to one that never set the property.

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
