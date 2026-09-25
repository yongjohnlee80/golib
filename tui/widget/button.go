package widget

import (
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
)

// BUTTON.
//
// This is the smallest complete interactive widget the toolkit can express, and
// it is deliberately the demonstration that the runtime layers underneath it
// carry their weight. It contains NO pointer arithmetic, no press/release
// bookkeeping, no hit-testing, no capture handling and no key binding of its
// own. It says what it is — activatable, focusable — implements Activate and
// SetArmed, and the runtime supplies the behaviour, identically across the
// terminal, the web backend and TestBackend.
//
// Everything a button traditionally re-implements lives elsewhere by now:
// press-arm/release-activate and drag-out-and-back in the gesture recogniser,
// pointer capture in the runtime, keyboard activation through a resolved
// action, and the look in a ButtonStyle it does not own.

// ButtonRole says what a button MEANS to the container holding it, which is how
// a Modal can find its default and cancel actions without reading labels.
//
// Matching on label text — "OK", "Cancel", "No" — breaks the moment an app is
// translated, and matching on position breaks the moment the buttons are
// reordered. A role is stated once by the author and survives both.
type ButtonRole uint8

const (
	// ButtonRoleNormal: no special meaning. The default.
	ButtonRoleNormal ButtonRole = iota
	// ButtonRoleDefault: the affirmative action, preferred on initial focus.
	ButtonRoleDefault
	// ButtonRoleCancel: the dismissive action, resolved by Escape.
	ButtonRoleCancel
)

// String names the role for traces and test failures.
func (r ButtonRole) String() string {
	switch r {
	case ButtonRoleNormal:
		return "normal"
	case ButtonRoleDefault:
		return "default"
	case ButtonRoleCancel:
		return "cancel"
	}
	return "unknown"
}

// Button is a labelled control that can be activated by keyboard, pointer or
// program.
//
// Its zero configured state is callback-free but ACTIVATABLE: normal role,
// enabled, mouse enabled, default style, no callback. Activating it succeeds
// and publishes ControlActivatedEvent — it simply runs no callback of its own,
// so observers on the bus still see the activation. That is the right default
// for a control an author has not finished wiring up, and it is not the same as
// inert.
type Button struct {
	Base

	label string
	role  ButtonRole
	st    *ButtonStyle
	deco  decoration
	// mnemonic is the key that reaches this button directly, or 0. METADATA
	// ONLY: the button never claims the rune itself. A standalone control that
	// grabbed an unmodified letter out of the air would break every text field
	// beside it — it is the CONTAINER, which knows the whole set and which of
	// them are enabled, that resolves one.
	mnemonic rune

	enabled  bool
	armed    bool
	onAction func()

	// pointerPolicy is the policy the author asked for, remembered so that a
	// chained WithPointerPolicy before mount is applied when the Context
	// arrives rather than silently discarded.
	pointerPolicy tui.PointerPolicy
}

// ButtonOption configures a Button at construction.
// decoration is what a button is wrapped in so it reads as a control.
//
// A terminal has no raised edge to make a word look pressable, so a bare label
// in a row of prose is indistinguishable from the prose. Brackets are the
// convention that has stood in for that edge since curses.
type decoration struct{ open, close string }

// defaultDecoration is the conventional pair. Replaceable, and removable by
// passing two empty strings, for a design that supplies its own frame.
var defaultDecoration = decoration{"[", "]"}

type ButtonOption func(*Button)

// NewButton builds a button with the given label.
func NewButton(label string, opts ...ButtonOption) *Button {
	b := &Button{label: label, enabled: true, deco: defaultDecoration}
	for _, o := range opts {
		if o != nil {
			o(b)
		}
	}
	return b
}

// WithMnemonic sets the key that reaches this button directly, such as 'y' on
// a Yes button.
//
// The button records it and underlines it IN THE LABEL WHEN THE LABEL CONTAINS
// IT — a mnemonic naming a letter the label does not have still works as a key
// and simply has nothing to mark, which is a legitimate shape for an icon or a
// translated label. It does NOT bind the key.
// Resolution belongs to the container: a Modal matches a keystroke against its
// own enabled buttons, so the same metadata can later mean something slightly
// different in a toolbar or a form without every button in the tree competing
// for unmodified letters.
func WithMnemonic(r rune) ButtonOption {
	return func(b *Button) { b.mnemonic = r }
}

