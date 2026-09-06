package streamcache

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

// A span straddling a segment boundary is returned correctly.
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

// A view's bytes stay valid under RELEASE pressure until Close.
//
// Named for what it does. There is no segment pool and nothing is recycled —
// dropped segments are garbage — so "reuse pressure" overstated it. What it
// exercises is real and worth a cell: acquire early, drive the
// writer to the end, ask for everything to be released, and read through the
// view afterwards.
func TestView_SurvivesReleasePressure(t *testing.T) {
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
		t.Fatalf("view after release pressure = %q, want %q — retention did not hold", got, want)
	}
}

// A held view never stalls the writer.
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

// A released span reports ErrReleased; it never returns bytes from
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

// This layer's half of the identical-tokens property: a []byte and a
// one-byte-at-a-time reader produce identical bytes at identical offsets. If the two paths could differ,
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

// NewBytes performs no copy of the caller's slice.
func TestNewBytes_DoesNotCopy(t *testing.T) {
	t.Parallel()
	src := []byte("borrowed, not copied")
	c := NewBytes(src)
	if &c.segs[0].buf[0] != &src[0] {
		t.Fatal("NewBytes copied the slice; the borrowed path must not")
	}
}

// Concurrent readers with a writer are race-clean, and every
// acquired view stays valid for its own lifetime.
//
// THE FIRST VERSION OF THIS CELL WAS INERT, and that is the finding worth
// recording: it used a strings.Reader (full blocks, so no held segment was ever
// PARTIAL and therefore never a write target), never called Release (so
// retention was never under pressure), swallowed every error from Acquire and
// String, and asserted nothing at the end. Full-module -race stayed green while
// a real data race existed in fillLocked. A concurrency test that cannot fail
// is worse than none, because it is counted as coverage.
//
// This version fixes each of those: a one-byte reader keeps segments partial,
// the writer releases behind itself, errors are collected rather than dropped,
// and progress is asserted at the end.
func TestCache_ConcurrentReadersAndWriter(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("abcdefghij", 2000)
	c := New(&slowReader{src: []byte(src)}, WithSegmentSize(128))

	var (
		mu   sync.Mutex
		errs []error
		hits int
	)
	record := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	// writerDone lets a reader tell "the window has not filled YET" apart from
	// "it never will", so neither case has to be guessed at from a lap count.
	var writerDone atomic.Bool
	// A backstop for the reader poll below and nothing wider: a writer that
	// never produces a window fails this cell instead of spinning it. It does
	// NOT bound the cell as a whole — the writer is in the WaitGroup, so one
	// that stalls mid-stream still blocks Wait, and the suite timeout is what
	// reports that.
	deadline := time.Now().Add(30 * time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			buf := make([]byte, 0, 16)
			// THE BUDGET COUNTS OPPORTUNITIES, NOT LAPS. A lap that finds no
			// live window examined nothing, so spending the budget on it makes
			// the count a proxy for what this cell measures rather than the
			// thing itself. It counted laps once, and on a loaded machine all
			// eight readers could spend all 2400 of them before the writer was
			// ever scheduled: the writer then finished, the progress guard was
			// satisfied, and the cell failed with "no reader ever validated a
			// view" while the cache was working perfectly. Two milliseconds of
			// head start for the readers is enough to do it.
			for attempts := 0; attempts < 300; {
				// READ INSIDE THE LIVE WINDOW. The first version picked offsets
				// from a fixed modulo range, so every request was either ahead of
				// the writer or behind the release watermark, every Acquire
				// failed, and the cell validated NOTHING — caught by its own
				// hits==0 guard rather than by review, which is the only reason
				// that guard was worth adding.
				head := c.Head()
				if head < 32 {
					if writerDone.Load() || time.Now().After(deadline) {
						break // no window is coming; the guards below say so properly
					}
					runtime.Gosched()
					continue
				}
				// head >= 32 puts the whole span at or above offset 9, so it is
				// the live window that bounds the offset, not a floor at zero.
				from := head - 16 - int64((i*3+attempts)%8)
				attempts++
				v, err := c.Acquire(from, from+10)
				if err != nil {
					continue // released or not yet read: both legitimate races
				}
				buf, err = v.AppendTo(buf[:0])
				if err != nil {
					record(fmt.Errorf("AppendTo [%d,%d): %w", from, from+10, err))
				} else if string(buf) != src[from:from+10] {
					record(fmt.Errorf("view [%d,%d) = %q, want %q",
						from, from+10, buf, src[from:from+10]))
				} else {
					mu.Lock()
					hits++
					mu.Unlock()
				}
				v.Close()
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer writerDone.Store(true)
		for off := int64(0); off < int64(len(src)); {
			n, err := c.Ensure(off, 128)
			if err != nil {
				record(fmt.Errorf("Ensure at %d: %w", off, err))
				return
			}
			if n == 0 {
				return
			}
			off += int64(n)
			c.Release(off - 512) // keep retention genuinely under pressure
		}
	}()
	wg.Wait()

	for _, err := range errs {
		t.Error(err)
	}
	// PROGRESS. Without this the cell passes when every Acquire fails, which is
	// exactly what a broken cache would do.
	if c.Head() != int64(len(src)) {
		t.Fatalf("writer reached %d of %d bytes", c.Head(), len(src))
	}
	if hits == 0 {
		t.Fatal("no reader ever validated a view: the cell proved nothing")
	}
	t.Logf("validated %d concurrent views while the writer streamed %d bytes", hits, c.Head())
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

// A non-positive segment size is a programming error and must be reported, not
// silently replaced by the default — which would hide the caller's mistake
// until a memory figure looked wrong.
func TestWithSegmentSize_RejectsNonPositive(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("WithSegmentSize(%d) did not panic; a silent default is the "+
						"fault this replaced", n)
				}
			}()
			New(strings.NewReader(""), WithSegmentSize(n))
		}()
	}
	// SPECIFICITY: a positive size must NOT panic, or the check above would pass
	// on a constructor that rejected everything.
	New(strings.NewReader(""), WithSegmentSize(1))
}

