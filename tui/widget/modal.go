package widget

import (
	"errors"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
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

	placement                         ModalPlacement
	wantScrim                         bool
	onDismiss                         func(DismissReason)
	maxWidthPercent, maxHeightPercent int

	host     modalHost
	open     bool
	cardRect tui.Rect // where the card was placed at the last layout, in the modal's cells
	// vimKeys adds h/k and l/j as aliases for the directional keys.
	vimKeys bool
	// dismissKeys are additional plain keys that mean "close this", alongside
	// Escape. Empty by default: a dialog that takes text must not lose a
	// letter to dismissal, and only the host knows which of its dialogs are
	// read-only or navigational.
	dismissKeys []rune
	// keyBinder reports what a WithModalKeys handler binds (WithModalKeyBinder), for KeyBindings.
	keyBinder func(*keymap.Map) []keymap.Binding
	// selected is the index of the focused button, or -1 when focus is on the
	// Modal node itself — which happens when it owns no enabled button.
	selected int
	// ownKeys are the dialog's own keys (WithModalKeys); nil for none.
	ownKeys func(tui.KeyEvent) bool
	// acceptGate says whether an Accept answer may accept now (WithAcceptGate).
	acceptGate func() bool
}

// NewModal builds a dialog around the caller's content.
//
// Panics on a button list the validator refuses: a nil entry, the same *Button
// twice, one already mounted elsewhere, two Defaults or two Cancels, or two
// buttons declaring the same mnemonic. At construction the list is written in
// source, so any of those is an authoring error. The runtime setter returns an
// error for the same rules instead, and both go through one validator so they
// cannot come to disagree.
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
	checkButtonList("widget: NewModal", m.card.buttons, m.card)
	// The FINAL list, validated, is the one the dialog takes: an option may
	// replace an earlier one's, and a list refused above takes no button.
	m.adopt(nil, m.card.buttons)
	return m
}

// ANSWERS — Qt's QDialogButtonBox roles, answered by the dialog itself, so a
// button declares what it means and never how the dialog closes:
//
//	ButtonRoleAccept       its callback, then accepted (DismissAccept) — through
//	                       WithAcceptGate when there is one
//	ButtonRoleReject       its callback, then rejected (DismissCancel); Escape
//	                       presses it
//	ButtonRoleDestructive  its callback, then closed with no answer (DismissDiscard)
//	ButtonRoleAction       its callback only; the dialog stays
//
// A focused button answers Enter and Space itself. When another focused
// control leaves Enter unclaimed, the dialog presses its explicit default
// button (WithDefault), or does nothing when none is declared.

// adopt makes buttons the dialog's — each answers by its role — and releases
// those in was that are no longer held.
func (m *Modal) adopt(was, now []*Button) {
	held := map[*Button]bool{}
	for _, b := range now {
		held[b] = true
		b.answer = func() { m.answerFor(b) }
	}
	for _, b := range was {
		if b != nil && !held[b] {
			b.answer = nil
		}
	}
}

// answerFor is the dialog's answer for b's role, as ANSWERS lays out.
func (m *Modal) answerFor(b *Button) {
	switch b.Role() {
	case ButtonRoleAccept:
		if m.acceptGate == nil || m.acceptGate() {
			m.Dismiss(DismissAccept)
		}
	case ButtonRoleReject:
		m.Dismiss(DismissCancel)
	case ButtonRoleDestructive:
		m.Dismiss(DismissDiscard)
	}
}

// defaultButton is the dialog's enabled default button, or nil.
func (m *Modal) defaultButton() *Button {
	for _, b := range m.card.buttons {
		if b != nil && b.IsDefault() && b.Enabled() {
			return b
		}
	}
	return nil
}

// SetTitle replaces the title in the card's top border, shown as given. It ends a title set
// by [Modal.SetTitleMessage].
func (m *Modal) SetTitle(s string) {
	if m.card.title == plainText(s) {
		return
	}
	m.card.title = plainText(s)
	m.card.RequestLayout() // the title sets a floor on the card's width
}

