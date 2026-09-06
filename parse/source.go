package parse

import (
	"sort"
	"strconv"
	"unicode/utf8"
)

// Location is a human-facing position: a byte offset with one-based line and
// column. Its Offset is int64, so it does not truncate on a 32-bit build the way
// an int offset would — which is the whole reason it is a type of its own rather
// than the package's existing Position, whose int Offset and callers are left as
// they are for now.
//
// Its semantics are pinned to what Scanner already does, so a diagnostic does
// not change meaning under the streaming core: lines and columns are one-based,
// COLUMNS ARE COUNTED IN RUNES, and invalid UTF-8 consumes one byte and one
// column. The pinning is not a comment — source_test.go cross-checks every
// offset against a Scanner walk of the same bytes.
type Location struct {
	Offset int64
	Line   int // one-based
	Column int // one-based, counted in runes
}

// String renders the location as line:column, the form a diagnostic quotes.
func (l Location) String() string {
	return strconv.Itoa(l.Line) + ":" + strconv.Itoa(l.Column)
}

// Source resolves a byte offset to a Location on demand.
//
// It holds a line index — the offsets at which lines begin — built from the
// newlines the lexer passes over anyway, and NOT a copy of the source. Line is
// answered from the index alone, in O(log lines). Column needs the bytes of the
// line up to the offset, because it is a RUNE count, so Source is given a byte
// accessor rather than owning the bytes: over a []byte it is the slice itself
// (no copy), and over a stream it reads back through the cache. Charging column
// resolution the cost of the line's bytes, only when a caller asks, is the trade
// that keeps the token stream itself O(1) per token.
//
// The zero value is unusable. Sources are built by Scan; newBytesSource builds
// one over a whole slice, which is also what the tests drive.
type Source struct {
	// lineStarts[i] is the offset of the first byte of line i+2 (line 1 begins
	// at offset 0 implicitly). Strictly increasing: appended once per '\n' the
	// lexer consumes, in offset order.
	lineStarts []int64

	// bytesAt returns the bytes in [from, to). It clamps its own arguments; over
	// a released stream region it may return an error, in which case Line is
	// still exact and Column is reported as 0 rather than guessed.
	bytesAt func(from, to int64) ([]byte, error)
}

// newSource returns an empty Source whose line index the caller fills with
// noteNewlineAt as it advances. Used by the streaming Scan path.
func newSource(bytesAt func(from, to int64) ([]byte, error)) *Source {
	return &Source{bytesAt: bytesAt}
}

// newBytesSource returns a Source over the whole of b, with its line index
// precomputed. The slice is NOT copied: bytesAt returns sub-slices of it.
//
// A '\n' byte begins a line however it is reached, which is exactly Scanner's
// rule — a bad lead byte consumes only itself, so a following 0x0A is still
// decoded as a newline — so scanning for the byte gives the same line breaks a
// rune walk would.
func newBytesSource(b []byte) *Source {
	var starts []int64
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			starts = append(starts, int64(i+1))
		}
	}
	return &Source{
		lineStarts: starts,
		bytesAt: func(from, to int64) ([]byte, error) {
			if from < 0 {
				from = 0
			}
			if to > int64(len(b)) {
				to = int64(len(b))
			}
			if from >= to {
				return nil, nil
			}
			return b[from:to], nil
		},
	}
}

// noteNewlineAt records that a line begins just after the '\n' at nlOffset. It
// must be called in increasing offset order and at most once per newline; the
// streaming lexer calls it as it consumes each 0x0A.
func (s *Source) noteNewlineAt(nlOffset int64) {
	s.lineStarts = append(s.lineStarts, nlOffset+1)
}

// LocationAt resolves off to a Location. The line comes from the index; the
// column is one plus the number of runes between the line's start and off.
//
// A negative offset is the caller's error and answers line 1, column 1 with a
// zero offset rather than panicking on a slice — a location is a diagnostic, and
// a diagnostic that panics is worse than one that is merely at the origin.
func (s *Source) LocationAt(off int64) (Location, error) {
	if off < 0 {
		return Location{Offset: 0, Line: 1, Column: 1}, nil
	}
	// The number of recorded line starts at or before off is the count of
	// newlines already passed, which is the line number minus one.
	n := sort.Search(len(s.lineStarts), func(i int) bool { return s.lineStarts[i] > off })
	line := n + 1
	var lineStart int64
	if n > 0 {
		lineStart = s.lineStarts[n-1]
	}
	b, err := s.bytesAt(lineStart, off)
	if err != nil {
		return Location{Offset: off, Line: line, Column: 0}, err
	}
	return Location{Offset: off, Line: line, Column: 1 + runeLen(b)}, nil
}

// runeLen counts runes the way Scanner advances over them: a valid rune counts
// once, and invalid UTF-8 counts one per byte. The guard is size <= 1 rather
// than a RuneError test alone, so a genuine U+FFFD in the source (three bytes)
// is one rune, not three.
func runeLen(b []byte) int {
	n := 0
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 {
			size = 1
		}
		i += size
		n++
	}
	return n
}
