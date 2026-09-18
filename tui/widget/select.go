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

// Dropdown Select & Overlay Option Picker Architecture
//
// Select provides a single-selection dropdown menu and option picker. In its
// resting closed state, it presents a compact 1-line field with a down-triangle
// affordance ("▾"). When activated (Enter, Space, or Down arrow), it projects a
// floating option list onto the application's [OverlayHost] stack layer, complete
// with focus trapping, interactive filtering, and outside-click dismissal.
//
// # Subsystem Role & Responsibilities
//
//  1. Two-Phase Presentation:
//     - Closed Phase: Occupies 1 row in the parent layout, displaying the currently
//     selected item label.
//     - Open Phase: Projects a modal [selectPopup] onto the root [OverlayHost] via an
//     nearest enclosing [OverlayHost], resolved from the component tree.
//  2. Focus Trap & Outside Dismissal:
//     When opened, focus is transferred to the overlay popup list. Tab traversal is
//     trapped within the popup. Clicking anywhere outside the popup or pressing Escape
//     dismisses the menu without altering the selection, cleanly restoring focus to the field.
//  3. Filter-As-You-Type ([WithFilter]):
//     Typing printable characters while open dynamically filters the visible options
//     using case-insensitive substring matching. Backspace edits the filter query.
//  4. Asynchronous Option Loading:
//     Compatible with [tui.App.Go]. Background loaders return `[]SelectItem[T]` in a
//     [tui.TaskResult] addressed to the Select's [tui.NodeID]. Select's [Select.HandleEvent]
//     automatically installs the arriving items or captures errors.
//
// # Layout & Overlay Handshake Hierarchy
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ [OverlayHost] Stack Canvas                                  │
//	│                                                             │
//	│  ┌───────────────────────────────────────────────────────┐  │
//	│  │ selectPopup (FocusScope Trap on Top Overlay Layer)    │  │
//	│  │ ┌───────────────────────────────────────────────────┐ │  │
//	│  │ │ [ Filter Input: "da..." ]                         │ │  │
//	│  │ │ ───────────────────────────────────────────────── │ │  │
//	│  │ │  > Database Alpha                                 │ │  │
//	│  │ │    Database Beta                                  │ │  │
//	│  │ └───────────────────────────────────────────────────┘ │  │
//	│  └───────────────────────────────────────────────────────┘  │
//	│                             ▲ (Projects above base UI)      │
//	│  Base UI:                   │                               │
//	│  [ Connection Profile: Database Alpha ▾ ]                   │
//	└─────────────────────────────────────────────────────────────┘
//
// # Architectural Invariants
//
//  1. Decoupled Overlay Attachment:
//     Select communicates with the enclosing [OverlayHost] exclusively via bus events.
//     A Select can be nested at arbitrary layout depths without requiring parent pointer passing.
//  2. State Isolation Across Cancellation:
//     Pressing Escape or clicking outside closes the popup and leaves the selected index untouched.
//  3. Notification Contract:
//     Committing a new selection publishes [SelectionChangedEvent]. Opening and closing publish
//     [OpenedEvent] and [ClosedEvent] respectively, stamped with the Select's [tui.NodeID].
//
// # Concurrency & Goroutine Ownership
//
// Select is loop-goroutine-owned. Selecting items or updating options must occur on the
// main application loop goroutine.
//
// # Usage Examples
//
// 1. Standard static option dropdown:
//
//	items := []widget.SelectItem[string]{
//		{Label: "PostgreSQL", Value: "postgres"},
//		{Label: "MySQL", Value: "mysql"},
//		{Label: "SQLite", Value: "sqlite"},
//	}
//	sel := widget.NewSelect(
//		widget.WithOptions(items),
//		widget.WithFilter[string](true),
//	)
//
// 2. Listening to selection changes:
//
//	tui.Subscribe(ctx, func(ev widget.SelectionChangedEvent) {
//		if ev.Owner == sel.NodeID() {
//			log.Printf("Selected engine: %s (index %d)", ev.Label, ev.Index)
//		}
//	})
type Select[T any] struct {
	Base
	items    []SelectItem[T]
	selected int // -1 = none
	filterOn bool
	fixedW   int // 0 = greedy

	open  bool
	armed bool
	popup *selectPopup[T]

	// placement decides where the open option list sits. See SelectPlacement.
	placement SelectPlacement
	// affordance draws the ▾/▴ triangle at the field's right edge. Some hosts
	// mark the focused row themselves and do not want a second indicator.
	affordance bool

	loadErr error

	// placeholder is shown, muted, while NOTHING is selected. Empty by
	// default, which renders a blank field -- indistinguishable from a field
	// that is still loading, or from one whose selected label happens to be
	// empty. A host that wants the empty state to say so supplies the words.
	placeholder string

	// focusedSt is merged over fieldSt while the keyboard is on this select,
	// or its options are open.
	//
	// A CLOSED SELECT IS ONE ROW OF TEXT, and without a focus look it is the
	// same one row whether the keyboard is in it or not. An operator tabbing
	// through a form could not see that they had reached it, pressed Enter to
	// find out, and got a popup they had not asked for.
	focusedSt style.Style

	fieldSt style.Style
	errSt   style.Style
}

