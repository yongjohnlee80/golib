package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// TextArea provides an interactive multi-line text editor suitable for commit messages,
// SQL query composition, comments, and configuration notes.
//
// # Buffer & Viewport Model
//
// TextArea embeds [textBuffer] for line-and-cluster addressing, paired with a viewport window:
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
	textBuffer
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
		t.styles = st.over(t.styles)
	}
}

// NewTextArea builds an empty editor.
func NewTextArea(opts ...TextAreaOption) *TextArea {
	t := &TextArea{
		textBuffer: newTextBuffer(),
		wrap:       WrapNone,
		styles:     defaultTextInputStyles(),
	}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	return t
}

// Value returns the buffer joined with newlines.
func (t *TextArea) Value() string { return t.value() }

// SetValue replaces the buffer (cursor to the end, selection cleared).
// No ChangeEvent — the event reports user edits.
func (t *TextArea) SetValue(s string) {
	t.setValue(s)
	t.ensureVisible()
	t.MarkDirty()
}

// AcceptsFocus implements tui.Focusable.
func (t *TextArea) AcceptsFocus() bool { return true }

// Caret is the caret's logical line and its column in grapheme clusters, for a view that draws the
// field itself (a table's open cell, in cells or in pixels).
func (t *TextArea) Caret() (line, col int) { return t.ln, t.col }

// cellsAt is the display offset (cells) of (ln, col).
func (t *TextArea) cellsAt(ln, col int) int {
	return t.textBuffer.cellsAt(ln, col, t.measure)
}

// colForCells returns the cluster column in line ln closest to the cell
// offset cells (for sticky vertical movement).
func (t *TextArea) colForCells(ln, cells int) int {
	return t.textBuffer.colForCells(ln, cells, t.measure)
}

// insert places text at the cursor (replacing any selection) as one atomic
// edit; newlines split lines. Emits one ChangeEvent.
func (t *TextArea) insert(text string) {
	t.insertText(text)
	t.edited()
}

func (t *TextArea) edited() {
	t.anchor = nil
	t.desired = -1
	t.ensureVisible()
	t.MarkDirty()
	if publishChange(t.ctx, t.NodeID(), t.Value) {
		t.changeValues++
	}
}

// moveTo moves the cursor, managing the selection anchor.
func (t *TextArea) moveTo(ln, col int, extend bool) {
	t.moveCursor(ln, col, extend)
	t.ensureVisible()
	t.MarkDirty()
}

// vertical moves the cursor by delta lines keeping the sticky column.
func (t *TextArea) vertical(delta int, extend bool) {
	ln, col := t.verticalTarget(delta, t.measure)
	d := t.desired
	t.moveTo(ln, col, extend)
	t.desired = d // moveTo clears nothing here, but keep it explicit
}

// HandleEvent implements the key contract.
func (t *TextArea) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.PasteEvent:
		t.insert(e.Text)
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
		if e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
			t.insert(e.Text)
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
		t.insert("\n") // newline — no submit event (Q5)
	case TextAreaBackspace.ID, TextAreaDelete.ID:
		lo, hi, ok := t.selection()
		switch {
		case ok:
		case id == TextAreaBackspace.ID:
			lo, hi, ok = t.backspaceRegion()
		default:
			lo, hi, ok = t.forwardRegion()
		}
		if ok {
			t.deleteRegion(lo, hi)
			t.edited()
		}
	case TextAreaLineStart.ID:
		t.desired = -1
		t.moveTo(t.ln, 0, false)
	case TextAreaLineEnd.ID:
		t.desired = -1
		t.moveTo(t.ln, len(t.lineClusters(t.ln)), false)
	case TextAreaKillLine.ID:
		if t.col > 0 {
			t.deleteRegion(taPos{t.ln, 0}, taPos{t.ln, t.col})
			t.edited()
		}
	case TextAreaKillWord.ID:
		if ln, col := t.wordLeft(); ln == t.ln && col < t.col {
			t.deleteRegion(taPos{ln, col}, taPos{t.ln, t.col})
			t.edited()
		}
	}
	return true
}

