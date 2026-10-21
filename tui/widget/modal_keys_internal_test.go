package widget

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldModalKey is Modal.keys and oldActivateKeys the Button's activateKeys at golib 6a0b271, verbatim
// but for their names: the oracles of the no-map differential, which compares the actions resolved.
func oldModalKey(m *Modal, ev tui.Event) (tui.Action, bool) {
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

func oldActivateKeys(ev tui.Event) (tui.Action, bool) {
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

func TestModalKeysMatchTheOldResolverWithNoMap(t *testing.T) {
	for _, vim := range []bool{false, true} {
		for _, dismiss := range [][]rune{nil, {'q'}} {
			for _, withDefault := range []bool{false, true} {
				m := NewModal(NewText("body"), WithModalVimNavigation(vim), WithModalDismissKeys(dismiss...))
				if err := m.SetButtons(
					NewButton("OK", WithMnemonic('o'), WithDefault(withDefault)),
					NewButton("Kill", WithMnemonic('k')), // a mnemonic on a vim key: the mnemonic wins
				); err != nil {
					t.Fatal(err)
				}
				show := func(a tui.Action, ok bool) string { return fmt.Sprintf("%v %#v", ok, a) }
				keyGridDiff(t, fmt.Sprintf("Modal (vim %v, dismiss %q, default %v)", vim, dismiss, withDefault),
					func(e tui.KeyEvent) string { return show(oldModalKey(m, e)) },
					func(e tui.KeyEvent) string { return show(m.keys(e)) },
					noTextShiftedPrintable)
			}
		}
	}
}

func TestButtonActivationMatchesTheOldResolverWithNoMap(t *testing.T) {
	show := func(a tui.Action, ok bool) string { return fmt.Sprintf("%v %#v", ok, a) }
	keyGridDiff(t, "Button",
		func(e tui.KeyEvent) string { return show(oldActivateKeys(e)) },
		func(e tui.KeyEvent) string { return show(activateKeys(noKeymap, e)) },
		shiftSpentOnText)
}
