package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// MaxEncoded is the longest element [Encode] writes, in bytes. File systems allow 255; the rest is
// room for a caller's suffix (".lock", a temporary name) inside the one element.
const MaxEncoded = 240

const (
	emptyName = "%_" // the empty name, the one escape that isn't two hex digits
	hashMark  = "%~" // begins the hash of a name too long to keep whole
	hashLen   = 32   // hex digits of it
	upperHex  = "0123456789ABCDEF"
)

// Encode is name as one legal path element, the same on every OS. Each byte other than a lowercase
// letter, a digit, '.', '_' or '-' becomes %XX in uppercase hex, which also keeps two names differing
// only by case apart where the file system can't. A last '.', and the first letter of a stem Windows
// reserves (con, nul, com1...), are escaped too. The result is never empty, "." or "..".
//
// A result past [MaxEncoded] bytes becomes a prefix of itself, [hashMark] and a hash of name:
// [Decode] then answers [ErrNotReversible].
func Encode(name string) string {
	if name == "" {
		return emptyName
	}
	var b strings.Builder
	reservedStem := isReservedStem(stemOf(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if plainByte(c) && !(c == '.' && i == len(name)-1) && !(reservedStem && i == 0) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&0xF])
	}
	e := b.String()
	if len(e) <= MaxEncoded {
		return e
	}
	cut := MaxEncoded - len(hashMark) - hashLen
	for _, back := range []int{1, 2} { // never inside an escape
		if e[cut-back] == '%' {
			cut -= back
			break
		}
	}
	sum := sha256.Sum256([]byte(name))
	return e[:cut] + hashMark + hex.EncodeToString(sum[:])[:hashLen]
}

// Decode reverses [Encode], and accepts only what Encode writes, so two elements never decode to one
// name: lowercase hex, an escape that wasn't needed and a bare % are each [ErrNotCanonical]. The
// hashed form of a long name is [ErrNotReversible].
func Decode(element string) (string, error) {
	if element == emptyName {
		return "", nil
	}
	if strings.Contains(element, hashMark) {
		if !wellFormedHashed(element) {
			return "", ErrNotCanonical
		}
		return "", ErrNotReversible
	}
	name, ok := unescape(element)
	if !ok || Encode(name) != element {
		return "", ErrNotCanonical
	}
	return name, nil
}

// unescape reads element's bytes and %XX escapes, uppercase hex only.
func unescape(element string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(element); i++ {
		c := element[i]
		switch {
		case c == '%':
			if i+2 >= len(element) {
				return "", false
			}
			hi, lo := strings.IndexByte(upperHex, element[i+1]), strings.IndexByte(upperHex, element[i+2])
			if hi < 0 || lo < 0 {
				return "", false
			}
			b.WriteByte(byte(hi<<4 | lo))
			i += 2
		case plainByte(c):
			b.WriteByte(c)
		default:
			return "", false
		}
	}
	return b.String(), true
}

// wellFormedHashed is whether element has the exact shape of Encode's hashed form.
func wellFormedHashed(element string) bool {
	i := strings.Index(element, hashMark)
	if i != strings.LastIndex(element, hashMark) || len(element) > MaxEncoded || len(element) < MaxEncoded-2 {
		return false
	}
	if _, ok := unescape(element[:i]); !ok {
		return false
	}
	sum := element[i+len(hashMark):]
	if len(sum) != hashLen {
		return false
	}
	for j := 0; j < len(sum); j++ {
		if !strings.ContainsRune("0123456789abcdef", rune(sum[j])) {
			return false
		}
	}
	return true
}

func plainByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
}

// stemOf is the part of an element before its first dot, which is what Windows reserves.
func stemOf(element string) string {
	stem, _, _ := strings.Cut(element, ".")
	return stem
}

// isReservedStem is whether Windows reserves stem as a device, in any case: what [Encode] escapes.
// Its ASCII names alone: a superscript (COM¹) is escaped by Encode as any non-plain byte is, so its
// encoded form is never a device, and Encode's output stays as it was.
func isReservedStem(stem string) bool {
	s := strings.ToLower(strings.TrimRight(stem, " "))
	switch s {
	case "con", "prn", "aux", "nul", "conin$", "conout$":
		return true
	}
	if len(s) == 4 && (s[:3] == "com" || s[:3] == "lpt") && s[3] >= '0' && s[3] <= '9' {
		return true
	}
	return false
}

// deviceStem is whether Windows opens a name of stem as a device, in any case, its trailing dots and
// spaces dropped as Windows drops them: isReservedStem's names, and COM or LPT with a superscript
// one, two or three (COM¹).
func deviceStem(stem string) bool {
	s := strings.ToLower(strings.TrimRight(stem, ". "))
	if isReservedStem(s) {
		return true
	}
	if len(s) < 4 || (s[:3] != "com" && s[:3] != "lpt") {
		return false
	}
	n := s[3:]
	return n == "\u00b9" || n == "\u00b2" || n == "\u00b3" // ¹ ² ³
}

// WindowsDevice reports whether Windows opens a path element named element as a device rather than
// a file: CON, PRN, AUX, NUL, CONIN$, CONOUT$, COM0 to COM9 and LPT0 to LPT9 (and COM¹ to COM³,
// LPT¹ to LPT³), in any case, with any extension ("nul.txt", "CON.tar.gz") and with trailing dots
// or spaces ("NUL.", "nul .md"). A near name is a file: "CONSOLE", "nul_", "COM10", "LPT". It is
// Windows' rule alone; on Unix every such name is an ordinary file.
func WindowsDevice(element string) bool { return deviceStem(stemOf(element)) }