var (
	_ tui.Focusable   = (*Select[any])(nil)
	_ tui.Activatable = (*Select[any])(nil)
)

// Activate opens the option list, which is what activation MEANS for a
// dropdown: there is nothing else it could do.
//
// IMPLEMENTING Activatable IS WHAT MAKES A CLICK WORK. The runtime only starts
// a pointer gesture on a target that implements this interface, so before it
// existed a click on a Select hit-tested to the field, moved focus to it, and
// stopped -- the options never opened and the widget looked inert under the
// mouse while answering Enter, Space and Down from the keyboard. Keyboard and
// pointer are one path now, resolving to the same method.
//
// Returns false when already open, so a second activation is not reported as
// having done something.
func (s *Select[T]) Activate(tui.ActionOrigin) bool {
	if s.open {
		return false
	}
	s.openPopup()
	return s.open
}

// SetArmed records the pressed state the recognizer drives between press and
// release. Held for the look; nothing else reads it.
func (s *Select[T]) SetArmed(v bool) {
	if s.armed == v {
		return
	}
	s.armed = v
	s.MarkDirty()
}

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

// SelectPlacement says where the open option list is put.
type SelectPlacement uint8

const (
	// SelectPlacementAnchored puts the list directly beneath the field and
	// aligned to its LEFT edge, flipping above when there is no room below.
	// It is the default, because a dropdown that is not attached to the
	// control it belongs to makes the reader find the relationship.
	SelectPlacementAnchored SelectPlacement = iota
	// SelectPlacementCentered puts the list in the middle of the overlay
	// area, which is what this widget did before placement was a choice.
	SelectPlacementCentered
)

// WithPopupPlacement chooses where the open option list sits.
func WithPopupPlacement[T any](p SelectPlacement) SelectOption[T] {
	return func(s *Select[T]) { s.placement = p }
}

// WithAffordance draws — or withholds — the ▾/▴ triangle at the field's right
// edge. Default: drawn.
//
// Withholding it is for a host that already marks the focused row itself, where
// the triangle is a second indicator of the same thing in a different place.
func WithAffordance[T any](on bool) SelectOption[T] {
	return func(s *Select[T]) { s.affordance = on }
}

// WithSelectPlaceholder is the text shown, muted, while nothing is selected.
//
// Default empty, which is the behaviour this widget always had: a blank field.
// Blank cannot be told apart from a field still loading its options, or one
// whose selection has an empty label, so a host that cares says the words.
func WithSelectPlaceholder[T any](s string) SelectOption[T] {
	return func(sel *Select[T]) { sel.placeholder = s }
}

// WithSelectFocusedStyle is the look merged over the field while this select
// holds the keyboard or has its options open.
//
// Default: reversed. REVERSE RATHER THAN A COLOUR PAIR, for the reason the
// button styles give -- naming tokens fails wherever a theme leaves foreground
// and background as the terminal's own defaults, because both sides resolve to
// "default" and the emphasis disappears. Reverse is an attribute the terminal
// applies to whatever the cell actually holds, so it inverts under every
// theme, including none.
func WithSelectFocusedStyle[T any](st style.Style) SelectOption[T] {
	return func(s *Select[T]) { s.focusedSt = st }
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
		selected:   -1,
		affordance: true,
		focusedSt:  style.New().Reverse(true),
		fieldSt:    style.New().Foreground(style.TokenForeground),
		errSt:      style.New().Foreground(style.TokenError),
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
			if host, ok := hostFor[layerHost](s.Context()); ok {
				host.removeLayer(s.popup)
			}
			s.open, s.popup = false, nil
		}
	})
}

