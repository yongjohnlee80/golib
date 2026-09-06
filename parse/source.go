package parse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf8"
)

// ErrLocationReleased reports that an offset was valid but its bytes have been
// released, so neither its line nor its column can be given any longer. It is
// not a caller error: a streaming Source keeps locations only for the live
// window, so a caller that wants a token's location resolves it before the
// retention behind that token is dropped.
var ErrLocationReleased = errors.New("parse: location released")

// ErrLocationRange reports an offset this Source cannot resolve: negative, past
// the bytes read so far, or inside a multibyte rune — a location is a position
// between runes, and the interior of one is not a position.
var ErrLocationRange = errors.New("parse: location offset out of range")

// Location is a human-facing position: a byte offset with one-based line and
// column. Its Offset is int64, so it does not truncate on a 32-bit build the way
// an int offset would — which is the whole reason it is a type of its own rather
// than the package's existing Position, whose int Offset and callers are left as
// they are for now.
//
// Line and column are one-based, columns are counted in RUNES, and invalid UTF-8
// counts one byte as one column — pinned to what Scanner already does so a
// diagnostic does not change meaning under the streaming core.
type Location struct {
	Offset int64
	Line   int // one-based
	Column int // one-based, counted in runes
}

// String renders the location as line:column, the form a diagnostic quotes.
func (l Location) String() string {
	return strconv.Itoa(l.Line) + ":" + strconv.Itoa(l.Column)
}

// Source resolves a byte offset to a Location on demand, over the LIVE WINDOW of
// a stream. It holds a line index — the offsets lines begin at, built from the
// newlines the lexer passes over — not a copy of the source, and it reclaims
// that index with the cache watermark so its memory is bounded by what is still
// retained rather than by the length of the stream. Column resolution reads the
// line's bytes through a lifetime-bearing accessor: over a []byte the slice
// itself, over a stream a cache View held only for the call, so Source never
// keeps a borrowed slice past a lookup and never copies for its own sake.
//
// The contract with the lexer:
//
//   - Line is answered from the index in O(log lines); column costs the runes
//     of the line up to the offset, and only when asked.
//   - An offset below the watermark is released: LocationAt reports
//     ErrLocationReleased for it, line AND column, because keeping one exact
//     while the other is gone is the worst of both. Resolve a token's location
//     before the retention behind it is dropped.
//   - reclaim advances at LINE boundaries, so every retained offset's line
//     begins at a retained offset and its column is always computable.
//   - Validation that must stay constant-memory builds no Source at all: the
//     index is the only per-line cost, and a caller that never asks for a
//     location never pays it.
//
// The zero value is unusable. Sources are built by Scan; newBytesSource builds
// one over a whole slice, which is also what the tests drive.
type Source struct {
	// read presents the bytes in [from, to) to fn as an io.Reader, for the
	// duration of the call, then releases them. Lifetime-bearing on purpose: it
	// is where a stream holds a View and closes it, and a []byte hands back a
	// reader over itself. Source must not retain the reader past fn.
	read func(from, to int64, fn func(io.Reader) error) error

	// lineStarts holds the offsets at which retained lines begin — those at or
	// above released — in ascending order. reclaimed counts the starts already
	// dropped below the watermark, so a line number is reclaimed + a search of
	// what remains.
	lineStarts []int64
	reclaimed  int

	released int64 // watermark: offsets below this are gone
	head     int64 // one past the last byte read; offsets above this are not yet knowable
}

// newSource returns an empty streaming Source. The lexer fills its index with
// noteNewlineAt, advances head with advanceHead, and drops the tail with reclaim
// as the cache watermark rises.
func newSource(read func(from, to int64, fn func(io.Reader) error) error) *Source {
	return &Source{read: read}
}

