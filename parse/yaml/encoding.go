package yaml

import (
	"unicode/utf16"
	"unicode/utf8"
)

// detectEncoding reads the encoding from a byte order mark, or else from the pattern of null bytes
// the first character makes, as spec 5.2 describes: a stream starts with an ASCII character or a
// BOM, so its first bytes say how wide its characters are.
func detectEncoding(b []byte) Encoding {
	at := func(i int) int {
		if i < len(b) {
			return int(b[i])
		}
		return -1
	}
	switch {
	case at(0) == 0 && at(1) == 0 && at(2) == 0xFE && at(3) == 0xFF:
		return UTF32BE
	case at(0) == 0 && at(1) == 0 && at(2) == 0 && at(3) > 0:
		return UTF32BE
	case at(0) == 0xFF && at(1) == 0xFE && at(2) == 0 && at(3) == 0:
		return UTF32LE
	case at(0) > 0 && at(1) == 0 && at(2) == 0 && at(3) == 0:
		return UTF32LE
	case at(0) == 0xFE && at(1) == 0xFF:
		return UTF16BE
	case at(0) == 0 && at(1) > 0:
		return UTF16BE
	case at(0) == 0xFF && at(1) == 0xFE:
		return UTF16LE
	case at(0) > 0 && at(1) == 0:
		return UTF16LE
	}
	return UTF8
}

// toUTF8 transcodes b from enc. It returns the offset of the first byte it cannot decode, or -1.
func toUTF8(b []byte, enc Encoding) ([]byte, int) {
	switch enc {
	case UTF16LE, UTF16BE:
		if len(b)%2 != 0 {
			return nil, len(b) - 1
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			if enc == UTF16LE {
				u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
			} else {
				u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
			}
		}
		out := make([]byte, 0, len(b))
		for i := 0; i < len(u); i++ {
			r := rune(u[i])
			if utf16.IsSurrogate(r) {
				if i+1 >= len(u) {
					return nil, 2 * i
				}
				r = utf16.DecodeRune(r, rune(u[i+1]))
				if r == utf8.RuneError {
					return nil, 2 * i
				}
				i++
			}
			out = utf8.AppendRune(out, r)
		}
		return out, -1
	case UTF32LE, UTF32BE:
		if len(b)%4 != 0 {
			return nil, len(b) - len(b)%4
		}
		out := make([]byte, 0, len(b)/2)
		for i := 0; i+4 <= len(b); i += 4 {
			var v uint32
			if enc == UTF32LE {
				v = uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
			} else {
				v = uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
			}
			if v > utf8.MaxRune || (v >= 0xD800 && v <= 0xDFFF) {
				return nil, i
			}
			out = utf8.AppendRune(out, rune(v))
		}
		return out, -1
	}
	if !utf8.Valid(b) {
		for i := 0; i < len(b); {
			r, size := utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && size <= 1 {
				return nil, i
			}
			i += size
		}
	}
	return b, -1
}

// printable is c-printable (spec 5.1): the characters a YAML stream may hold.
func printable(r rune) bool {
	switch {
	case r == 0x9 || r == 0xA || r == 0xD || (r >= 0x20 && r <= 0x7E) || r == 0x85:
		return true
	case r >= 0xA0 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000 && r <= 0x10FFFF:
		return true
	}
	return false
}

// firstUnprintable returns the offset of the first character of src that is not c-printable, or -1.
func firstUnprintable(src []byte) int {
	for i := 0; i < len(src); {
		c := src[i]
		if c < 0x80 {
			if !printable(rune(c)) {
				return i
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(src[i:])
		if !printable(r) {
			return i
		}
		i += size
	}
	return -1
}

// charLen is the byte length of the UTF-8 character at the start of b.
func charLen(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	if b[0] < 0x80 {
		return 1
	}
	_, size := utf8.DecodeRune(b)
	return size
}
