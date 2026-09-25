package widget

import (
	"errors"

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
	// pointerPolicy is the subtree-wide pointer setting, remembered so a
	// chained WithPointerPolicy before mount is applied when the Context
	// arrives rather than being silently dropped.
	pointerPolicy tui.PointerPolicy
	// vimKeys adds h/k and l/j as aliases for the directional keys.
	vimKeys bool
	// dismissKeys are additional plain keys that mean "close this", alongside
	// Escape. Empty by default: a dialog that takes text must not lose a
	// letter to dismissal, and only the host knows which of its dialogs are
	// read-only or navigational.
	dismissKeys []rune
	// selected is the index of the focused button, or -1 when focus is on the
	// Modal node itself — which happens when it owns no enabled button.
	selected int
	// ownKeys are the dialog's own keys (WithModalKeys); nil for none.
	ownKeys func(tui.KeyEvent) bool
	// acceptGate says whether an Accept answer may accept now (WithAcceptGate).
	acceptGate func() bool
}

// ModalOption configures a Modal at construction.
type ModalOption func(*Modal)

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
		card:          newModalCard(body),
		placement:     PlacementCenter,
		wantScrim:     true,
		selected:      -1,
		pointerPolicy: tui.PointerInherit,
	}
	for _, o := range opts {
		if o != nil {
			o(m)
		}
	}
	checkButtonList("widget: NewModal", m.card.buttons, m.card)
	return m
}

// WithButtons supplies the dialog's buttons. The list is validated at
// construction; see [NewModal] for what is refused.
func WithButtons(b ...*Button) ModalOption {
	return func(m *Modal) {
		m.card.buttons = append([]*Button(nil), b...)
		m.adopt(nil, m.card.buttons)
	}
}

