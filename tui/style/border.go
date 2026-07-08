package style

// BorderStyle describes the eight pieces of a box border. The fields are
// strings, not runes: border pieces may be multi-byte graphemes; cells hold
// grapheme strings (ADR-0003). BorderStyle is a flat comparable struct, so
// Style stays comparable (ADR-0006 §2.1).
type BorderStyle struct {
	Top, Bottom, Left, Right                   string
	TopLeft, TopRight, BottomLeft, BottomRight string
}

// Standard border prefabs (ADR-0006 §2.3).
var (
	// BorderNormal is the standard single-line box border.
	BorderNormal = BorderStyle{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "┌", TopRight: "┐", BottomLeft: "└", BottomRight: "┘",
	}

	// BorderRounded is the single-line border with rounded corners.
	BorderRounded = BorderStyle{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯",
	}

	// BorderThick is the heavy single-line border.
	BorderThick = BorderStyle{
		Top: "━", Bottom: "━", Left: "┃", Right: "┃",
		TopLeft: "┏", TopRight: "┓", BottomLeft: "┗", BottomRight: "┛",
	}

	// BorderDouble is the double-line border.
	BorderDouble = BorderStyle{
		Top: "═", Bottom: "═", Left: "║", Right: "║",
		TopLeft: "╔", TopRight: "╗", BottomLeft: "╚", BottomRight: "╝",
	}

	// BorderHidden paints spaces, preserving frame size for alignment
	// stability: a focused panel can swap BorderHidden for a visible border
	// without shifting its content.
	BorderHidden = BorderStyle{
		Top: " ", Bottom: " ", Left: " ", Right: " ",
		TopLeft: " ", TopRight: " ", BottomLeft: " ", BottomRight: " ",
	}
)