// handleMove moves the cursor by a movement command, extending the selection with shift.
func (t *TextArea) handleMove(id string, shift bool) {
	switch id {
	case TextAreaLeft.ID, TextAreaSelectLeft.ID, TextAreaWordLeft.ID, TextAreaSelectWordLeft.ID:
		t.desired = -1
		switch {
		case id == TextAreaWordLeft.ID || id == TextAreaSelectWordLeft.ID:
			ln, col := t.wordLeft()
			t.moveTo(ln, col, shift)
		case t.col > 0:
			t.moveTo(t.ln, t.col-1, shift)
		case t.ln > 0:
			t.moveTo(t.ln-1, len(t.lineClusters(t.ln-1)), shift)
		default:
			t.moveTo(0, 0, shift)
		}
	case TextAreaRight.ID, TextAreaSelectRight.ID, TextAreaWordRight.ID, TextAreaSelectWordRight.ID:
		t.desired = -1
		switch {
		case id == TextAreaWordRight.ID || id == TextAreaSelectWordRight.ID:
			ln, col := t.wordRight()
			t.moveTo(ln, col, shift)
		case t.col < len(t.lineClusters(t.ln)):
			t.moveTo(t.ln, t.col+1, shift)
		case t.ln < len(t.textLines)-1:
			t.moveTo(t.ln+1, 0, shift)
		default:
			t.moveTo(t.ln, t.col, shift)
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
		t.desired = -1
		t.moveTo(t.ln, 0, shift)
	case TextAreaStart.ID, TextAreaSelectStart.ID:
		t.desired = -1
		t.moveTo(0, 0, shift)
	case TextAreaEnd.ID, TextAreaSelectEnd.ID:
		t.desired = -1
		t.moveTo(t.ln, len(t.lineClusters(t.ln)), shift)
	case TextAreaFinish.ID, TextAreaSelectFinish.ID:
		t.desired = -1
		t.moveTo(len(t.textLines)-1, len(t.lineClusters(len(t.textLines)-1)), shift)
	}
}

// wrapWidth is the content width available for text (minus the scroll
// indicator column when one is painted).
func (t *TextArea) wrapWidth() int { return wrapUsableWidth(t.textLines, t.view()) }

// PointerShapes is the I-beam over the text: the scroll indicator keeps the arrow.
func (t *TextArea) PointerShapes(dst []tui.PointerRegion) []tui.PointerRegion {
	return append(dst, tui.PointerRegion{Rect: tui.Rect{W: t.wrapWidth(), H: t.h}, Shape: tui.PointerText})
}

// scrollable reports whether content exceeds the viewport vertically.
func (t *TextArea) scrollable() bool { return wrapScrollable(t.textLines, t.view()) }

// rowsOfLine is the wrapped visual height of line i at the current width.
func (t *TextArea) rowsOfLine(i int) int { return wrapRowsOfLine(t.textLines, i, t.view()) }

// ensureVisible adjusts top/left so the cursor stays inside the viewport
// (using the last layout size).
func (t *TextArea) ensureVisible() {
	t.top, t.left = revealCaret(t.textLines, t.view(), t.top, t.left, t.ln, t.cellsAt(t.ln, t.col))
}

// Layout is greedy on both axes.
func (t *TextArea) Layout(c tui.Constraints) tui.Size {
	t.w = fillW(c)
	t.h = fillH(c)
	t.ensureVisible()
	return c.Constrain(tui.Size{W: t.w, H: t.h})
}

// Cursor implements tui.CursorReporter (viewport-local insertion point).
func (t *TextArea) Cursor() (int, int, bool) {
	return caretAt(t.textLines, t.view(), t.top, t.left, t.ln, t.col, t.cellsAt(t.ln, t.col))
}

// wrapPos locates cluster column col inside line ln's wrapped rows: the row
// index and the cell offset within that row.
func (t *TextArea) wrapPos(ln, col int) (row, x int) {
	return wrapPosOf(t.textLines, ln, col, t.view())
}

// Render paints the viewport, the selection fill, and the scroll indicator.
func (t *TextArea) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	w := t.wrapWidth()
	if len(t.textLines) == 1 && t.textLines[0] == "" {
		if ph := t.placeholder.inLanguage(&t.Base); ph != "" {
			drawText(s, 0, 0, truncate(ph, w, s.StringWidth), t.styles.Placeholder)
		}
		return
	}
	selLo, selHi, hasSel := t.selection()
	paintCluster := func(x, y int, cl string, ln, col int) {
		st := t.styles.Text
		if hasSel && t.focused() &&
			(taPos{ln, col}).in(selLo, selHi) {
			st = t.styles.Selection.Inherit(st)
		}
		s.SetCell(x, y, cl, st)
	}
	y := 0
	for ln := t.top; ln < len(t.textLines) && y < sz.H; ln++ {
		cs := t.lineClusters(ln)
		if t.wrap == WrapNone {
			x := -t.left
			for col, cl := range cs {
				cw := s.StringWidth(cl)
				if x+cw > w {
					break
				}
				if x >= 0 {
					paintCluster(x, y, cl, ln, col)
				}
				x += cw
			}
			y++
			continue
		}
		// WrapSoft: paint each wrapped row by cluster range.
		for _, r := range wrapRanges(cs, w, s.StringWidth) {
			if y >= sz.H {
				break
			}
			x := 0
			for col := r[0]; col < r[1]; col++ {
				paintCluster(x, y, cs[col], ln, col)
				x += s.StringWidth(cs[col])
			}
			y++
		}
	}
	if t.scrollable() {
		paintScrollIndicator(s, sz.W-1, sz.H, t.top, wrapMaxTop(t.textLines, t.view()))
	}
}

// view is the layout state the shared soft-wrap geometry needs. It is the
// only place this widget's viewport is handed to textBuffer.
func (t *TextArea) view() wrapView {
	return wrapView{w: t.w, h: t.h, wrap: t.wrap, measure: t.measure}
}
