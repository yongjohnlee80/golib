package markdown

import (
	"unicode"
	"unicode/utf8"
)

// Option configures Parse.
type Option func(*config)

type config struct {
	gfm     bool
	special [256]bool // the bytes that end a run of plain text in the inline phase
}

// init completes a config once its options have run.
func (c *config) init() {
	for _, b := range []byte("\n\\`*_[!]<&") {
		c.special[b] = true
	}
	if c.gfm {
		for _, b := range []byte("~.:") {
			c.special[b] = true
		}
	}
}

func utf8Len(b []byte) int {
	_, size := utf8.DecodeRune(b)
	return size
}

func isSpaceOrTab(c byte) bool { return c == ' ' || c == '\t' }

func isLineEnd(c byte) bool { return c == '\n' || c == '\r' }

func isASCIIPunct(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isASCIIAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isASCIIAlnum(c byte) bool { return isASCIIAlpha(c) || isDigit(c) }

// isUnicodeWhitespace is CommonMark's Unicode whitespace: the Zs category, tab, line feed, form feed
// and carriage return.
//
// https://spec.commonmark.org/0.31.2/#unicode-whitespace-character
func isUnicodeWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\f', '\r':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

func isUnicodeLetterOrDigit(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// isUnicodePunct is CommonMark's Unicode punctuation: ASCII punctuation, or the P and S categories.
//
// https://spec.commonmark.org/0.31.2/#unicode-punctuation-character
func isUnicodePunct(r rune) bool {
	if r < 0x80 {
		return isASCIIPunct(byte(r))
	}
	return unicode.In(r, unicode.P, unicode.S)
}

// runeBefore decodes the rune ending at i in b, or reports a line end when i is the start.
func runeBefore(b []byte, i int) rune {
	if i <= 0 {
		return '\n'
	}
	r, _ := utf8.DecodeLastRune(b[:i])
	return r
}

// runeAt decodes the rune starting at i in b, or reports a line end past the end.
func runeAt(b []byte, i int) rune {
	if i >= len(b) {
		return '\n'
	}
	r, _ := utf8.DecodeRune(b[i:])
	return r
}
