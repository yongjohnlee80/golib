package decl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldDrawerCloses is the drawer frame's closing-key test before commands, verbatim but for closeOnQ,
// its argument: the oracle of the no-map differential.
func oldDrawerCloses(k tui.KeyEvent, closeOnQ bool) bool {
	return k.Kind == tui.KeyPress && k.Mods.Chord() == 0 && (k.Code == tui.KeyEscape || closeOnQ && k.Code == 'q')
}

func TestDrawerKeysMatchTheOldSwitchWithNoMap(t *testing.T) {
	codes := []rune{'q', 'Q', 'x', tui.KeyEscape, tui.KeyEnter, tui.KeyF10}
	mods := []tui.Mods{0, tui.ModShift, tui.ModCtrl, tui.ModAlt, tui.ModSuper, tui.ModHyper, tui.ModMeta,
		tui.ModNumLock, tui.ModCapsLock, tui.ModShift | tui.ModNumLock}
	agree := 0
	for _, closeOnQ := range []bool{false, true} {
		f := &drawerFrame{owner: &drawerNode{closeOnQ: closeOnQ}}
		for _, code := range codes {
			for _, m := range mods {
				typed := string(code)
				if m&tui.ModShift != 0 {
					typed = strings.ToUpper(typed) // Shift types the capital
				}
				for _, text := range []string{"", typed} {
					if text != "" && (code > 127 || m&(tui.ModCtrl|tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) != 0) {
						continue
					}
					for _, kind := range []tui.KeyKind{tui.KeyPress, tui.KeyRepeat, tui.KeyRelease} {
						k := tui.KeyEvent{Kind: kind, Code: code, Mods: m, Text: text}
						o := oldDrawerCloses(k, closeOnQ)
						n := k.Kind == tui.KeyPress && f.closes(k)
						if o == n {
							agree++
							continue
						}
						t.Errorf("closeOnQ %v, %s: old %v, new %v", closeOnQ, fmt.Sprintf("{%d %q %v %q}", kind, code, m, text), o, n)
					}
				}
			}
		}
	}
	if agree == 0 {
		t.Fatal("the grid compared nothing")
	}
}
