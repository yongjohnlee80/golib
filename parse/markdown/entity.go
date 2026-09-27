package markdown

import (
	"html"
	"strconv"
	"unicode/utf8"
)

// entityAt decodes an entity or numeric character reference at b[i] ('&'), returning its UTF-8 value
// and length. Named references must be HTML5 entity names ending in ';'; numeric ones of 0, of a
// surrogate or beyond U+10FFFF decode to U+FFFD.
//
// https://spec.commonmark.org/0.31.2/#entity-and-numeric-character-references
func entityAt(b []byte, i int) ([]byte, int, bool) {
	if i+2 >= len(b) || b[i] != '&' {
		return nil, 0, false
	}
	j := i + 1
	if b[j] == '#' {
		j++
		hex := j < len(b) && (b[j] == 'x' || b[j] == 'X')
		if hex {
			j++
		}
		start := j
		for j < len(b) && ((hex && isHexDigit(b[j])) || (!hex && isDigit(b[j]))) {
			j++
		}
		digits := j - start
		if digits == 0 || (hex && digits > 6) || (!hex && digits > 7) || j >= len(b) || b[j] != ';' {
			return nil, 0, false
		}
		base := 10
		if hex {
			base = 16
		}
		v, err := strconv.ParseUint(string(b[start:j]), base, 32)
		r := rune(v)
		if err != nil || v == 0 || !utf8.ValidRune(r) {
			r = utf8.RuneError
		}
		return utf8.AppendRune(nil, r), j + 1 - i, true
	}
	start := j
	for j < len(b) && j-start < 32 && isASCIIAlnum(b[j]) {
		j++
	}
	if j == start || !isASCIIAlpha(b[start]) || j >= len(b) || b[j] != ';' {
		return nil, 0, false
	}
	ref := string(b[i : j+1])
	v := html.UnescapeString(ref)
	// UnescapeString also expands a legacy prefix of an unknown name ("&ampx;" → "&x;"), leaving
	// the rest of the name and its ';'; only a complete match leaves no ';' (bar "&semi;" itself)
	if v == ref || (len(v) > 1 && v[len(v)-1] == ';') {
		return nil, 0, false
	}
	return []byte(v), j + 1 - i, true
}

// unescapeAll decodes backslash escapes and entity references, for destinations, titles and info
// strings.
func unescapeAll(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case c == '\\' && i+1 < len(b) && isASCIIPunct(b[i+1]):
			out = append(out, b[i+1])
			i++
		case c == '&':
			if v, n, ok := entityAt(b, i); ok {
				out = append(out, v...)
				i += n - 1
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	if out == nil {
		out = []byte{}
	}
	return out
}

func decodeInline(b []byte) []byte { return unescapeAll(b) }
