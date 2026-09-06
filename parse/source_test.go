package parse

import (
	"errors"
	"testing"
)

// scannerLocs walks b with the package's existing Scanner and records the
// Location at every offset the Scanner stops on — every rune start, and the EOF
// offset. Scanner is the authority the ADR pins the new core's line/column
// semantics to, so it is the oracle here rather than a second hand-written
// expectation that could be wrong in the same way.
func scannerLocs(b []byte) map[int64]Location {
	sc := NewScanner(b)
	locs := map[int64]Location{}
	for {
		p := sc.Pos()
		locs[int64(p.Offset)] = Location{Offset: int64(p.Offset), Line: p.Line, Column: p.Column}
		if _, ok := sc.Next(); !ok {
			break
		}
	}
	return locs
}

func TestSourceLocationMatchesScanner(t *testing.T) {
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
		checkEveryOffset(t, []byte(s))
	}
	// Invalid UTF-8: a lone 0xff, and a truncated two-byte sequence 0xc3 0x28.
	// Scanner consumes each bad byte as one column; Source must agree, and the
	// 0x0a after the 0xff must still register as a newline.
	checkEveryOffset(t, []byte{'a', 0xff, 'b', '\n', 0xc3, 0x28, 'z'})
}

func checkEveryOffset(t *testing.T, b []byte) {
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

// TestSourceIncrementalIndexMatchesPrecomputed proves the line index the
// streaming path fills byte by byte with noteNewlineAt lands in the same place
// as the whole-slice precompute. Identical positions from a []byte and a stream
// (acceptance criterion 1) rest on this.
func TestSourceIncrementalIndexMatchesPrecomputed(t *testing.T) {
	b := []byte("a\nbb\n\nc\n日\n")
	pre := newBytesSource(b)

	inc := newSource(pre.bytesAt) // same byte accessor, empty index
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			inc.noteNewlineAt(int64(i))
		}
	}

	for off := range scannerLocs(b) {
		gotPre, _ := pre.LocationAt(off)
		gotInc, _ := inc.LocationAt(off)
		if gotPre != gotInc {
			t.Errorf("offset %d: precomputed %+v, incremental %+v", off, gotPre, gotInc)
		}
	}
}

// TestLocationColumnUnknownWhenBytesUnavailable pins the released-region
// contract: Line is answered from the index and stays exact, and Column is
// reported as 0 with the error rather than guessed from bytes that are gone.
func TestLocationColumnUnknownWhenBytesUnavailable(t *testing.T) {
	released := errors.New("streamcache: span is no longer retained")
	src := &Source{
		lineStarts: []int64{2, 5}, // line 1 [0,2), line 2 [2,5), line 3 [5,…)
		bytesAt:    func(from, to int64) ([]byte, error) { return nil, released },
	}
	loc, err := src.LocationAt(6)
	if !errors.Is(err, released) {
		t.Fatalf("LocationAt error = %v, want %v", err, released)
	}
	if loc.Line != 3 {
		t.Errorf("Line = %d, want 3 (must be exact from the index)", loc.Line)
	}
	if loc.Column != 0 {
		t.Errorf("Column = %d, want 0 (unknown, not guessed)", loc.Column)
	}
}

func TestLocationNegativeOffsetIsOrigin(t *testing.T) {
	loc, err := newBytesSource([]byte("abc")).LocationAt(-1)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if want := (Location{Offset: 0, Line: 1, Column: 1}); loc != want {
		t.Errorf("LocationAt(-1) = %+v, want %+v", loc, want)
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
