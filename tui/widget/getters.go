package widget

import "github.com/yongjohnlee80/golib/tui"

// Read-only state for a backend that draws these widgets natively. Each getter returns what the
// widget already holds; text positions are grapheme clusters and scroll offsets are cells.

// Hovered reports whether the pointer is over the button.
func (b *Button) Hovered() bool { return b.ctx != nil && b.ctx.Hovered() }

// MnemonicIndex is the cluster of Label that Render underlines as the button's key, and whether
// there is one: the marked cluster of a message label, else the first cluster starting with the
// mnemonic, compared case-insensitively.
func (b *Button) MnemonicIndex() (int, bool) {
	if b.markSet {
		return b.markAt, true
	}
	if b.mnemonic == 0 {
		return 0, false
	}
	i := 0
	for cluster := range tui.Graphemes(b.label) {
		if eqFold([]rune(cluster)[0], b.mnemonic) {
			return i, true
		}
		i++
	}
	return 0, false
}

// HoverChanged opts the button into hover, so the App repaints it when the pointer comes or goes.
func (b *Button) HoverChanged(bool) {}

// CursorIndex is the cursor as a cluster index into the value.
func (t *TextInput) CursorIndex() int { return t.cursor() }

// Selection is the selected span as cluster indexes [start, end); ok is false with nothing selected.
func (t *TextInput) Selection() (start, end int, ok bool) { return t.selection() }

// Scroll is the number of cells scrolled off the left edge at the last render.
func (t *TextInput) Scroll() int { return t.scroll }

// Padding is the cells left empty inside each end of the field: 1 on a backend that frames the
// field natively, else 0. Text columns start after it.
func (t *TextInput) Padding() int { return t.pad }

// Placeholder is the text shown while the value is empty.
func (t *TextInput) Placeholder() string { return t.placeholder.inLanguage(&t.Base) }

// Masked reports whether the value is drawn as a mask character.
func (t *TextInput) Masked() bool { return t.mask != 0 }

// Mask is the character every cluster is drawn as, or 0 when the value is shown.
func (t *TextInput) Mask() rune { return t.mask }

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

// Affordance reports whether the field draws its ▾/▴ triangle (WithAffordance).
func (s *Select[T]) Affordance() bool { return s.affordance }

// FieldWidth is the closed field's width in cells, as last laid out: its slot may be wider, and
// the field, its triangle in its last cell, keeps to these.
func (s *Select[T]) FieldWidth() int { return s.fieldW }

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

// Spinner reports whether the bar is a one-cell spinner (WithSpinner): it draws a frame while
// indeterminate and one blank cell otherwise, never a track, so a native painter leaves it to its
// cells.
func (p *ProgressBar) Spinner() bool { return p.frames != nil }

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
func (b *Box) Title() string { return b.title.inLanguage(&b.Base) }

// Status is the text on the bottom border.
func (b *Box) Status() string { return b.status }

// FocusWithin reports whether focus is on the box or inside it.
func (b *Box) FocusWithin() bool { return b.focusWithin }

// Title is the dialog's title.
func (m *Modal) Title() string { return m.card.title.inLanguage(&m.card.Base) }

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

// TabSpan is one tab's label on the bar: its first cell and its width, in cells.
type TabSpan struct{ X, W int }

// TabSpans are the tabs' labels on the bar, in order, where a press selects them.
func (t *Tabs) TabSpans() []TabSpan { return t.strip.spans() }

// ActiveTabIndex is the active tab's place among TabSpans, -1 when it is not shown: with the bar
// overflowing, the spans hold the tabs in view and the < and > around them, so it is not Active.
func (t *Tabs) ActiveTabIndex() int { return t.strip.activeIndex() }

// TabSpanner is a tab strip as a native painter reads it: the stretches its row draws (TabSpans)
// and the active tab's place among them (-1 when it is not shown). Tabs, TabStrip and TerminalTabs
// all draw through one strip, so each answers the same way.
type TabSpanner interface {
	TabSpans() []TabSpan
	ActiveTabIndex() int
}

// TabControl is a window control at a tab strip's right end (SetWindowButtons): its cells, and what
// a press on it does.
type TabControl struct {
	X, W int
	Op   WindowOperation
}

// TabWindowController is a tab strip whose row may end in window controls, as a native painter reads
// them: none unless its owner shows them (Tabs.SetWindowButtons, TerminalTabs.SetWindowButtons).
type TabWindowController interface {
	TabWindowControls() []TabControl
}

var (
	_ TabWindowController = (*Tabs)(nil)
	_ TabWindowController = (*TerminalTabs)(nil)
)

// SetWindowButtons shows _, + and x at the bar's right end, side by side as the title bar's, or takes
// them off (the default, none): a panel's own minimize, maximize and close. A press runs SetOnWindowButton's function with the control's
// WindowOperation.
func (t *Tabs) SetWindowButtons(on bool) { t.strip.setWindowButtons(on) }

// SetOnWindowButton is what a press on a window control does (SetWindowButtons): fn with its
// operation, WindowMinimize, WindowMaximize or WindowClose. nil does nothing.
func (t *Tabs) SetOnWindowButton(fn func(WindowOperation)) { t.strip.onWindow = fn }

// TabWindowControls are the window controls on the bar, as drawn now.
func (t *Tabs) TabWindowControls() []TabControl { return t.strip.controlSpans() }

// SetWindowButtons shows _, + and x at the strip's right end, side by side as the title bar's, or
// takes them off (the default, none): a panel's own minimize, maximize and close. A press runs SetOnWindowButton's function with the control's
// WindowOperation.
func (t *TerminalTabs) SetWindowButtons(on bool) { t.strip.setWindowButtons(on) }

// SetOnWindowButton is what a press on a window control does (SetWindowButtons): fn with its
// operation, WindowMinimize, WindowMaximize or WindowClose. nil does nothing.
func (t *TerminalTabs) SetOnWindowButton(fn func(WindowOperation)) { t.strip.onWindow = fn }

// TabWindowControls are the window controls on the strip, as drawn now.
func (t *TerminalTabs) TabWindowControls() []TabControl { return t.strip.controlSpans() }

var (
	_ TabSpanner = (*Tabs)(nil)
	_ TabSpanner = (*TabStrip)(nil)
	_ TabSpanner = (*TerminalTabs)(nil)
)

// CardRect is where the dialog's card was placed at the last layout, in the modal's cells.
func (m *Modal) CardRect() tui.Rect { return m.cardRect }
