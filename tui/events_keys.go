package tui

import "github.com/yongjohnlee80/golib/tui/key"

// KeyEvent represents one keyboard action.
//
// Codepoint Mapping:
// Code holds the key's Unicode codepoint or a Key* constant (defined in keys.go)
// allocated in the Unicode private-use plane so functional keys (such as KeyF1,
// KeyEnter, KeyArrowUp) never collide with printable characters.
//
// Kitty Keyboard Protocol Extensions:
// When connected to a Kitty-protocol capable terminal:
//   - Kind distinguishes KeyPress, KeyRepeat, and KeyRelease.
//   - Base and Shifted carry the Kitty "alternate keys" (base-layout and shifted
//     codepoints, 0 when unreported), enabling layout-independent keyboard shortcut matching.
//   - Text contains the associated text string ("" for non-text keys).
//
// On legacy terminals without Kitty protocol support, Kind is always KeyPress,
// and Base/Shifted remain 0.
type KeyEvent struct {
	Kind    KeyKind
	Code    rune
	Base    rune
	Shifted rune
	Mods    Mods
	Text    string
}

func (KeyEvent) isEvent() {}

// --- keyboard. The fields below carry kitty-protocol information, which the
//     backend negotiates at startup; they are zero on terminals that do not
//     answer.
// REFERENCE: https://sw.kovidgoyal.net/kitty/keyboard-protocol/ ---

// KeyKind distinguishes press, repeat, and release key actions.
type KeyKind uint8

const (
	KeyPress   KeyKind = iota
	KeyRepeat          // kitty flag 2 terminals only
	KeyRelease         // kitty flag 2 terminals only; never synthesized elsewhere
)

// Mods is the modifier bitmask, in kitty modifier order. It is key.Mods: the type, its bits and its
// methods (Chord, String) live in the leaf package tui/key, so tui/keymap can name keys without
// importing tui.
type Mods = key.Mods

const (
	ModShift    = key.Shift
	ModAlt      = key.Alt
	ModCtrl     = key.Ctrl
	ModSuper    = key.Super
	ModHyper    = key.Hyper
	ModMeta     = key.Meta
	ModCapsLock = key.CapsLock
	ModNumLock  = key.NumLock
)

// LockMods are the modifiers that report a LOCK STATE rather than a key the user is holding down
// (key.LockMods). Compare bindings with Mods.Chord(), which removes them.
const LockMods = key.LockMods

// Functional key codes for KeyEvent.Code: the kitty keyboard protocol's assignments, defined in
// tui/key and re-exported here. The terminal decoder in tui/term maps escape sequences onto exactly
// these constants.
// REFERENCE: tui/term
const (
	KeyEnter     = key.Enter
	KeyTab       = key.Tab
	KeyBackspace = key.Backspace
	KeyEscape    = key.Escape

	KeyUp       = key.Up
	KeyDown     = key.Down
	KeyLeft     = key.Left
	KeyRight    = key.Right
	KeyHome     = key.Home
	KeyEnd      = key.End
	KeyPageUp   = key.PageUp
	KeyPageDown = key.PageDown
	KeyInsert   = key.Insert
	KeyDelete   = key.Delete

	KeyF1  = key.F1
	KeyF2  = key.F2
	KeyF3  = key.F3
	KeyF4  = key.F4
	KeyF5  = key.F5
	KeyF6  = key.F6
	KeyF7  = key.F7
	KeyF8  = key.F8
	KeyF9  = key.F9
	KeyF10 = key.F10
	KeyF11 = key.F11
	KeyF12 = key.F12

	// KeyMenu is the Menu (application) key: a terminal speaking the kitty protocol sends it; others,
	// and Gio's native window, never do.
	KeyMenu = key.Menu
)