// WithButtonDecoration replaces the pair a button is wrapped in. Two empty
// strings remove it, leaving the bare label for a caller framing the control
// some other way.
func WithButtonDecoration(open, close string) ButtonOption {
	return func(b *Button) { b.deco = decoration{open, close} }
}

// WithRole sets what the button means to its container.
//
// An unknown value is RETAINED rather than rejected, and treated as normal by
// containers that do not recognise it. Application-specific roles are therefore
// additive: an app can define its own and carry it through the toolkit without
// the toolkit needing to know what it means.
func WithRole(r ButtonRole) ButtonOption {
	return func(b *Button) { b.role = r }
}

// WithButtonStyle associates a style at construction. The Button does not own
// it; several buttons may share one.
func WithButtonStyle(s *ButtonStyle) ButtonOption {
	return func(b *Button) { b.st = s }
}

// WithOnActivate sets the callback run when the button is activated.
//
// It runs synchronously inside the activation, before the activation event
// reaches the bus, so anything observing that event sees state the callback has
// already settled.
func WithOnActivate(fn func()) ButtonOption {
	return func(b *Button) { b.onAction = fn }
}

// WithStyle associates a style at runtime and returns the button, so it can be
// chained onto a constructor.
//
// Named WithStyle rather than SetStyle because it reads as configuration and
// chains; the package-level WithStyle belongs to Box and does not collide with
// a method.
func (b *Button) WithStyle(s *ButtonStyle) *Button {
	b.st = s
	b.markDirty()
	return b
}

// WithPointerPolicy sets whether this button and its subtree accept pointer
// input, and returns the button for chaining.
//
// A runtime method rather than a construction option because the option name
// cannot be shared across widget option types, and because turning the mouse
// off is usually a decision made while the app is running.
// The request is REMEMBERED as well as applied. NewButton(...).WithPointerPolicy(...)
// is the most natural way to write this and runs before there is any Context;
// applying it only when mounted made that chain a silent no-op.
//
// An invalid value is rejected before the stored policy changes, so a refused
// call leaves the previous request intact rather than half-applied.
func (b *Button) WithPointerPolicy(p tui.PointerPolicy) *Button {
	if !p.Valid() {
		panic(errs.Fatal{
			Op:     "widget: Button.WithPointerPolicy",
			Rule:   "value outside PointerInherit, PointerEnabled, PointerDisabled",
			Detail: "got " + itoa(int(p)),
		})
	}
	b.pointerPolicy = p
	if ctx := b.Context(); ctx != nil {
		ctx.SetPointerPolicy(p)
	}
	return b
}

// Mnemonic is the key that reaches this button directly, or 0 for none. The
// button records it and marks it in the label; the CONTAINER resolves it.
func (b *Button) Mnemonic() rune { return b.mnemonic }

// Role reports what the button means to its container.
func (b *Button) Role() ButtonRole { return b.role }

// SetRole changes what the button means to its container — for a container
// giving the buttons it adopts their answers (a dialog's button box).
func (b *Button) SetRole(r ButtonRole) { b.role = r }

// SetMnemonic changes the key that reaches the button directly; 0 for none.
func (b *Button) SetMnemonic(r rune) {
	if b.mnemonic == r {
		return
	}
	b.mnemonic = r
	b.MarkDirty()
}

// OnActivate is what the button runs when activated, nil for nothing.
func (b *Button) OnActivate() func() { return b.onAction }

// SetOnActivate replaces what the button runs when activated — for a
// container that adds its own step after the button's own (a dialog's answer).
func (b *Button) SetOnActivate(fn func()) { b.onAction = fn }

// Label reports the button's text.
func (b *Button) Label() string { return b.label }

// SetLabel replaces the text.
func (b *Button) SetLabel(s string) {
	if b.label == s {
		return
	}
	b.label = s
	// RequestLayout, not MarkDirty: the label IS the button's intrinsic width,
	// so a parent that only repainted would keep stale geometry and stale hit
	// bounds after a short-to-long change. RequestLayout schedules the repaint
	// too, so a second invalidation would be redundant.
	b.RequestLayout()
}

