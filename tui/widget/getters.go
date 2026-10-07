package widget

import "github.com/yongjohnlee80/golib/tui"

// Read-only state for a backend that draws these widgets natively. Each getter returns what the
// widget already holds; text positions are grapheme clusters and scroll offsets are cells.

// Hovered reports whether the pointer is over the button.
func (b *Button) Hovered() bool { return b.ctx != nil && b.ctx.Hovered() }

// HoverChanged opts the button into hover, so the App repaints it when the pointer comes or goes.
func (b *Button) HoverChanged(bool) {}

// CursorIndex is the cursor as a cluster index into the value.
func (t *TextInput) CursorIndex() int { return t.cur }

// Selection is the selected span as cluster indexes [start, end); ok is false with nothing selected.
func (t *TextInput) Selection() (start, end int, ok bool) { return t.selection() }

// Scroll is the number of cells scrolled off the left edge at the last render.
func (t *TextInput) Scroll() int { return t.scroll }

// Placeholder is the text shown while the value is empty.
func (t *TextInput) Placeholder() string { return t.placeholder }

// Masked reports whether the value is drawn as a mask character.
func (t *TextInput) Masked() bool { return t.mask != 0 }

// CursorPos is the cursor as a logical line and a cluster within it.
func (t *TextArea) CursorPos() (line, cluster int) { return t.ln, t.col }

// Selection is the selected span from its start to its end, each a line and a cluster; ok is
// false with nothing selected.
func (t *TextArea) Selection() (fromLine, fromCluster, toLine, toCluster int, ok bool) {
	lo, hi, ok := t.selection()
	return lo.ln, lo.col, hi.ln, hi.col, ok
}

// Scroll is the first visible logical line and, unwrapped only, the cells scrolled off the left.
func (t *TextArea) Scroll() (top, left int) { return t.top, t.left }

// Open reports whether the options popup is up.
func (s *Select[T]) Open() bool { return s.open }

// Label is what the field shows: the selected option's label, the placeholder, or the load error.
func (s *Select[T]) Label() string {
	switch {
	case s.loadErr != nil:
		return s.errText
	case s.selected >= 0 && s.selected < len(s.items):
		return s.items[s.selected].Label
	}
	return s.placeholder
}

// Progress is the fraction done in [0, 1]; determinate is false while it is indeterminate.
func (p *ProgressBar) Progress() (f float64, determinate bool) {
	return p.progress, !p.indeterminate
}

// Titles are the tab labels, in order.
func (t *Tabs) Titles() []string {
	out := make([]string, len(t.tabs))
	for i, e := range t.tabs {
		out[i] = e.label
	}
	return out
}

// Left is the left segment's text.
func (s *StatusBar) Left() string { return s.left.text }

// Center is the centre segment's text.
func (s *StatusBar) Center() string { return s.center.text }

// Right is the right segment's text.
func (s *StatusBar) Right() string { return s.right.text }

// Title is the text on the top border.
func (b *Box) Title() string { return b.title }

// Status is the text on the bottom border.
func (b *Box) Status() string { return b.status }

// FocusWithin reports whether focus is on the box or inside it.
func (b *Box) FocusWithin() bool { return b.focusWithin }

// Title is the dialog's title.
func (m *Modal) Title() string { return m.card.title }

// Items are the toasts showing, oldest first.
func (t *Toasts) Items() []Toast {
	out := make([]Toast, len(t.showing))
	for i, it := range t.showing {
		out[i] = it.Toast
	}
	return out
}

// ScrollState implements tui.Scroller, in items.
func (l *List[T]) ScrollState() (offset, viewport, content int) {
	return l.top, l.viewRows(), l.count
}

// ScrollState implements tui.Scroller, in rows; the header row is not counted.
func (t *Table[T]) ScrollState() (offset, viewport, content int) { return t.list.ScrollState() }

// ScrollState implements tui.Scroller, in visible rows.
func (t *Tree) ScrollState() (offset, viewport, content int) {
	return t.top, max(t.h, 0), len(t.flatten())
}

// ScrollState implements tui.Scroller, in wrapped rows.
func (v *BufferView) ScrollState() (offset, viewport, content int) {
	offset = v.scrollRow
	if v.follow {
		offset = v.maxScroll()
	}
	return offset, max(v.h, 0), v.totalRows
}

var (
	_ tui.HoverObserver = (*Button)(nil)
	_ tui.Scroller      = (*List[int])(nil)
	_ tui.Scroller      = (*Table[int])(nil)
	_ tui.Scroller      = (*Tree)(nil)
	_ tui.Scroller      = (*BufferView)(nil)
)
