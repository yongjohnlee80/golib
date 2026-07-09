package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// SelectItem pairs a display label with a typed value.
type SelectItem[T any] struct {
	Label string
	Value T
}

// Select is the dropdown (ADR-0007 §2.4). Closed, it renders as a one-line
// field (current label + "▾"); Enter/Space/Down opens a floating option
// list on the overlay layer with a focus trap (both ADR-0004 mechanisms,
// realized by OverlayHost). Esc closes without change and restores the
// prior focus; Enter commits (SelectionChangedEvent); clicks outside close
// — the overlay layer sees them first.
//
// Filter-as-you-type (WithFilter): printable keys in the open state narrow
// the visible options case-insensitively; Backspace edits the filter.
//
// Options are loadable via App.Go: schedule the load with the Select's
// NodeID as owner and return []SelectItem[T]; the addressed TaskResult is
// converted and installed by Select's own HandleEvent (§2.6). Compare
// TaskResult.ID against the last-issued TaskID for staleness when issuing
// concurrent loads.
//
// Positioning note: ADR-0007 anchors the open list below the field; the
// runtime exposes no absolute-rect query to components in v1, so the list
// opens centered on the overlay area (command-palette style) — reported as
// a core follow-up.
type Select[T any] struct {
	Base
	items    []SelectItem[T]
	selected int // -1 = none
	filterOn bool
	fixedW   int // 0 = greedy

	open  bool
	popup *selectPopup[T]

	loadErr error

	fieldSt style.Style
	errSt   style.Style
}

var _ tui.Focusable = (*Select[any])(nil)

// SelectOption customizes a Select under construction.
type SelectOption[T any] func(*Select[T])

// WithOptions sets the initial option items.
func WithOptions[T any](items []SelectItem[T]) SelectOption[T] {
	return func(s *Select[T]) { s.items = append([]SelectItem[T](nil), items...) }
}

// WithFilter enables filter-as-you-type in the open state.
func WithFilter[T any](enabled bool) SelectOption[T] {
	return func(s *Select[T]) { s.filterOn = enabled }
}

// WithWidth fixes the closed field width (default: greedy).
func WithWidth[T any](w int) SelectOption[T] {
	if w < 1 {
		panic("widget: WithWidth: width must be >= 1")
	}
	return func(s *Select[T]) { s.fixedW = w }
}

// NewSelect builds a dropdown with no selection.
func NewSelect[T any](opts ...SelectOption[T]) *Select[T] {
	s := &Select[T]{
		selected: -1,
		fieldSt:  style.New().Foreground(style.TokenForeground),
		errSt:    style.New().Foreground(style.TokenError),
	}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	return s
}

// Value returns the selected value; false until a selection exists.
func (s *Select[T]) Value() (T, bool) {
	if s.selected < 0 || s.selected >= len(s.items) {
		var zero T
		return zero, false
	}
	return s.items[s.selected].Value, true
}

// SetOptions replaces the option items (loop goroutine; e.g. from a
// TaskResult). An out-of-range selection resets to none.
func (s *Select[T]) SetOptions(items []SelectItem[T]) {
	s.items = append([]SelectItem[T](nil), items...)
	if s.selected >= len(s.items) {
		s.selected = -1
	}
	s.loadErr = nil
	if s.open && s.popup != nil {
		s.popup.refilter()
	}
	s.MarkDirty()
}

// Err returns the load-error state (set by a failed addressed TaskResult).
func (s *Select[T]) Err() error { return s.loadErr }

// AcceptsFocus implements tui.Focusable.
func (s *Select[T]) AcceptsFocus() bool { return true }

// Init registers overlay cleanup: an unmounting Select takes its popup
// down with it.
func (s *Select[T]) Init(ctx *tui.Context) {
	s.Base.Init(ctx)
	s.open, s.popup = false, nil
	ctx.OnUnmount(func() {
		if s.open && s.popup != nil {
			s.publish(overlayCloseEvent{layer: s.popup})
			s.open, s.popup = false, nil
		}
	})
}

// openPopup mounts the option list on the OverlayHost (via the internal Bus
// handshake) and emits OpenedEvent.
func (s *Select[T]) openPopup() {
	if s.open {
		return
	}
	s.popup = &selectPopup[T]{owner: s}
	s.open = true
	s.publish(overlayOpenEvent{layer: s.popup})
	s.publish(OpenedEvent{Owner: s.NodeID()})
	s.MarkDirty()
}

// closePopup unmounts the option list (the runtime restores the prior
// focus — normally this Select) and emits ClosedEvent.
func (s *Select[T]) closePopup() {
	if !s.open {
		return
	}
	popup := s.popup
	s.open, s.popup = false, nil
	s.publish(overlayCloseEvent{layer: popup})
	s.publish(ClosedEvent{Owner: s.NodeID()})
	s.MarkDirty()
}

