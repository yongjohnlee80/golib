package widget

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// WrapMode selects how text-bearing widgets treat overflow. Text accepts
// Truncate (default) and Wrap; TextArea accepts WrapNone and WrapSoft.
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

// Text provides a lightweight, styled static text label supporting both single-line
// truncation (with ellipsis) and multi-line soft wrapping. It is purely presentational:
// not focusable, consumes no input events, and occupies minimal memory.
//
// # Layout Models: Truncate vs Wrap
//
// Text operates in one of two distinct layout modes configured via [WithWrapMode]:
//
//  1. Truncate Mode (Default):
//     - Flattened into a single line (newlines replaced with spaces).
//     - Height is strictly 1 row.
//     - Overflow beyond constraint MaxW is truncated with an ellipsis "…".
//
//     ┌──────────────────────────────────────────────┐
//     │ System Status: All cluster nodes operational…│ (w=46, h=1)
//     └──────────────────────────────────────────────┘
//
//  2. Wrap Mode:
//     - Hard newlines preserved as paragraph breaks.
//     - Each line soft-wrapped at viewport boundary w = MaxW.
//     - Height expands dynamically to accommodate wrapped lines.
//
//     ┌───────────────────────────┐
//     │ Antigravity runtime       │ (row 0)
//     │ initialized on node-042   │ (row 1)
//     │ with 16 worker threads.   │ (row 2)
//     └───────────────────────────┘
//
// # Architectural Invariants
//
//  1. Pure Presentation: Text implements only [tui.Component]. It never accepts focus,
//     has no cursor, and emits no bus events.
//  2. Layout Invalidation on Content Mutation: Calling [Text.SetText] issues both
//     [Base.RequestLayout] (to recount wrap geometry or width) and [Base.MarkDirty]
//     (to schedule repainting).
//  3. Ellipsis Grace: In Truncate mode, if available width is exactly 1 cell and overflow
//     occurs, Text renders only the single ellipsis glyph "…".
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. Mutation via [Text.SetText] must be performed
//     on the application event loop goroutine or via App.Update.
//   - Allocation Profile: Rendering splits and formats text lines against layout width constraints.
//
// # Usage Examples
//
// 1. Single-line truncated header label:
//
//	title := widget.NewText("Active Project: golib / tui / widget",
//		widget.WithTextStyle(style.New().Bold(true)),
//		widget.WithWrapMode(widget.Truncate),
//	)
//
// 2. Multi-line wrapped description block:
//
//	desc := widget.NewText("This panel displays high-volume logging output with backpressure.",
//		widget.WithTextStyle(style.New().Faint(true)),
//		widget.WithWrapMode(widget.Wrap),
//	)
type Text struct {
	Base
	text    string
	st      style.Style
	color   style.Color
	colored bool
	mode    WrapMode
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

// SetColor is Qt Text.color: it overrides the inherited palette foreground
// without replacing the rest of the widget style.
func (t *Text) SetColor(c style.Color) {
	if t.colored && t.color == c {
		return
	}
	t.color, t.colored = c, true
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

// Layout measures content within constraints (grapheme width):
// Truncate reports one line; Wrap reports the wrapped height.
func (t *Text) Layout(c tui.Constraints) tui.Size {
	if t.mode == Truncate {
		w := t.measure(t.lines()[0])
		if c.MaxW != tui.Unbounded {
			w = min(w, c.MaxW)
		}
		return c.Constrain(tui.Size{W: w, H: 1})
	}
	// The widest line, capped at the offered width. A wrapped Text that
	// reported MaxW whatever it held claimed columns it never draws in, so
	// anything sized to its content — a dialog's card — grew to the width of
	// the screen around three short lines.
	w := 0
	for _, ln := range t.lines() {
		w = max(w, t.measure(ln))
	}
	if c.MaxW != tui.Unbounded {
		w = min(w, c.MaxW)
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
	st := t.st
	if t.colored {
		st = st.Foreground(t.color)
	}
	if t.mode == Truncate {
		drawText(s, 0, 0, truncate(t.lines()[0], sz.W, s.StringWidth), st)
		return
	}
	y := 0
	for _, ln := range t.lines() {
		for _, row := range wrapLine(ln, sz.W, s.StringWidth) {
			if y >= sz.H {
				return
			}
			drawText(s, 0, y, row, st)
			y++
		}
	}
}
