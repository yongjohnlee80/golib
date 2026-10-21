package widget

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
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
//     - Open Phase: Opens its options as a popup on the nearest enclosing [OverlayHost],
//     resolved from the component tree: the host's popupLayer, holding the options' panel,
//     placed under the field (above it where they do not fit).
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
//	│  │ popupLayer (focus trap) holding the selectPanel       │  │
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
//     Select finds the nearest enclosing [OverlayHost] through the component tree, so it can be
//     nested at any layout depth without a parent pointer passed in.
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
	// onChange tells the owner of a choice at once (WithSelectOnChange), after the popup closed
	onChange func(i int)
	filterOn bool
	fixedW   int // 0 = content-sized
	maxW     int
	fieldW   int

	open  bool
	armed bool
	popup *selectPanel[T] // the open options; nil while closed
	layer *popupLayer     // the popup holding them
	// chosen is a commit's option, reported to onChange once the popup has gone; -1 for none
	chosen int

	// affordance draws the ▾/▴ triangle at the field's right edge. Some hosts
	// mark the focused row themselves and do not want a second indicator.
	affordance bool

	loadErr error
	// errText is loadErr as the field shows it, in the App's language: set when the error
	// arrives and again each layout.
	errText string

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
	_ tui.Focusable        = (*Select[any])(nil)
	_ tui.Activatable      = (*Select[any])(nil)
	_ tui.PointerContainer = (*Select[any])(nil)
)

func (s *Select[T]) ContainsPointer(x, y int) bool {
	return x >= 0 && x < s.fieldW && y == 0
}

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

// WithSelectOnChange sets what runs, at once, when option i is chosen, after the popup closes: the
// bus's SelectionChangedEvent reaches other subscribers a loop turn later. An owner composing a
// Select uses it rather than subscribing app-wide and filtering by Owner. fn may replace the options.
func WithSelectOnChange[T any](fn func(i int)) SelectOption[T] {
	return func(s *Select[T]) { s.onChange = fn }
}

// WithFilter enables filter-as-you-type in the open state.
func WithFilter[T any](enabled bool) SelectOption[T] {
	return func(s *Select[T]) { s.filterOn = enabled }
}

// THE OPEN LIST IS UNDER ITS FIELD: at the field's left edge on the row below
// it, or above the field when the rows below cannot hold it, and kept on the
// screen. Centred only while the field cannot be found (no layout yet).
//
// The field is resolved from the POPUP's context: Context.ResolveAnchor answers
// relative to the node asking. The anchored placement of v0.5.26 asked from the
// field's own context, so every dropdown opened at the screen's top-left corner;
// the popup is a full-area layer, so the field relative to it is where the panel
// goes. The layer stays full-area: its outside-click dismissal depends on that.

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

// WithWidth fixes the closed field width (default: content-sized).
func WithWidth[T any](w int) SelectOption[T] {
	if w < 1 {
		panic("widget: WithWidth: width must be >= 1")
	}
	return func(s *Select[T]) { s.fixedW = w }
}

// WithSelectMaxWidth caps the content-sized field; the default is 40 cells.
// WithWidth, when supplied, remains an explicit fixed-width override.
func WithSelectMaxWidth[T any](w int) SelectOption[T] {
	return func(s *Select[T]) { s.maxW = max(w, 3) }
}

// SetFieldStyle restyles the closed field — its row and its label — as a
// palette change does at runtime; the focus look still goes on over it.
func (s *Select[T]) SetFieldStyle(st style.Style) {
	s.fieldSt = st
	s.MarkDirty()
}