// SetRule turns the rule under the body on or off.
func (m *Modal) SetRule(v bool) {
	if m.card.rule == v {
		return
	}
	m.card.rule = v
	m.card.RequestLayout()
}

// SetFooter replaces the help line under the buttons — for a dialog whose keys
// depend on where the keyboard is inside it. "" removes it.
func (m *Modal) SetFooter(text string) {
	if m.card.footer == plainText(text) {
		return
	}
	m.card.footer = plainText(text)
	m.card.RequestLayout()
}

// SetTitleMessage shows a catalog message as the title, in the App's language from the next
// layout.
func (m *Modal) SetTitleMessage(msg tui.Message) {
	m.card.title = messageText(msg)
	m.card.RequestLayout()
}

// SetFooterMessage shows a catalog message as the help line under the buttons, in the App's
// language from the next layout: a footer that stays right when the language changes.
func (m *Modal) SetFooterMessage(msg tui.Message) {
	m.card.footer = messageText(msg)
	m.card.RequestLayout()
}

// Footer returns the help line under the buttons.
func (m *Modal) Footer() string { return m.card.footer.inLanguage(&m.card.Base) }

// wantsScrim reports whether the dialog asked for a backdrop (WithScrim); its host places it.
func (m *Modal) wantsScrim() bool { return m.wantScrim }

// scrimStyle is the look of the dialog's backdrop: its card's style.
func (m *Modal) scrimStyle() *ModalStyle { return m.card.st }

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
	if f := validateButtonList(b, m.card); f != nil {
		return f.err()
	}
	// The card owns the children, so it does the reconcile: the list and the
	// mounted tree are two views of the same thing and must move together. It
	// batches, so the focus repair below is the only one that runs.
	was := m.card.buttons
	m.card.setButtons(b)
	m.adopt(was, m.card.buttons)
	if ctx := m.Context(); ctx != nil {
		ctx.RequestLayout()
		// Focusability across the whole scope has changed: buttons appeared or
		// vanished, so the Modal node itself may have just started or stopped
		// being the fallback focus target, and focus on a removed button must
		// move — by the dialog's Tab-order nomination.
		ctx.InvalidateFocusability()
	}
	// Selection is recomputed from where focus ACTUALLY is, after every repair
	// has run — never assigned optimistically. Clearing it to -1 here used to
	// leave a still-focused surviving button paired with "nothing selected",
	// because a focus that remains valid emits no event for the Modal to
	// observe and nothing put the number back.
	m.refreshSelection()
	return nil
}

// InitialFocus nominates where focus belongs inside this dialog: the body's
// first control in Tab order that takes focus; else its enabled default
// button; else its first enabled button; else the Modal node. An input dialog
// starts in its first field. A message, whose body takes no focus, starts on
// its default button, so Enter answers the default: a Yes/No defaulting to No
// answers Enter with No. With no default, or a disabled one, it starts on its
// first enabled button.
//
// TAB ORDER, NOT THE DEFAULT ROLE. A dialog with fields that started on its
// affirmative button pressed it with the first Space typed into what the user
// took for the first field. The default button still answers the Enter
// a focused control leaves unclaimed, so starting in the body loses nothing.
//
// This is consulted on EVERY repair, not only when the dialog opens. A dialog
// that only reached for focus at open time lost its preference the first time
// anything changed underneath it — enabling a button, replacing the list —
// because the runtime's fallback knows nothing about the dialog.
//
// The Modal node itself is the last resort, which is what keeps the ring inside
// the trap non-empty and Escape reachable when every control is disabled.
func (m *Modal) InitialFocus() (tui.Component, bool) {
	if f := firstFocusable(m.Context(), m.card.body); f != nil {
		return f, true
	}
	if b := m.defaultButton(); b != nil {
		return b, true // a focused button claims Enter: the first would answer it, not the default
	}
	for _, b := range m.card.buttons {
		if b != nil && b.Enabled() {
			return b, true
		}
	}
	return m, true
}

