package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

// TextArea provides an interactive multi-line text editor suitable for commit messages,
// SQL query composition, comments, and configuration notes.
//
// # Buffer & Viewport Model
//
// TextArea keeps a text buffer (buf) for line-and-cluster addressing, paired with a viewport window:
//
//	┌──────────────────────────────────────────────────────────┐
//	│ Viewport Window (w=58, h=6)                              │
//	│                                                          │
//	│ 0: fix(tui): handle ambiguous-width grapheme clusters    │
//	│ 1:                                                       │
//	│ 2: In terminal environments where emoji or CJK glyphs    │
//	│ 3: are rendered, character width varies across policies. │
//	│ 4: This commit synchronizes layout with paint cells.█    │ (cursor at ln=4, col=54)
//	│                                                          │
//	└──────────────────────────────────────────────────────────┘
//
// # Key Responsibilities
//
//  1. Multi-Line Editing: Grapheme-accurate insertion, deletion, newline splitting, and backspace.
//  2. Flexible Wrap Disciplines: Supports both [WrapNone] (horizontal scrolling) and [WrapSoft]
//     (visual wrapping within the viewport width).
//  3. Sticky Cursor Physics: Maintains a desired horizontal column during vertical navigation
//     across lines of unequal length.
//  4. Hardware Cursor Integration: Implements [tui.CursorReporter] to anchor terminal IME composition
//     windows directly at the insertion point.
//
// # Architectural Invariants
//
//  1. Deliberate Absence of Submit Key: Unlike [TextInput], Enter in TextArea always inserts a newline.
//     Form submission must be triggered by an app-level keybinding (e.g. Ctrl+S or Space leader).
//  2. Tab Traversal Integrity: Tab and Shift+Tab are intentionally unconsumed, ensuring focus can always
//     traverse forward and backward to sibling widgets without trapping the user.
//  3. Coalesced Change Notifications: Multi-character pastes or programmatic updates emit exactly one
//     [ChangeEvent] rather than flooding the bus per rune.
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. All editing methods, value setters, and focus changes must run
//     on the application loop goroutine.
//   - Non-Thread-Safe Buffer: Direct manipulation from background goroutines is forbidden; use App.Update.
//
// # Usage Examples
//
// 1. Commit message editor wrapped in a titled panel:
//
//	commitMsg := widget.NewTextArea(
//		widget.WithWrap(widget.WrapSoft),
//		widget.WithTextAreaPlaceholder("Enter commit title and summary..."),
//	)
//	panel := widget.NewBox(commitMsg,
//		widget.WithTitle("Commit Message"),
//		widget.WithStatus("Ctrl+S: commit | Esc: cancel"),
//	)
//
// 2. Query editor with pre-filled content:
//
//	queryInput := widget.NewTextArea(
//		widget.WithWrap(widget.WrapNone),
//	)
//	queryInput.SetValue("SELECT id, name, created_at\nFROM users\nWHERE active = true;")
type TextArea struct {
	Base
	buf txt.Buffer
	// changeValues counts the ChangeEvent values built (publishChange): tests read it
	changeValues int

	wrap WrapMode
	top  int // first visible logical line
	left int // horizontal scroll (cells; WrapNone only)
	w, h int // last layout size

	placeholder uiText // shown (muted) while the value is empty
	styles      TextInputStyles
}

var (
	_ tui.Focusable      = (*TextArea)(nil)
	_ tui.CursorReporter = (*TextArea)(nil)
)

// TextAreaOption customizes a TextArea under construction.
type TextAreaOption func(*TextArea)

// WithWrap selects WrapNone (default; horizontal scroll) or WrapSoft
// (visual wrap at the viewport width). Any other mode panics.
func WithWrap(m WrapMode) TextAreaOption {
	if m != WrapNone && m != WrapSoft {
		panic(fmt.Sprintf("widget: WithWrap: mode %d is not WrapNone or WrapSoft", m))
	}
	return func(t *TextArea) { t.wrap = m }
}

