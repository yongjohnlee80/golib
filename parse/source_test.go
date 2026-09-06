package parse

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

// scannerLocs walks b with the package's existing Scanner and records the
// Location at every offset the Scanner stops on — every RUNE BOUNDARY and the
// EOF offset, and nothing in between. Scanner is the authority the ADR pins the
// new core's line/column semantics to, so it is the oracle here rather than a
// second hand-written expectation that could be wrong in the same way.
func scannerLocs(b []byte) map[int64]Location {
	sc := NewScanner(b)
	locs := map[int64]Location{}
	for {
		p := sc.Pos()
		locs[int64(p.Offset)] = Location{Offset: int64(p.Offset), Line: int64(p.Line), Column: int64(p.Column)}
		if _, ok := sc.Next(); !ok {
			break
		}
	}
	return locs
}

func TestSourceLocationMatchesScannerAtEveryBoundary(t *testing.T) {
	// The corpus mixes single- and multi-byte runes and an empty line so the
	// rune-counted column differs from a byte-counted one: a Source that counted
	// bytes would pass on the ASCII rows and fail here, which is the point.
	corpus := []string{
		"",
		"a",
		"abc",
		"line1\nline2\nline3",
		"a\n\nb",       // an empty line in the middle
		"héllo\nwörld", // two-byte runes either side of a newline
		"日本語\n中文",      // three-byte runes
		"tea 😀 time",   // a four-byte rune
		"tab\there",    // a tab is one column, like any other rune
		"trailing\n",   // a newline as the final byte
		"\n\n\n",       // nothing but newlines
	}
	for _, s := range corpus {
		checkBoundaries(t, []byte(s))
	}
	// Invalid UTF-8: a lone 0xff, and a truncated two-byte sequence 0xc3 0x28.
	// Scanner consumes each bad byte as one column; Source must agree, and the
	// 0x0a after the 0xff must still register as a newline.
	checkBoundaries(t, []byte{'a', 0xff, 'b', '\n', 0xc3, 0x28, 'z'})
}

func checkBoundaries(t *testing.T, b []byte) {
	t.Helper()
	src := newBytesSource(b)
	for off, want := range scannerLocs(b) {
		got, err := src.LocationAt(off)
		if err != nil {
			t.Fatalf("%q: LocationAt(%d): unexpected error %v", b, off, err)
		}
		if got != want {
			t.Errorf("%q: LocationAt(%d) = %+v, want %+v", b, off, got, want)
		}
	}
}

// TestLocationAt_EveryByteOffsetIsSoundOrRejected is the domain guarantee: over
// every byte offset — not only the Scanner boundaries — LocationAt either
// reproduces the boundary location or REJECTS the offset as out of range. It
// never invents a Location for an interior byte, which is the defect the earlier
// version shipped (columns marched 2,3,4 then fell back to 2 as a rune closed).
func TestLocationAt_EveryByteOffsetIsSoundOrRejected(t *testing.T) {
	for _, str := range []string{"abc", "héllo", "日本語", "😀x", "a\n😀"} {
		b := []byte(str)
		s := newBytesSource(b)
		bounds := scannerLocs(b)
		for off := int64(0); off <= int64(len(b)); off++ {
			loc, err := s.LocationAt(off)
			if want, ok := bounds[off]; ok {
				if err != nil || loc != want {
					t.Errorf("%q LocationAt(%d) = (%+v, %v), want %+v", str, off, loc, err, want)
				}
				continue
			}
			// Not a rune boundary: it is the interior of a multibyte rune and
			// must be refused, never resolved.
			if !errors.Is(err, ErrLocationRange) {
				t.Errorf("%q LocationAt(%d) interior = (%+v, %v), want ErrLocationRange", str, off, loc, err)
			}
		}
	}
}

func TestLocationAt_BeyondHeadOrNegativeRejected(t *testing.T) {
	s := newBytesSource([]byte("abc")) // head = 3
	for _, off := range []int64{-1, 4, 100} {
		if _, err := s.LocationAt(off); !errors.Is(err, ErrLocationRange) {
			t.Errorf("LocationAt(%d) err = %v, want ErrLocationRange", off, err)
		}
	}
	// off == head is the EOF position and IS resolvable.
	loc, err := s.LocationAt(3)
	if err != nil || loc != (Location{Offset: 3, Line: 1, Column: 4}) {
		t.Errorf("LocationAt(3) = (%+v, %v), want {3,1,4} nil", loc, err)
	}
}