// WithStyle associates a style with the dialog and returns it for chaining.
//
// It restyles the LIVE dialog, card and currently-owned scrim together, so a
// theme swap takes effect on an open dialog rather than only on the next one.
// The scrim is the host's layer but wears this dialog's style, which is why it
// has to be reached from here; a dialog that is not on top owns no scrim and
// there is simply nothing extra to repaint.
//
// nil reverts to the default look, so there is no separate clear API.
func (m *Modal) WithStyle(s *ModalStyle) *Modal {
	m.card.st = s
	if ctx := m.card.Context(); ctx != nil {
		ctx.MarkDirty()
	}
	if m.host != nil {
		m.host.restyleScrim(m, s)
	}
	return m
}

// WithPointerPolicy sets whether this dialog and its WHOLE SUBTREE accept
// pointer input, and returns the dialog for chaining.
//
// Set on the Modal node, which is the subtree root, so it reaches the card, the
// body and every button through ordinary inheritance rather than being pushed
// to each. A dialog is the natural place to state this once: "this dialog is
// keyboard-only" is one decision, not one per control.
//
// The request is REMEMBERED as well as applied, because
// NewModal(...).WithPointerPolicy(...) is the natural way to write it and runs
// before there is any Context; applying it only when mounted made that chain a
// silent no-op. An invalid value is rejected before the stored policy changes.
func (m *Modal) WithPointerPolicy(p tui.PointerPolicy) *Modal {
	m.setPointerPolicy("widget: Modal.WithPointerPolicy", p)
	return m
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
	return m.openOn(h)
}

// openOn is Open on any modal host: the CommandLine opens its prompt dialog on the host it found
// (hostFor[modalHost]).
func (m *Modal) openOn(h modalHost) error {
	if m.open {
		return ErrModalAlreadyOpen
	}
	// Validate before mutating, for the same reason SetButtons does: a dialog
	// that fails to open must leave no trace. The card is not mounted yet, so
	// "mounted elsewhere" here means genuinely elsewhere.
	if f := validateButtonList(m.card.buttons, m.card); f != nil {
		return f.err()
	}
	// The host mounts, and only a successful mount commits open/host state. The
	// previous order set them first, so a descendant that failed to mount left
	// a dialog marked open with nothing on screen and no way to close it —
	// contradicting the rollback this method documents.
	if err := h.openModal(m); err != nil {
		return err
	}
	m.host = h
	m.open = true
	// Focus moves in SYNCHRONOUSLY, before Open returns. Deferring it to a
	// scheduled Update left an interval in which the dialog was mounted and
	// covering the UI while the control underneath still held focus, so input
	// already queued behind Open reached content the dialog was supposed to be
	// trapping. RequestFocus is explicitly legal before layout, so there was
	// never a reason to wait.
	m.focusInitial()
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
	// open and host are set together by a successful Open and cleared together by
	// finishDismiss, so an open dialog always has its host; there is no
	// open-but-hostless state to guard against.
	//
	// The HOST performs the transition, because dismissing a dialog that is not
	// on top means closing the ones above it first, and only the host knows the
	// order. A Modal deciding its own removal could unwind nothing above itself
	// and leave a stack with a hole in it.
	m.host.dismissModal(m, reason)
}

// finishDismiss performs one dialog's close, in the order the lifecycle
// requires. Called only by the host, once per transition.
//
// THE STRUCTURE COMES DOWN FIRST, then the callback, then the event. The old
// order ran the callback while the component was still mounted, so a callback
// that reopened the dialog — an entirely reasonable "ask again" — passed the
// open guard and panicked inside the runtime on mounting the same component
// twice. Settling the tree first makes reopening from a callback ordinary.
//
// The bus and the owner id are captured while the context is still live,
// because after the unmount there is no context to ask and the event would
// silently not be published.
func (m *Modal) finishDismiss(reason DismissReason, unmount func()) {
	// Already closed. Reachable from a callback: unwinding a stack runs each
	// dialog's onDismiss, and one of those may dismiss a dialog further down that
	// the same unwind is about to reach. Without this the second visit unmounts
	// nothing and publishes a duplicate event, so a listener counting closures
	// counts one dialog twice.
	if !m.open {
		return
	}
	m.open = false
	m.host = nil

	var bus *tui.Bus
	owner := m.NodeID()
	if ctx := m.Context(); ctx != nil {
		bus = ctx.Bus()
	}

	unmount()

	if m.onDismiss != nil {
		m.onDismiss(reason)
	}
	// Published after the callback so an observer sees state the callback has
	// already settled. Bus.Publish is enqueue-only, so a subscriber runs in a
	// later drain regardless; the ordering that matters here is that the
	// component is gone from the tree before either runs.
	if bus != nil {
		bus.Publish(OverlayDismissedEvent{Owner: owner, Reason: reason})
	}
}

