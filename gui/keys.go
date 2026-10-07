package gui

import (
	"runtime"
	"unicode"
	"unicode/utf8"

	"gioui.org/io/key"
	"github.com/yongjohnlee80/golib/tui"
)

// Keys reach the window two ways, and the backend turns both into the tui.KeyEvent that tui/term
// decodes for the same keystroke under the kitty keyboard protocol (flags 1 and 2):
//
//   - text arrives as Gio edit events (key.EditEvent): plain printable keys, shifted ones, and IME
//     commits. Each rune becomes KeyEvent{Code: r, Text: string(r)}, as a terminal's printed text does.
//   - everything else arrives as key.Event: functional keys, and printable keys held with Ctrl,
//     Alt or Super. Those become KeyEvent{Code, Mods} with no text, and their release is reported.
//
// Gio sends a key.Event for a plain printable key as well as its edit event; that one is dropped,
// so the text is not delivered twice.

// named maps Gio's functional key names to tui's codes (tui/events_keys.go).
var named = map[key.Name]rune{
	key.NameReturn:         tui.KeyEnter,
	key.NameEnter:          tui.KeyEnter,
	key.NameTab:            tui.KeyTab,
	key.NameDeleteBackward: tui.KeyBackspace,
	key.NameEscape:         tui.KeyEscape,
	key.NameUpArrow:        tui.KeyUp,
	key.NameDownArrow:      tui.KeyDown,
	key.NameLeftArrow:      tui.KeyLeft,
	key.NameRightArrow:     tui.KeyRight,
	key.NameHome:           tui.KeyHome,
	key.NameEnd:            tui.KeyEnd,
	key.NamePageUp:         tui.KeyPageUp,
	key.NamePageDown:       tui.KeyPageDown,
	key.NameDeleteForward:  tui.KeyDelete,
	key.NameF1:             tui.KeyF1,
	key.NameF2:             tui.KeyF2,
	key.NameF3:             tui.KeyF3,
	key.NameF4:             tui.KeyF4,
	key.NameF5:             tui.KeyF5,
	key.NameF6:             tui.KeyF6,
	key.NameF7:             tui.KeyF7,
	key.NameF8:             tui.KeyF8,
	key.NameF9:             tui.KeyF9,
	key.NameF10:            tui.KeyF10,
	key.NameF11:            tui.KeyF11,
	key.NameF12:            tui.KeyF12,
}

// modsOf maps Gio's modifiers to tui's. macOS's Command key is Super, as kitty reports it.
func modsOf(m key.Modifiers) tui.Mods {
	var out tui.Mods
	if m.Contain(key.ModShift) {
		out |= tui.ModShift
	}
	if m.Contain(key.ModAlt) {
		out |= tui.ModAlt
	}
	if m.Contain(key.ModCtrl) {
		out |= tui.ModCtrl
	}
	if m.Contain(key.ModSuper) || m.Contain(key.ModCommand) {
		out |= tui.ModSuper
	}
	return out
}

// chordMods are the modifiers that stop a printable key from typing text.
const chordMods = key.ModCtrl | key.ModAlt | key.ModSuper | key.ModCommand

// translateKey is the tui event for a Gio key event, or ok=false when the key's text arrives as
// an edit event instead, or the key is a modifier on its own.
func translateKey(e key.Event) (tui.KeyEvent, bool) {
	kind := tui.KeyPress
	if e.State == key.Release {
		kind = tui.KeyRelease
	}
	mods := modsOf(e.Modifiers)
	if code, ok := named[e.Name]; ok {
		return tui.KeyEvent{Kind: kind, Code: code, Mods: mods}, true
	}
	chord := e.Modifiers&chordMods != 0
	if e.Name == key.NameSpace {
		if !chord {
			return tui.KeyEvent{}, false // the space arrives as text
		}
		return tui.KeyEvent{Kind: kind, Code: ' ', Mods: mods}, true
	}
	r, size := utf8.DecodeRuneInString(string(e.Name))
	if size == 0 || size != len(e.Name) || !unicode.IsPrint(r) {
		return tui.KeyEvent{}, false // Ctrl, Shift, Alt, Super, Command, Back: modifiers and system keys
	}
	if !chord {
		return tui.KeyEvent{}, false // the text arrives as an edit event
	}
	// Gio names a letter by its capital; kitty reports the key's own code point, lower case,
	// with Shift as a modifier: Ctrl+Shift+A is Code 'a', Mods ctrl|shift.
	return tui.KeyEvent{Kind: kind, Code: unicode.ToLower(r), Mods: mods}, true
}

// textKeys is the tui events for typed text: one per rune, as tui/term decodes printed text.
func textKeys(s string) []tui.KeyEvent {
	out := make([]tui.KeyEvent, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		out = append(out, tui.KeyEvent{Code: r, Text: string(r)})
	}
	return out
}

// isPasteChord reports the chord the window keeps for itself to paste the system clipboard, as a
// terminal emulator does: Cmd+V on macOS, Ctrl+Shift+V elsewhere. It becomes a tui.PasteEvent.
func isPasteChord(e key.Event) bool {
	if e.State != key.Press || e.Name != "V" {
		return false
	}
	if runtime.GOOS == "darwin" {
		return e.Modifiers == key.ModCommand
	}
	return e.Modifiers == key.ModCtrl|key.ModShift
}
