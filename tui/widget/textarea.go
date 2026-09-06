package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TextArea is the multi-line editor. The buffer is a
// []string of lines, grapheme-addressed within a line — deliberately simple,
// no rope/gap buffer in v1: the target is commit-message / query-editor
// scale (kilobytes), not code editors; very large single lines degrade. The
// line-slice is internal representation, swappable later without API
// change.
//
// Keys: TextInput's set plus Up/Down/PgUp/PgDn, Enter = newline, and
// Ctrl+Home/End (buffer start/end). There is deliberately NO submit key:
// a Box-wrapped TextArea form submits via an app-level
// keybinding. Tab is not consumed (focus traversal).
//
// Emits ChangeEvent, coalesced per input event (one per paste, not per
// rune). Implements the real-cursor IME rule via tui.CursorReporter.
type TextArea struct {
	Base
	textBuffer

	wrap WrapMode
	top  int // first visible logical line
	left int // horizontal scroll (cells; WrapNone only)
	w, h int // last layout size

	styles TextInputStyles
}

type taPos struct{ ln, col int }

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

// WithTextAreaStyles overrides the style hooks (same slots as TextInput;
// zero fields keep defaults).
func WithTextAreaStyles(st TextInputStyles) TextAreaOption {
	return func(t *TextArea) {
		t.styles = TextInputStyles{
			Text:        st.Text.Inherit(t.styles.Text),
			Placeholder: st.Placeholder.Inherit(t.styles.Placeholder),
			Selection:   st.Selection.Inherit(t.styles.Selection),
			Error:       st.Error.Inherit(t.styles.Error),
		}
	}
}

// NewTextArea builds an empty editor.
func NewTextArea(opts ...TextAreaOption) *TextArea {
	t := &TextArea{
		textBuffer: newTextBuffer(),
		wrap:       WrapNone,
		styles: TextInputStyles{
			Selection: style.New().Background(style.TokenSecondary).Foreground(style.TokenTextOnSecondary),
		},
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
	t.publish(ChangeEvent{Owner: t.NodeID(), Value: t.Value()})
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

func (t *TextArea) handleKey(e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	shift := e.Mods&tui.ModShift != 0
	ctrl := e.Mods&tui.ModCtrl != 0
	word := e.Mods&(tui.ModCtrl|tui.ModAlt) != 0

	switch e.Code {
	case tui.KeyEnter:
		t.insert("\n") // newline — no submit event (ADR-0007 Q5)
		return true
	case tui.KeyBackspace:
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRegion(lo, hi)
		case t.col > 0:
			t.deleteRegion(taPos{t.ln, t.col - 1}, taPos{t.ln, t.col})
		case t.ln > 0:
			t.deleteRegion(taPos{t.ln - 1, len(t.lineClusters(t.ln - 1))}, taPos{t.ln, 0})
		default:
			return true
		}
		t.edited()
		return true
	case tui.KeyDelete:
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRegion(lo, hi)
		case t.col < len(t.lineClusters(t.ln)):
			t.deleteRegion(taPos{t.ln, t.col}, taPos{t.ln, t.col + 1})
		case t.ln < len(t.lines)-1:
			t.deleteRegion(taPos{t.ln, t.col}, taPos{t.ln + 1, 0})
		default:
			return true
		}
		t.edited()
		return true
	case tui.KeyLeft:
		t.desired = -1
		switch {
		case word:
			ln, col := t.wordLeft()
			t.moveTo(ln, col, shift)
		case t.col > 0:
			t.moveTo(t.ln, t.col-1, shift)
		case t.ln > 0:
			t.moveTo(t.ln-1, len(t.lineClusters(t.ln-1)), shift)
		default:
			t.moveTo(0, 0, shift)
		}
		return true
	case tui.KeyRight:
		t.desired = -1
		switch {
		case word:
			ln, col := t.wordRight()
			t.moveTo(ln, col, shift)
		case t.col < len(t.lineClusters(t.ln)):
			t.moveTo(t.ln, t.col+1, shift)
		case t.ln < len(t.lines)-1:
			t.moveTo(t.ln+1, 0, shift)
		default:
			t.moveTo(t.ln, t.col, shift)
		}
		return true
	case tui.KeyUp:
		t.vertical(-1, shift)
		return true
	case tui.KeyDown:
		t.vertical(1, shift)
		return true
	case tui.KeyPageUp:
		t.vertical(-max(t.h, 1), shift)
		return true
	case tui.KeyPageDown:
		t.vertical(max(t.h, 1), shift)
		return true
	case tui.KeyHome:
		t.desired = -1
		if ctrl {
			t.moveTo(0, 0, shift)
		} else {
			t.moveTo(t.ln, 0, shift)
		}
		return true
	case tui.KeyEnd:
		t.desired = -1
		if ctrl {
			t.moveTo(len(t.lines)-1, len(t.lineClusters(len(t.lines)-1)), shift)
		} else {
			t.moveTo(t.ln, len(t.lineClusters(t.ln)), shift)
		}
		return true
	}

	if ctrl {
		switch e.Code {
		case 'a':
			t.desired = -1
			t.moveTo(t.ln, 0, false)
			return true
		case 'e':
			t.desired = -1
			t.moveTo(t.ln, len(t.lineClusters(t.ln)), false)
			return true
		case 'u':
			if t.col > 0 {
				t.deleteRegion(taPos{t.ln, 0}, taPos{t.ln, t.col})
				t.edited()
			}
			return true
		case 'w':
			if ln, col := t.wordLeft(); ln == t.ln && col < t.col {
				t.deleteRegion(taPos{ln, col}, taPos{t.ln, t.col})
				t.edited()
			}
			return true
		}
		return false
	}

	if e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
		t.insert(e.Text)
		return true
	}
	return false
}