// WithTextAreaPlaceholder sets the text drawn (muted) while the value is empty.
func WithTextAreaPlaceholder(s string) TextAreaOption {
	return func(t *TextArea) { t.placeholder = plainText(s) }
}

// WithTextAreaPlaceholderMessage sets the placeholder to a catalog message, shown in the App's
// language.
func WithTextAreaPlaceholderMessage(m tui.Message) TextAreaOption {
	return func(t *TextArea) { t.placeholder = messageText(m) }
}

// WithTextAreaStyles overrides the style hooks (same slots as TextInput;
// zero fields keep defaults).
func WithTextAreaStyles(st TextInputStyles) TextAreaOption {
	return func(t *TextArea) {
		t.styles = st.Over(t.styles)
	}
}

// NewTextArea builds an empty editor.
func NewTextArea(opts ...TextAreaOption) *TextArea {
	t := &TextArea{
		buf:    txt.NewBuffer(),
		wrap:   WrapNone,
		styles: DefaultTextInputStyles(),
	}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	return t
}

// Value returns the buffer joined with newlines.
func (t *TextArea) Value() string { return t.buf.Value() }

// SetValue replaces the buffer (cursor to the end, selection cleared).
// No ChangeEvent — the event reports user edits.
func (t *TextArea) SetValue(s string) {
	t.buf.SetValue(s)
	t.ensureVisible()
	t.MarkDirty()
}

// AcceptsFocus implements tui.Focusable.
func (t *TextArea) AcceptsFocus() bool { return true }

// Caret is the caret's logical line and its column in grapheme clusters, for a view that draws the
// field itself (a table's open cell, in cells or in pixels).
func (t *TextArea) Caret() (line, col int) { return t.buf.Ln, t.buf.Col }

// cellsAt is the display offset (cells) of (ln, col).
func (t *TextArea) cellsAt(ln, col int) int {
	return t.buf.CellsAt(ln, col, t.Measure)
}

// InsertText places text at the cursor (replacing any selection) as one atomic
// edit; newlines split lines. Emits one ChangeEvent. A view of your own that edits through a
// TextArea (a table's open cell) types with it.
func (t *TextArea) InsertText(text string) {
	t.buf.InsertText(text)
	t.edited()
}

func (t *TextArea) edited() {
	t.buf.Anchor = nil
	t.buf.Desired = -1
	t.ensureVisible()
	t.MarkDirty()
	if PublishChange(t.ctx, t.NodeID(), t.Value) {
		t.changeValues++
	}
}

// moveTo moves the cursor, managing the selection anchor.
func (t *TextArea) moveTo(ln, col int, extend bool) {
	t.buf.MoveCursor(ln, col, extend)
	t.ensureVisible()
	t.MarkDirty()
}

// vertical moves the cursor by delta lines keeping the sticky column.
func (t *TextArea) vertical(delta int, extend bool) {
	ln, col := t.buf.VerticalTarget(delta, t.Measure)
	d := t.buf.Desired
	t.moveTo(ln, col, extend)
	t.buf.Desired = d // moveTo clears nothing here, but keep it explicit
}

// HandleEvent implements the key contract.
func (t *TextArea) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.PasteEvent:
		t.InsertText(e.Text)
		return true
	case tui.KeyEvent:
		return t.handleKey(e)
	}
	return false
}

// keyCommand is the TextArea command e is, if any.
func (t *TextArea) keyCommand(e tui.KeyEvent) (keymap.Command, bool) {
	return t.Context().KeyCommand(e, textAreaCommands...)
}

// KeyScope is a TextArea's key-map scope.
func (t *TextArea) KeyScope() keymap.Scope { return TextAreaScope }

// KeyBindings is the TextArea's commands as m binds them (tui.KeyBinder). Typed text is not a command.
func (t *TextArea) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, textAreaCommands...)
}