// openPopup mounts the option list on the NEAREST ENCLOSING OverlayHost and
// emits OpenedEvent.
//
// It used to publish an unaddressed request on the Bus, which every mounted
// OverlayHost received: with two hosts in one application both tried to mount
// the same popup component and the runtime panicked — a component value mounts
// at most once — so opening a dropdown took the application down. The host is
// now resolved from the tree and called directly, and a Select with no host
// above it simply does not open rather than publishing into the void.
func (s *Select[T]) openPopup() {
	if s.open {
		return
	}
	host, ok := hostFor[layerHost](s.Context())
	if !ok {
		return // nothing can hold the popup; opening would be a lie
	}
	s.popup = &selectPopup[T]{owner: s}
	s.open = true
	host.addLayer(s.popup)
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
	if host, ok := hostFor[layerHost](s.Context()); ok {
		host.removeLayer(popup)
	}
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

// HandleEvent: closed-state keys, plus the TaskResult conversion.
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
		label = s.placeholder
		st = style.New().Foreground(style.TokenTextMuted).Faint(true).Inherit(st)
	}
	if s.loadErr != nil {
		st = s.errSt.Inherit(st)
		label = "error: " + s.loadErr.Error()
	}
	// THE FOCUS LOOK GOES ON LAST, so it marks the field whatever it is
	// currently saying -- a selection, a placeholder, or a load error.
	//
	// `|| s.open` keeps it lit while the options are up: focus is inside the
	// popup then, so the field is not focused by the framework's reckoning,
	// and letting it go dark would say the operator had left the control they
	// are in the middle of using.
	ctx := s.Context()
	focused := (ctx != nil && ctx.Focused()) || s.open
	if focused {
		st = s.focusedSt.Inherit(st)
	}
	// THE FOCUS LOOK PAINTS THE FIELD, not merely its text. An empty select
	// draws no characters at all, so a style carried only by the label reached
	// no cell and the focused state was invisible in exactly the case that
	// needed it most -- a field with nothing chosen yet.
	if focused {
		for x := range sz.W {
			sur.SetCell(x, 0, " ", st)
		}
	}
	if sz.W > 2 {
		drawText(sur, 0, 0, truncate(label, sz.W-2, sur.StringWidth), st)
	}
	if !s.affordance {
		return
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
	p.panel = p.place(w, h, pw, ph)
	p.ensureVisible()
	return c.Constrain(tui.Size{W: w, H: h})
}

// place decides where the panel sits inside the full-area overlay.
//
// ANCHORED IS THE DEFAULT: directly beneath the field and aligned to its LEFT
// edge, which is where a dropdown belongs -- a list floating in the middle of
// the screen makes the reader work out which control it came from, and on a
// form of several selects that is a real question rather than a rhetorical
// one.
//
// It FLIPS ABOVE the field when there is not room below, and slides left when
// the panel would overhang the right edge, because a list that runs off the
// screen has hidden the options it exists to show. Falls back to centred when
// the owner's rect cannot be resolved -- it has no rect before its first
// layout, and a guess would be worse than the old behaviour.
func (p *selectPopup[T]) place(w, h, pw, ph int) tui.Rect {
	centered := tui.Rect{X: max((w-pw)/2, 0), Y: max((h-ph)/2, 0), W: pw, H: ph}
	if p.owner.placement != SelectPlacementAnchored {
		return centered
	}
	ctx := p.owner.Context()
	if ctx == nil {
		return centered
	}
	field, ok := ctx.ResolveAnchor(ctx.NodeAnchor())
	if !ok {
		return centered
	}
	x := min(max(field.X, 0), max(w-pw, 0))
	y := field.Y + field.H
	if y+ph > h {
		// No room below: sit above the field instead, and only fall back to
		// clamping when it does not fit on either side.
		if above := field.Y - ph; above >= 0 {
			y = above
		} else {
			y = max(h-ph, 0)
		}
	}
	return tui.Rect{X: x, Y: y, W: pw, H: ph}
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
		case 'k':
			// VIM MOTION, BUT ONLY WHERE THE LETTER IS FREE. A filtering
			// select spends every printable character on the query, so j and k
			// there are the two letters the operator most needs to TYPE --
			// "jetbrains", "sqlite". The arrows work in both modes and are
			// what the footer names; this is the convenience on top.
			if p.owner.filterOn {
				break
			}
			p.moveHi(-1)
			return true
		case 'j':
			if p.owner.filterOn {
				break
			}
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
	hiSt := style.New().Reverse(true)
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