// NewSelect builds a dropdown with no selection.
func NewSelect[T any](opts ...SelectOption[T]) *Select[T] {
	s := &Select[T]{
		selected:   -1,
		maxW:       40,
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
	s.RequestLayout()
}

// SetSelectedIndex chooses option i, or none for i < 0 or out of range —
// for a view keeping the same record chosen as the options around it change.
func (s *Select[T]) SetSelectedIndex(i int) {
	if i < 0 || i >= len(s.items) {
		i = -1
	}
	if s.selected == i {
		return
	}
	s.selected = i
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
	s.open, s.popup, s.layer, s.chosen = false, nil, nil, -1
}

// openPopup opens the options as a popup on the NEAREST ENCLOSING OverlayHost and emits
// OpenedEvent. The popup is the host's (popupLayer): it lies over the whole host, so a press
// outside the options closes them; it traps focus, giving it back to the field when it goes; and
// it places the options at the field's top-left corner, under the field or, where they do not fit,
// above it. A Select with no host above it does not open rather than publishing into the void.
func (s *Select[T]) openPopup() {
	if s.open {
		return
	}
	ctx := s.Context()
	host, ok := hostFor[popupHost](ctx)
	if !ok {
		return // nothing can hold the popup; opening would be a lie
	}
	panel := &selectPanel[T]{owner: s}
	layer, err := host.openPopupAt(LayerID(fmt.Sprintf("select.options.%d", ctx.ID())), s, panel, tui.Point{}, s.popupClosed)
	if err != nil {
		return
	}
	s.open, s.popup, s.layer, s.chosen = true, panel, layer, -1
	s.publish(OpenedEvent{Owner: s.NodeID()})
	s.MarkDirty()
}

// closePopup asks the popup to close, for the reason given. The options ask it from inside their own
// dispatch, so the popup goes after it (popupClosed reports it then).
func (s *Select[T]) closePopup(reason DismissReason) {
	if !s.open || s.layer == nil {
		return
	}
	s.layer.requestClose(reason)
}

// popupClosed is the popup gone, however it went (a commit, Esc, a press outside, the field
// unmounted): ClosedEvent, then, for a commit, the owner told, last, as the owner may replace the
// options.
func (s *Select[T]) popupClosed(DismissReason) {
	chosen := s.chosen
	s.open, s.popup, s.layer, s.chosen = false, nil, nil, -1
	s.publish(ClosedEvent{Owner: s.NodeID()})
	s.MarkDirty()
	if chosen >= 0 && s.onChange != nil {
		s.onChange(chosen)
	}
}

// commit installs option index i, emits SelectionChangedEvent, and closes; the owner hears of it once
// the popup has gone (popupClosed).
func (s *Select[T]) commit(i int) {
	if i >= 0 && i < len(s.items) {
		s.selected = i
		s.publish(SelectionChangedEvent{Owner: s.NodeID(), Index: i, Label: s.items[i].Label})
		s.chosen = i
	}
	s.closePopup(DismissAccept)
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
			s.errText = s.errorText()
			s.MarkDirty()
			return true
		}
		if items, ok := e.Value.([]SelectItem[T]); ok {
			s.SetOptions(items)
			return true
		}
		return false
	case tui.KeyEvent:
		if s.opens(e) {
			s.openPopup()
			return true
		}
	}
	return false
}

// opens reports whether e opens the options.
func (s *Select[T]) opens(e tui.KeyEvent) bool {
	_, ok := s.Context().KeyCommand(e, SelectOpen)
	return ok
}

// KeyScope is a closed Select's key-map scope.
func (s *Select[T]) KeyScope() keymap.Scope { return SelectScope }

// KeyBindings is the Select's command as m binds it (tui.KeyBinder).
func (s *Select[T]) KeyBindings(m *keymap.Map) []keymap.Binding { return tui.BindingsOf(m, SelectOpen) }

// Layout: closed = height 1, capped content width or WithWidth.
func (s *Select[T]) Layout(c tui.Constraints) tui.Size {
	if s.loadErr != nil {
		s.errText = s.errorText()
	}
	w := s.fixedW
	if w == 0 {
		w = min(max(max(s.longestLabel(), s.measure(s.placeholder))+3, 6), s.maxW)
	}
	sz := c.Constrain(tui.Size{W: w, H: 1})
	s.fieldW = min(w, sz.W)
	return sz
}