// WithAcceptGate says whether an Accept answer may accept NOW: a file dialog
// with nothing chosen yet. A refused accept leaves the dialog open. The button's
// own callback has run either way.
func WithAcceptGate(fn func() bool) ModalOption {
	return func(m *Modal) { m.acceptGate = fn }
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
// ENTER IS THE DIALOG'S: a dialog's buttons leave it unclaimed, and the dialog
// presses its default button (WithDefault) — or nothing, when none is declared,
// so an irreversible answer is never one stray Enter away. Space presses the
// focused button.

// adopt makes buttons the dialog's — each answers by its role, and leaves
// Enter to the dialog — and releases those in was that are no longer held.
func (m *Modal) adopt(was, now []*Button) {
	held := map[*Button]bool{}
	for _, b := range now {
		held[b] = true
		b.inDialog = true
		b.answer = func() { m.answerFor(b) }
	}
	for _, b := range was {
		if b != nil && !held[b] {
			b.inDialog, b.answer = false, nil
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

// WithModalRule draws a line across the card between the body and the buttons,
// joined to the frame at both ends: the message above it, the decision below.
// Without it the two are separated by a blank line.
func WithModalRule(v bool) ModalOption {
	return func(m *Modal) { m.card.rule = v }
}

// WithModalKeys gives the dialog keys of its own: fn sees every key press the
// controls inside it leave unconsumed, while it is open, and reports whether it
// took it — a viewer's `y` copying, say.
func WithModalKeys(fn func(tui.KeyEvent) bool) ModalOption {
	return func(m *Modal) { m.ownKeys = fn }
}

// WithModalWidth sets the card's width in cells, frame included, where it
// would otherwise take its content's: a prompt whose field would fill the
// screen. Clamped to the host; 0 is the content's width.
func WithModalWidth(w int) ModalOption {
	return func(m *Modal) { m.card.width = max(w, 0) }
}

// WithModalFooter sets a line of help text under the buttons — the keys the
// dialog answers to, say. It belongs to the CARD, not the body, so the buttons
// stay the card's and keep their mnemonics: a body cannot resolve a bare
// letter, which is what moving the buttons into one to get a footer below
// them used to cost.
func WithModalFooter(text string) ModalOption {
	return func(m *Modal) { m.card.footer = text }
}

// SetTitle replaces the title in the card's top border.
func (m *Modal) SetTitle(s string) {
	if m.card.title == s {
		return
	}
	m.card.title = s
	if ctx := m.card.Context(); ctx != nil {
		ctx.RequestLayout() // the title sets a floor on the card's width
	}
}

// SetRule turns the rule under the body on or off.
func (m *Modal) SetRule(v bool) {
	if m.card.rule == v {
		return
	}
	m.card.rule = v
	if ctx := m.card.Context(); ctx != nil {
		ctx.RequestLayout()
	}
}

// SetFooter replaces the help line under the buttons — for a dialog whose keys
// depend on where the keyboard is inside it. "" removes it.
func (m *Modal) SetFooter(text string) {
	if m.card.footer == text {
		return
	}
	m.card.footer = text
	if ctx := m.card.Context(); ctx != nil {
		ctx.RequestLayout()
	}
}

// Footer returns the help line under the buttons.
func (m *Modal) Footer() string { return m.card.footer }

// WithModalStyle associates a style. The Modal does not own it; several dialogs
// may share one.
func WithModalStyle(s *ModalStyle) ModalOption {
	return func(m *Modal) { m.card.st = s }
}

// ButtonAlign is where a dialog's row of buttons sits within its card.
//
// The zero value CENTRES them, which is the conventional look for a
// confirmation and the one a caller who says nothing should get: a pair of
// controls hugging one edge of a card wider than they are reads as detached
// from the question above.
type ButtonAlign uint8

const (
	// ButtonsCenter centres the row. The default.
	ButtonsCenter ButtonAlign = iota
	// ButtonsLeft packs the row against the leading edge.
	ButtonsLeft
	// ButtonsRight packs it against the trailing edge, the placement a form-like
	// dialog with a wide body usually wants.
	ButtonsRight
)

// Valid reports whether a is one of the declared alignments.
func (a ButtonAlign) Valid() bool { return a <= ButtonsRight }

// String names the alignment for traces and test failures.
func (a ButtonAlign) String() string {
	switch a {
	case ButtonsCenter:
		return "center"
	case ButtonsLeft:
		return "left"
	case ButtonsRight:
		return "right"
	}
	return "unknown"
}

// WithModalVimNavigation adds h/k and l/j as aliases for the arrow keys that
// move between a dialog's buttons. Off by default.
//
// OPT-IN, because h and l are ordinary letters: a dialog whose buttons carry
// mnemonics may legitimately answer to one of them, and a consumer that is not
// a Vim-shaped application should not have two of its letters quietly taken.
// An explicit mnemonic still wins over an alias, so enabling this cannot make
// a declared key unreachable.
func WithModalVimNavigation(v bool) ModalOption {
	return func(m *Modal) { m.vimKeys = v }
}

// WithModalDismissKeys adds plain keys that close the dialog, alongside Escape.
//
// A configured key is an ESCAPE EQUIVALENT in full: it resolves through the same
// path, so a dialog with a Cancel-role button has that button ACTIVATED, and the
// dismissal is reported as DismissEscape — the reason names the intention, not
// the key. A key that closed the dialog without running Cancel would be a
// second, quieter way out with different consequences.
//
// A terminal application usually has a second dismiss key — `q` is the common
// one — and a dialog that traps focus swallows it, so a surface that honoured it
// as a Float stops honouring it as a Modal with nothing to say so. The keys
// belong to the HOST rather than to this widget: only the application knows
// which of its dialogs a reader navigates and which take typed input, and a `q`
// that closes the dialog is a `q` the operator cannot type into it.
//
// RESOLVED AFTER MNEMONICS AND AFTER THE FOCUSED CONTROL. A declared mnemonic is
// specific intent and wins; and a dismiss key never reaches this while a control
// that consumes text has focus, because the runtime offers the key there first.
// Both orderings matter: a dialog whose Cancel button is mnemonic `q` keeps
// activating Cancel, and a dialog containing a text input keeps typing `q`.
//
// Modified keys are never dismiss keys — Ctrl-Q is not `q`.
func WithModalDismissKeys(keys ...rune) ModalOption {
	return func(m *Modal) {
		m.dismissKeys = append(m.dismissKeys, keys...)
	}
}

// WithButtonAlign sets where the button row sits. An invalid value is refused
// at construction rather than stored, like every other closed set here.
func WithButtonAlign(a ButtonAlign) ModalOption {
	return func(m *Modal) {
		if !a.Valid() {
			panic(tuiFatal("widget: WithButtonAlign",
				"value outside the declared ButtonAlign set", int(a)))
		}
		m.card.align = a
	}
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
		// being the fallback focus target, and the dialog's Default-role
		// preference may now name a different control.
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

// InitialFocus nominates where focus belongs inside this dialog: the enabled
// Default-role button, else the first enabled button, else the first control
// in the body that takes focus — a prompt's field, as a Qt Dialog's content
// takes it — else the Modal node.
//
// The DEFAULT ROLE IS PREFERRED UNCONDITIONALLY, not as a tie-break. A dialog's
// affirmative action is where a user expects to land, and choosing it only when
// nothing else qualified would make the landing spot depend on the order the
// buttons happened to be listed in.
//
// This is consulted on EVERY repair, not only when the dialog opens. A dialog
// that only reached for focus at open time lost its preference the first time
// anything changed underneath it — enabling a button, replacing the list — because
// the runtime's fallback is document order and knows nothing about roles.
//
// The Modal node itself is the last resort, which is what keeps the ring inside
// the trap non-empty and Escape reachable when every control is disabled.
func (m *Modal) InitialFocus() (tui.Component, bool) {
	if b := m.defaultButton(); b != nil {
		return b, true
	}
	for _, b := range m.card.buttons {
		if b != nil && b.Enabled() {
			return b, true
		}
	}
	if f := firstFocusable(m.card.body); f != nil {
		return f, true
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
	if !p.Valid() {
		panic(tuiFatal("widget: Modal.WithPointerPolicy",
			"value outside PointerInherit, PointerEnabled, PointerDisabled", int(p)))
	}
	m.pointerPolicy = p
	if ctx := m.Context(); ctx != nil {
		ctx.SetPointerPolicy(p)
	}
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
	return m.enabledButtonCount() == 0 && firstFocusable(m.card.body) == nil
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
	ctx.SetPointerPolicy(m.pointerPolicy)
	ctx.Mount(m.card)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(m.keys))
}

// modalKeys turns Escape into a dismissal action. Enter is deliberately NOT
// bound here: it belongs to whichever button holds focus, and binding it at the
// dialog would shadow every button's own activation.
func (m *Modal) keys(ev tui.Event) (tui.Action, bool) {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind == tui.KeyRelease || k.Mods.Chord() != 0 {
		return nil, false
	}
	if k.Code == tui.KeyEscape {
		return dismissAction{}, true
	}
	// ENTER, unclaimed by the control that has focus: the default button's,
	// through the runtime as a mnemonic is. None declared, no answer.
	if k.Code == tui.KeyEnter {
		if b := m.defaultButton(); b != nil {
			return modalActivateAction{target: b}, true
		}
		return nil, false
	}
	// MNEMONIC FIRST, ALWAYS. A declared key is specific intent; a direction is
	// a convenience. Resolving the alias first would make a button whose
	// mnemonic happens to be 'l' unreachable the moment Vim keys were enabled,
	// and the author who declared it would have no way to know why.
	if b := m.mnemonicButton(k.Code); b != nil {
		return modalActivateAction{target: b}, true
	}
	// A HOST DISMISS KEY, after the mnemonics. See WithModalDismissKeys for why the
	// order is load-bearing in both directions.
	for _, r := range m.dismissKeys {
		if k.Code == r {
			return dismissAction{}, true
		}
	}
	switch k.Code {
	case tui.KeyLeft, tui.KeyUp:
		return modalStepAction{delta: -1}, true
	case tui.KeyRight, tui.KeyDown:
		return modalStepAction{delta: +1}, true
	}
	if m.vimKeys {
		switch k.Code {
		case 'h', 'k':
			return modalStepAction{delta: -1}, true
		case 'l', 'j':
			return modalStepAction{delta: +1}, true
		}
	}
	return nil, false
}

// modalStepAction moves the focus between a dialog's buttons.
type modalStepAction struct{ delta int }

func (modalStepAction) ActionID() tui.ActionID { return "modal.step" }

// modalActivateAction presses the button a mnemonic names.
type modalActivateAction struct{ target *Button }

func (modalActivateAction) ActionID() tui.ActionID { return "modal.activate" }

// mnemonicButton is the ENABLED button answering to r, or nil.
//
// Enabled only: a greyed control does not answer its key, for the same reason
// it does not answer Enter. The list is validated against two enabled buttons
// claiming one key, so the first match is the only match.
func (m *Modal) mnemonicButton(r rune) *Button {
	if r == 0 {
		return nil
	}
	for _, b := range m.card.buttons {
		if b != nil && b.Enabled() && b.Mnemonic() != 0 &&
			lowerRune(b.Mnemonic()) == lowerRune(r) {
			return b
		}
	}
	return nil
}

// stepFocus moves focus to the next enabled button in the given direction,
// wrapping, and skipping the disabled.
//
// Anchored on the button that HAS focus rather than on a remembered index: the
// user may have arrived by Tab or by clicking, and an index of our own would
// disagree with the runtime about where they are.
func (m *Modal) stepFocus(delta int) bool {
	ctx := m.Context()
	if ctx == nil || len(m.card.buttons) == 0 {
		return false
	}
	cur := -1
	for i, b := range m.card.buttons {
		if b == nil {
			continue
		}
		if bc := b.Context(); bc != nil && bc.Focused() {
			cur = i
			break
		}
	}
	n := len(m.card.buttons)
	for step := 1; step <= n; step++ {
		j := ((cur+delta*step)%n + n) % n
		if b := m.card.buttons[j]; b != nil && b.Enabled() {
			ctx.FocusComponent(b)
			return true
		}
	}
	return false
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
	switch a := inv.Action.(type) {
	case modalStepAction:
		return m.stepFocus(a.delta)
	case modalActivateAction:
		// THROUGH THE RUNTIME, never by calling Activate directly. The runtime
		// is the sole publisher of ControlActivatedEvent, so a direct call
		// would run the button's own callback and nothing else: a command log,
		// an undo stack or a test watching the bus would see a keystroke that
		// activated nothing. ForwardAction carries this invocation's
		// provenance, so a mnemonic press is recorded as the keyboard event it
		// was — identical to Enter and to a click.
		if ctx := m.Context(); ctx != nil {
			return ctx.ForwardAction(a.target, tui.ActivateAction{})
		}
		return false
	}
	if _, ok := inv.Action.(dismissAction); !ok {
		return false
	}
	// A Reject-role button, if the dialog has one, is activated so a DISMISS
	// REQUEST means exactly what pressing that button means — whether the
	// request arrived as Escape or as a host-configured dismiss key. The
	// resolution is the same for both, deliberately: a `q` that closed the
	// dialog without running Cancel would be a second, quieter way out with
	// different consequences.
	//
	// Forwarded through the RUNTIME rather than by calling Button.Activate
	// directly. The runtime is the sole publisher of ControlActivatedEvent, so a
	// direct call ran the button's own callback and nothing else: anything
	// watching the bus for activations — a command log, an undo stack, a test —
	// saw a keypress that activated nothing, and the equivalence this comment
	// claims was not one the code kept. ForwardAction carries this invocation's
	// provenance, so the activation is recorded as the keyboard event it was.
	for _, b := range m.card.buttons {
		if b != nil && b.Role() == ButtonRoleReject && b.Enabled() {
			if ctx := m.Context(); ctx != nil {
				ctx.ForwardAction(b, tui.ActivateAction{})
			}
			// Dismiss even if the button refused: Escape closes the dialog, and
			// a Cancel handler that declined is not a reason to trap the user.
			// Idempotent, so a callback that dismissed already costs nothing.
			m.Dismiss(DismissCancel)
			return true
		}
	}
	// No Reject role: the dialog simply closes. DismissEscape is the reason for
	// either key, because it names the INTENTION — leaving without choosing —
	// rather than the physical key that carried it.
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

// tuiFatal builds the package's standard construction panic.
func tuiFatal(op, rule string, got int) error {
	return fatalOf(op, rule, "got "+itoa(got))
}
