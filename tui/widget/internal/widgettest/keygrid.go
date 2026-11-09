package widgettest

import (
	"fmt"
	"unicode"

	"github.com/yongjohnlee80/golib/tui"
)

// NoKeymap is an unmounted widget's Context: a nil *tui.Context, which matches each command's
// declared keys. A bare nil would be a nil KeyResolver, which has no KeyCommand to call.
var NoKeymap tui.KeyResolver = (*tui.Context)(nil)

// gridCodes are the keys the grid presses: every key a converted widget binds, and some no widget does.
var gridCodes = []rune{
	'a', 'g', 'j', 'k', 'h', 'l', 'x', 'q', 'G', 'J', 'K', 'H', 'L', ' ', '?', '/', '0', '$',
	'[', ']', '+', '=', 'e', 'u', 'w', 'd', 'c', 'y', '~', 'n', 'Y', 'N', 'Q', '\\', '<', 'i', 'b', 'f',
	'v', 'V',
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

// KeyGrid is every (code, mods, text, kind) the grid presses. Each printable key comes both as a
// legacy terminal sends it (the character, no text) and as Gio and a kitty terminal with associated
// text do (its text, Shift spent on it or kept as the event reports).
func KeyGrid() []tui.KeyEvent {
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

// NoTextShiftedPrintable is the one event form whose reading the key map changes on purpose: a
// printable key with Shift and no text, which no golib backend sends (the terminal asks kitty for
// flags 1+2 only, so a typed character arrives as itself; Gio sends its text). The old handlers read
// the unshifted key; key.Of reads Shift, as Qt does: ('g', Shift) is Shift+G.
func NoTextShiftedPrintable(e tui.KeyEvent) string {
	if e.Text == "" && e.Mods&tui.ModShift != 0 && unicode.IsPrint(e.Code) &&
		e.Mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) == 0 {
		return "a printable key with Shift and no text, which no backend sends, now reads Shift"
	}
	return ""
}

// KeyGridDiff compares old and new over the grid. deviation names why an event may differ, or "".
func KeyGridDiff(t TB, name string, old, new func(tui.KeyEvent) string, deviation func(tui.KeyEvent) string) {
	t.Helper()
	compared, deviated := 0, 0
	for _, e := range KeyGrid() {
		o, n := old(e), new(e)
		if o == n {
			compared++
			continue
		}
		if why := deviation(e); why != "" {
			deviated++
			continue
		}
		t.Errorf("%s: %s: old %q, new %q", name, DescribeKey(e), o, n)
	}
	if compared == 0 {
		t.Fatalf("%s: the grid compared nothing", name)
	}
	t.Logf("%s: %d events agree, %d deviate as documented", name, compared, deviated)
}

// DescribeKey is e as a failure message names it: its kind, code, mods and text.
func DescribeKey(e tui.KeyEvent) string {
	return fmt.Sprintf("{kind %d code %q mods %v text %q}", e.Kind, e.Code, e.Mods, e.Text)
}

// ShiftSpentOnPrintable is shiftSpentOnText for any character: Shift held on a key whose text is one
// printable rune is spent on that text, so a "[" typed with Shift is [, which a handler testing for
// no modifier refused.
func ShiftSpentOnPrintable(e tui.KeyEvent) string {
	if r := []rune(e.Text); e.Mods&tui.ModShift != 0 && len(r) == 1 && unicode.IsPrint(r[0]) &&
		e.Mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) == 0 {
		return "Shift spent on the character it typed"
	}
	return ""
}

// CapitalWithModifier is the deviation key.Of makes on purpose for a capital letter with Ctrl, Alt or
// Super and no text: its case is its Shift, so it reaches the letter's chord (Ctrl+K) by the Shift-less
// fallback, where an old test compared the code to the lower-case letter.
func CapitalWithModifier(e tui.KeyEvent) string {
	if e.Code >= 'A' && e.Code <= 'Z' && e.Text == "" && e.Mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper) != 0 {
		return "a capital with a modifier and no text is its letter's chord"
	}
	return ""
}

// TextlessCharacter is the deviation key.Of makes on purpose for a printable key with no text: it
// reads the code, where an old test read only the text.
func TextlessCharacter(e tui.KeyEvent) string {
	if e.Text == "" && e.Code > ' ' && e.Code < 127 && e.Mods.Chord()&^tui.ModShift == 0 {
		return "a character with no text reads its code"
	}
	return ""
}

// ShiftSpentOnText adds the deviation key.Of makes on purpose: Shift held on a key
// whose text is one printable rune is spent on that text, so Shift+Space typed as " " is Space. A
// control that refused every modifier (a Button, a menu) now activates on it, as it does on Space.
func ShiftSpentOnText(e tui.KeyEvent) string {
	if e.Mods&tui.ModShift != 0 && e.Text == " " {
		return "Shift+Space typed as a space is Space"
	}
	return NoTextShiftedPrintable(e)
}
