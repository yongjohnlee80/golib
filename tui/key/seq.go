package key

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/errs"
)

// Seq is one chord: a key and the modifiers held with it. It is comparable, so a scope's keys can be
// a map[Seq]string.
//
// Code is a printable rune (a letter always lower-cased: the case of a letter is its Shift) or one of
// the functional key constants. Mods is a subset of Shift|Alt|Ctrl|Super: never a lock bit, never
// Hyper or Meta, which no binding can name.
type Seq struct {
	Code rune
	Mods Mods
}

// bindable are the modifiers a Seq can hold.
const bindable = Shift | Alt | Ctrl | Super

// heavy are the modifiers with which a printable key is not text: the key is a chord, not a character.
const heavy = Alt | Ctrl | Super

// ErrMultiChord refuses a sequence of several chords ("Ctrl+K, Ctrl+C"): only single chords bind.
var ErrMultiChord = errs.Sentinel(errs.ErrUnsupported, "key: a sequence of several chords")

// modifierNames maps a modifier name, lower-cased, onto its bit. "meta" reads as Alt, the Emacs
// meaning golib's Shortcut has always given it; Qt's Meta is the platform's Meta key (the Windows key
// on Linux), which is Super here. A recorded deviation (ADR 1792255248 rationale §R12).
var modifierNames = map[string]Mods{
	"ctrl": Ctrl, "control": Ctrl,
	"alt": Alt, "meta": Alt,
	"shift": Shift,
	"super": Super,
}

// keyNames maps a named key, lower-cased, onto its code. A new key is a row here and in names.
var keyNames = map[string]rune{
	"esc": Escape, "escape": Escape,
	"enter": Enter, "return": Enter,
	"tab": Tab, "backspace": Backspace, "space": ' ',
	"up": Up, "down": Down, "left": Left, "right": Right,
	"home": Home, "end": End,
	"pgup": PageUp, "pageup": PageUp,
	"pgdown": PageDown, "pagedown": PageDown,
	"ins": Insert, "insert": Insert,
	"del": Delete, "delete": Delete,
	"f1": F1, "f2": F2, "f3": F3, "f4": F4,
	"f5": F5, "f6": F6, "f7": F7, "f8": F8,
	"f9": F9, "f10": F10, "f11": F11, "f12": F12,
	"menu": Menu,
}

// names is the canonical spelling of each named key, what String writes.
var names = map[rune]string{
	Escape: "Esc", Enter: "Enter", Tab: "Tab", Backspace: "Backspace", ' ': "Space",
	Up: "Up", Down: "Down", Left: "Left", Right: "Right", Home: "Home", End: "End",
	PageUp: "PgUp", PageDown: "PgDown", Insert: "Ins", Delete: "Del",
	F1: "F1", F2: "F2", F3: "F3", F4: "F4", F5: "F5", F6: "F6",
	F7: "F7", F8: "F8", F9: "F9", F10: "F10", F11: "F11", F12: "F12",
	Menu: "Menu",
}

// Parse reads one chord in Qt's portable text, its names case-insensitive as Qt's are:
//
//	"Ctrl+S", "Shift+Down", "F10", "?", "G" (the key g), "Shift+G" (the capital)
//	"+", "Ctrl++"     the plus key: a '+' after a '+' separator, or alone, is the key
//	",", "Ctrl+,"     the comma key: a ',' that ends the text is the key
//	"Ctrl+K, Ctrl+C"  refused with ErrMultiChord: Qt separates chords with ", "
//
// Shift with a printable key that is not a letter, and no Ctrl, Alt or Super, is refused: Shift+/
// types '?', and that key is written "?", the character it types.
func Parse(s string) (Seq, error) {
	text := strings.TrimSpace(s)
	if text == "" {
		return Seq{}, errs.Wrap(errs.ErrInvalidArgument, "key: an empty key sequence")
	}
	if strings.Contains(text, ", ") {
		return Seq{}, fmt.Errorf("%w: %q", ErrMultiChord, s)
	}
	var mods []string
	var name string
	switch {
	case text == "+":
		name = "+"
	case strings.HasSuffix(text, "++"):
		mods, name = strings.Split(text[:len(text)-2], "+"), "+"
	default:
		parts := strings.Split(text, "+")
		mods, name = parts[:len(parts)-1], parts[len(parts)-1]
	}
	var seq Seq
	for _, m := range mods {
		m = strings.TrimSpace(m)
		if m == "" {
			return Seq{}, errs.Wrap(errs.ErrInvalidArgument, "key: the key sequence %q has an empty part", s)
		}
		bit, ok := modifierNames[strings.ToLower(m)]
		if !ok {
			return Seq{}, errs.Wrap(errs.ErrInvalidArgument, "key: %q in %q is not a modifier (Ctrl, Alt, Shift or Super)", m, s)
		}
		seq.Mods |= bit
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Seq{}, errs.Wrap(errs.ErrInvalidArgument, "key: the key sequence %q has an empty part", s)
	}
	if code, ok := keyNames[strings.ToLower(name)]; ok {
		seq.Code = code
	} else {
		r, size := utf8.DecodeRuneInString(name)
		if size != len(name) || !unicode.IsPrint(r) {
			return Seq{}, errs.Wrap(errs.ErrInvalidArgument, "key: %q in %q is not a key name or a single character", name, s)
		}
		seq.Code = unicode.ToLower(r)
	}
	if seq.Mods&Shift != 0 && seq.Mods&heavy == 0 && printable(seq.Code) && !unicode.IsLetter(seq.Code) {
		return Seq{}, errs.Wrap(errs.ErrInvalidArgument, "key: %q: write the character Shift types, not Shift with its key", s)
	}
	return seq, nil
}

// MustParse is the defaults a command declares in Go: each text parsed, a typo a panic at start.
func MustParse(texts ...string) []Seq {
	out := make([]Seq, len(texts))
	for i, t := range texts {
		s, err := Parse(t)
		if err != nil {
			panic(err)
		}
		out[i] = s
	}
	return out
}

// String is the canonical spelling: Ctrl, Alt, Super, Shift in that order, then the key's name, a
// letter upper-cased as Qt writes it ("Ctrl+S"). Parse(s.String()) is s.
func (s Seq) String() string {
	var b strings.Builder
	for _, m := range []struct {
		bit  Mods
		name string
	}{{Ctrl, "Ctrl"}, {Alt, "Alt"}, {Super, "Super"}, {Shift, "Shift"}} {
		if s.Mods&m.bit != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	switch name, ok := names[s.Code]; {
	case ok:
		b.WriteString(name)
	case printable(s.Code):
		b.WriteRune(unicode.ToUpper(s.Code))
	default:
		fmt.Fprintf(&b, "Key(0x%x)", s.Code)
	}
	return b.String()
}

// printable reports whether code is a character a key types, rather than a functional key.
func printable(code rune) bool {
	if _, named := names[code]; named && code != ' ' {
		return false
	}
	return unicode.IsPrint(code)
}