// TestLocationAt_ReleasedOffsetsBecomeUnavailable pins the live-window contract:
// after the watermark rises past a line, offsets below it report
// ErrLocationReleased for BOTH line and column, while retained offsets keep
// exact locations — the reclaimed line count keeping their line numbers right.
func TestLocationAt_ReleasedOffsetsBecomeUnavailable(t *testing.T) {
	b := []byte("l1\nl2\nl3\n") // lines begin at 0, 3, 6, 9; head = 9
	s := newBytesSource(b)
	s.reclaim(6) // release lines 1 and 2 (through offset 5); line 3 begins at 6

	for _, off := range []int64{0, 3, 5} {
		if _, err := s.LocationAt(off); !errors.Is(err, ErrLocationReleased) {
			t.Errorf("LocationAt(%d) after reclaim(6) err = %v, want ErrLocationReleased", off, err)
		}
	}
	// The retained line keeps its true line number (3), not 1, because reclaim
	// accounts for the dropped starts.
	if loc, err := s.LocationAt(6); err != nil || loc != (Location{Offset: 6, Line: 3, Column: 1}) {
		t.Errorf("LocationAt(6) = (%+v, %v), want {6,3,1} nil", loc, err)
	}
	if loc, err := s.LocationAt(7); err != nil || loc != (Location{Offset: 7, Line: 3, Column: 2}) {
		t.Errorf("LocationAt(7) = (%+v, %v), want {7,3,2} nil", loc, err)
	}
}

// TestReclaim_SnapsToLineBoundaryNotMidLine is the MF2 regression: a token
// watermark that falls in the middle of a line must not release mid-line and
// then count a column from the wrong start. reclaim snaps DOWN to the greatest
// known line start at or below the watermark and returns it, so Scan advances
// the cache to the same safe offset.
func TestReclaim_SnapsToLineBoundaryNotMidLine(t *testing.T) {
	b := []byte("abcd\n") // one line beginning at 0; lineStarts = [5]; head = 5
	s := newBytesSource(b)

	if eff := s.reclaim(2); eff != 0 {
		t.Errorf("reclaim(2) effective watermark = %d, want 0 (the line begins at 0)", eff)
	}
	// Nothing was released, so the column is still counted from the true start.
	if loc, err := s.LocationAt(3); err != nil || loc != (Location{Offset: 3, Line: 1, Column: 4}) {
		t.Errorf("LocationAt(3) after reclaim(2) = (%+v, %v), want {3,1,4} nil", loc, err)
	}

	// A watermark at the next line start does release the whole first line.
	if eff := s.reclaim(5); eff != 5 {
		t.Errorf("reclaim(5) effective watermark = %d, want 5", eff)
	}
	if _, err := s.LocationAt(3); !errors.Is(err, ErrLocationReleased) {
		t.Errorf("LocationAt(3) after reclaim(5) err = %v, want ErrLocationReleased", err)
	}
}

// TestSourceIncrementalMatchesPrecomputed proves the streaming path — an empty
// Source fed newlines and a head as the lexer advances — lands the same
// locations as the whole-slice precompute. Identical positions from a []byte and
// a stream (acceptance criterion 1) rest on this.
func TestSourceIncrementalMatchesPrecomputed(t *testing.T) {
	b := []byte("a\nbb\n\nc\n日\n")
	pre := newBytesSource(b)

	inc := newSource(pre.read) // same lifetime-bearing accessor, empty index
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			inc.noteNewlineAt(int64(i))
		}
	}
	inc.advanceHead(int64(len(b)))

	for off := range scannerLocs(b) {
		gotPre, errPre := pre.LocationAt(off)
		gotInc, errInc := inc.LocationAt(off)
		if errPre != nil || errInc != nil {
			t.Fatalf("offset %d: pre err %v, inc err %v", off, errPre, errInc)
		}
		if gotPre != gotInc {
			t.Errorf("offset %d: precomputed %+v, incremental %+v", off, gotPre, gotInc)
		}
	}
}