// Enabled reports whether the button can be activated.
func (b *Button) Enabled() bool { return b.enabled }

// SetEnabled turns activation on or off.
//
// Disabling while the button is armed also disarms it: the pressed look
// describes a gesture that can no longer complete, and leaving it would show a
// control held down that nothing will ever release.
func (b *Button) SetEnabled(v bool) {
	if b.enabled == v {
		return
	}
	b.enabled = v
	if !v {
		// Cancel first, so disabling mid-press releases the capture through the
		// ordinary loss path and the owner is told once, rather than being left
		// holding a gesture that can no longer produce anything.
		if ctx := b.Context(); ctx != nil {
			ctx.CancelGesture()
		}
		b.armed = false
	}
	// Focusability just changed, so the active scope has to be revalidated
	// SYNCHRONOUSLY and on BOTH transitions.
	//
	// On disable, the focused node may be this one and is now ineligible. On
	// ENABLE it is just as necessary and less obvious: enabling the first
	// control in a dialog where everything was disabled makes the dialog itself
	// stop being the fallback focus, which is a change to a node other than
	// this one.
	if ctx := b.Context(); ctx != nil {
		ctx.InvalidateFocusability()
	}
	b.markDirty()
}

// Activate triggers the button and reports whether it happened.
//
// THE DISABLED CHECK LIVES HERE AND ONLY HERE. Every producer — a key
// resolver, the gesture recogniser, a container calling DoAction — reaches this
// one method, so none of them has to ask first and none of them can disagree
// about the answer.
//
// An unmounted button refuses too: its callback may reference a tree that no
// longer exists.
func (b *Button) Activate(origin tui.ActionOrigin) bool {
	if !b.enabled || b.Context() == nil {
		return false
	}
	if b.onAction != nil {
		b.onAction()
	}
	return true
}

// SetArmed shows or clears the pressed look. Visual only — the runtime drives
// it, on transitions, and guarantees the button ends up unarmed however the
// gesture finishes.
//
// A disabled button never shows armed, so that disabling mid-press cannot leave
// a control looking pressed and unavailable at once.
func (b *Button) SetArmed(v bool) {
	if v && !b.enabled {
		return
	}
	if b.armed == v {
		return
	}
	b.armed = v
	b.markDirty()
}

// Armed reports whether the button is currently showing pressed.
func (b *Button) Armed() bool { return b.armed }

// AcceptsFocus reports whether a button is currently a tab stop.
//
// A DISABLED BUTTON LEAVES THE RING. Tab must not stop on a control that cannot
// be used, and the containers built on this depend on it: a dialog cycles its
// enabled buttons, prefers the enabled default on opening, and falls back to
// itself only when every button is disabled. None of that is expressible while
// disabled controls remain focusable.
//
// Focus already ON a disabled button is repaired synchronously by SetEnabled
// rather than left dangling.
func (b *Button) AcceptsFocus() bool { return b.enabled }

// ActivationAvailable reports whether activating this button could do anything.
//
// The runtime uses it to decide whether starting a pointer gesture is
// worthwhile. It is a routing hint only: Activate is still the authority, and
// still refuses on its own.
func (b *Button) ActivationAvailable() bool { return b.enabled }

// State reports the look that applies right now, by the toolkit's one
// precedence rule.
func (b *Button) State() WidgetState {
	return resolveWidgetState(!b.enabled, b.armed, b.focused())
}

// Init installs the button's own key bindings as its DEFAULT resolver layer, so
// a consumer can add bindings without having to re-supply these.
//
// Enter and Space are resolved into the same ActivateAction the pointer gesture
// produces, which is what makes keyboard and mouse a single path: the button
// implements activation once and does not care which arrived.
func (b *Button) Init(ctx *tui.Context) {
	b.Base.Init(ctx)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(activateKeys))
	// Apply a policy requested before there was a Context to apply it to.
	if b.pointerPolicy != tui.PointerInherit {
		ctx.SetPointerPolicy(b.pointerPolicy)
	}
}

