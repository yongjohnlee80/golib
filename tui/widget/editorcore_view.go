package widget

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/style"
)

// What a widget reads from its core to draw it, in buffer positions. The tui Editor's cell
// layout reads through these, as a widget in another package must.

// LineCount is the number of lines in the buffer; never 0.
func (c *EditorCore) LineCount() int { return len(c.buf.lines) }

// LineAt is line i's text, without copying the buffer. i outside the buffer is "".
func (c *EditorCore) LineAt(i int) string {
	if i < 0 || i >= len(c.buf.lines) {
		return ""
	}
	return c.buf.lines[i]
}

// Selected reports whether cluster col of line ln is inside the Visual selection. In
// Visual-line mode every column of a selected line is, so a widget may fill the whole row.
func (c *EditorCore) Selected(ln, col int) bool { return c.inVisual(ln, col) }

// HighlightFrame is one render's walk over the highlighter's styles, from the first visible
// line on. Ask Styles for the visible lines in order.
type HighlightFrame struct {
	c   *EditorCore
	f   *hlFrame
	top int
	// Behind is set when the walk did not reach top within its budget: the styles shown are
	// provisional, and the widget asks for another frame (a render cannot mark itself dirty).
	Behind bool
}

// BeginHighlight starts a render's walk with top the first line on screen.
func (c *EditorCore) BeginHighlight(top int) *HighlightFrame {
	f, behind := c.hl.beginFrame(c.buf.lines, top, c.takeHighlightChanged)
	return &HighlightFrame{c: c, f: f, top: top, Behind: behind}
}

// Styles is each cluster's highlight style on line ln; nil with no highlighter.
func (h *HighlightFrame) Styles(ln int) []highlight.Style {
	return h.c.hl.highlighted(h.c.buf.lines, h.top, ln, h.f)
}

// Highlighting reports whether a highlighter is set.
func (c *EditorCore) Highlighting() bool { return c.hl.hl != nil }

// SyntaxStyle is the look a highlight style adds over the text, if it has one.
func (c *EditorCore) SyntaxStyle(k highlight.Style) (style.Style, bool) { return c.hl.syntaxStyle(k) }

// SetToggleRendered is how a widget with a Rendered view answers ActToggleRendered (Ctrl+T by
// default): fn switches the view and reports whether it took the key. nil, the default, leaves
// the key unconsumed, as the tui Editor, which has no Rendered view, leaves it.
func (c *EditorCore) SetToggleRendered(fn func() bool) { c.toggleRendered = fn }
