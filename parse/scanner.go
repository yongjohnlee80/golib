package parse

import (
	"unicode/utf8"
)

// Scanner is a rune cursor over source text that keeps track of where it is.
//
// Every format needs this and none of them need a different one, which is why
// it lives in the core rather than being written once per format. It does no
// tokenizing of its own: what counts as a string, a comment or a word differs
// per format, and a scanner that guessed would be wrong for most of them.
//
// A Scanner is a mutable cursor, so it is used through a pointer and is not
// safe for concurrent use. Create one per parse.
type Scanner struct {
	src  []byte
	pos  Position
	prev Position
}

// NewScanner returns a Scanner positioned at the first rune of src.
func NewScanner(src []byte) *Scanner {
	start := Position{Offset: 0, Line: 1, Column: 1}
	return &Scanner{src: src, pos: start, prev: start}
}

// Pos returns the position of the next rune to be read.
func (s *Scanner) Pos() Position { return s.pos }

// Done reports whether the whole source has been consumed.
func (s *Scanner) Done() bool { return s.pos.Offset >= len(s.src) }

// Next consumes one rune and returns it. The boolean is false at end of input,
// where the rune is meaningless.
//
// Invalid UTF-8 is returned as utf8.RuneError and consumed one BYTE at a time,
// so a malformed source still advances and cannot spin a caller forever.
func (s *Scanner) Next() (rune, bool) {
	if s.Done() {
		return 0, false
	}
	s.prev = s.pos

	r, size := utf8.DecodeRune(s.src[s.pos.Offset:])
	if r == utf8.RuneError && size <= 1 {
		size = 1
	}
	s.pos.Offset += size
	if r == '\n' {
		s.pos.Line++
		s.pos.Column = 1
	} else {
		s.pos.Column++
	}
	return r, true
}

// Peek returns the next rune without consuming it. The boolean is false at end
// of input.
func (s *Scanner) Peek() (rune, bool) {
	if s.Done() {
		return 0, false
	}
	r, _ := utf8.DecodeRune(s.src[s.pos.Offset:])
	return r, true
}

// PeekAt returns the rune n runes ahead without consuming anything, where
// PeekAt(0) is the same rune Peek returns. The boolean is false when the source
// ends before that rune.
func (s *Scanner) PeekAt(n int) (rune, bool) {
	off := s.pos.Offset
	var r rune
	for i := 0; i <= n; i++ {
		if off >= len(s.src) {
			return 0, false
		}
		var size int
		r, size = utf8.DecodeRune(s.src[off:])
		if r == utf8.RuneError && size <= 1 {
			size = 1
		}
		off += size
	}
	return r, true
}

// Unread steps back over the rune most recently returned by Next.
//
// Only one step is kept, so two calls without an intervening Next return to the
// same place rather than going further back. This is enough for the one-rune
// lookahead a hand-written scanner needs, and refusing to pretend otherwise
// keeps callers from relying on a deeper history that is not there.
func (s *Scanner) Unread() { s.pos = s.prev }

// HasPrefix reports whether the source at the cursor begins with lit, without
// consuming anything.
func (s *Scanner) HasPrefix(lit string) bool {
	rest := s.src[s.pos.Offset:]
	if len(rest) < len(lit) {
		return false
	}
	return string(rest[:len(lit)]) == lit
}

// Take consumes lit and reports whether it was there. Nothing is consumed when
// it was not.
func (s *Scanner) Take(lit string) bool {
	if !s.HasPrefix(lit) {
		return false
	}
	for range lit {
		s.Next()
	}
	return true
}

// Slice returns the source between two offsets. It is the caller's job to pass
// offsets that came from this scanner's positions.
func (s *Scanner) Slice(from, to int) []byte {
	if from < 0 {
		from = 0
	}
	if to > len(s.src) {
		to = len(s.src)
	}
	if from >= to {
		return nil
	}
	return s.src[from:to]
}

// Errorf builds a [SyntaxError] at the cursor for the given format.
//
// It exists so a format's scanning code reads as one line per failure and every
// such error comes out with a position attached, rather than each format
// remembering to fill the fields in the same order.
func (s *Scanner) Errorf(format, want, got string) error {
	return SyntaxError{Format: format, Pos: s.pos, Want: want, Got: got}
}