// errorText is the field's report of a failed load, in the App's language.
func (s *Select[T]) errorText() string {
	return withArg(s.translate(tui.Msg("tui.select.error")), s.loadErr.Error())
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
		st = mutedPlaceholder().Inherit(st)
	}
	if s.loadErr != nil {
		st = s.errSt.Inherit(st)
		label = s.errText
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
	// THE LOOK PAINTS THE FIELD, not merely its text, focused or not: a look
	// carried only by the label reached no other cell, so an empty select
	// showed no focus, and an unfocused one was its text on a box of its look
	// over whatever it sat on.
	for x := range s.fieldW {
		sur.SetCell(x, 0, " ", st)
	}
	if s.fieldW > 2 {
		drawText(sur, 0, 0, truncate(label, s.fieldW-2, sur.StringWidth), st)
	}
	if !s.affordance {
		return
	}
	arrow := "▾"
	if s.open {
		arrow = "▴"
	}
	sur.SetCell(s.fieldW-1, 0, arrow, st.Bold(true))
}

// selectPanel is the open options inside the popup: the filter line, when filtering, over the rows
// that match it. The popup (popupLayer) places it, closes it on Esc or a press outside, and traps
// focus; the panel sizes itself, takes the keys, the row presses and the wheel, and paints.
type selectPanel[T any] struct {
	Base
	owner *Select[T]

	filter  string
	matches []int // item indices surviving the filter
	hi      int   // highlight position within matches
	top     int
	size    tui.Size // as the last layout gave it
}

var _ tui.Focusable = (*selectPanel[any])(nil)

// AcceptsFocus: the popup gives its first focusable control the keyboard, and the panel is it.
func (p *selectPanel[T]) AcceptsFocus() bool { return true }

// Init highlights the current selection when it survives the (empty) filter.
func (p *selectPanel[T]) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	p.filter = ""
	p.refilter()
	for mi, idx := range p.matches {
		if idx == p.owner.selected {
			p.hi = mi
			break
		}
	}
}

// refilter recomputes the visible options (case-insensitive substring).
func (p *selectPanel[T]) refilter() {
	p.matches = p.matches[:0]
	for i, it := range p.owner.items {
		if matchFold(it.Label, p.filter) {
			p.matches = append(p.matches, i)
		}
	}
	p.hi = max(0, min(p.hi, len(p.matches)-1))
	p.top = 0
	p.RequestLayout()
}

// rows is the option-row capacity of the panel.
func (p *selectPanel[T]) rows() int {
	h := p.size.H - 2 // borders
	if p.owner.filterOn {
		h--
	}
	return max(h, 1)
}

func (p *selectPanel[T]) ensureVisible() { p.top = revealRow(p.top, p.hi, p.rows()) }

// Layout sizes the panel within the room the popup offers (the whole host): wide enough for the
// longest label and the filter, and at least the field's width; tall enough for every match, capped
// to the room with scrolling inside.
func (p *selectPanel[T]) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	labelW := max(p.owner.longestLabel(), 8)
	if p.owner.filterOn {
		labelW = max(labelW, p.owner.measure(p.filter)+2)
	}
	pw := min(max(labelW+2, p.owner.fieldW), w)
	rows := max(len(p.matches), 1)
	ph := rows + 2
	if p.owner.filterOn {
		ph++
	}
	ph = min(ph, max(h-2, 3))
	p.size = tui.Size{W: pw, H: ph}
	p.ensureVisible()
	return c.Constrain(p.size)
}

// popupKey is what a key does to the open options.
type popupKey int

const (
	popupNone popupKey = iota
	popupClose
	popupCommit
	popupUp
	popupDown
	popupPageUp
	popupPageDown
	popupFirst
	popupLast
	popupBackspace
	popupType // the key's text, onto the filter
)

var popupKeyOf = map[string]popupKey{
	SelectClose.ID: popupClose, SelectCommit.ID: popupCommit, SelectUp.ID: popupUp, SelectDown.ID: popupDown,
	SelectVimUp.ID: popupUp, SelectVimDown.ID: popupDown, SelectPageUp.ID: popupPageUp,
	SelectPageDown.ID: popupPageDown, SelectFirst.ID: popupFirst, SelectLast.ID: popupLast,
	SelectBackspace.ID: popupBackspace,
}