// TrapsFocus reports that a dialog confines input to itself. This is what stops
// a key or a click reaching the controls the dialog is covering.
func (m *Modal) TrapsFocus() bool { return true }

// AcceptsFocus reports whether the Modal NODE itself is a focus target.
//
// True exactly when the dialog owns no enabled button and its body no control
// that takes focus. The ring inside a trap must never be empty: with every
// button disabled and the Modal refusing focus too, there would be nothing
// focusable inside the trap and Escape would become unreachable — the dialog
// would be inert and uncloseable by keyboard. So the Modal steps in as the
// target of last resort, and steps out again as soon as a real control is
// available.
func (m *Modal) AcceptsFocus() bool {
	return m.enabledButtonCount() == 0 && trapFallback(m.Context(), m.card.body)
}

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
	// Applied before the subtree exists, so every descendant inherits it as it
	// mounts rather than needing a second pass afterwards.
	ctx.Mount(m.card)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(m.keys))
}

// Layout fills the host and PLACES the card within itself.
//
// The card is measured against loose constraints so it takes its natural size,
// then positioned by the placement rule. This is the only legal way to centre
// anything: the parent decides where a child goes.
func (m *Modal) Layout(cs tui.Constraints) tui.Size {
	full := tui.Size{W: cs.MaxW, H: cs.MaxH}
	if ctx := m.Context(); ctx != nil {
		offer := full
		if m.maxWidthPercent > 0 {
			offer.W = pctOf(full.W, m.maxWidthPercent)
		}
		if m.maxHeightPercent > 0 {
			offer.H = pctOf(full.H, m.maxHeightPercent)
		}
		cardSize := ctx.LayoutChild(m.card, tui.Loose(offer))
		card := placeCard(m.placement, full, cardSize)
		ctx.PlaceChild(m.card, card)
		ctx.AfterLayout(modalCommitKey, func() { m.cardRect = card }) // CardRect's: committed, so a measure moves nothing
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
	if k, ok := ev.(tui.KeyEvent); ok && m.ownKeys != nil && k.Kind == tui.KeyPress {
		return m.ownKeys(k)
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

// focusInitial moves focus to the dialog's preferred starting control.
//
// The DEFAULT-role button is preferred unconditionally, not merely as a
// tie-break: a dialog's affirmative action is where a user expects to land, and
// choosing it only when nothing else qualified would make the preference depend
// on button order. Failing that, the first enabled button; failing that, the
// body's first focusable control; failing that, the Modal itself, which is the
// target of last resort that keeps Escape reachable.
func (m *Modal) focusInitial() {
	// InitialFocus always nominates — a button, a control, or failing those
	// the Modal node itself — so there is no "no nomination" case to handle here. Guarding for
	// one would be a branch nothing can reach.
	target, _ := m.InitialFocus()
	if ctx := m.Context(); ctx != nil {
		ctx.FocusComponent(target)
	}
	// Read back from where focus actually landed rather than assuming the
	// nomination was honoured: it can be refused, and a selection index derived
	// from the request instead of the result would then name the wrong button.
	m.refreshSelection()
}

// modalCommitKey is the Modal's card rect commit (tui.Context.AfterLayout).
const modalCommitKey tui.CommitKey = "widget.modal"