func (t *TextArea) handleKey(e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	cmd, ok := t.keyCommand(e)
	if !ok {
		if e.Text != "" && e.Mods&kit.NonTextMods == 0 && e.Code != tui.KeyTab {
			t.InsertText(e.Text)
			return true
		}
		return false
	}
	id := cmd.ID
	switch id {
	case TextAreaSelectLeft.ID, TextAreaSelectWordLeft.ID, TextAreaSelectRight.ID, TextAreaSelectWordRight.ID,
		TextAreaSelectUp.ID, TextAreaSelectDown.ID, TextAreaSelectPageUp.ID, TextAreaSelectPageDown.ID,
		TextAreaSelectHome.ID, TextAreaSelectStart.ID, TextAreaSelectEnd.ID, TextAreaSelectFinish.ID:
		t.handleMove(id, true)
		return true
	case TextAreaLeft.ID, TextAreaWordLeft.ID, TextAreaRight.ID, TextAreaWordRight.ID, TextAreaUp.ID,
		TextAreaDown.ID, TextAreaPageUp.ID, TextAreaPageDown.ID, TextAreaHome.ID, TextAreaStart.ID,
		TextAreaEnd.ID, TextAreaFinish.ID:
		t.handleMove(id, false)
		return true
	}
	switch id {
	case TextAreaNewline.ID:
		t.InsertText("\n") // newline — no submit event (Q5)
	case TextAreaBackspace.ID, TextAreaDelete.ID:
		lo, hi, ok := t.buf.Selection()
		switch {
		case ok:
		case id == TextAreaBackspace.ID:
			lo, hi, ok = t.buf.BackspaceRegion()
		default:
			lo, hi, ok = t.buf.ForwardRegion()
		}
		if ok {
			t.buf.DeleteRegion(lo, hi)
			t.edited()
		}
	case TextAreaLineStart.ID:
		t.buf.Desired = -1
		t.moveTo(t.buf.Ln, 0, false)
	case TextAreaLineEnd.ID:
		t.buf.Desired = -1
		t.moveTo(t.buf.Ln, len(t.buf.LineClusters(t.buf.Ln)), false)
	case TextAreaKillLine.ID:
		if t.buf.Col > 0 {
			t.buf.DeleteRegion(txt.Pos{Ln: t.buf.Ln, Col: 0}, txt.Pos{Ln: t.buf.Ln, Col: t.buf.Col})
			t.edited()
		}
	case TextAreaKillWord.ID:
		if ln, col := t.buf.WordLeft(); ln == t.buf.Ln && col < t.buf.Col {
			t.buf.DeleteRegion(txt.Pos{Ln: ln, Col: col}, txt.Pos{Ln: t.buf.Ln, Col: t.buf.Col})
			t.edited()
		}
	}
	return true
}