// keyOf is what e does to the open options: a command first; then, where the select filters, a key's
// text is typed onto the query. VIM MOTION, BUT ONLY WHERE THE LETTER IS FREE: a filtering select
// spends every printable character on the query, so j and k there are the two letters the operator
// most needs to TYPE -- "jetbrains", "sqlite". The arrows work in both modes and are what the footer
// names; this is the convenience on top.
func (p *selectPanel[T]) keyOf(e tui.KeyEvent, filterOn bool) popupKey {
	if cmd, ok := p.Context().KeyCommand(e, selectPopupCommands...); ok {
		vim := cmd.ID == SelectVimUp.ID || cmd.ID == SelectVimDown.ID
		if !vim || !filterOn {
			return popupKeyOf[cmd.ID]
		}
	}
	if filterOn && e.Kind != tui.KeyRelease && e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
		return popupType
	}
	return popupNone
}

// KeyScope is the open options' key-map scope.
func (p *selectPanel[T]) KeyScope() keymap.Scope { return SelectPopupScope }

// KeyBindings is the open options' commands as m binds them (tui.KeyBinder). Typed filter text is not
// a command.
func (p *selectPanel[T]) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, selectPopupCommands...)
}

// HandleEvent implements the open-state contract: filter typing, cursor movement, Enter commit, Esc
// close; a press on a row commits it; the wheel moves the highlight. A press outside the panel is the
// popup's: it closes them.
func (p *selectPanel[T]) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		switch p.keyOf(e, p.owner.filterOn) {
		case popupNone:
			return false
		case popupClose:
			p.owner.closePopup(DismissEscape)
		case popupCommit:
			if len(p.matches) > 0 {
				p.owner.commit(p.matches[p.hi])
			} else {
				p.owner.closePopup(DismissCancel)
			}
		case popupUp:
			p.moveHi(-1)
		case popupDown:
			p.moveHi(1)
		case popupPageUp:
			p.moveHi(-p.rows())
		case popupPageDown:
			p.moveHi(p.rows())
		case popupFirst:
			p.moveHi(-len(p.matches))
		case popupLast:
			p.moveHi(len(p.matches))
		case popupBackspace:
			if p.owner.filterOn && p.filter != "" {
				cs := clusters(p.filter)
				p.filter = strings.Join(cs[:len(cs)-1], "")
				p.refilter()
			}
		case popupType:
			p.filter += sanitizeLine(e.Text)
			p.refilter()
		}
		return true

	case tui.MouseEvent:
		switch e.Kind {
		case tui.MousePress:
			row := e.Y - 1 // the panel's own cells: its top border, then the rows
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
		return true // the open options swallow other mouse traffic
	}
	return false
}

func (p *selectPanel[T]) moveHi(delta int) {
	if len(p.matches) == 0 {
		return
	}
	p.hi = max(0, min(p.hi+delta, len(p.matches)-1))
	p.ensureVisible()
	p.MarkDirty()
}

// Render paints the panel: its frame, the filter line when filtering, and the rows from the first shown.
func (p *selectPanel[T]) Render(s tui.Surface) {
	r := tui.Rect{W: s.Size().W, H: s.Size().H}
	if r.Empty() {
		return
	}
	surface := style.New().Inherit(p.owner.fieldSt)
	if _, ok := surface.GetBackground(); !ok {
		surface = surface.Background(style.TokenSurface)
	}
	border := style.New().Foreground(style.TokenBorder).Inherit(surface)
	optSt := style.New().Inherit(surface)
	hiSt := style.New().Reverse(true).Inherit(optSt)
	filterSt := style.New().Foreground(style.TokenTextMuted).Inherit(surface)

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
	styleOf := func(mi int) style.Style {
		if mi == p.hi {
			return hiSt
		}
		return optSt
	}
	paintRows(s.Sub(tui.Rect{X: innerX, Y: y, W: innerW, H: rows}), p.top, len(p.matches), innerW, styleOf,
		func(s tui.Surface, mi, y, w int, st style.Style) {
			drawText(s, 0, y, truncate(p.owner.items[p.matches[mi]].Label, w, s.StringWidth), st)
		})
	if len(p.matches) > rows {
		sub := s.Sub(tui.Rect{X: r.X + r.W - 1, Y: y, W: 1, H: rows})
		paintScrollIndicator(sub, 0, rows, p.top, len(p.matches)-rows)
	}
}
