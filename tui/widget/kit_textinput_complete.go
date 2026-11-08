package widget

import (
	"strings"
	"unicode/utf8"
)

// walkStep is Tab's next place in a walk through n candidates from at (-1 before the first):
// forward from the first, or with back (Shift+Tab) from the last, wrapping round. CommandLine's
// completion and TextInput's walk by it.
func walkStep(at, n int, back bool) int {
	switch {
	case at < 0 && back:
		return n - 1
	case at < 0:
		return 0
	case back:
		return (at + n - 1) % n
	}
	return (at + 1) % n
}

// wordBefore is the word ending at cursor and where it begins.
func wordBefore(text string, cursor int) (start int, word string) {
	cursor = min(max(cursor, 0), len(text))
	if open := openQuote(text[:cursor]); open >= 0 {
		return open, text[open:cursor]
	}
	start = cursor
	for start > 0 && !completeStop(text[start-1]) {
		start--
	}
	return start, text[start:cursor]
}

// openQuote is where an unclosed double quote opens in s, or -1; a quote inside a single-quoted
// string does not count, nor one doubled inside a quoted name.
func openQuote(s string) int {
	open, single := -1, false
	for i := 0; i < len(s); i++ {
		switch {
		case open >= 0 && s[i] == '"' && i+1 < len(s) && s[i+1] == '"':
			i++ // a doubled quote inside the name
		case open >= 0 && s[i] == '"':
			open = -1
		case open < 0 && s[i] == '\'':
			single = !single
		case open < 0 && !single && s[i] == '"':
			open = i
		}
	}
	return open
}

func completeStop(b byte) bool {
	return orderSpace(b) || strings.IndexByte(`()=!<>'",`, b) >= 0
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// commonPrefix is the longest prefix every candidate's name shares, at least typed.
func commonPrefix(cs []string, typed string) string {
	p := cs[0]
	for _, c := range cs[1:] {
		for !strings.HasPrefix(c, p) {
			_, size := utf8.DecodeLastRuneInString(p) // a script's arguments need not be ASCII
			p = p[:len(p)-size]
		}
	}
	if len(p) < len(typed) {
		return typed
	}
	return p
}
