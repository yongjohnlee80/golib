package style

// BorderStyle describes the eight pieces of a box border.
//
// # Design Rationale: Strings vs Runes
//
// The fields are strings rather than runes because terminal cells render grapheme
// clusters, not isolated Unicode code points. Box-drawing glyphs are multi-byte UTF-8
// characters (e.g. "─" is 3 bytes: 0xE2 0x94 0x80), and custom borders may incorporate
// multi-codepoint sequences. By storing flat string fields, BorderStyle remains a pure
// comparable struct with no slice or pointer overhead, preserving the core invariant
// that [Style] is comparable with == and can serve as a map key for render-cache lookups.
//
// # Spatial Layout Model
//
// The eight border segments map to the box perimeter as follows:
//
//	     TopLeft                   Top (repeated)                 TopRight
//	        ┌───────────────────────────┬───────────────────────────┐
//	        │                           │                           │
//	 Left   │                      Content Area                     │   Right
//	(repeat)│                 w = Width, h = Height                 │ (repeat)
//	        │                           │                           │
//	        └───────────────────────────┴───────────────────────────┘
//	    BottomLeft                Bottom (repeated)              BottomRight
//
// The frame math in package tui accounts for active border edges: horizontal border size
// is (width of Left) + (width of Right), and vertical border size is (height of Top) +
// (height of Bottom). When an edge is disabled via [Style.Border], its contribution to
// the frame size becomes zero.
//
// # Usage Examples
//
// 1. Using standard prefabs with full border:
//
//	st := style.New().
//		Border(style.BorderRounded).
//		BorderForeground(style.TokenPrimary)
//
// 2. Selective edges (e.g., a left accent bar for callouts / blockquotes):
//
//	// CSS shorthand: Border(b, top, right, bottom, left)
//	callout := style.New().
//		Border(style.BorderThick, false, false, false, true).
//		BorderLeftForeground(style.TokenAccent).
//		PaddingLeft(1)
//
// 3. Defining a custom ASCII border for minimum-capability terminals:
//
//	asciiBorder := style.BorderStyle{
//		Top: "-", Bottom: "-", Left: "|", Right: "|",
//		TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
//	}
//	st := style.New().Border(asciiBorder)
type BorderStyle struct {
	Top, Bottom, Left, Right                   string
	TopLeft, TopRight, BottomLeft, BottomRight string
}

// Standard border prefabs.
var (
	// BorderNormal is the standard single-line box border using box-drawing characters:
	//
	//	┌───┐
	//	│   │
	//	└───┘
	BorderNormal = BorderStyle{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "┌", TopRight: "┐", BottomLeft: "└", BottomRight: "┘",
	}

	// BorderRounded is the single-line border with rounded corners:
	//
	//	╭───╮
	//	│   │
	//	╰───╯
	//
	// Commonly used for dialog windows, popups, and elevated cards.
	BorderRounded = BorderStyle{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯",
	}

	// BorderThick is the heavy single-line border:
	//
	//	┏━━━┓
	//	┃   ┃
	//	┗━━━┛
	//
	// Ideal for emphasizing active modals or focused input panels.
	BorderThick = BorderStyle{
		Top: "━", Bottom: "━", Left: "┃", Right: "┃",
		TopLeft: "┏", TopRight: "┓", BottomLeft: "┗", BottomRight: "┛",
	}

	// BorderDouble is the classical double-line box border:
	//
	//	╔═══╗
	//	║   ║
	//	╚═══╝
	//
	// Frequently utilized for prominent header blocks or main application frames.
	BorderDouble = BorderStyle{
		Top: "═", Bottom: "═", Left: "║", Right: "║",
		TopLeft: "╔", TopRight: "╗", BottomLeft: "╚", BottomRight: "╝",
	}

	// BorderHidden paints space characters (" ") along all eight perimeter segments.
	//
	// Layout Stability Rationale:
	// In TUI layout engines, removing a border changes the total frame size by 2 horizontal
	// cells and 2 vertical cells. If an unfocused widget has no border and gains a border on
	// focus, the interior content shifts by 1 cell, causing visible visual jitter ("layout jump").
	//
	// By styling unfocused widgets with BorderHidden and focused widgets with a visible border
	// (e.g. BorderRounded), the frame dimensions remain identical across focus transitions:
	//
	//	unfocused := style.New().Border(style.BorderHidden).Padding(1)
	//	focused   := unfocused.Border(style.BorderRounded).BorderForeground(style.TokenBorderFocused)
	//	// Both styles share the exact same GetHorizontalBorderSize() and GetVerticalBorderSize().
	BorderHidden = BorderStyle{
		Top: " ", Bottom: " ", Left: " ", Right: " ",
		TopLeft: " ", TopRight: " ", BottomLeft: " ", BottomRight: " ",
	}
)
