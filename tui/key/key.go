// Package key names keys as Qt's portable text does ("Ctrl+Shift+S", "F10", "PgDown") and decides
// which key an event is. It imports nothing from tui, so tui, tui/keymap and tui/decl share one
// parser and one matching model.
//
// A key binding is a [Seq]: one key and the modifiers held with it. An event becomes a Seq through
// [Of], and a lookup tries [Candidates] in order, the first candidate a binding holds winning. A
// query that asks which events reach a binding drives the same rules over [Accepts]; a warning about
// a contested event uses [Overlaps].
//
// REFERENCE: https://doc.qt.io/qt-6/qkeysequence.html (portable text)
// REFERENCE: ADR 1792255248 §4.2 (KB adrs/1792255248-golib-tui-overridable-key-maps.md)
package key

import "strings"

// Mods is the modifier bitmask, in kitty modifier order. tui re-exports it as tui.Mods.
type Mods uint8

const (
	Shift Mods = 1 << iota
	Alt
	Ctrl
	Super
	Hyper
	Meta
	CapsLock
	NumLock
)

// LockMods are the modifiers that report a LOCK STATE rather than a key the
// user is holding down. They are on for every keystroke while the lock is
// engaged, including keystrokes the user thinks are unmodified.
const LockMods = CapsLock | NumLock

// Chord is the modifiers a key BINDING should match on: everything the user is
// actually holding, with the lock state removed.
//
// COMPARE WITH THIS, NEVER WITH THE RAW FIELD. `Mods != 0` looks like "the key
// was pressed on its own" and is not: under the kitty keyboard protocol the
// terminal reports Caps Lock and Num Lock as modifier bits, so with Num Lock on
// — which is its normal state on most keyboards — every arrow key, Enter and
// Escape carries a modifier and a bare inequality rejects all of them. The bug
// hides completely under tmux and under any terminal still speaking the legacy
// sequences, because those cannot express a lock bit at all; it appears only on
// a terminal that negotiated the modern protocol, which is where it looks like
// the widget has simply stopped responding to the keyboard.
//
// Shift is deliberately KEPT: shift is a key the user is holding, and Shift-Tab
// is a different binding from Tab.
func (m Mods) Chord() Mods { return m &^ LockMods }

// String names the modifiers that are set, for traces and test failures.
//
// Worth having for this type in particular: a failure reporting "mods 128" is
// unreadable, and 128 is Num Lock — the bit most likely to be the reason a
// binding did not fire.
func (m Mods) String() string {
	if m == 0 {
		return "none"
	}
	var out []string
	for _, p := range []struct {
		bit  Mods
		name string
	}{
		{Shift, "shift"}, {Alt, "alt"}, {Ctrl, "ctrl"},
		{Super, "super"}, {Hyper, "hyper"}, {Meta, "meta"},
		{CapsLock, "capslock"}, {NumLock, "numlock"},
	} {
		if m&p.bit != 0 {
			out = append(out, p.name)
		}
	}
	return strings.Join(out, "+")
}

// Functional key codes. Printable keys carry their Unicode code point; functional keys use the kitty
// keyboard protocol's Unicode Private Use Area assignments, so a kitty-mode decode is identity and the
// legacy CSI/SS3 decoder maps onto the same constants. The C0-derived keys keep their traditional code
// points, exactly as kitty specifies them. tui re-exports each as tui.Key<Name>.
//
// REFERENCE: https://sw.kovidgoyal.net/kitty/keyboard-protocol/#functional-key-definitions
const (
	// C0-derived (legacy byte values, kitty-compatible).
	Enter     rune = 13  // CR
	Tab       rune = 9   // HT
	Backspace rune = 127 // DEL
	Escape    rune = 27  // ESC

	// Functional keys (kitty PUA assignments).
	Up       rune = 57352
	Down     rune = 57353
	Left     rune = 57350
	Right    rune = 57351
	Home     rune = 57356
	End      rune = 57357
	PageUp   rune = 57354
	PageDown rune = 57355
	Insert   rune = 57348
	Delete   rune = 57349

	F1  rune = 57364
	F2  rune = 57365
	F3  rune = 57366
	F4  rune = 57367
	F5  rune = 57368
	F6  rune = 57369
	F7  rune = 57370
	F8  rune = 57371
	F9  rune = 57372
	F10 rune = 57373
	F11 rune = 57374
	F12 rune = 57375

	// Menu is the Menu (application) key, the kitty keyboard protocol's code for it: a terminal
	// speaking the protocol sends it; others, and Gio's native window, never do.
	Menu rune = 57363
)
