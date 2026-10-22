package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// MODAL KEYS: how a dialog answers its keys (Escape and its dismiss keys, the default button's
// Enter, mnemonics, focus stepping) through its own actions.

// keys handles Escape and the dialog's fallback Enter. Routing starts at the
// focused control, so a focused button claims Enter before it reaches here.
func (m *Modal) keys(ev tui.Event) (tui.Action, bool) {
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind == tui.KeyRelease {
		return nil, false
	}
	cmd, isCmd := m.Context().KeyCommand(k, modalCommands...)
	switch {
	case isCmd && cmd.ID == ModalDismiss.ID:
		return dismissAction{}, true
	case isCmd && cmd.ID == ModalAccept.ID:
		// ENTER, unclaimed by the control that has focus: the default button's,
		// through the runtime as a mnemonic is. None declared, no answer.
		if b := m.defaultButton(); b != nil {
			return modalActivateAction{target: b}, true
		}
		return nil, false
	}
	if k.Mods.Chord() == 0 {
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
	}
	switch {
	case !isCmd:
	case cmd.ID == ModalPrev.ID, cmd.ID == ModalVimPrev.ID && m.vimKeys:
		return modalStepAction{delta: -1}, true
	case cmd.ID == ModalNext.ID, cmd.ID == ModalVimNext.ID && m.vimKeys:
		return modalStepAction{delta: +1}, true
	}
	return nil, false
}

// KeyScope is a Modal's key-map scope.
func (m *Modal) KeyScope() keymap.Scope { return ModalScope }

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
			eqFold(b.Mnemonic(), r) {
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
	switch a := actionValue(inv.Action).(type) {
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
	if _, ok := actionValue(inv.Action).(dismissAction); !ok {
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

// KeyBindings is the Modal's commands as m binds them, then what the host's WithModalKeys consults if
// it said so with WithModalKeyBinder (tui.KeyBinder). Its buttons' mnemonics and a host's
// WithModalDismissKeys are not commands, and are not listed.
func (m *Modal) KeyBindings(km *keymap.Map) []keymap.Binding {
	out := tui.BindingsOf(km, modalCommands...)
	if m.keyBinder != nil {
		out = append(out, m.keyBinder(km)...)
	}
	return out
}