// commit installs option index i and emits SelectionChangedEvent, then
// closes.
func (s *Select[T]) commit(i int) {
	if i >= 0 && i < len(s.items) {
		s.selected = i
		s.publish(SelectionChangedEvent{Owner: s.NodeID(), Index: i, Label: s.items[i].Label})
	}
	s.closePopup()
}

// HandleEvent: closed-state keys, plus the §2.6 TaskResult conversion.
func (s *Select[T]) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TaskResult:
		if e.Owner != s.NodeID() {
			return false
		}
		if e.Err != nil {
			s.loadErr = e.Err
			s.MarkDirty()
			return true
		}
		if items, ok := e.Value.([]SelectItem[T]); ok {
			s.SetOptions(items)
			return true
		}
		return false
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease || e.Mods&nonTextMods != 0 {
			return false
		}
		switch e.Code {
		case tui.KeyEnter, ' ', tui.KeyDown:
			s.openPopup()
			return true
		}
	}
	return false
}

// Layout: closed = height 1, width greedy or WithWidth.
func (s *Select[T]) Layout(c tui.Constraints) tui.Size {
	w := s.fixedW
	if w == 0 {
		w = boundedMax(c.MaxW, max(c.MinW, s.longestLabel()+2))
	}
	return c.Constrain(tui.Size{W: w, H: 1})
}

func (s *Select[T]) longestLabel() int {
	w := 0
	for _, it := range s.items {
		w = max(w, s.measure(it.Label))
	}
	return w
}

// Render paints the closed field: current label (muted placeholder dash
// when none) and the ▾ affordance.
func (s *Select[T]) Render(sur tui.Surface) {
	sz := sur.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	st := s.fieldSt
	label := ""
	if s.selected >= 0 && s.selected < len(s.items) {
		label = s.items[s.selected].Label
	} else {
		st = style.New().Foreground(style.TokenTextMuted).Inherit(st)
	}
	if s.loadErr != nil {
		st = s.errSt.Inherit(st)
		label = "error: " + s.loadErr.Error()
	}
	if sz.W > 2 {
		drawText(sur, 0, 0, truncate(label, sz.W-2, sur.StringWidth), st)
	}
	arrow := "▾"
	if s.open {
		arrow = "▴"
	}
	sur.SetCell(sz.W-1, 0, arrow, s.fieldSt)
}

// selectPopup is the open-state overlay layer: full-area (so outside clicks
// close), focus-trapping, with the option panel centered inside it.
type selectPopup[T any] struct {
	Base
	owner *Select[T]

	filter  string
	matches []int // item indices surviving the filter
	hi      int   // highlight position within matches
	top     int
	panel   tui.Rect // computed each layout
}

var (
	_ tui.Focusable  = (*selectPopup[any])(nil)
	_ tui.FocusScope = (*selectPopup[any])(nil)
)

func (p *selectPopup[T]) AcceptsFocus() bool { return true }
func (p *selectPopup[T]) TrapsFocus() bool   { return true }

// Init takes focus (the trap records the prior focus for Esc-restore).
func (p *selectPopup[T]) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	p.filter = ""
	p.refilter()
	// Highlight the current selection when it survives the (empty) filter.
	for mi, idx := range p.matches {
		if idx == p.owner.selected {
			p.hi = mi
			break
		}
	}
	ctx.RequestFocus()
}

// refilter recomputes the visible options (case-insensitive substring).
func (p *selectPopup[T]) refilter() {
	p.matches = p.matches[:0]
	needle := strings.ToLower(p.filter)
	for i, it := range p.owner.items {
		if needle == "" || strings.Contains(strings.ToLower(it.Label), needle) {
			p.matches = append(p.matches, i)
		}
	}
	p.hi = max(0, min(p.hi, len(p.matches)-1))
	p.top = 0
	p.MarkDirty()
}

// rows is the option-row capacity of the current panel.
func (p *selectPopup[T]) rows() int {
	h := p.panel.H - 2 // borders
	if p.owner.filterOn {
		h--
	}
	return max(h, 1)
}

func (p *selectPopup[T]) ensureVisible() {
	rows := p.rows()
	if p.hi < p.top {
		p.top = p.hi
	}
	if p.hi >= p.top+rows {
		p.top = p.hi - rows + 1
	}
	p.top = max(p.top, 0)
}

// Layout spans the whole overlay and centers the option panel: width sized
// to the longest label, height capped to the available space with internal
// scrolling.
func (p *selectPopup[T]) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	labelW := max(p.owner.longestLabel(), 8)
	if p.owner.filterOn {
		labelW = max(labelW, p.owner.measure(p.filter)+2)
	}
	pw := min(labelW+2, max(w-2, 3))
	rows := max(len(p.matches), 1)
	ph := rows + 2
	if p.owner.filterOn {
		ph++
	}
	ph = min(ph, max(h-2, 3))
	p.panel = tui.Rect{X: max((w-pw)/2, 0), Y: max((h-ph)/2, 0), W: pw, H: ph}
	p.ensureVisible()
	return c.Constrain(tui.Size{W: w, H: h})
}

