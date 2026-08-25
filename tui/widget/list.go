package widget

import (
	"sort"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// ListSource is the data seam List renders through (ADR-0007 §2.4, rev 1).
// It is v1 API: the shape a windowed/lazy source implements later WITHOUT
// List needing a v2. Sources are consulted only on the loop goroutine.
type ListSource[T any] interface {
	// Len is the total row count.
	Len() int
	// Item returns row i. It MUST be cheap and non-blocking — fetch
	// elsewhere (e.g. App.Go) and serve from memory here.
	Item(i int) T
}

// sliceSource adapts an in-memory slice — the provided v1 source.
type sliceSource[T any] []T

func (s sliceSource[T]) Len() int     { return len(s) }
func (s sliceSource[T]) Item(i int) T { return s[i] }

// SliceSource adapts an in-memory slice into a ListSource. NOTE (ADR-0007
// N3): this source holds every item in memory, so very large datasets
// (100k+-row query results) must page at the data layer until the windowed
// source follow-up (§2.7#2) lands — a new ListSource implementation, no
// List API change.
func SliceSource[T any](items []T) ListSource[T] { return sliceSource[T](items) }

// List renders selectable rows through a ListSource (ADR-0007 §2.4). The
// rendering path is virtualization-shaped: Item(i) is called only for rows
// intersecting the viewport, and Len() once per render pass.
//
// Keys consumed: Up/Down/PgUp/PgDn/Home/End (cursor movement, emitting
// SelectionChangedEvent in single-select mode), Space (multi-select
// toggle), Enter (ActivateEvent). Mouse: click selects, wheel scrolls,
// double-click activates.
type List[T any] struct {
	Base
	src    ListSource[T]
	render func(T) string
	multi  bool

	cursor int
	sel    map[int]struct{} // multi-select set
	top    int
	count  int // Len() cached at render/refresh; handlers use the cache
	// lastPressIdx is the LOGICAL row of the previous press, so a double-click
	// pair straddling a viewport scroll cannot activate a different row.
	lastPressIdx int
	w, h         int

	emptyText string // shown (muted) in place of rows when the source is empty

	styles ListStyles
}

var _ tui.Focusable = (*List[any])(nil)

// ListStyles are the row style hooks. Zero fields keep the defaults.
type ListStyles struct {
	Row            style.Style // default: theme default
	CursorRow      style.Style // default: TokenPrimary fill
	SelectedRow    style.Style // default: TokenSecondary fill
	CursorSelected style.Style // default: CursorRow merged over SelectedRow
}

// ListOption customizes a List under construction.
type ListOption[T any] func(*List[T])

// WithSource sets the data source and the row renderer.
func WithSource[T any](src ListSource[T], render func(T) string) ListOption[T] {
	if render == nil {
		panic("widget: WithSource: nil render func")
	}
	return func(l *List[T]) {
		if src == nil {
			src = SliceSource[T](nil)
		}
		l.src = src
		l.render = render
	}
}

// WithItems is sugar for WithSource(SliceSource(items), render).
func WithItems[T any](items []T, render func(T) string) ListOption[T] {
	return WithSource(SliceSource(items), render)
}

// WithMultiSelect enables the multi-select mode (Space toggles;
// SelectedAll reports).
func WithMultiSelect[T any](enabled bool) ListOption[T] {
	return func(l *List[T]) { l.multi = enabled }
}

// WithEmptyText sets a muted placeholder rendered in the list's first row when
// the source has no items (e.g. "No results yet"). Empty by default: an empty
// list paints nothing.
func WithEmptyText[T any](s string) ListOption[T] {
	return func(l *List[T]) { l.emptyText = s }
}

// WithListStyles overrides the style hooks; zero fields keep defaults.
func WithListStyles[T any](st ListStyles) ListOption[T] {
	return func(l *List[T]) {
		l.styles = ListStyles{
			Row:            st.Row.Inherit(l.styles.Row),
			CursorRow:      st.CursorRow.Inherit(l.styles.CursorRow),
			SelectedRow:    st.SelectedRow.Inherit(l.styles.SelectedRow),
			CursorSelected: st.CursorSelected.Inherit(l.styles.CursorSelected),
		}
	}
}

// SetStyles replaces the row styles at runtime; zero fields keep their
// current values. Hosts use this for focus-dependent styling — the
// widget cannot see focus that rests on a delegating wrapper.
func (l *List[T]) SetStyles(st ListStyles) {
	l.styles = ListStyles{
		Row:            st.Row.Inherit(l.styles.Row),
		CursorRow:      st.CursorRow.Inherit(l.styles.CursorRow),
		SelectedRow:    st.SelectedRow.Inherit(l.styles.SelectedRow),
		CursorSelected: st.CursorSelected.Inherit(l.styles.CursorSelected),
	}
	l.MarkDirty()
}

// SetCursor moves the cursor to i (clamped), scrolling it into view.
// The programmatic sibling of the j/k/arrow motions — hosts drive it for
// search, restore-selection, and reveal.
func (l *List[T]) SetCursor(i int) { l.moveCursor(i) }

// Len reports the current row count (cached per render pass).
func (l *List[T]) Len() int { return l.count }

// NewList builds a List. A source (WithSource or WithItems) is required —
// misconfiguration panics at construction (golib convention).
func NewList[T any](opts ...ListOption[T]) *List[T] {
	l := &List[T]{
		sel: make(map[int]struct{}),
		styles: ListStyles{
			CursorRow:   style.New().Background(style.TokenPrimary).Foreground(style.TokenTextOnPrimary),
			SelectedRow: style.New().Background(style.TokenSecondary).Foreground(style.TokenTextOnSecondary),
		},
	}
	l.styles.CursorSelected = l.styles.CursorRow.Inherit(l.styles.SelectedRow).Bold(true)
	for _, o := range opts {
		if o != nil {
			o(l)
		}
	}
	if l.src == nil || l.render == nil {
		panic("widget: NewList requires WithSource or WithItems")
	}
	l.count = l.src.Len()
	return l
}

// SetSource replaces the data source (loop goroutine), resetting cursor and
// viewport.
func (l *List[T]) SetSource(src ListSource[T]) {
	if src == nil {
		src = SliceSource[T](nil)
	}
	l.src = src
	l.count = src.Len()
	l.cursor, l.top = 0, 0
	clear(l.sel)
	l.MarkDirty()
}

// SetItems is sugar for SetSource(SliceSource(items)).
func (l *List[T]) SetItems(items []T) { l.SetSource(SliceSource(items)) }

// RefreshSource re-reads Len after in-place source mutation, clamps the
// cursor, and repaints.
func (l *List[T]) RefreshSource() {
	l.count = l.src.Len()
	l.clamp()
	l.MarkDirty()
}

// Selected returns the cursor index; ok is false while the list is empty.
func (l *List[T]) Selected() (int, bool) {
	if l.count == 0 {
		return 0, false
	}
	return l.cursor, true
}

// SelectedAll returns the multi-select indices in ascending order.
func (l *List[T]) SelectedAll() []int {
	out := make([]int, 0, len(l.sel))
	for i := range l.sel {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// AcceptsFocus implements tui.Focusable.
func (l *List[T]) AcceptsFocus() bool { return true }

func (l *List[T]) clamp() {
	l.cursor = max(0, min(l.cursor, l.count-1))
	if l.count == 0 {
		l.cursor = 0
	}
	maxTop := max(l.count-l.viewRows(), 0)
	l.top = max(0, min(l.top, maxTop))
}

// viewRows is the number of content rows in the viewport.
func (l *List[T]) viewRows() int { return max(l.h, 0) }

// ensureVisible scrolls the viewport to keep the cursor inside it.
func (l *List[T]) ensureVisible() {
	rows := l.viewRows()
	if rows <= 0 {
		return
	}
	if l.cursor < l.top {
		l.top = l.cursor
	}
	if l.cursor >= l.top+rows {
		l.top = l.cursor - rows + 1
	}
	l.top = max(l.top, 0)
}

// moveCursor moves the cursor to i (clamped) and emits
// SelectionChangedEvent in single-select mode.
func (l *List[T]) moveCursor(i int) {
	if l.count == 0 {
		return
	}
	i = max(0, min(i, l.count-1))
	if i == l.cursor {
		return
	}
	l.cursor = i
	l.ensureVisible()
	l.MarkDirty()
	if !l.multi {
		l.publish(SelectionChangedEvent{Owner: l.NodeID(), Index: i, Label: l.render(l.src.Item(i))})
	}
}

// activate emits ActivateEvent for the cursor row.
func (l *List[T]) activate() {
	if l.count == 0 {
		return
	}
	l.publish(ActivateEvent{Owner: l.NodeID(), Index: l.cursor})
}

// HandleEvent implements the §2.4 key/mouse contract.
func (l *List[T]) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease {
			return false
		}
		// Ctrl/Alt/Super chords belong to the application (pane motion,
		// quit, …) — a list must never eat them as plain letters.
		if e.Mods&nonTextMods != 0 {
			return false
		}
		// Vim motions alongside the arrows: the widget set is used in
		// vim-keyed applications, and j/k/g/G are the house vocabulary
		// (widget.Tree has honored them since ADR-0008 §2.2).
		code := e.Code
		if e.Text != "" {
			code = []rune(e.Text)[0]
		}
		switch code {
		case 'k', tui.KeyUp:
			l.moveCursor(l.cursor - 1)
			return true
		case 'j', tui.KeyDown:
			l.moveCursor(l.cursor + 1)
			return true
		case 'g':
			l.moveCursor(0)
			return true
		case 'G':
			l.moveCursor(l.count - 1)
			return true
		case tui.KeyPageUp:
			l.moveCursor(l.cursor - max(l.viewRows(), 1))
			return true
		case tui.KeyPageDown:
			l.moveCursor(l.cursor + max(l.viewRows(), 1))
			return true
		case tui.KeyHome:
			l.moveCursor(0)
			return true
		case tui.KeyEnd:
			l.moveCursor(l.count - 1)
			return true
		case tui.KeyEnter:
			l.activate()
			return true
		case ' ':
			if !l.multi {
				return false
			}
			if l.count > 0 {
				if _, on := l.sel[l.cursor]; on {
					delete(l.sel, l.cursor)
				} else {
					l.sel[l.cursor] = struct{}{}
				}
				l.MarkDirty()
			}
			return true
		}
		return false

	case tui.MouseEvent:
		switch {
		case e.Kind == tui.MousePress && e.Button == tui.MouseLeft:
			idx := l.top + e.Y
			if idx < 0 || idx >= l.count {
				return true
			}
			l.moveCursor(idx)
			// Timing, cell and target identity come from the App (ADR-0010 §2.5).
			// LOGICAL ROW identity stays here, because only List knows it: the
			// App's continuity is an absolute CELL, and a viewport that scrolls
			// between the two presses maps the same cell to a different row. The
			// old per-widget detection compared logical rows and refused that
			// pair; dropping the comparison silently activated the wrong row
			// (lector r1 finding 2).
			//
			// EXACTLY 2, not >= 2: three presses would otherwise activate twice,
			// on counts 2 and 3, while "double-click = Enter" means one
			// activation per pair (finding 3).
			if e.Count == 2 && idx == l.lastPressIdx {
				l.activate()
			}
			l.lastPressIdx = idx
			return true
		case e.Kind == tui.MouseWheel && e.Button == tui.WheelUp:
			l.top = max(l.top-1, 0)
			l.MarkDirty()
			return true
		case e.Kind == tui.MouseWheel && e.Button == tui.WheelDown:
			l.top = min(l.top+1, max(l.count-l.viewRows(), 0))
			l.MarkDirty()
			return true
		}
		return false
	}
	return false
}

// Layout is greedy on both axes. It uses the cached count — Len() is read
// once per render pass (ADR-0007 §5.11), not here.
func (l *List[T]) Layout(c tui.Constraints) tui.Size {
	l.w = boundedMax(c.MaxW, max(c.MinW, 1))
	l.h = boundedMax(c.MaxH, max(c.MinH, 1))
	l.ensureVisible()
	l.clamp()
	return c.Constrain(tui.Size{W: l.w, H: l.h})
}

// rowStyle resolves the style for row i.
func (l *List[T]) rowStyle(i int) style.Style {
	_, selected := l.sel[i]
	if !l.multi {
		selected = i == l.cursor
	}
	switch {
	case i == l.cursor && selected && l.multi:
		return l.styles.CursorSelected
	case i == l.cursor:
		return l.styles.CursorRow
	case selected:
		return l.styles.SelectedRow
	default:
		return l.styles.Row
	}
}

// Render fetches and paints exactly the viewport rows: Item(i) only for
// i in [top, top+rows), Len() once.
func (l *List[T]) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	l.count = l.src.Len() // the one Len() read of this pass
	l.clamp()
	if l.count == 0 {
		if l.emptyText != "" {
			muted := style.New().Foreground(style.TokenTextMuted).Italic(true)
			drawText(s, 0, 0, truncate(l.emptyText, sz.W, s.StringWidth), muted)
		}
		return
	}
	rows := min(sz.H, l.count-l.top)
	scrollable := l.count > sz.H
	contentW := sz.W
	if scrollable {
		contentW--
	}
	for r := 0; r < rows; r++ {
		i := l.top + r
		st := l.rowStyle(i)
		if _, bg := st.GetBackground(); bg {
			s.Fill(tui.Rect{X: 0, Y: r, W: contentW, H: 1}, " ", st)
		}
		text := truncate(l.render(l.src.Item(i)), contentW, s.StringWidth)
		drawText(s, 0, r, text, st)
	}
	if scrollable {
		paintScrollIndicator(s, sz.W-1, sz.H, l.top, max(l.count-sz.H, 1)+1)
	}
}
