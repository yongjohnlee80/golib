package widget

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

// MODAL.
//
// A dialog is three responsibilities that are easy to confuse, and the tree
// below keeps them apart:
//
//	OverlayHost (Stack)
//	└── Modal          ← FILLS the host. Owns the focus trap, the lifecycle,
//	    │                dismissal, the card's placement, and the scrim
//	    │                PREFERENCE. This node is the one named by
//	    │                OverlayDismissedEvent.
//	    └── card       ← a plain, non-focusable layout child that Modal places
//	        │            within itself. Carries the border, title and body.
//	        ├── body   ← the caller's content
//	        └── buttons
//
// The split is forced rather than tidy. A component cannot place ITSELF — its
// parent decides where it goes — so a dialog that wanted to centre its own card
// had to be two nodes. And a card-sized surface cannot paint a full-screen
// backdrop, because a component may only draw inside its own rect.
//
// Scrim ORDERING belongs to the host, not here. "Only the topmost dialog dims
// the background" is a question about global stack order, which a Modal cannot
// answer without reaching outside itself; the host already owns that order, so
// Modal states the preference and the host decides where the dimming goes.

// ErrModalAlreadyOpen is returned by Open on a Modal that is already open.
var ErrModalAlreadyOpen = errors.New("widget: modal already open")

// ErrNilOverlayHost is returned by Open when given no host.
var ErrNilOverlayHost = errors.New("widget: nil overlay host")

// Modal is a dialog layer: a focus trap filling its host, holding a placed card.
type Modal struct {
	Base

	card *modalCard

	placement ModalPlacement
	wantScrim bool
	onDismiss func(DismissReason)

	host *OverlayHost
	open bool
	// selected is the index of the focused button, or -1 when focus is on the
	// Modal node itself — which happens when it owns no enabled button.
	selected int
}

// ModalOption configures a Modal at construction.
type ModalOption func(*Modal)

// NewModal builds a dialog around the caller's content.
//
// Panics on a button list carrying two Defaults or two Cancels: at construction
// the list is written in source, so a duplicate is an authoring error. The
// runtime setter returns an error for the same rule instead, and both go
// through one validator so they cannot come to disagree.
func NewModal(body tui.Component, opts ...ModalOption) *Modal {
	m := &Modal{
		card:      newModalCard(body),
		placement: PlacementCenter,
		wantScrim: true,
		selected:  -1,
	}
	for _, o := range opts {
		if o != nil {
			o(m)
		}
	}
	checkButtonRoles("widget: NewModal", m.card.buttons)
	return m
}

// WithButtons supplies the dialog's buttons.
func WithButtons(b ...*Button) ModalOption {
	return func(m *Modal) { m.card.buttons = append([]*Button(nil), b...) }
}

// WithOnDismiss sets the callback run when the dialog closes. It runs before
// the dismissal event reaches the bus, so an observer sees settled state.
func WithOnDismiss(fn func(DismissReason)) ModalOption {
	return func(m *Modal) { m.onDismiss = fn }
}

// WithModalTitle sets the card's title text.
//
// Named WithModalTitle rather than WithTitle because the package already has a
// WithTitle for Box; this one is qualified, and ModalStyle.WithTitle — a method
// — sets the title's LOOK rather than its text.
func WithModalTitle(s string) ModalOption {
	return func(m *Modal) { m.card.title = s }
}

// WithModalStyle associates a style. The Modal does not own it; several dialogs
// may share one.
func WithModalStyle(s *ModalStyle) ModalOption {
	return func(m *Modal) { m.card.st = s }
}

// WithPlacement sets where the card sits within the host. Default is centred,
// which is what a dialog almost always wants — and what a plain stack layer
// does NOT do on its own, since a stack places an unaligned layer top-left.
//
// An invalid value is refused at construction rather than stored.
func WithPlacement(p ModalPlacement) ModalOption {
	return func(m *Modal) {
		if !p.Valid() {
			panic(tuiFatal("widget: WithPlacement",
				"value outside the declared ModalPlacement set", int(p)))
		}
		m.placement = p
	}
}

// WithScrim sets whether the background is dimmed behind this dialog. Default
// is true. This decides WHETHER; ModalStyle.WithScrim decides what it looks
// like.
func WithScrim(v bool) ModalOption {
	return func(m *Modal) { m.wantScrim = v }
}