// handleMove moves the cursor by a movement command, extending the selection with shift.
func (t *TextArea) handleMove(id string, shift bool) {
	switch id {
	case TextAreaLeft.ID, TextAreaSelectLeft.ID, TextAreaWordLeft.ID, TextAreaSelectWordLeft.ID:
		t.buf.Desired = -1
		switch {
		case id == TextAreaWordLeft.ID || id == TextAreaSelectWordLeft.ID:
			ln, col := t.buf.WordLeft()
			t.moveTo(ln, col, shift)
		case t.buf.Col > 0:
			t.moveTo(t.buf.Ln, t.buf.Col-1, shift)
		case t.buf.Ln > 0:
			t.moveTo(t.buf.Ln-1, len(t.buf.LineClusters(t.buf.Ln-1)), shift)
		default:
			t.moveTo(0, 0, shift)
		}
	case TextAreaRight.ID, TextAreaSelectRight.ID, TextAreaWordRight.ID, TextAreaSelectWordRight.ID:
		t.buf.Desired = -1
		switch {
		case id == TextAreaWordRight.ID || id == TextAreaSelectWordRight.ID:
			ln, col := t.buf.WordRight()
			t.moveTo(ln, col, shift)
		case t.buf.Col < len(t.buf.LineClusters(t.buf.Ln)):
			t.moveTo(t.buf.Ln, t.buf.Col+1, shift)
		case t.buf.Ln < len(t.buf.Lines)-1:
			t.moveTo(t.buf.Ln+1, 0, shift)
		default:
			t.moveTo(t.buf.Ln, t.buf.Col, shift)
		}
	case TextAreaUp.ID, TextAreaSelectUp.ID:
		t.vertical(-1, shift)
	case TextAreaDown.ID, TextAreaSelectDown.ID:
		t.vertical(1, shift)
	case TextAreaPageUp.ID, TextAreaSelectPageUp.ID:
		t.vertical(-max(t.h, 1), shift)
	case TextAreaPageDown.ID, TextAreaSelectPageDown.ID:
		t.vertical(max(t.h, 1), shift)
	case TextAreaHome.ID, TextAreaSelectHome.ID:
		t.buf.Desired = -1
		t.moveTo(t.buf.Ln, 0, shift)
	case TextAreaStart.ID, TextAreaSelectStart.ID:
		t.buf.Desired = -1
		t.moveTo(0, 0, shift)
	case TextAreaEnd.ID, TextAreaSelectEnd.ID:
		t.buf.Desired = -1
		t.moveTo(t.buf.Ln, len(t.buf.LineClusters(t.buf.Ln)), shift)
	case TextAreaFinish.ID, TextAreaSelectFinish.ID:
		t.buf.Desired = -1
		t.moveTo(len(t.buf.Lines)-1, len(t.buf.LineClusters(len(t.buf.Lines)-1)), shift)
	}
}

// wrapWidth is the content width available for text (minus the scroll
// indicator column when one is painted).
func (t *TextArea) wrapWidth() int { return txt.WrapUsableWidth(t.buf.Lines, t.view()) }

// PointerShapes is the I-beam over the text: the scroll indicator keeps the arrow.
func (t *TextArea) PointerShapes(dst []tui.PointerRegion) []tui.PointerRegion {
	return append(dst, tui.PointerRegion{Rect: tui.Rect{W: t.wrapWidth(), H: t.h}, Shape: tui.PointerText})
}

// scrollable reports whether content exceeds the viewport vertically.
func (t *TextArea) scrollable() bool { return txt.WrapScrollable(t.buf.Lines, t.view()) }

// ensureVisible adjusts top/left so the cursor stays inside the viewport
// (using the last layout size).
func (t *TextArea) ensureVisible() {
	t.top, t.left = txt.RevealCaret(t.buf.Lines, t.view(), t.top, t.left, t.buf.Ln, t.cellsAt(t.buf.Ln, t.buf.Col))
}

// Layout is greedy on both axes.
func (t *TextArea) Layout(c tui.Constraints) tui.Size {
	t.w = kit.FillW(c)
	t.h = kit.FillH(c)
	t.ensureVisible()
	return c.Constrain(tui.Size{W: t.w, H: t.h})
}

// Cursor implements tui.CursorReporter (viewport-local insertion point).
func (t *TextArea) Cursor() (int, int, bool) {
	return txt.CaretAt(t.buf.Lines, t.view(), t.top, t.left, t.buf.Ln, t.buf.Col, t.cellsAt(t.buf.Ln, t.buf.Col))
}

// Render paints the viewport, the selection fill, and the scroll indicator. Every cell wears the
// area's own text style first, so the cells past a line's end and below the last line are the
// area's, never its parent's.
func (t *TextArea) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", t.styles.Text)
	if ph, ok := t.shownPlaceholder(s.StringWidth); ok {
		kit.DrawText(s, 0, 0, ph, t.styles.Placeholder)
		return
	}
	selLo, selHi, hasSel := t.buf.Selection()
	t.eachDrawn(sz.H, s.StringWidth, func(x, y int, cl string, ln, col int) {
		st := t.styles.Text
		if hasSel && t.Focused() && (txt.Pos{Ln: ln, Col: col}).In(selLo, selHi) {
			st = t.styles.Selection.Inherit(st)
		}
		s.SetCell(x, y, cl, st)
	})
	if t.scrollable() {
		kit.PaintScrollIndicator(s, sz.W-1, sz.H, t.top, txt.WrapMaxTop(t.buf.Lines, t.view()))
	}
}

