package widget

import (
	"fmt"
	"testing"
	"unicode"

	"github.com/yongjohnlee80/golib/tui"
)

// THE NO-MAP DIFFERENTIAL. Each converted widget keeps its old key handler in
// its test file as the oracle, verbatim but for its effects, which become the name of what it did.
// keyGridDiff drives the oracle and the converted decision over one grid of events and reports every
// disagreement that is not a listed, reasoned deviation.

// gridCodes are the keys the grid presses: every key a phase-1 widget binds, and some no widget does.
var gridCodes = []rune{
	'a', 'g', 'j', 'k', 'h', 'l', 'x', 'q', 'G', 'J', 'K', 'H', 'L', ' ', '?', '/', '0', '$',
	tui.KeyUp, tui.KeyDown, tui.KeyLeft, tui.KeyRight, tui.KeyHome, tui.KeyEnd,
	tui.KeyPageUp, tui.KeyPageDown, tui.KeyEnter, tui.KeyTab, tui.KeyEscape, tui.KeyBackspace,
	tui.KeyDelete, tui.KeyInsert, tui.KeyF1, tui.KeyF2, tui.KeyF10, tui.KeyMenu,
}

// gridMods are the modifier states the grid holds, locks and the unbindable ones included.
var gridMods = []tui.Mods{
	0, tui.ModShift, tui.ModCtrl, tui.ModAlt, tui.ModSuper, tui.ModHyper, tui.ModMeta,
	tui.ModCapsLock, tui.ModNumLock, tui.ModShift | tui.ModNumLock, tui.ModCtrl | tui.ModShift,
	tui.ModAlt | tui.ModShift, tui.ModCtrl | tui.ModNumLock,
}

// keyGrid is every (code, mods, text, kind) the grid presses. Each printable key comes both as a
// legacy terminal sends it (the character, no text) and as Gio and a kitty terminal with associated
// text do (its text, Shift spent on it or kept as the event reports).
func keyGrid() []tui.KeyEvent {
	var out []tui.KeyEvent
	for _, code := range gridCodes {
		for _, mods := range gridMods {
			texts := []string{""}
			if unicode.IsPrint(code) && mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) == 0 {
				r := code
				if mods&tui.ModShift != 0 {
					r = unicode.ToUpper(r)
				}
				texts = append(texts, string(r))
			}
			for _, text := range texts {
				for _, kind := range []tui.KeyKind{tui.KeyPress, tui.KeyRepeat, tui.KeyRelease} {
					out = append(out, tui.KeyEvent{Kind: kind, Code: code, Mods: mods, Text: text})
				}
			}
		}
	}
	return out
}

// noTextShiftedPrintable is the one event form whose reading the key map changes on purpose: a
// printable key with Shift and no text, which no golib backend sends (the terminal asks kitty for
// flags 1+2 only, so a typed character arrives as itself; Gio sends its text). The old handlers read
// the unshifted key; key.Of reads Shift, as Qt does: ('g', Shift) is Shift+G.
func noTextShiftedPrintable(e tui.KeyEvent) string {
	if e.Text == "" && e.Mods&tui.ModShift != 0 && unicode.IsPrint(e.Code) &&
		e.Mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) == 0 {
		return "a printable key with Shift and no text, which no backend sends, now reads Shift"
	}
	return ""
}

// keyGridDiff compares old and new over the grid. deviation names why an event may differ, or "".
func keyGridDiff(t *testing.T, name string, old, new func(tui.KeyEvent) string, deviation func(tui.KeyEvent) string) {
	t.Helper()
	compared, deviated := 0, 0
	for _, e := range keyGrid() {
		o, n := old(e), new(e)
		if o == n {
			compared++
			continue
		}
		if why := deviation(e); why != "" {
			deviated++
			continue
		}
		t.Errorf("%s: %s: old %q, new %q", name, describeKey(e), o, n)
	}
	if compared == 0 {
		t.Fatalf("%s: the grid compared nothing", name)
	}
	t.Logf("%s: %d events agree, %d deviate as documented", name, compared, deviated)
}

func describeKey(e tui.KeyEvent) string {
	return fmt.Sprintf("{kind %d code %q mods %v text %q}", e.Kind, e.Code, e.Mods, e.Text)
}

// shiftSpentOnText adds the deviation key.Of makes on purpose: Shift held on a key
// whose text is one printable rune is spent on that text, so Shift+Space typed as " " is Space. A
// control that refused every modifier (a Button, a menu) now activates on it, as it does on Space.
func shiftSpentOnText(e tui.KeyEvent) string {
	if e.Mods&tui.ModShift != 0 && e.Text == " " {
		return "Shift+Space typed as a space is Space"
	}
	return noTextShiftedPrintable(e)
}
