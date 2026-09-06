package streamcache

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// slowReader delivers one byte per Read, which is the shape that separates a
// design that streams from one that only claims to.
type slowReader struct {
	src []byte
	off int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.off >= len(r.src) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.src[r.off]
	r.off++
	return 1, nil
}

func mustAcquire(t *testing.T, c *Cache, from, to int64) *View {
	t.Helper()
	v, err := c.Acquire(from, to)
	if err != nil {
		t.Fatalf("Acquire(%d,%d): %v", from, to, err)
	}
	return v
}

func mustText(t *testing.T, v *View) string {
	t.Helper()
	s, err := v.String()
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	return s
}

// CRITERION 6: a span straddling a segment boundary is returned correctly.
//
// This is the case a contiguous []byte accessor could not have represented at
// all, which is why View walks segments instead of returning a slice. The
// control is the segment size: at 4 bytes, a 10-byte span MUST cross.
func TestView_SpanStraddlesSegments(t *testing.T) {
	t.Parallel()
	const src = "abcdefghijklmnop"
	c := New(strings.NewReader(src), WithSegmentSize(4))

	v := mustAcquire(t, c, 3, 13)
	defer v.Close()

	// CONTROL: the span genuinely crosses. With 4-byte segments, [3,13) touches
	// four of them — if it did not, this cell would pass on a contiguous span
	// and prove nothing about the case it is named for.
	if len(v.held) < 3 {
		t.Fatalf("control: span held %d segments, want >=3 — it does not straddle, "+
			"so this cell is not testing what it claims", len(v.held))
	}
	if got, want := mustText(t, v), src[3:13]; got != want {
		t.Fatalf("AppendTo across segments = %q, want %q", got, want)
	}

	got, err := io.ReadAll(v.Reader())
	if err != nil {
		t.Fatalf("Reader: %v", err)
	}
	if string(got) != src[3:13] {
		t.Fatalf("Reader across segments = %q, want %q", got, src[3:13])
	}
}

// CRITERION 7: a view's bytes stay valid under reuse pressure until Close.
//
// Acquire early, then force the writer to need space far beyond that segment,
// then read through the view. If retention were advisory the bytes would be
// gone or wrong.
func TestView_SurvivesReusePressure(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("0123456789", 200) // 2000 bytes
	c := New(strings.NewReader(src), WithSegmentSize(16))

	v := mustAcquire(t, c, 0, 10)
	defer v.Close()

	// Drive the writer to the end and release everything it will let go of.
	if _, err := c.Ensure(0, len(src)); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	c.Release(c.Head())

	if got, want := mustText(t, v), src[0:10]; got != want {
		t.Fatalf("view after reuse pressure = %q, want %q — retention did not hold", got, want)
	}
}

// CRITERION 8: a held view never stalls the writer.
//
// THE DEADLOCK THIS EXISTS FOR: hold the first token of a statement while
// scanning forward for its terminator. If retention made reuse WAIT, the writer
// would block on a segment the reader will not release until the writer
// finishes — and neither ever does. Growth is what breaks the cycle.
//
// A stall here hangs rather than fails, so the suite's own timeout is the
// backstop; the assertion is that we reach the end at all.
func TestCache_HeldViewDoesNotStallTheWriter(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("x", 64<<10) // far larger than any segment budget
	c := New(strings.NewReader(src), WithSegmentSize(64))

	first := mustAcquire(t, c, 0, 8) // held for the whole scan
	defer first.Close()

	for off := int64(0); off < int64(len(src)); {
		n, err := c.Ensure(off, 64)
		if err != nil {
			t.Fatalf("Ensure at %d: %v", off, err)
		}
		if n == 0 {
			break
		}
		off += int64(n)
		c.Release(off - 64) // release behind, as a scanner would
	}

	if c.Head() != int64(len(src)) {
		t.Fatalf("writer reached %d of %d — a held view stalled it", c.Head(), len(src))
	}
	if got, want := mustText(t, first), src[0:8]; got != want {
		t.Fatalf("the held view was corrupted: %q, want %q", got, want)
	}
}