// newBytesSource returns a Source over the whole of b, with its line index
// precomputed and nothing ever released. The slice is NOT copied: read hands
// back a reader over sub-slices of it.
//
// A '\n' byte begins a line however it is reached, which is Scanner's rule — a
// bad lead byte consumes only itself, so a following 0x0A is still a newline —
// so scanning for the byte gives the same line breaks a rune walk would.
func newBytesSource(b []byte) *Source {
	var starts []int64
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			starts = append(starts, int64(i+1))
		}
	}
	return &Source{
		lineStarts: starts,
		head:       int64(len(b)),
		read: func(from, to int64, fn func(io.Reader) error) error {
			if from < 0 {
				from = 0
			}
			if to > int64(len(b)) {
				to = int64(len(b))
			}
			if from >= to {
				return fn(bytes.NewReader(nil))
			}
			return fn(bytes.NewReader(b[from:to]))
		},
	}
}

// noteNewlineAt records that a line begins just after the '\n' at nlOffset. It
// must be called in increasing offset order, at most once per newline.
func (s *Source) noteNewlineAt(nlOffset int64) {
	s.lineStarts = append(s.lineStarts, nlOffset+1)
}

// advanceHead moves the known head forward as the lexer reads. Offsets at or
// below head can be resolved; offsets above it cannot, so a short read can never
// yield a Location for bytes it did not return.
func (s *Source) advanceHead(head int64) {
	if head > s.head {
		s.head = head
	}
}

// reclaim drops the line starts below watermark and raises the release point,
// freeing the dropped entries rather than holding the backing array. The
// watermark is expected at a line boundary (the lexer's discipline), so no
// retained offset is left with a released line start.
func (s *Source) reclaim(watermark int64) {
	i := 0
	for i < len(s.lineStarts) && s.lineStarts[i] < watermark {
		i++
	}
	if i > 0 {
		kept := make([]int64, len(s.lineStarts)-i)
		copy(kept, s.lineStarts[i:])
		s.lineStarts = kept
		s.reclaimed += i
	}
	if watermark > s.released {
		s.released = watermark
	}
}

// LocationAt resolves off to a Location, or reports why it cannot. Line comes
// from the index; column is one plus the runes between the line's start and off,
// counted through the byte accessor with Scanner's invalid-UTF-8 rule.
func (s *Source) LocationAt(off int64) (Location, error) {
	if off < 0 || off > s.head {
		return Location{}, fmt.Errorf("%w: offset %d not in [0, %d]", ErrLocationRange, off, s.head)
	}
	if off < s.released {
		return Location{}, fmt.Errorf("%w: offset %d below watermark %d", ErrLocationReleased, off, s.released)
	}

	n := sort.Search(len(s.lineStarts), func(i int) bool { return s.lineStarts[i] > off })
	line := 1 + s.reclaimed + n
	lineStart := s.released // the live window's base line begins here
	if n > 0 {
		lineStart = s.lineStarts[n-1]
	}

	col, err := s.columnAt(lineStart, off)
	if err != nil {
		return Location{}, err
	}
	return Location{Offset: off, Line: line, Column: col}, nil
}

// columnAt counts the runes in [lineStart, off) and returns one more than that.
// It reads a little past off so a rune straddling off decodes in full and is
// recognised as straddling — an interior-rune offset — rather than mistaken for
// a truncated one.
func (s *Source) columnAt(lineStart, off int64) (int, error) {
	upto := off + int64(utf8.UTFMax) - 1
	if upto > s.head {
		upto = s.head
	}

	col := 1
	interior := false
	err := s.read(lineStart, upto, func(r io.Reader) error {
		br := bufio.NewReader(r)
		cur := lineStart
		for cur < off {
			_, size, e := br.ReadRune()
			if e != nil {
				// The bytes ran out before reaching off: off is inside the
				// truncated final rune at head, which is not a rune boundary.
				interior = true
				return nil
			}
			// ReadRune returns size 1 for an invalid byte, matching Scanner, so a
			// bad byte is a boundary and never straddles.
			if cur+int64(size) > off {
				interior = true // a multibyte rune covers off; off is in its interior
				return nil
			}
			cur += int64(size)
			col++
		}
		return nil
	})
	if err != nil {
		// The bytes were released between the domain check and the read.
		return 0, fmt.Errorf("%w: %v", ErrLocationReleased, err)
	}
	if interior {
		return 0, fmt.Errorf("%w: offset %d is inside a multibyte rune", ErrLocationRange, off)
	}
	return col, nil
}