// HandleEvent implements the open-state contract: filter typing, cursor
// movement, Enter commit, Esc close, outside-click close, wheel scroll.
func (p *selectPopup[T]) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease {
			return false
		}
		switch e.Code {
		case tui.KeyEscape:
			p.owner.closePopup()
			return true
		case tui.KeyEnter:
			if len(p.matches) > 0 {
				p.owner.commit(p.matches[p.hi])
			} else {
				p.owner.closePopup()
			}
			return true
		case tui.KeyUp:
			p.moveHi(-1)
			return true
		case tui.KeyDown:
			p.moveHi(1)
			return true
		case tui.KeyPageUp:
			p.moveHi(-p.rows())
			return true
		case tui.KeyPageDown:
			p.moveHi(p.rows())
			return true
		case tui.KeyHome:
			p.moveHi(-len(p.matches))
			return true
		case tui.KeyEnd:
			p.moveHi(len(p.matches))
			return true
		case tui.KeyBackspace:
			if p.owner.filterOn && p.filter != "" {
				cs := clusters(p.filter)
				p.filter = strings.Join(cs[:len(cs)-1], "")
				p.refilter()
			}
			return true
		}
		if p.owner.filterOn && e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
			p.filter += sanitizeLine(e.Text)
			p.refilter()
			return true
		}
		return false

	case tui.MouseEvent:
		switch e.Kind {
		case tui.MousePress:
			if !p.panel.Contains(e.X, e.Y) {
				p.owner.closePopup() // outside click: the overlay sees it first
				return true
			}
			row := e.Y - p.panel.Y - 1
			if p.owner.filterOn {
				row--
			}
			if i := p.top + row; row >= 0 && i < len(p.matches) {
				p.owner.commit(p.matches[i])
			}
			return true
		case tui.MouseWheel:
			switch e.Button {
			case tui.WheelUp:
				p.moveHi(-1)
			case tui.WheelDown:
				p.moveHi(1)
			}
			return true
		}
		return true // the open overlay swallows other mouse traffic
	}
	return false
}

func (p *selectPopup[T]) moveHi(delta int) {
	if len(p.matches) == 0 {
		return
	}
	p.hi = max(0, min(p.hi+delta, len(p.matches)-1))
	p.ensureVisible()
	p.MarkDirty()
}

// Render paints the option panel (the backdrop stays transparent — lower
// Stack layers show through).
func (p *selectPopup[T]) Render(s tui.Surface) {
	r := p.panel
	if r.Empty() {
		return
	}
	surface := style.New().Background(style.TokenBoost)
	border := style.New().Foreground(style.TokenBorderFocused).Background(style.TokenBoost)
	optSt := style.New().Foreground(style.TokenForeground).Background(style.TokenBoost)
	hiSt := style.New().Background(style.TokenPrimary).Foreground(style.TokenTextOnPrimary)
	filterSt := style.New().Foreground(style.TokenTextMuted).Background(style.TokenBoost)

	s.Fill(tui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, " ", surface)
	bs := style.BorderNormal
	s.Fill(tui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, bs.Top, border)
	s.Fill(tui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, bs.Bottom, border)
	s.Fill(tui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, bs.Left, border)
	s.Fill(tui.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, bs.Right, border)
	s.SetCell(r.X, r.Y, bs.TopLeft, border)
	s.SetCell(r.X+r.W-1, r.Y, bs.TopRight, border)
	s.SetCell(r.X, r.Y+r.H-1, bs.BottomLeft, border)
	s.SetCell(r.X+r.W-1, r.Y+r.H-1, bs.BottomRight, border)

	innerX, innerW := r.X+1, r.W-2
	y := r.Y + 1
	if p.owner.filterOn {
		f := "/" + p.filter
		sub := s.Sub(tui.Rect{X: innerX, Y: y, W: innerW, H: 1})
		drawText(sub, 0, 0, truncate(f, innerW, s.StringWidth), filterSt)
		y++
	}
	rows := p.rows()
	for row := 0; row < rows; row++ {
		mi := p.top + row
		if mi >= len(p.matches) {
			break
		}
		st := optSt
		if mi == p.hi {
			st = hiSt
		}
		s.Fill(tui.Rect{X: innerX, Y: y + row, W: innerW, H: 1}, " ", st)
		sub := s.Sub(tui.Rect{X: innerX, Y: y + row, W: innerW, H: 1})
		label := p.owner.items[p.matches[mi]].Label
		drawText(sub, 0, 0, truncate(label, innerW, s.StringWidth), st)
	}
	if len(p.matches) > rows {
		sub := s.Sub(tui.Rect{X: r.X + r.W - 1, Y: y, W: 1, H: rows})
		paintScrollIndicator(sub, 0, rows, p.top, max(len(p.matches)-rows, 1)+1)
	}
}