// retainedBytesLocked is retainedBytes WITHOUT taking the lock, for an
// observing reader: Read runs inside the cache's own critical section, on the
// same goroutine, so the locking version would deadlock. TEST-ONLY.
func (c *Cache) retainedBytesLocked() int64 {
	var n int64
	for _, s := range c.segs {
		n += int64(len(s.buf))
	}
	return n
}

// directoryLen reports how many segment ENTRIES the cache is carrying,
// including any whose buffer is already freed. TEST-ONLY: freeing the bytes
// while the entries accumulate would still grow without bound over a stream.
func (c *Cache) directoryLen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.segs)
}

// retainedBytes reports how many bytes of segment buffer the cache is still
// holding. TEST-ONLY: the number is the memory bound the ADR states, and a
// bound nothing can read is a bound nothing can check.
func (c *Cache) retainedBytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var n int64
	for _, s := range c.segs {
		n += int64(len(s.buf))
	}
	return n
}

// A nil reader is refused at construction, where the mistake was made, rather
// than dereferenced inside the first fill under the Cache's own lock.
func TestNew_RejectsNilReader(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("New(nil) did not panic; the nil surfaces later as a dereference " +
				"inside fillLocked, on whichever goroutine advanced the stream")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "New(nil)") {
			t.Fatalf("panic message %q does not name the call", r)
		}
	}()
	_ = New(nil)
}

// SPECIFICITY: the panic is about nil, not about construction. NewBytes(nil) is
// a legitimate empty stream and the message above points callers at it, so that
// advice has to be true.
func TestNewBytes_NilIsAnEmptyStream(t *testing.T) {
	t.Parallel()
	c := NewBytes(nil)
	if n, err := c.Ensure(0, 1); n != 0 || err != nil {
		t.Fatalf("Ensure(0,1) on NewBytes(nil) = %d, %v; want 0, nil", n, err)
	}
	if got := c.Head(); got != 0 {
		t.Fatalf("Head() = %d, want 0", got)
	}
}
