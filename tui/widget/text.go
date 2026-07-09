package widget

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// WrapMode selects how text-bearing widgets treat overflow. Text accepts
// Truncate (default) and Wrap; TextArea accepts WrapNone and WrapSoft
// (ADR-0007 §2.4/§2.5).
type WrapMode uint8

const (
	// Truncate reports one line and paints an ellipsis at overflow (Text).
	Truncate WrapMode = iota
	// Wrap reports the wrapped height (Text).
	Wrap
	// WrapNone scrolls horizontally instead of wrapping (TextArea).
	WrapNone
	// WrapSoft wraps visually at the viewport width (TextArea).
	WrapSoft
)

// Text is a styled static label (ADR-0007 §2.5): plain text, truncated with
// ellipsis or soft-wrapped. Not focusable; consumes nothing.
type Text struct {
	Base
	text string
	st   style.Style
	mode WrapMode
}

var _ tui.Component = (*Text)(nil)

// TextOption customizes a Text under construction.
type TextOption func(*Text)

// WithTextStyle sets the text style.
func WithTextStyle(st style.Style) TextOption { return func(t *Text) { t.st = st } }

// WithWrapMode sets Truncate (default, with ellipsis) or Wrap. Any other
// mode panics.
func WithWrapMode(m WrapMode) TextOption {
	if m != Truncate && m != Wrap {
		panic(fmt.Sprintf("widget: WithWrapMode: mode %d is not Truncate or Wrap", m))
	}
	return func(t *Text) { t.mode = m }
}

// NewText builds a label.
func NewText(s string, opts ...TextOption) *Text {
	t := &Text{text: s, mode: Truncate}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	return t
}

// SetText replaces the content (loop goroutine, like all widget state).
func (t *Text) SetText(s string) {
	if t.text == s {
		return
	}
	t.text = s
	t.RequestLayout()
	t.MarkDirty()
}

// lines splits on hard newlines; Truncate mode flattens them to spaces
// (a Text in Truncate mode is one line by contract).
func (t *Text) lines() []string {
	if t.mode == Truncate {
		return []string{strings.ReplaceAll(t.text, "\n", " ")}
	}
	return strings.Split(t.text, "\n")
}

// Layout measures content within constraints (grapheme width, ADR-0003):
// Truncate reports one line; Wrap reports the wrapped height.
func (t *Text) Layout(c tui.Constraints) tui.Size {
	if t.mode == Truncate {
		w := t.measure(t.lines()[0])
		if c.MaxW != tui.Unbounded {
			w = min(w, c.MaxW)
		}
		return c.Constrain(tui.Size{W: w, H: 1})
	}
	w := c.MaxW
	if w == tui.Unbounded {
		w = 0
		for _, ln := range t.lines() {
			w = max(w, t.measure(ln))
		}
	}
	h := 0
	for _, ln := range t.lines() {
		h += len(wrapLine(ln, w, t.measure))
	}
	return c.Constrain(tui.Size{W: w, H: max(h, 1)})
}

// Render paints the (truncated or wrapped) text.
func (t *Text) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	if t.mode == Truncate {
		drawText(s, 0, 0, truncate(t.lines()[0], sz.W, s.StringWidth), t.st)
		return
	}
	y := 0
	for _, ln := range t.lines() {
		for _, row := range wrapLine(ln, sz.W, s.StringWidth) {
			if y >= sz.H {
				return
			}
			drawText(s, 0, y, row, t.st)
			y++
		}
	}
}
