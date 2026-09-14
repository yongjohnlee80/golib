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
// own. It says what it is — activatable, focusable — implements two methods,
// and the runtime supplies the behaviour, identically across the terminal, the
// web backend and TestBackend.
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
// Its zero configured state is inert but usable: normal role, enabled, mouse
// enabled, default style, no callback. It is visible and focusable and does
// nothing when activated, which is the right default for a control an author
// has not finished wiring up.
type Button struct {
	Base

	label string
	role  ButtonRole
	st    *ButtonStyle

	enabled  bool
	armed    bool
	onAction func()
}

// ButtonOption configures a Button at construction.
type ButtonOption func(*Button)

// NewButton builds a button with the given label.
func NewButton(label string, opts ...ButtonOption) *Button {
	b := &Button{label: label, enabled: true}
	for _, o := range opts {
		if o != nil {
			o(b)
		}
	}
	return b
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
func (b *Button) WithPointerPolicy(p tui.PointerPolicy) *Button {
	if ctx := b.Context(); ctx != nil {
		ctx.SetPointerPolicy(p)
	}
	return b
}

// Role reports what the button means to its container.
func (b *Button) Role() ButtonRole { return b.role }

// Label reports the button's text.
func (b *Button) Label() string { return b.label }

// SetLabel replaces the text.
func (b *Button) SetLabel(s string) {
	if b.label == s {
		return
	}
	b.label = s
	b.markDirty()
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
		b.armed = false
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

// AcceptsFocus reports that a button is a tab stop.
//
// A DISABLED BUTTON STILL TAKES FOCUS. Removing it from the ring as it is
// disabled would move the user's focus out from under them mid-interaction,
// and a keyboard user would lose their place in a dialog every time a field
// validated. It focuses, shows its unavailable look, and refuses to activate.
func (b *Button) AcceptsFocus() bool { return true }

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
}

// activateKeys turns the two conventional activation keys into the runtime's
// activation action.
func activateKeys(ev tui.Event) (tui.Action, bool) {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind == tui.KeyRelease {
		return nil, false
	}
	if k.Mods != 0 {
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
	return cs.Constrain(tui.Size{W: len([]rune(b.label)) + 2, H: 1})
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

	// Centre horizontally; a label wider than the box starts at the left edge
	// and is clipped by the surface rather than overflowing it.
	w := s.StringWidth(b.label)
	x := (sz.W - w) / 2
	if x < 0 {
		x = 0
	}
	for _, r := range b.label {
		if x >= sz.W {
			break
		}
		s.SetCell(x, 0, string(r), st)
		x += s.StringWidth(string(r))
	}
}

// markDirty asks for a repaint after a visual change. Before the first mount
// there is nothing to repaint and nothing to ask.
func (b *Button) markDirty() {
	if ctx := b.Context(); ctx != nil {
		ctx.MarkDirty()
	}
}

// checkButtonRoles rejects a second Default or Cancel among buttons that share
// a container.
//
// A panic rather than a silent precedence rule: with two defaults, Enter would
// pick one of them by an ordering the author never stated, and the dialog would
// do the wrong thing in a way that looks like a toolkit bug rather than a
// mistake in the button list. Containers call this when they take their
// buttons.
func checkButtonRoles(op string, buttons []*Button) {
	var seenDefault, seenCancel bool
	for i, b := range buttons {
		if b == nil {
			continue
		}
		switch b.role {
		case ButtonRoleDefault:
			if seenDefault {
				panic(errs.Fatal{
					Op:     op,
					Rule:   "at most one ButtonRoleDefault per container",
					Detail: "second one at index " + itoa(i),
				})
			}
			seenDefault = true
		case ButtonRoleCancel:
			if seenCancel {
				panic(errs.Fatal{
					Op:     op,
					Rule:   "at most one ButtonRoleCancel per container",
					Detail: "second one at index " + itoa(i),
				})
			}
			seenCancel = true
		}
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
