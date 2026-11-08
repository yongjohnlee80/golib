package widgettest

import "github.com/yongjohnlee80/golib/tui"

// PressKey hands c a press of each code in turn, as a key with no modifier and no text.
func PressKey(c interface{ HandleEvent(tui.Event) bool }, codes ...rune) {
	for _, code := range codes {
		c.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: code})
	}
}

// HyperOrMeta is the deviation key.Of makes on purpose: a key with Hyper or Meta held
// matches no command (the old TextInput moved the cursor under them, as though they were not held).
//
// And one the old field got wrong: its completion compared the raw Mods (e.Mods&^ModShift == 0), so
// a lock bit a kitty terminal reports (Num Lock, Caps Lock) stopped Tab completing. Every binding now
// drops lock bits, as Mods.Chord's contract says it must.
func HyperOrMeta(e tui.KeyEvent) string {
	switch {
	case e.Mods&(tui.ModHyper|tui.ModMeta) != 0:
		return "Hyper or Meta held matches no command"
	case e.Code == tui.KeyTab && e.Mods&tui.LockMods != 0:
		return "a lock bit no longer disables Tab completion"
	}
	return NoTextShiftedPrintable(e)
}