// SetButtons replaces the dialog's buttons and reports why if it cannot.
//
// A typed error rather than a panic, because at runtime a caller supplying a
// bad list is an ordinary outcome to be handled, not a programmer error to
// abort on. It shares its rule with the construction-time panic.
//
// ATOMIC: the list is validated in full before anything changes, so a rejected
// call leaves the dialog exactly as it was rather than half-replaced. A setter
// that validated while copying would install the acceptable entries and then
// fail, which is the worst of both.
func (m *Modal) SetButtons(b ...*Button) error {
	if f := validateButtonRoles(b); f != nil {
		return fmt.Errorf("%w: %s", ErrDuplicateButtonRole, f.describe())
	}
	// The card owns the children, so it does the reconcile: the list and the
	// mounted tree are two views of the same thing and must move together.
	m.card.setButtons(b)
	m.selected = -1
	if ctx := m.Context(); ctx != nil {
		ctx.RequestLayout()
		// Focusability across the whole scope has changed: buttons appeared or
		// vanished, so the Modal node itself may have just started or stopped
		// being the fallback focus target.
		ctx.InvalidateFocusability()
	}
	return nil
}

// Buttons returns the dialog's buttons, as a fresh slice so a caller cannot
// reorder the dialog's own list by writing through it.
func (m *Modal) Buttons() []*Button {
	return append([]*Button(nil), m.card.buttons...)
}

// SelectedButton reports the index of the focused button, or -1 when focus sits
// on the Modal itself — which is the state when the dialog owns no enabled
// button at all.
func (m *Modal) SelectedButton() int { return m.selected }

// IsOpen reports whether the dialog is currently mounted in a host.
func (m *Modal) IsOpen() bool { return m.open }

// Open mounts the dialog into a host and moves focus into it.
//
// Opening one that is already open returns an error and changes NOTHING: no
// second mount, no second stack entry, no focus movement. A dialog opened twice
// by two code paths would otherwise appear twice and leave one copy
// unreachable.
//
// On failure nothing is mounted and focus is untouched.
func (m *Modal) Open(h *OverlayHost) error {
	if h == nil {
		return ErrNilOverlayHost
	}
	if m.open {
		return ErrModalAlreadyOpen
	}
	// Validate before mutating, for the same reason SetButtons does: a dialog
	// that fails to open must leave no trace.
	if f := validateButtonRoles(m.card.buttons); f != nil {
		return fmt.Errorf("%w: %s", ErrDuplicateButtonRole, f.describe())
	}
	m.host = h
	m.open = true
	h.openModal(m)
	return nil
}

// Dismiss closes the dialog and reports why.
//
// A no-op when already closed — no unmount, no callback, no event. Dismissal
// arrives from several directions at once (Escape, a Cancel button, the program
// itself), so idempotence here is what stops one closure producing three
// events.
func (m *Modal) Dismiss(reason DismissReason) {
	if !m.open {
		return
	}
	m.open = false
	owner := m.NodeID()
	host := m.host
	m.host = nil

	// The callback runs BEFORE the event is published, so anything observing
	// the bus sees state the callback has already settled.
	if m.onDismiss != nil {
		m.onDismiss(reason)
	}
	if host != nil {
		host.closeModal(m)
	}
	if ctx := m.Context(); ctx != nil {
		ctx.Bus().Publish(OverlayDismissedEvent{Owner: owner, Reason: reason})
	}
}

// TrapsFocus reports that a dialog confines input to itself. This is what stops
// a key or a click reaching the controls the dialog is covering.
func (m *Modal) TrapsFocus() bool { return true }

// AcceptsFocus reports whether the Modal NODE itself is a focus target.
//
// True exactly when the dialog owns no enabled button. The ring inside a trap
// must never be empty: with every button disabled and the Modal refusing focus
// too, there would be nothing focusable inside the trap and Escape would become
// unreachable — the dialog would be inert and uncloseable by keyboard. So the
// Modal steps in as the target of last resort, and steps out again as soon as a
// real control is available.
func (m *Modal) AcceptsFocus() bool { return m.enabledButtonCount() == 0 }

// enabledButtonCount counts the buttons that can currently be activated.
func (m *Modal) enabledButtonCount() int {
	n := 0
	for _, b := range m.card.buttons {
		if b != nil && b.Enabled() {
			n++
		}
	}
	return n
}

