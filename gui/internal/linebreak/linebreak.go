// Package linebreak is where a line of text may break: one rule for gui's wrappers, flow's
// paragraphs and the off-loop Measurer. A line breaks at white space, and after a CJK character or
// a hyphen with no space following (UAX #14's ID and HY classes, approximately). A run with
// neither never breaks: a long word overflows its line.
package linebreak

import "unicode"

// IsSpace reports white space a line breaks at: the ASCII spaces CSS collapses. A no-break space
// is not one.
func IsSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }

// After reports whether a line may break after r with no space following it: after a CJK
// ideograph, kana or Hangul syllable, or a hyphen.
func After(r rune) bool {
	return r == '-' || unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// Unit is a run of text no line breaks inside, and whether white space comes before it.
type Unit struct {
	Text   string
	Spaced bool
}

// Units splits s into its units: the runs between white space, each cut after every character
// After allows a break after. The white space itself is dropped; a unit's Spaced says where it was.
func Units(s string) []Unit {
	var out []Unit
	spaced, start := false, -1
	for i, r := range s {
		switch {
		case IsSpace(r):
			if start >= 0 {
				out = append(out, Unit{Text: s[start:i], Spaced: spaced})
				start = -1
			}
			spaced = len(out) > 0
		default:
			if start < 0 {
				start = i
			}
			if After(r) {
				end := i + len(string(r))
				out = append(out, Unit{Text: s[start:end], Spaced: spaced})
				start, spaced = -1, false
			}
		}
	}
	if start >= 0 {
		out = append(out, Unit{Text: s[start:], Spaced: spaced})
	}
	return out
}
