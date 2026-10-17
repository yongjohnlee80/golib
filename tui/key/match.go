package key

import (
	"unicode"
	"unicode/utf8"
)

// Of is the chord a key event is. ok is false for an event no binding may match: one with Hyper or
// Meta held, which must never fire the command bound to the same key unmodified.
//
//  1. Lock bits are dropped (Mods.Chord).
//  2. With no Ctrl, Alt or Super, a one-rune printable text IS the key, and its Shift is spent on it:
//     kitty's ('/', Shift, "?") and a legacy terminal's '?' are both Seq{'?'}.
//  3. A letter's case is its Shift: 'G' and ('g', Shift, "G") are both {g, Shift}.
//  4. With Ctrl, Alt or Super a letter is lower-cased and Shift kept as the event reports it: a
//     terminal cannot be relied on to report the case of a Ctrl letter (Candidates).
func Of(code rune, mods Mods, text string) (Seq, bool) {
	mods = mods.Chord()
	if mods&(Hyper|Meta) != 0 {
		return Seq{}, false
	}
	if mods&heavy == 0 {
		if r, size := utf8.DecodeRuneInString(text); size > 0 && size == len(text) && unicode.IsPrint(r) {
			code = r
			mods &^= Shift
		}
	}
	if unicode.IsUpper(code) {
		if mods&heavy == 0 {
			mods |= Shift
		}
		code = unicode.ToLower(code)
	}
	return Seq{Code: code, Mods: mods & bindable}, true
}

// Candidates is what an event's chord may be bound as, in order: the exact chord, then, for a Ctrl or
// Alt letter with Shift, the chord without Shift, because a legacy terminal sends Ctrl+Shift+S as
// Ctrl+S. A lookup takes the first candidate any binding holds, so with both bound the exact one wins.
func Candidates(s Seq) []Seq {
	if fallback(s) {
		return []Seq{s, {Code: s.Code, Mods: s.Mods &^ Shift}}
	}
	return []Seq{s}
}

// Accepts is every event chord whose Candidates hold s: s itself and, for a Ctrl or Alt letter without
// Shift, the same chord with Shift. It is finite, so a query can drive the dispatch rules over it.
func Accepts(s Seq) []Seq {
	if shifted := (Seq{Code: s.Code, Mods: s.Mods | Shift}); s.Mods&Shift == 0 && fallback(shifted) {
		return []Seq{s, shifted}
	}
	return []Seq{s}
}

// Overlaps reports whether some event's Candidates hold both a and b. It is symmetric, so it answers
// "is some event contested?" (a warning), never "is b still reachable past a?", which is directional.
func Overlaps(a, b Seq) bool {
	for _, x := range Accepts(a) {
		for _, y := range Accepts(b) {
			if x == y {
				return true
			}
		}
	}
	return false
}

// fallback reports whether s is a chord Candidates widens: a Ctrl or Alt letter with Shift.
func fallback(s Seq) bool {
	return s.Mods&Shift != 0 && s.Mods&(Ctrl|Alt) != 0 && unicode.IsLetter(s.Code)
}
