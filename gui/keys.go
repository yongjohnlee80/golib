package gui

import (
	"runtime"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/gio/io/key"
	"github.com/yongjohnlee80/golib/tui"
)

// Keyboard event translation and protocol mapping.
//
// Keyboard inputs in the native GUI backend are normalized to match the Kitty keyboard
// protocol conventions implemented by the terminal driver (tui/term):
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                 Gio Raw Key & Edit Events                   │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	        ┌──────────────────────┴──────────────────────┐
//	        ▼                                             ▼
//	key.EditEvent (Printable / IME)              key.Event (Functional / Chords)
//	(e.g., typing 'a', 'Z', '9', or IME commit)   (e.g., Enter, Esc, Ctrl+C, F1)
//	        │                                             │
//	        ▼                                             ▼
//	Emits tui.KeyEvent with Text:                 Emits tui.KeyEvent with Code & Mods:
//	KeyEvent{Code: 'a', Text: "a"}               KeyEvent{Code: 'c', Mods: ModCtrl}
//	        │                                             │
//	        └──────────────────────┬──────────────────────┘
//	                               ▼
//	┌─────────────────────────────────────────────────────────────┐
//	│       tui.App (Consumes standardized tui.KeyEvent)          │
//	└─────────────────────────────────────────────────────────────┘
//
// Dual-Event Deduplication:
// Gio emits both a key.Event and a key.EditEvent for standard printable keys. To prevent
// duplicate character delivery, un-chorded printable key.Event instances are dropped,
// allowing the key.EditEvent to serve as the single source of truth for text entry.

// named maps Gio functional key names to standardized golib/tui key codes.
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

// translateKey translates a raw Gio [key.Event] into a standardized [tui.KeyEvent].
//
// Key Mapping Behavior:
//   - Functional keys (Enter, Esc, arrows, F-keys, etc.) map directly to their tui keycodes.
//   - Modifier-chorded keys (Ctrl, Alt, Super) produce KeyPress or KeyRelease events with
//     lowercase rune codes and active modifier bitflags.
//   - Unchorded printable keys return ok=false because their text content arrives via
//     corresponding [key.EditEvent] callbacks.
//   - Bare modifier keys (Shift, Ctrl, Alt, Super pressed alone) return ok=false.
func translateKey(e key.Event) (tui.KeyEvent, bool) {
	kind := tui.KeyPress
	if e.State == key.Release {
		kind = tui.KeyRelease
	}
	code, typed, ok := keyCode(e.Name)
	if !ok || typed && e.Modifiers&chordMods == 0 {
		return tui.KeyEvent{}, false // a modifier or system key; or text, which arrives as an edit event
	}
	return tui.KeyEvent{Kind: kind, Code: code, Mods: modsOf(e.Modifiers)}, true
}

// keyCode is the tui code of a key Gio names: a named key's, the space's, or a printable key's own
// rune in lower case. Gio names a letter by its capital; kitty reports the key's own code point,
// with Shift as a modifier, so Ctrl+Shift+A is Code 'a'. typed is that the key types text (the
// space, a printable), which arrives as an edit event when no chord is held. ok is false for a name
// that is not one printable rune: Ctrl, Shift, Alt, Super, Back. Command's name, "⌘", is one, so
// it reads as a typed key: a caller that reports modifiers looks them up first.
func keyCode(name key.Name) (code rune, typed, ok bool) {
	if code, ok := named[name]; ok {
		return code, false, true
	}
	if name == key.NameSpace {
		return ' ', true, true
	}
	r, size := utf8.DecodeRuneInString(string(name))
	if size == 0 || size != len(name) || !unicode.IsPrint(r) {
		return 0, false, false
	}
	return unicode.ToLower(r), true, true
}

// textKeys converts a string of typed characters into individual [tui.KeyEvent] records,
// mimicking standard terminal ANSI stream reception.
func textKeys(s string) []tui.KeyEvent {
	out := make([]tui.KeyEvent, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		out = append(out, tui.KeyEvent{Code: r, Text: string(r)})
	}
	return out
}

// isPasteChord reports whether a keyboard event matches the host platform's standard clipboard paste shortcut.
//
// Platform Mappings:
//   - macOS: Command + V (Cmd+V)
//   - Linux / Windows / BSD: Ctrl + Shift + V
//
// When matched, the backend intercepts the keystroke and dispatches a [tui.PasteEvent]
// carrying the current OS clipboard text.
func isPasteChord(e key.Event) bool {
	if e.State != key.Press || e.Name != "V" {
		return false
	}
	if runtime.GOOS == "darwin" {
		return e.Modifiers == key.ModCommand
	}
	return e.Modifiers == key.ModCtrl|key.ModShift
}
