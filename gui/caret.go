package gui

import (
	"image"
	"image/color"

	"github.com/yongjohnlee80/golib/tui"
)

// Caret positioning and visual integration for native graphical views.
//
// In standard terminal applications, cursor positioning is restricted to discrete character
// cell coordinates (X, Y). However, native pixel-rendered text components (such as rich text
// editors, markdown viewers, and proportional font displays) position their text cursors
// at arbitrary sub-pixel locations between character glyphs.
//
// # Architectural Model: Cell Cursor vs. Pixel Caret
//
//	Terminal Cell Grid:            Native Pixel View:
//	┌───┬───┬───┬───┐              ┌───────────────────────────┐
//	│ H │ E │ L │ L │              │ Proportional text layout  │
//	└───┴───┴───┴───┘              │ H e l l o ,   w o r l d ! │
//	      ▲                                        │
//	      │ Cell Cursor                            ▼ Pixel Caret (Sub-pixel rect & baseline)
//	      └── Restricted to [2,0]                  └── At exact fractional (X, Y)
//
// When a native component implements [CaretView], the GUI backend queries its pixel-precise
// caret bounds during frame composition. If no terminal cell cursor is active, the backend:
//  1. Informs the host operating system's Input Method Editor (IME) of the exact pixel
//     caret coordinates so that IME candidate popup windows appear directly anchored to the text.
//  2. Renders in-progress IME composition text (preedit) inline at the caret with matching
//     font size, baseline alignment, and foreground/background colors.

// CaretView is implemented by a [View] that manages its own pixel-accurate text caret.
//
// Because pixel layouts position characters using proportional fonts and fractional kerning,
// their carets frequently fall between cell grid columns. After a view paints, [CaretView.Caret]
// returns the caret bounding box in logical pixels and its typographical baseline offset.
//
// Return values:
//   - r: caret bounding rectangle in logical pixels relative to the view's origin.
//   - baseline: vertical distance from the top of rect r to the font baseline.
//   - ok: true if the caret is currently visible and active; false if hidden or defocused.
type CaretView interface {
	View
	Caret() (r Rect, baseline float32, ok bool)
}

// CaretColorsView extends [CaretView] to report foreground and background text colors at the caret.
//
// This enables the backend to style inline IME preedit text to seamlessly match the surrounding
// document theme rather than defaulting to the global window theme palette.
type CaretColorsView interface {
	CaretView
	CaretColors() (fg, bg color.NRGBA)
}

// caretAt represents a resolved caret position scaled to physical device pixels.
type caretAt struct {
	rect   image.Rectangle // device pixel bounding rectangle
	base   int             // baseline offset in device pixels from rect.Min.Y
	fg, bg color.NRGBA     // text colors for styling composition text
	tinted bool            // true if fg and bg were explicitly supplied by CaretColorsView
}

// viewCaret resolves the active pixel caret from the frame's placed native views.
//
// Iterates through all [tui.NativePlacement] instances in top-to-bottom paint order.
// The last view that reports an active [CaretView.Caret] wins. If that view also implements
// [CaretColorsView], its custom colors are latched into the resulting [caretAt].
//
// Returns the resolved caret in device pixels and true if an active caret was found.
func viewCaret(m metrics, natives []tui.NativePlacement) (caretAt, bool) {
	var out caretAt
	found := false
	for _, p := range natives {
		cv, ok := p.View.(CaretView)
		if !ok {
			continue
		}
		r, b, ok := cv.Caret()
		if !ok {
			continue
		}
		dev, within, in := m.anchored(tui.Rect{X: p.X, Y: p.Y, W: p.Cols, H: p.Rows})
		if !in {
			continue // over no pane: nothing to anchor the caret to
		}
		o := dev.Min // the view's own origin, as its canvas has it
		s := m.scale
		rect := image.Rect(o.X+int(r.X*s), o.Y+int(r.Y*s), o.X+int((r.X+max(r.W, 1))*s), o.Y+int((r.Y+r.H)*s))
		if m.layout != nil {
			// Only the part the view painted: a caret its pane cuts away is not shown, so the
			// input method is not told about it either.
			if rect = rect.Intersect(dev.Intersect(within)); rect.Empty() {
				continue
			}
		}
		out = caretAt{rect: rect, base: int(b * s)}
		if ccv, ok := p.View.(CaretColorsView); ok {
			out.fg, out.bg = ccv.CaretColors()
			out.tinted = true
		}
		found = true
	}
	return out, found
}
