package widget

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldMenuKey is Menu.resolveKey at golib 6a0b271, verbatim but for its receiver, and oldVimDirection
// the helper it called: the oracle of the no-map differential. The bar and column logic it calls is
// unchanged, so the differential compares the actions the two resolve.
func oldMenuKey(m *Menu, e tui.KeyEvent) (tui.Action, bool) {
	if e.Kind == tui.KeyRelease || e.Mods.Chord() != 0 {
		return nil, false
	}
	// A MNEMONIC IS CHECKED BEFORE A VIM ALIAS, and that ordering is the whole
	// reason the aliases are safe to offer: a row whose hotkey is 'k' must stay
	// reachable with 'k'. Only a key no visible row answers to becomes a
	// direction. The arrow keys themselves are unaffected — no row's mnemonic
	// is an arrow — so this costs nothing when the aliases are off.
	code := e.Code
	if m.vimKeys {
		if _, isMnemonic := m.mnemonic(code); !isMnemonic {
			if dir, ok := oldVimDirection(code); ok {
				code = dir
			}
		}
	}
	e.Code = code

	if m.horizontal {
		if a, ok := m.resolveBarKey(e); ok {
			return a, ok
		}
	} else if a, ok := m.resolveColumnKey(e); ok {
		return a, ok
	}
	switch e.Code {
	case tui.KeyEscape:
		// STAGED, ONE LEVEL PER PRESS, and unhandled once there are none.
		//
		// Escape used to close the whole cascade and report itself handled even
		// with nothing open. Both halves were wrong. Closing everything skips
		// the intermediate states a user is aiming at — from a nested submenu
		// the first Escape should return to the dropdown that opened it, not to
		// the bar. And claiming the key with nothing open swallows it: a menu
		// may be nested inside something with its own meaning for Escape, and
		// the consumer is the only one who knows what leaving the root means.
		//
		// MenuCloseAction{All: true} remains the programmatic close — DoAction,
		// hide, unmount — it is simply not what the keyboard does.
		if len(m.levels) > 0 {
			return MenuCloseAction{All: false}, true
		}
		return nil, false
	case tui.KeyEnter, ' ':
		if id, ok := m.Selected(); ok {
			return MenuActivateAction{ItemID: id}, true
		}
		return nil, false
	}
	// A mnemonic activates its row directly, from wherever the selection is.
	// Code carries the printable rune for an ordinary key, so there is no
	// separate "is this text" flag to consult.
	if id, ok := m.mnemonic(e.Code); ok {
		return MenuActivateAction{ItemID: id}, true
	}
	return nil, false
}

func oldVimDirection(r rune) (rune, bool) {
	switch r {
	case 'h':
		return tui.KeyLeft, true
	case 'j':
		return tui.KeyDown, true
	case 'k':
		return tui.KeyUp, true
	case 'l':
		return tui.KeyRight, true
	}
	return 0, false
}

func TestMenuKeysMatchTheOldResolverWithNoMap(t *testing.T) {
	model := []MenuItemModel{
		{ID: "file", Label: "File", Hotkey: 'f'},
		{ID: "kill", Label: "Kill", Hotkey: 'k'}, // a mnemonic on a vim key: the mnemonic wins
		{ID: "exit", Label: "Exit", Hotkey: 'x'},
	}
	for _, horizontal := range []bool{false, true} {
		for _, vim := range []bool{false, true} {
			m := NewMenu(WithMenuVimNavigation(vim))
			if err := m.SetModel(model); err != nil {
				t.Fatal(err)
			}
			m.horizontal = horizontal
			show := func(a tui.Action, ok bool) string { return fmt.Sprintf("%v %#v", ok, a) }
			keyGridDiff(t, fmt.Sprintf("Menu (horizontal %v, vim %v)", horizontal, vim),
				func(e tui.KeyEvent) string { return show(oldMenuKey(m, e)) },
				func(e tui.KeyEvent) string { return show(m.resolveKey(e)) },
				noTextShiftedPrintable)
		}
	}
}
