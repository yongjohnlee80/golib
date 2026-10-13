package widget

import (
	"fmt"
	"testing"
)

// Every action a host can name is bindable somewhere: an action actionModes leaves out panics in
// an overlay (WithKeymap) though the built-in tables, which skip that check, bind it.
func TestEveryActionHasAMode(t *testing.T) {
	for a := ActUnbound + 1; a < actMax; a++ {
		if n, v, i := actionModes(a); !n && !v && !i {
			t.Errorf("%v is bindable in no mode", a)
		}
	}
}

// What a built-in table binds, an overlay may bind too: each entry passes the overlay's check.
func TestBuiltInBindingsPassTheOverlayCheck(t *testing.T) {
	for name, km := range map[string]Keymap{"Vim": VimKeymap(), "Nano": NanoKeymap(), "Standard": StandardKeymap()} {
		for kc, act := range km {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s binds %v to %v, which an overlay may not: %v", name, kc, act, fmt.Sprint(r))
					}
				}()
				validateKeymapEntry(kc, act)
			}()
		}
	}
}