// wrapWidth is the content width available for text (minus the scroll
// indicator column when one is painted).
func (t *TextArea) wrapWidth() int { return wrapUsableWidth(t.lines, t.view()) }

// scrollable reports whether content exceeds the viewport vertically.
func (t *TextArea) scrollable() bool { return wrapScrollable(t.lines, t.view()) }

// rowsOfLine is the wrapped visual height of line i at the current width.
func (t *TextArea) rowsOfLine(i int) int { return wrapRowsOfLine(t.lines, i, t.view()) }

// ensureVisible adjusts top/left so the cursor stays inside the viewport
// (using the last layout size).
func (t *TextArea) ensureVisible() {
	if t.h <= 0 || t.w <= 0 {
		return
	}
	if t.ln < t.top {
		t.top = t.ln
	}
	// Walk down until the cursor line's rows fit in the viewport.
	for t.top < t.ln {
		rows := 0
		for i := t.top; i <= t.ln && rows <= t.h; i++ {
			rows += t.rowsOfLine(i)
		}
		if rows <= t.h {
			break
		}
		t.top++
	}
	t.top = max(0, min(t.top, len(t.lines)-1))
	if t.wrap == WrapNone {
		cx := t.cellsAt(t.ln, t.col)
		w := t.wrapWidth()
		if cx < t.left {
			t.left = cx
		}
		if cx >= t.left+w {
			t.left = cx - w + 1
		}
		t.left = max(t.left, 0)
	} else {
		t.left = 0
	}
}

// Layout is greedy on both axes.
func (t *TextArea) Layout(c tui.Constraints) tui.Size {
	t.w = boundedMax(c.MaxW, max(c.MinW, 1))
	t.h = boundedMax(c.MaxH, max(c.MinH, 1))
	t.ensureVisible()
	return c.Constrain(tui.Size{W: t.w, H: t.h})
}

// Cursor implements tui.CursorReporter (viewport-local insertion point).
func (t *TextArea) Cursor() (int, int, bool) {
	if t.ln < t.top {
		return 0, 0, false
	}
	if t.wrap == WrapNone {
		x := t.cellsAt(t.ln, t.col) - t.left
		y := t.ln - t.top
		if y >= t.h && t.h > 0 {
			return 0, 0, false
		}
		return max(x, 0), max(y, 0), true
	}
	y := 0
	for i := t.top; i < t.ln; i++ {
		y += t.rowsOfLine(i)
	}
	row, x := t.wrapPos(t.ln, t.col)
	y += row
	if t.h > 0 && y >= t.h {
		return 0, 0, false
	}
	return x, y, true
}

// wrapPos locates cluster column col inside line ln's wrapped rows: the row
// index and the cell offset within that row.
func (t *TextArea) wrapPos(ln, col int) (row, x int) {
	return wrapPosOf(t.lines, ln, col, t.view())
}

// posKey orders buffer positions for selection painting.
func posLE(a taPos, bLn, bCol int) bool {
	return a.ln < bLn || (a.ln == bLn && a.col <= bCol)
}

// Render paints the viewport, the selection fill, and the scroll indicator.
func (t *TextArea) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	w := t.wrapWidth()
	selLo, selHi, hasSel := t.selection()
	paintCluster := func(x, y int, cl string, ln, col int) {
		st := t.styles.Text
		if hasSel && t.focused() &&
			posLE(selLo, ln, col) && !posLE(selHi, ln, col) {
			st = t.styles.Selection.Inherit(st)
		}
		s.SetCell(x, y, cl, st)
	}
	y := 0
	for ln := t.top; ln < len(t.lines) && y < sz.H; ln++ {
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
		paintScrollIndicator(s, sz.W-1, sz.H, t.top, len(t.lines))
	}
}

// view is the layout state the shared soft-wrap geometry needs. It is the
// only place this widget's viewport is handed to textBuffer.
func (t *TextArea) view() wrapView {
	return wrapView{w: t.w, h: t.h, wrap: t.wrap, measure: t.measure}
}
