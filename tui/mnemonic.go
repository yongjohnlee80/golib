package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui/i18n"
)

// ParseMnemonic splits "&"-marked text, as Qt writes a mnemonic: "&File" is the label
// "File" with hotkey 'f', and "파일(&f)" is the label "파일(f)" with hotkey 'f'.
//
// label is the text to show; hotkey is the marked character, lower-cased, or 0; idx is the
// grapheme cluster of label to underline, counted from 0, or -1 when nothing is marked.
// Only the first marker counts. "&&" is a literal "&", and so is a "&" that ends the text.
//
// Every widget and the declarative layer parse "&" here, so a label cannot mean one key in
// a menu and another on a button.
func ParseMnemonic(text string) (label string, hotkey rune, idx int) {
	idx = -1
	var b strings.Builder
	clusters := 0
	marking := false
	for c := range Graphemes(text) {
		switch {
		case marking:
			marking = false
			if c == "&" {
				b.WriteString("&")
				clusters++
				continue
			}
			if idx < 0 {
				r, _ := utf8.DecodeRuneInString(c)
				idx, hotkey = clusters, unicode.ToLower(r)
			}
		case c == "&":
			marking = true
			continue
		}
		b.WriteString(c)
		clusters++
	}
	if marking {
		b.WriteString("&")
	}
	return b.String(), hotkey, idx
}

// englishMnemonic keeps a translated label's hotkey on English's letter, so a shortcut
// never changes with the language: English "&File" shown in German as "&Datei" becomes
// "Datei(&f)", still on Alt+F.
//
// A translation's own marker is kept only where it marks English's letter. A translation
// marking another letter, or none, has its marker dropped and English's appended in
// parentheses; one English gives no hotkey to has its marker dropped. An id English does
// not carry has no letter to keep, and is shown as the catalog wrote it.
func englishMnemonic(text string, s *i18n.Set, id string) string {
	en, ok := s.Lookup(i18n.English, id)
	if !ok {
		return text
	}
	_, want, _ := ParseMnemonic(en)
	label, got, _ := ParseMnemonic(text)
	switch {
	case got == want:
		return text
	case want == 0:
		return escapeAmp(label)
	default:
		return escapeAmp(label) + "(&" + string(want) + ")"
	}
}

// escapeAmp writes a parsed label back as marked text, so a literal "&" stays one.
func escapeAmp(label string) string { return strings.ReplaceAll(label, "&", "&&") }