func TestSource_AdvanceHeadGatesUnreadOffsets(t *testing.T) {
	// A streaming Source cannot resolve an offset it has not read to yet: a short
	// read must never yield a Location for bytes it did not return.
	b := []byte("abcdef")
	s := newSource(func(from, to int64, fn func(io.Reader) error) error {
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
	})
	if _, err := s.LocationAt(3); !errors.Is(err, ErrLocationRange) {
		t.Errorf("LocationAt(3) with head 0 = %v, want ErrLocationRange", err)
	}
	s.advanceHead(6)
	if loc, err := s.LocationAt(3); err != nil || loc != (Location{Offset: 3, Line: 1, Column: 4}) {
		t.Errorf("LocationAt(3) after advanceHead(6) = (%+v, %v), want {3,1,4} nil", loc, err)
	}
}

// TestColumnAt_AccessorErrorKeepsItsIdentity: an unexpected accessor failure is
// preserved, not relabelled — the domain check already refused released offsets,
// so a failure past it is neither a release nor a range error.
func TestColumnAt_AccessorErrorKeepsItsIdentity(t *testing.T) {
	sentinel := errors.New("accessor boom")
	s := newSource(func(from, to int64, fn func(io.Reader) error) error { return sentinel })
	s.advanceHead(10)

	_, err := s.LocationAt(5)
	if !errors.Is(err, sentinel) {
		t.Errorf("LocationAt err = %v, want it to wrap the accessor sentinel", err)
	}
	if errors.Is(err, ErrLocationRange) || errors.Is(err, ErrLocationReleased) {
		t.Errorf("err = %v must not be reclassified as Range or Released", err)
	}
}

// TestColumnAt_UnderDeliveredRangeIsUnexpectedEOF: when head claims more bytes
// than the reader supplies, a valid boundary yields io.ErrUnexpectedEOF — an
// accessor contract failure — not ErrLocationRange, which would blame the caller.
func TestColumnAt_UnderDeliveredRangeIsUnexpectedEOF(t *testing.T) {
	s := newSource(func(from, to int64, fn func(io.Reader) error) error {
		return fn(bytes.NewReader([]byte("abc"))) // only 3 bytes, whatever the range
	})
	s.advanceHead(10) // but head claims 10

	_, err := s.LocationAt(8)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("LocationAt(8) over an under-delivering accessor err = %v, want io.ErrUnexpectedEOF", err)
	}
	if errors.Is(err, ErrLocationRange) || errors.Is(err, ErrLocationReleased) {
		t.Errorf("err = %v must not be reclassified as Range or Released", err)
	}
}

// TestColumnAt_LookaheadOverflowGuard: near MaxInt64, off+UTFMax-1 would wrap
// negative; the guard must clamp the lookahead to head instead, so the range the
// accessor is asked for never inverts.
func TestColumnAt_LookaheadOverflowGuard(t *testing.T) {
	const near = math.MaxInt64
	var gotFrom, gotTo int64
	s := newSource(func(from, to int64, fn func(io.Reader) error) error {
		gotFrom, gotTo = from, to
		return fn(bytes.NewReader(nil))
	})
	s.advanceHead(near)

	_, _ = s.LocationAt(near) // errors (under-delivery); the range is the point
	if gotTo < gotFrom {
		t.Errorf("read range [%d, %d) inverted — the lookahead addition overflowed", gotFrom, gotTo)
	}
	if gotTo != near {
		t.Errorf("read upto = %d, want it clamped to head %d", gotTo, int64(near))
	}
}

func TestTokenLenAndString(t *testing.T) {
	tk := Token{Kind: Word, Start: 3, End: 8}
	if tk.Len() != 5 {
		t.Errorf("Len = %d, want 5", tk.Len())
	}
	if got := tk.String(); got != "Word[3:8)" {
		t.Errorf("String = %q, want %q", got, "Word[3:8)")
	}
}

func TestLocationString(t *testing.T) {
	if got := (Location{Offset: 12, Line: 3, Column: 7}).String(); got != "3:7" {
		t.Errorf("String = %q, want %q", got, "3:7")
	}
}