// shownPlaceholder is the placeholder as Render draws it, at (0, 0): ok only while the area is
// empty and has one.
func (t *TextArea) shownPlaceholder(measure func(string) int) (string, bool) {
	if len(t.buf.Lines) != 1 || t.buf.Lines[0] != "" {
		return "", false
	}
	ph := kit.Truncate(t.placeholder.inLanguage(&t.Base), t.wrapWidth(), measure)
	return ph, ph != ""
}

// eachDrawn walks the clusters Render draws in a viewport rows high: each one's cell and its line
// and cluster index. Render and SelectionCells share it, so a host's selection cells are the
// drawn ones.
func (t *TextArea) eachDrawn(rows int, measure func(string) int, fn func(x, y int, cl string, ln, col int)) {
	w := t.wrapWidth()
	y := 0
	for ln := t.top; ln < len(t.buf.Lines) && y < rows; ln++ {
		cs := t.buf.LineClusters(ln)
		if t.wrap == WrapNone {
			x := -t.left
			for col, cl := range cs {
				cw := measure(cl)
				if x+cw > w {
					break
				}
				if x >= 0 {
					fn(x, y, cl, ln, col)
				}
				x += cw
			}
			y++
			continue
		}
		// WrapSoft: each wrapped row by cluster range.
		for _, r := range txt.WrapRanges(cs, w, measure) {
			if y >= rows {
				break
			}
			x := 0
			for col := r[0]; col < r[1]; col++ {
				fn(x, y, cs[col], ln, col)
				x += measure(cs[col])
			}
			y++
		}
	}
}

// SelectionCells implements tui.SelectionCells: one rect per row the selection is drawn on, over
// the selected clusters' cells; none while the area is unfocused, as Render draws none.
func (t *TextArea) SelectionCells() ([]tui.Rect, bool) {
	lo, hi, ok := t.buf.Selection()
	if !ok || !t.Focused() || t.w <= 0 || t.h <= 0 {
		return nil, false
	}
	var out []tui.Rect
	t.eachDrawn(t.h, t.Measure, func(x, y int, cl string, ln, col int) {
		if !(txt.Pos{Ln: ln, Col: col}).In(lo, hi) {
			return
		}
		cw := t.Measure(cl)
		if n := len(out); n > 0 && out[n-1].Y == y && out[n-1].X+out[n-1].W == x {
			out[n-1].W += cw
			return
		}
		out = append(out, tui.Rect{X: x, Y: y, W: cw, H: 1})
	})
	return out, len(out) > 0
}

// PlaceholderCells is the cells the placeholder is drawn over, in the area's own frame, as
// SelectionCells reports the selection's, so a host that paints the area natively (gui) can tell
// them from the area's own: one rect on row 0, ok only while the placeholder shows.
func (t *TextArea) PlaceholderCells() ([]tui.Rect, bool) {
	if t.w <= 0 || t.h <= 0 {
		return nil, false
	}
	ph, ok := t.shownPlaceholder(t.Measure)
	if !ok {
		return nil, false
	}
	return []tui.Rect{{W: t.Measure(ph), H: 1}}, true
}

// view is the layout state the shared soft-wrap geometry needs. It is the
// only place this widget's viewport is handed to txt.Buffer.
func (t *TextArea) view() txt.WrapView {
	return txt.WrapView{W: t.w, H: t.h, Wrap: t.wrap != WrapNone, Measure: t.Measure}
}
