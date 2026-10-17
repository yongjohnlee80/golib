package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
)

// Every key a scheme can spell has an Editor chord, and the chord is the key: a
// conversion that dropped a modifier would bind the unmodified key instead. And an event's chord is the
// same chord the scheme's key converts to, so a binding meets the press it names.
func TestEditorChordsRoundTripEveryModifier(t *testing.T) {
	codes := []rune{'a', 'z', '?', '/', '1', ' ', tui.KeyUp, tui.KeyTab, tui.KeyEnter, tui.KeyF10, tui.KeyBackspace}
	all := tuikey.Shift | tuikey.Alt | tuikey.Ctrl | tuikey.Super
	core := NewEditorCore()
	for _, code := range codes {
		for m := tuikey.Mods(0); m <= all; m++ {
			if m&^all != 0 {
				continue
			}
			s := tuikey.Seq{Code: code, Mods: m}
			if _, err := tuikey.Parse(s.String()); err != nil {
				continue // a key no scheme can write (Shift with a character it types)
			}
			kc := chordOfSeq(ModeNormal, s)
			if back := seqOfChord(kc); back != s {
				t.Errorf("%v → %+v → %v: the chord lost a modifier", s, kc, back)
			}
			ev, ok := core.chordOf(tui.KeyEvent{Kind: tui.KeyPress, Code: code, Mods: m})
			if !ok || ev != kc {
				t.Errorf("the press %v reads as %+v, its scheme key as %+v", s, ev, kc)
			}
		}
	}
}