// Init mounts the card and installs the dialog's own key bindings as its
// DEFAULT resolver layer, so a consumer can add bindings without re-supplying
// these.
func (m *Modal) Init(ctx *tui.Context) {
	m.Base.Init(ctx)
	ctx.Mount(m.card)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(modalKeys))
}

// modalKeys turns Escape into a dismissal action. Enter is deliberately NOT
// bound here: it belongs to whichever button holds focus, and binding it at the
// dialog would shadow every button's own activation.
func modalKeys(ev tui.Event) (tui.Action, bool) {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind == tui.KeyRelease || k.Mods != 0 {
		return nil, false
	}
	if k.Code == tui.KeyEscape {
		return dismissAction{}, true
	}
	return nil, false
}

// dismissAction is the dialog's own semantic action for "close this".
type dismissAction struct{}

// ActionID returns the stable published name of this action.
func (dismissAction) ActionID() tui.ActionID { return "modal.dismiss" }

// HandleAction resolves Escape against the dialog's roles.
//
// ROLES DECIDE, never labels or position. Matching "Cancel" or "No" breaks the
// moment an application is translated, and matching the last button breaks the
// moment the list is reordered — both are real bugs that role-based resolution
// cannot have.
func (m *Modal) HandleAction(inv tui.ActionInvocation) bool {
	if _, ok := inv.Action.(dismissAction); !ok {
		return false
	}
	// A Cancel-role button, if the dialog has one, is activated so its own
	// callback runs; Escape then means exactly what pressing that button means.
	for _, b := range m.card.buttons {
		if b != nil && b.Role() == ButtonRoleCancel && b.Enabled() {
			b.Activate(inv.Origin)
			m.Dismiss(DismissCancel)
			return true
		}
	}
	m.Dismiss(DismissEscape)
	return true
}

// Layout fills the host and PLACES the card within itself.
//
// The card is measured against loose constraints so it takes its natural size,
// then positioned by the placement rule. This is the only legal way to centre
// anything: the parent decides where a child goes.
func (m *Modal) Layout(cs tui.Constraints) tui.Size {
	full := tui.Size{W: cs.MaxW, H: cs.MaxH}
	if ctx := m.Context(); ctx != nil {
		cardSize := ctx.LayoutChild(m.card, tui.Loose(full))
		ctx.PlaceChild(m.card, placeCard(m.placement, full, cardSize))
	}
	return cs.Constrain(full)
}

// placeCard positions a card of size within a host of full, by rule.
//
// Clamped at zero on both axes: a card larger than the host would otherwise be
// placed at a negative offset and have its top-left corner clipped away, losing
// the title and border rather than the overflow.
func placeCard(p ModalPlacement, full, size tui.Size) tui.Rect {
	maxX, maxY := max(full.W-size.W, 0), max(full.H-size.H, 0)
	var x, y int
	switch p {
	case PlacementTopLeft:
		x, y = 0, 0
	case PlacementTopRight:
		x, y = maxX, 0
	case PlacementBottomLeft:
		x, y = 0, maxY
	case PlacementBottomRight:
		x, y = maxX, maxY
	default: // PlacementCenter
		x, y = maxX/2, maxY/2
	}
	return tui.Rect{X: x, Y: y, W: size.W, H: size.H}
}

// Render paints nothing itself: the Modal is a full-host container whose only
// visible content is its card, and the scrim behind it belongs to the host.
func (m *Modal) Render(tui.Surface) {}

// HandleEvent tracks which button holds focus, so SelectedButton can answer
// without querying the tree.
//
// Focus events bubble from the buttons through the card to here, which is why
// the Modal can observe a selection change it did not cause.
func (m *Modal) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.FocusEvent); ok {
		m.refreshSelection()
	}
	return false
}

// refreshSelection recomputes the focused button index, or -1.
func (m *Modal) refreshSelection() {
	m.selected = -1
	for i, b := range m.card.buttons {
		if b == nil {
			continue
		}
		if ctx := b.Context(); ctx != nil && ctx.Focused() {
			m.selected = i
			return
		}
	}
}

// tuiFatal builds the package's standard construction panic.
func tuiFatal(op, rule string, got int) error {
	return fatalOf(op, rule, "got "+itoa(got))
}