// CRITERION 9: a released span reports ErrReleased; it never returns bytes from
// a recycled segment. Silent wrong bytes are the failure this prevents.
func TestCache_ReleasedSpanReportsRatherThanLies(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("abcdefgh", 500)
	c := New(strings.NewReader(src), WithSegmentSize(16))

	if _, err := c.Ensure(0, len(src)); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// Release everything up to 64 bytes short of the head, so the PREFIX is gone
	// and the TAIL is still retained. Releasing to Head() would drop the tail too
	// — correct behaviour, but it would leave this cell with no specificity half,
	// which the first draft of it did.
	c.Release(c.Head() - 64)

	if _, err := c.Acquire(0, 8); !errors.Is(err, ErrReleased) {
		t.Fatalf("Acquire on a released span: err=%v, want ErrReleased. Returning bytes "+
			"here would be silent corruption, which is the whole reason this returns "+
			"an error at all", err)
	}

	// SPECIFICITY: a span that is still retained is still readable, so the check
	// above is not simply refusing everything.
	v := mustAcquire(t, c, c.Head()-8, c.Head())
	defer v.Close()
	if got := mustText(t, v); got != src[len(src)-8:] {
		t.Fatalf("retained span = %q, want %q", got, src[len(src)-8:])
	}
}

// CRITERION 1 (this layer's half): a []byte and a one-byte-at-a-time reader
// produce identical bytes at identical offsets. If the two paths could differ,
// "the tokens are the same" would be a hope rather than a property.
func TestCache_SlowReaderAndBytesAgree(t *testing.T) {
	t.Parallel()
	src := []byte("select $tag$ body; $tag$ from t -- done\n")

	slow := New(&slowReader{src: src}, WithSegmentSize(7))
	fixed := NewBytes(src)

	for from := int64(0); from < int64(len(src)); from += 3 {
		to := from + 11
		if to > int64(len(src)) {
			to = int64(len(src))
		}
		a := mustAcquire(t, slow, from, to)
		b := mustAcquire(t, fixed, from, to)
		if x, y := mustText(t, a), mustText(t, b); x != y {
			t.Fatalf("[%d,%d): slow reader %q != bytes %q", from, to, x, y)
		}
		a.Close()
		b.Close()
	}
}

// CRITERION 13: NewBytes performs no copy of the caller's slice.
func TestNewBytes_DoesNotCopy(t *testing.T) {
	t.Parallel()
	src := []byte("borrowed, not copied")
	c := NewBytes(src)
	if &c.segs[0].buf[0] != &src[0] {
		t.Fatal("NewBytes copied the slice; the borrowed path must not")
	}
}

// CRITERION 10: concurrent readers with a writer appending are race-clean and
// every view stays valid for its own lifetime. Run under -race to mean anything.
func TestCache_ConcurrentReadersAndWriter(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("abcdefghij", 2000)
	c := New(strings.NewReader(src), WithSegmentSize(128))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k := 0; k < 200; k++ {
				from := int64((i*97 + k*13) % 4000)
				v, err := c.Acquire(from, from+10)
				if err != nil {
					continue // released behind us is legitimate
				}
				got, err := v.String()
				if err == nil && got != src[from:from+10] {
					t.Errorf("concurrent view [%d,%d) = %q, want %q",
						from, from+10, got, src[from:from+10])
				}
				v.Close()
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for off := int64(0); off < int64(len(src)); off += 128 {
			if _, err := c.Ensure(off, 128); err != nil {
				return
			}
		}
	}()
	wg.Wait()
}

// Close is idempotent: a double release must not decrement twice and let a live
// segment be recycled under another view.
func TestView_CloseIsIdempotent(t *testing.T) {
	t.Parallel()
	c := New(strings.NewReader("abcdefgh"), WithSegmentSize(4))
	a := mustAcquire(t, c, 0, 4)
	b := mustAcquire(t, c, 0, 4) // same segment, second reference

	a.Close()
	a.Close() // the double release this cell exists for

	if got, want := mustText(t, b), "abcd"; got != want {
		t.Fatalf("second view = %q, want %q — a double Close dropped a live reference",
			got, want)
	}
	b.Close()
}

func TestCache_RangeErrors(t *testing.T) {
	t.Parallel()
	c := New(bytes.NewReader([]byte("abc")))
	for _, tc := range []struct{ from, to int64 }{{-1, 2}, {2, 1}} {
		if _, err := c.Acquire(tc.from, tc.to); !errors.Is(err, ErrRange) {
			t.Fatalf("Acquire(%d,%d): err=%v, want ErrRange", tc.from, tc.to, err)
		}
	}
	if _, err := c.Acquire(0, 99); !errors.Is(err, ErrRange) {
		t.Fatalf("Acquire past end: err=%v, want ErrRange", err)
	}
}

func ExampleCache_Acquire() {
	c := New(strings.NewReader("hello, world"))
	v, err := c.Acquire(7, 12)
	if err != nil {
		panic(err)
	}
	defer v.Close()
	s, _ := v.String()
	fmt.Println(s)
	// Output: world
}
