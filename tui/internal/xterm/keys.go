package xterm

import "github.com/yongjohnlee80/golib/tui/key"

// letterKeys are the keys sent as a CSI or SS3 final letter: the cursor keys, Home, End and
// F1–F4.
var letterKeys = []struct {
	Final byte
	Key   rune
}{
	{'A', key.Up}, {'B', key.Down}, {'C', key.Right}, {'D', key.Left},
	{'H', key.Home}, {'F', key.End},
	{'P', key.F1}, {'Q', key.F2}, {'R', key.F3}, {'S', key.F4},
}

// SS3KeypadEnter is SS3's final for keypad Enter. It is only ever read: Enter is sent as CR.
const SS3KeypadEnter = 'M'

// LetterKey is the key a CSI or SS3 final letter names.
func LetterKey(final byte) (rune, bool) {
	for _, l := range letterKeys {
		if l.Final == final {
			return l.Key, true
		}
	}
	return 0, false
}

// KeyLetter is the final letter a key is sent with, for one of letterKeys.
func KeyLetter(k rune) (byte, bool) {
	for _, l := range letterKeys {
		if l.Key == k {
			return l.Final, true
		}
	}
	return 0, false
}

// tildeKeys are the keys sent as CSI n ~. Sent marks the code a key is written with; the others
// are only read (Home, End and F1–F4 are sent as letters, and some terminals send these codes
// for them).
var tildeKeys = []struct {
	Code int
	Key  rune
	Sent bool
}{
	{1, key.Home, false}, {2, key.Insert, true}, {3, key.Delete, true}, {4, key.End, false},
	{5, key.PageUp, true}, {6, key.PageDown, true}, {7, key.Home, false}, {8, key.End, false},
	{11, key.F1, false}, {12, key.F2, false}, {13, key.F3, false}, {14, key.F4, false},
	{15, key.F5, true}, {17, key.F6, true}, {18, key.F7, true}, {19, key.F8, true},
	{20, key.F9, true}, {21, key.F10, true}, {23, key.F11, true}, {24, key.F12, true},
}

// TildeKey is the key CSI code ~ names.
func TildeKey(code int) (rune, bool) {
	for _, t := range tildeKeys {
		if t.Code == code {
			return t.Key, true
		}
	}
	return 0, false
}

// TildeCode is the code a key is sent with as CSI code ~.
func TildeCode(k rune) (int, bool) {
	for _, t := range tildeKeys {
		if t.Sent && t.Key == k {
			return t.Code, true
		}
	}
	return 0, false
}

// c0Keys are the keys sent as a single C0 byte (and DEL). A Ctrl+letter's byte is CtrlByte's.
var c0Keys = []struct {
	Byte byte
	Key  rune
}{
	{0x0D, key.Enter}, {0x09, key.Tab}, {0x7F, key.Backspace}, {0x1B, key.Escape},
}

// C0Key is the key a lone C0 byte (or DEL) is: Enter, Tab, Backspace or Escape.
func C0Key(b byte) (rune, bool) {
	for _, c := range c0Keys {
		if c.Byte == b {
			return c.Key, true
		}
	}
	return 0, false
}

// C0Byte is the byte Enter, Tab, Backspace or Escape is sent as.
func C0Byte(k rune) (byte, bool) {
	for _, c := range c0Keys {
		if c.Key == k {
			return c.Byte, true
		}
	}
	return 0, false
}

// ctrlPunct are the Ctrl+punctuation keys: the byte each is sent as, and whether a decoder reads
// that byte back as it. The digits are the VT220's aliases ('2' is Ctrl+@), sent but never read;
// 0x1B and 0x7F read back as Escape and Backspace (C0Key), never as Ctrl+[ or Ctrl+?.
var ctrlPunct = []struct {
	Key  rune
	Byte byte
	Read bool
}{
	{' ', 0x00, true}, {'@', 0x00, false}, {'2', 0x00, false},
	{'[', 0x1b, false}, {'3', 0x1b, false},
	{'\\', 0x1c, true}, {'4', 0x1c, false},
	{']', 0x1d, true}, {'5', 0x1d, false},
	{'^', 0x1e, true}, {'6', 0x1e, false},
	{'_', 0x1f, true}, {'-', 0x1f, false}, {'/', 0x1f, false}, {'7', 0x1f, false},
	{'?', 0x7f, false}, {'8', 0x7f, false},
}

// CtrlByte is the C0 byte Ctrl+r sends: a letter's (either case), or a punctuation key's.
func CtrlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r - 'a' + 1), true
	case r >= 'A' && r <= 'Z':
		return byte(r - 'A' + 1), true
	}
	for _, c := range ctrlPunct {
		if c.Key == r {
			return c.Byte, true
		}
	}
	return 0, false
}

// CtrlKey is the key a C0 byte is read as with Ctrl held: Ctrl+a…z for 0x01–0x1A, and Ctrl+Space,
// \ ] ^ _ for 0x00 and 0x1C–0x1F. The bytes C0Key names are not Ctrl keys.
func CtrlKey(b byte) (rune, bool) {
	if b >= 0x01 && b <= 0x1A {
		return rune('a' + b - 1), true
	}
	for _, c := range ctrlPunct {
		if c.Read && c.Byte == b {
			return c.Key, true
		}
	}
	return 0, false
}

// ModParam is the modifier parameter xterm appends to a key: 1 + shift(1) + alt(2) + ctrl(4) +
// meta(8). 1 means none.
func ModParam(m key.Mods) int {
	n := 1
	if m&key.Shift != 0 {
		n += 1
	}
	if m&key.Alt != 0 {
		n += 2
	}
	if m&key.Ctrl != 0 {
		n += 4
	}
	if m&key.Meta != 0 {
		n += 8
	}
	return n
}

// KittyModParam is the kitty keyboard protocol's modifier parameter: 1 + shift(1) + alt(2) +
// ctrl(4) + super(8) + hyper(16) + meta(32), key.Mods's own bits in the same order. It is not
// ModParam: xterm counts meta as 8, where kitty puts super.
func KittyModParam(m key.Mods) int {
	const kitty = key.Shift | key.Alt | key.Ctrl | key.Super | key.Hyper | key.Meta
	return 1 + int(m&kitty)
}

// ParamMods is the modifiers a key's modifier parameter p carries, read as kitty's (p-1 is
// key.Mods's bits). A parameter under 1 is none.
func ParamMods(p int) key.Mods {
	if p < 1 {
		p = 1
	}
	return key.Mods(p - 1)
}

// Bracketed paste (mode 2004): a paste arrives between PasteStart and PasteEnd, CSI 200 ~ and
// CSI 201 ~.
const (
	PasteStartCode = 200
	PasteEndCode   = 201
	PasteStart     = "\x1b[200~"
	PasteEnd       = "\x1b[201~"
)