// activateKeys turns the two conventional activation keys into the runtime's
// activation action.
func activateKeys(ev tui.Event) (tui.Action, bool) {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind == tui.KeyRelease {
		return nil, false
	}
	if k.Mods.Chord() != 0 {
		return nil, false // Ctrl-Enter and friends are somebody else's binding
	}
	if k.Code == tui.KeyEnter || k.Code == ' ' {
		return tui.ActivateAction{}, true
	}
	return nil, false
}

// Layout sizes the button to its label plus one cell of padding either side,
// clamped to the offered constraints.
func (b *Button) Layout(cs tui.Constraints) tui.Size {
	// measure, not a rune count. A rune count is wrong in three separate ways:
	// a CJK ideograph occupies two columns, a combining mark occupies none, and
	// an emoji ZWJ sequence is many runes in one cell. Base.measure asks the
	// mounted Context for the active width policy, so layout agrees with what
	// the backend will actually draw.
	// The DECORATED form, because that is the string Render paints. Measuring
	// the bare label here would size the button two cells short of its own
	// brackets and clip the closing one.
	if d := b.decorated(); d != b.label {
		return cs.Constrain(tui.Size{W: b.measure(d), H: 1})
	}
	return cs.Constrain(tui.Size{W: b.measure(b.label) + 2, H: 1})
}

// decorated is the label as it is painted: "[ Save ]" by default, or the bare
// label when the decoration has been cleared.
func (b *Button) decorated() string {
	if b.deco.open == "" && b.deco.close == "" {
		return b.label
	}
	return b.deco.open + " " + b.label + " " + b.deco.close
}

// Render paints the label centred in the button's current look.
//
// It asks State for the look rather than choosing one, so the precedence rule
// lives in one place and this cannot drift from the rest of the toolkit.
func (b *Button) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	st := b.st.Style(b.State())
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, " ", st)

	// Centre using the SURFACE's policy, which is the one that will draw this.
	// Layout used the Context's; they are the same policy, and taking each from
	// its own phase is what keeps them so.
	painted := b.decorated()
	w := s.StringWidth(painted)
	x := (sz.W - w) / 2
	if x < 0 {
		x = 0
	}
	// Iterate extended grapheme CLUSTERS, not runes. A cluster is the unit the
	// terminal draws: painting rune by rune puts a zero-width combining mark in
	// its own cell, where it overwrites the character it belongs to.
	marked := false
	for cluster := range tui.Graphemes(painted) {
		cw := s.StringWidth(cluster)
		// Stop before writing a cluster that does not fit. Half of a
		// double-width cluster in the final column is a broken cell rather than
		// a truncated string.
		if x+cw > sz.W {
			break
		}
		cst := st
		// THE FIRST MATCHING CLUSTER ONLY. A mnemonic names one key, so marking
		// every "o" in "Choose Folder" would advertise three ways in where
		// there is one.
		if !marked && b.mnemonic != 0 && eqFold([]rune(cluster)[0], b.mnemonic) {
			cst = st.Underline(true)
			marked = true
		}
		s.SetCell(x, 0, cluster, cst)
		x += cw
	}
}

// markDirty asks for a repaint after a visual change. Before the first mount
// there is nothing to repaint and nothing to ask.
func (b *Button) markDirty() {
	if ctx := b.Context(); ctx != nil {
		ctx.MarkDirty()
	}
}

// checkButtonList is the CONSTRUCTION adapter over validateButtonList.
//
// A panic rather than a silent precedence rule: with two defaults, Enter would
// pick one by an ordering the author never stated, and the dialog would do the
// wrong thing in a way that reads as a toolkit bug rather than a mistake in the
// button list. Construction options have no error to return, and the list is
// written in source, so this is a programmer error.
//
// It shares its rules with the runtime setter's typed error rather than
// restating them, so the two cannot come to disagree about what a valid list is.
func checkButtonList(op string, buttons []*Button, owner tui.Component) {
	if f := validateButtonList(buttons, owner); f != nil {
		panic(errs.Fatal{Op: op, Rule: f.describe()})
	}
}

// itoa renders a small non-negative int for a panic detail.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
