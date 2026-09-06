package web

import (
	"html/template"
	"io"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// Metrics are the client's MEASURED font metrics.
//
// The client measures and the server never guesses. Sizes are in device pixels
// rather than `ch` units, because `ch` is itself font-relative and so begs the
// very question being asked — "how wide is a cell in this font?".
type Metrics struct {
	// CellW and CellH are the measured advance and line box, in px.
	CellW, CellH float64
}

// valid reports whether metrics are usable. Zero or negative sizes would
// collapse the grid, so they are refused rather than defaulted: a client that
// could not measure must say so and be told to retry, not be handed a layout
// built on a guess.
func (m Metrics) valid() bool { return m.CellW > 0 && m.CellH > 0 }

// renderRow emits the cells of one row as span elements.
//
// # The wide-grapheme hazard, and what actually contains it
//
// A grapheme golib counts as Width 2 (CJK, emoji) must occupy exactly two
// columns or the row drifts — the classic xterm.js bug class. The containment is
// GEOMETRIC, not typographic: a Width-2 head spans exactly two grid tracks and a
// Width-0 continuation emits NO element at all, so the row's track assignment is
// decided by Cell.Width and nothing else. Each box then clips its own overflow,
// so a glyph the font renders wider than its box is visually clipped and cannot
// push its neighbours along.
//
// The font probe informs the capability report; the boxes are what make a
// mismatch safe. Those are different jobs and conflating them is how this class
// of bug survives.
func renderRow(w io.Writer, y int, cells []tui.Cell) {
	col := 1 // CSS grid columns are 1-based
	for _, c := range cells {
		if c.Width == 0 {
			// A continuation cell. Emitting nothing is not an optimisation: an
			// empty box here would occupy a track and shift the rest of the row
			// by one column for every wide grapheme on it.
			continue
		}
		span := int(c.Width)
		writeCellSpan(w, col, span, c)
		col += span
	}
	_ = y
}

// writeCellSpan emits one cell box.
func writeCellSpan(w io.Writer, col, span int, c tui.Cell) {
	io.WriteString(w, `<i class="c" style="grid-column:`)
	io.WriteString(w, strconv.Itoa(col))
	if span > 1 {
		io.WriteString(w, ` / span `)
		io.WriteString(w, strconv.Itoa(span))
	}
	if s := inlineStyle(c.Attrs); s != "" {
		io.WriteString(w, `;`)
		io.WriteString(w, s)
	}
	io.WriteString(w, `">`)
	// Content is HTML-escaped: it is application data, and an App that renders a
	// filename is not thinking about markup.
	template.HTMLEscape(w, []byte(c.Content))
	io.WriteString(w, `</i>`)
}

// inlineStyle converts resolved cell attributes to CSS declarations.
//
// Reverse is applied by SWAPPING the emitted colors rather than by a CSS filter:
// a filter would also invert whatever the theme put behind the cell, and the
// terminal semantics are specifically an fg/bg exchange.
func inlineStyle(a tui.CellAttrs) string {
	fg, bg := a.FG, a.BG
	if a.Mask&tui.AttrReverse != 0 {
		fg, bg = bg, fg
		// A default-on-default reverse still has to be visible, so the swap
		// resolves both sides to the theme's explicit tokens.
		if fg.Kind == tui.CellColorDefault {
			fg = defaultBGToken
		}
		if bg.Kind == tui.CellColorDefault {
			bg = defaultFGToken
		}
	}

	var b strings.Builder
	if s := cssColor(fg); s != "" {
		b.WriteString("color:")
		b.WriteString(s)
		b.WriteString(";")
	}
	if s := cssColor(bg); s != "" {
		b.WriteString("background:")
		b.WriteString(s)
		b.WriteString(";")
	}
	if a.Mask&tui.AttrBold != 0 {
		b.WriteString("font-weight:700;")
	}
	if a.Mask&tui.AttrFaint != 0 {
		// Opacity rather than a dimmer color: the color may be a theme custom
		// property whose value the server does not know.
		b.WriteString("opacity:.6;")
	}
	if a.Mask&tui.AttrItalic != 0 {
		b.WriteString("font-style:italic;")
	}
	if deco := decoration(a.Mask); deco != "" {
		b.WriteString("text-decoration:")
		b.WriteString(deco)
		b.WriteString(";")
	}
	if a.Mask&tui.AttrBlink != 0 {
		// Deliberately NOT a CSS animation. Blink is an accessibility hazard and
		// browsers dropped it on purpose; the cell is marked so a stylesheet can
		// opt in, and nothing moves by default.
		b.WriteString("--blink:1;")
	}
	return b.String()
}

// decoration combines underline and strikethrough into one declaration, since
// they share a CSS property and emitting two would mean the second wins.
func decoration(m tui.AttrMask) string {
	var parts []string
	if m&tui.AttrUnderline != 0 {
		parts = append(parts, "underline")
	}
	if m&tui.AttrStrikethrough != 0 {
		parts = append(parts, "line-through")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// Theme tokens the reverse-video swap resolves against (custom properties).
var (
	defaultFGToken = tui.CellColor{Kind: cellColorToken, Index: tokenFG}
	defaultBGToken = tui.CellColor{Kind: cellColorToken, Index: tokenBG}
)

// cellColorToken is a rendering-only marker for "emit the theme's custom
// property". It is deliberately outside tui's CellColorKind range: resolution
// already happened upstream and this package must not appear to
// add a new resolved kind.
const cellColorToken tui.CellColorKind = 200

const (
	tokenFG uint8 = 0
	tokenBG uint8 = 1
)

// cssColor renders one resolved color, or "" for the terminal default (which the
// stylesheet already supplies).
func cssColor(c tui.CellColor) string {
	switch c.Kind {
	case tui.CellColorDefault:
		return ""
	case tui.CellColorANSI, tui.CellColorANSI256:
		return "var(--a" + strconv.Itoa(int(c.Index)) + ")"
	case tui.CellColorRGB:
		return "#" + hex2(c.R) + hex2(c.G) + hex2(c.B)
	case cellColorToken:
		if c.Index == tokenBG {
			return "var(--bg)"
		}
		return "var(--fg)"
	}
	return ""
}

const hexDigits = "0123456789abcdef"

func hex2(v uint8) string {
	return string([]byte{hexDigits[v>>4], hexDigits[v&0x0f]})
}
