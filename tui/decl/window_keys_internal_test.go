package decl

import (
	"fmt"
	"testing"
	"unicode"

	"github.com/yongjohnlee80/golib/tui"
)

// oldWindowMenuKey is the decl Window's per-menu key switch at golib 6a0b271, verbatim but for its
// effects, which are named: the oracle of the no-map differential.
func oldWindowMenuKey(k tui.KeyEvent, focused bool, openHotkey func(rune) bool) string {
	switch {
	case k.Code == tui.KeyF10 && k.Mods&shortcutMods == 0:
		return "toggle"
	case k.Mods&shortcutMods == tui.ModAlt:
		if openHotkey(k.Code) {
			return "hotkey"
		}
	case k.Code == tui.KeyEscape && focused:
		return "leave"
	}
	return ""
}

// windowDeviation names the differences the key map makes on purpose:
//   - F10 or Esc with Hyper or Meta held matches no command; the old switch ignored them;
//   - a modified Esc (Shift, Ctrl, Alt, Super) no longer leaves a focused menu bar: leaving is
//     tui.menu.close, Esc, the menu's own key. A Global "any Esc" command would shadow every widget's Esc.
func windowDeviation(k tui.KeyEvent) string {
	switch {
	case k.Mods&(tui.ModHyper|tui.ModMeta) != 0:
		return "Hyper or Meta held matches no command"
	case k.Code == tui.KeyEscape && k.Mods.Chord() != 0:
		return "a modified Esc no longer leaves the menu bar"
	}
	return ""
}

func TestWindowMenuKeysMatchTheOldSwitchWithNoMap(t *testing.T) {
	codes := []rune{'f', 'e', 'x', 'q', 'F', tui.KeyF10, tui.KeyF1, tui.KeyEscape, tui.KeyEnter, tui.KeyDown}
	mods := []tui.Mods{0, tui.ModShift, tui.ModCtrl, tui.ModAlt, tui.ModSuper, tui.ModHyper, tui.ModMeta,
		tui.ModNumLock, tui.ModCapsLock, tui.ModAlt | tui.ModShift, tui.ModAlt | tui.ModNumLock, tui.ModCtrl | tui.ModShift}
	hotkey := func(r rune) bool { return unicode.ToLower(r) == 'f' || unicode.ToLower(r) == 'e' }
	agree := 0
	for _, focused := range []bool{false, true} {
		for _, code := range codes {
			for _, m := range mods {
				k := tui.KeyEvent{Kind: tui.KeyPress, Code: code, Mods: m}
				o, n := oldWindowMenuKey(k, focused, hotkey), windowMenuKey(nil, k, focused, hotkey)
				if o == n {
					agree++
					continue
				}
				if windowDeviation(k) == "" {
					t.Errorf("focused %v, %s: old %q, new %q", focused, fmt.Sprintf("{%q %v}", code, m), o, n)
				}
			}
		}
	}
	if agree == 0 {
		t.Fatal("the grid compared nothing")
	}
}
