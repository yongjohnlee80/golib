package style

// Getters mirror setters. Single-value getters return (value, isSet) — the
// bool distinguishes an explicitly set property from an untouched one
// (set-ness lives in the props bitfield, not the value's zero-ness).
// Four-side getters (padding, margin, border edges) return plain values;
// unset sides read as zero/false.

// GetForeground returns the foreground color and whether it is set.
func (s Style) GetForeground() (Color, bool) { return s.fg, s.isSet(propForeground) }

// GetBackground returns the background color and whether it is set.
func (s Style) GetBackground() (Color, bool) { return s.bg, s.isSet(propBackground) }

func (s Style) getAttr(k propKey, bit uint16) (bool, bool) {
	return s.attrs&bit != 0, s.isSet(k)
}

// GetBold returns the bold value and whether it is set:
// New().Bold(false) reports (false, true); New() reports (false, false).
func (s Style) GetBold() (bool, bool) { return s.getAttr(propBold, attrBold) }

// GetItalic returns the italic value and whether it is set.
func (s Style) GetItalic() (bool, bool) { return s.getAttr(propItalic, attrItalic) }

// GetUnderline returns the underline value and whether it is set.
func (s Style) GetUnderline() (bool, bool) { return s.getAttr(propUnderline, attrUnderline) }

// GetStrikethrough returns the strikethrough value and whether it is set.
func (s Style) GetStrikethrough() (bool, bool) {
	return s.getAttr(propStrikethrough, attrStrikethrough)
}

// GetReverse returns the reverse-video value and whether it is set.
func (s Style) GetReverse() (bool, bool) { return s.getAttr(propReverse, attrReverse) }

// GetBlink returns the blink value and whether it is set.
func (s Style) GetBlink() (bool, bool) { return s.getAttr(propBlink, attrBlink) }

// GetFaint returns the faint value and whether it is set.
func (s Style) GetFaint() (bool, bool) { return s.getAttr(propFaint, attrFaint) }

// GetPadding returns the four padding sides; unset sides are 0.
func (s Style) GetPadding() (top, right, bottom, left int) {
	return int(s.padding[0]), int(s.padding[1]), int(s.padding[2]), int(s.padding[3])
}

// GetMargin returns the four margin sides; unset sides are 0.
func (s Style) GetMargin() (top, right, bottom, left int) {
	return int(s.margin[0]), int(s.margin[1]), int(s.margin[2]), int(s.margin[3])
}

// GetWidth returns the fixed width and whether it is set.
func (s Style) GetWidth() (int, bool) { return int(s.width), s.isSet(propWidth) }

// GetHeight returns the fixed height and whether it is set.
func (s Style) GetHeight() (int, bool) { return int(s.height), s.isSet(propHeight) }

// GetMaxWidth returns the width cap and whether it is set.
func (s Style) GetMaxWidth() (int, bool) { return int(s.maxWidth), s.isSet(propMaxWidth) }

// GetMaxHeight returns the height cap and whether it is set.
func (s Style) GetMaxHeight() (int, bool) { return int(s.maxHeight), s.isSet(propMaxHeight) }

// GetAlignHorizontal returns the horizontal alignment and whether it is set.
func (s Style) GetAlignHorizontal() (Align, bool) {
	return s.alignH, s.isSet(propAlignHorizontal)
}

// GetAlignVertical returns the vertical alignment and whether it is set.
func (s Style) GetAlignVertical() (Align, bool) {
	return s.alignV, s.isSet(propAlignVertical)
}

// GetBorder returns the border style and whether it is set.
func (s Style) GetBorder() (BorderStyle, bool) { return s.border, s.isSet(propBorderStyle) }

// GetBorderEdges returns which edges the border paints; all false when no
// border is set.
func (s Style) GetBorderEdges() (top, right, bottom, left bool) {
	return s.borderEdges&edgeTop != 0,
		s.borderEdges&edgeRight != 0,
		s.borderEdges&edgeBottom != 0,
		s.borderEdges&edgeLeft != 0
}

// GetBorderTopForeground returns the top border foreground color and whether
// it is set.
func (s Style) GetBorderTopForeground() (Color, bool) {
	return s.borderFg[0], s.isSet(propBorderTopForeground)
}

// GetBorderRightForeground returns the right border foreground color and
// whether it is set.
func (s Style) GetBorderRightForeground() (Color, bool) {
	return s.borderFg[1], s.isSet(propBorderRightForeground)
}

// GetBorderBottomForeground returns the bottom border foreground color and
// whether it is set.
func (s Style) GetBorderBottomForeground() (Color, bool) {
	return s.borderFg[2], s.isSet(propBorderBottomForeground)
}

// GetBorderLeftForeground returns the left border foreground color and
// whether it is set.
func (s Style) GetBorderLeftForeground() (Color, bool) {
	return s.borderFg[3], s.isSet(propBorderLeftForeground)
}

// Frame math. The box model is lipgloss's, kept verbatim:
// content → padding → border → margin, border outside padding, margin
// outside border. Used by layout and Box to convert
// outer ↔ content rects.

// borderEdgeSize is 1 when the border is set, the edge is enabled, and its
// glyph is non-empty (BorderHidden's spaces still count — it preserves frame
// size for alignment stability); otherwise 0. Border pieces occupy one cell
// each: cells hold grapheme strings, so multi-byte glyphs are
// still one column.
func (s Style) borderEdgeSize(edge uint8, glyph string) int {
	if !s.isSet(propBorderStyle) || s.borderEdges&edge == 0 || glyph == "" {
		return 0
	}
	return 1
}

// GetHorizontalPadding returns left + right padding.
func (s Style) GetHorizontalPadding() int { return int(s.padding[3]) + int(s.padding[1]) }

// GetVerticalPadding returns top + bottom padding.
func (s Style) GetVerticalPadding() int { return int(s.padding[0]) + int(s.padding[2]) }

// GetHorizontalBorderSize returns the columns occupied by the left and right
// border edges (0, 1, or 2).
func (s Style) GetHorizontalBorderSize() int {
	return s.borderEdgeSize(edgeLeft, s.border.Left) + s.borderEdgeSize(edgeRight, s.border.Right)
}

// GetVerticalBorderSize returns the rows occupied by the top and bottom
// border edges (0, 1, or 2).
func (s Style) GetVerticalBorderSize() int {
	return s.borderEdgeSize(edgeTop, s.border.Top) + s.borderEdgeSize(edgeBottom, s.border.Bottom)
}

// GetHorizontalFrameSize returns left + right margin + border + padding —
// the difference between the outer width and the content width.
func (s Style) GetHorizontalFrameSize() int {
	return int(s.margin[3]) + int(s.margin[1]) + s.GetHorizontalBorderSize() + s.GetHorizontalPadding()
}

// GetVerticalFrameSize returns top + bottom margin + border + padding —
// the difference between the outer height and the content height.
func (s Style) GetVerticalFrameSize() int {
	return int(s.margin[0]) + int(s.margin[2]) + s.GetVerticalBorderSize() + s.GetVerticalPadding()
}
