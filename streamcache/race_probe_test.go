package streamcache

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// F1 — THE PROBE THAT SHOULD HAVE EXISTED.
//
// A held PARTIAL segment is the case the committed suite could not reach: its
// reader delivered full blocks, so every held segment was already complete and
// the writer never touched one again. With a one-byte reader the writer is
// still filling the segment a view is holding, and `s.n` is written under
// c.mu while View.AppendTo reads it under v.mu.
//
// This is a -race probe: it asserts nothing about values. Its verdict is
// whether the detector fires, which is why it must be run with -race to mean
// anything at all.
func TestRace_WriterFillsAHeldPartialSegment(t *testing.T) {
	src := strings.Repeat("abcdefghij", 400)
	c := New(&slowReader{src: []byte(src)}, WithSegmentSize(512))

	// Acquire a span inside a segment the writer has NOT finished: one byte per
	// Read means segment 0 is partial for a long time.
	if _, err := c.Ensure(0, 8); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	v, err := c.Acquire(0, 8)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer v.Close()

	// The errors are NOT discarded. A writer that stops on its first error and a
	// reader that ignores AppendTo's would leave both goroutines idling while
	// the test still passed: the detector cannot fire on work that did not
	// happen, so a silent early exit turns this into a probe of nothing.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		errs    []error
		fills   int
		reads   int
		lastGot string
	)
	fail := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	wg.Add(2)
	go func() { // the writer keeps filling the segment the view holds
		defer wg.Done()
		for off := int64(0); off+64 <= int64(len(src)); off += 64 {
			n, err := c.Ensure(off, 64)
			if err != nil {
				fail(fmt.Errorf("Ensure(%d, 64): %w", off, err))
				return
			}
			if n != 64 {
				fail(fmt.Errorf("Ensure(%d, 64) = %d, want 64", off, n))
				return
			}
			mu.Lock()
			fills++
			mu.Unlock()
		}
	}()
	go func() { // the reader keeps reading through the held view
		defer wg.Done()
		buf := make([]byte, 0, 8)
		for i := 0; i < 2000; i++ {
			var err error
			buf, err = v.AppendTo(buf[:0])
			if err != nil {
				fail(fmt.Errorf("AppendTo #%d: %w", i, err))
				return
			}
			if len(buf) != 8 {
				fail(fmt.Errorf("AppendTo #%d returned %d bytes, want 8", i, len(buf)))
				return
			}
			mu.Lock()
			reads++
			lastGot = string(buf)
			mu.Unlock()
		}
	}()
	wg.Wait()

	for _, err := range errs {
		t.Error(err)
	}
	// PROGRESS IS THE PRECONDITION. Without both counts the -race verdict is
	// "no race was detected in the work that ran", and the work that ran may
	// have been none of it.
	if wantFills := len(src)/64 - 1; fills < wantFills {
		t.Fatalf("writer completed %d fills, want at least %d — the reader and writer "+
			"did not overlap and the detector had nothing to watch", fills, wantFills)
	}
	if reads < 2000 {
		t.Fatalf("reader completed %d reads of 2000", reads)
	}
	if lastGot != src[:8] {
		t.Fatalf("view read %q, want %q", lastGot, src[:8])
	}
}

// F2 — a released span must STAY released.
//
// Release(Head) skips a segment a view still holds. When that view closes, the
// segment must actually go: otherwise Acquire of a span the caller was told was
// released succeeds, and the retained set grows without bound as views close.
func TestRelease_HeldSegmentGoesWhenItsViewCloses(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("0123456789", 200)
	c := New(strings.NewReader(src), WithSegmentSize(16))

	v, err := c.Acquire(0, 8)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := c.Ensure(0, len(src)); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	c.Release(c.Head()) // asked for everything; segment 0 is held, so it stays
	v.Close()           // ... and now the reason to keep it is gone

	if _, err := c.Acquire(0, 8); !errors.Is(err, ErrReleased) {
		t.Fatalf("Acquire on a span Release was asked to drop, whose only holder has "+
			"closed: err=%v, want ErrReleased.\n"+
			"%s", err,
			"A release watermark must outlive the view that deferred it, or 'released' "+
				"means nothing and the retained set only grows.")
	}
}

// F3a — a reader that returns (0, nil) forever must not spin under the lock.
//
// (0, nil) is legal per io.Reader. A loop that treats it as "try again" never
// terminates, and it holds c.mu while doing so, so every other goroutine stops
// too. The test's own deadline is the witness.
func TestEnsure_ZeroProgressReaderDoesNotSpin(t *testing.T) {
	t.Parallel()
	done := make(chan error, 1)
	go func() {
		c := New(zeroReader{}, WithSegmentSize(16))
		_, err := c.Ensure(0, 8) // must return, not spin
		done <- err
	}()
	select {
	case err := <-done:
		// ASSERT THE SENTINEL, not merely that it returned. "It came back" is
		// also true of a version that returns a nil error and a short count,
		// which would leave a caller looping forever one level up.
		if !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("Ensure on a (0, nil) reader: err=%v, want io.ErrNoProgress", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ensure spun on a (0, nil) reader — legal per io.Reader, and it holds " +
			"c.mu while spinning, so it stops every other goroutine too")
	}
}

// F3b — a source error must reach the caller, not be replaced by ErrRange.
func TestEnsure_SourceErrorSurvives(t *testing.T) {
	t.Parallel()
	c := New(&errAfter{n: 4, err: errBoom}, WithSegmentSize(16))
	if _, err := c.Ensure(0, 4); err != nil {
		t.Fatalf("first 4 bytes should read cleanly: %v", err)
	}
	_, err := c.Ensure(0, 64) // past what the source will give
	if !errors.Is(err, errBoom) {
		t.Fatalf("Ensure past a failed source: err=%v, want the source's own error. "+
			"Replacing it with ErrRange tells the caller their span was wrong when in "+
			"fact their source broke", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { return 0, nil }

type errAfter struct {
	n   int
	off int
	err error
}

func (r *errAfter) Read(p []byte) (int, error) {
	if r.off >= r.n {
		return 0, r.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = byte('a' + r.off%26)
	r.off++
	return 1, nil
}

// A sentinel of our own. Using io.ErrUnexpectedEOF would let this pass on any
// unrelated short-read path in the cache rather than on the source's own error
// travelling through.
var errBoom = errors.New("streamcache_test: the source broke")

// F7 — RECLAMATION MUST NOT REDEFINE RELEASE.
//
// Reclaiming only the directory's leading run stops at the first segment a view
// still holds. Everything AFTER that segment is then neither dropped nor
// refused, so a span the caller was told to release stays acquirable — the
// speed of the reclamation pass silently became the definition of "released".
//
// The counterexample is small on purpose: four-byte segments, a view on the
// first byte, sixteen bytes read, release everything. The span at [8,9) is in a
// segment no view holds, wholly below the watermark, and behind the held one.
func TestRelease_SuffixBehindAHeldSegmentIsReleased(t *testing.T) {
	t.Parallel()
	c := New(strings.NewReader("0123456789abcdef"), WithSegmentSize(4))

	v, err := c.Acquire(0, 1) // pins segment 0 and nothing else
	if err != nil {
		t.Fatalf("Acquire(0,1): %v", err)
	}
	defer v.Close()
	if _, err := c.Ensure(0, 16); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	c.Release(c.Head())

	if _, err := c.Acquire(8, 9); !errors.Is(err, ErrReleased) {
		t.Fatalf("Acquire(8,9) after Release(%d): err=%v, want ErrReleased.\n"+
			"%s", c.Head(), err,
			"Release drops every eligible segment, not the leading run of them. A view "+
				"holding the FIRST byte of a stream must not make the rest of it "+
				"acquirable again.")
	}

	// SPECIFICITY: the held view's own bytes are untouched by all of this.
	if got, err := v.String(); err != nil || got != "0" {
		t.Fatalf("held view after Release: got %q, %v; want %q, nil", got, err, "0")
	}
}

// F9 — THE WATERMARK DECIDES, NOT THE DIRECTORY.
//
// F7 above is satisfied by freeing the buffer: once the segment is gone the
// lookup fails and ErrReleased comes out for free. That makes it BLIND to the
// rule it is named for. Removing the watermark check from Acquire leaves F7
// green, which is how this probe came to exist.
//
// So: two spans that are below the watermark while their bytes are DEMONSTRABLY
// still in the cache. Both must be refused, or "released" means "released
// unless some unrelated view is holding something in front of it" — a rule no
// caller can reason about.
func TestAcquire_BelowWatermarkIsRefusedWhileTheBytesAreStillThere(t *testing.T) {
	t.Parallel()

	t.Run("segment straddling the watermark", func(t *testing.T) {
		t.Parallel()
		c := New(strings.NewReader("0123456789abcdef"), WithSegmentSize(8))
		if _, err := c.Ensure(0, 16); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		c.Release(4) // segment 0 is [0,8): NOT wholly below, so it survives

		// The bytes are still here — proven, not assumed.
		if v, err := c.Acquire(4, 8); err != nil {
			t.Fatalf("control failed: [4,8) should still be acquirable: %v", err)
		} else {
			if got, _ := v.String(); got != "4567" {
				t.Fatalf("control failed: [4,8) reads %q, want %q", got, "4567")
			}
			v.Close()
		}
		if _, err := c.Acquire(0, 1); !errors.Is(err, ErrReleased) {
			t.Fatalf("Acquire(0,1) after Release(4): err=%v, want ErrReleased.\n%s", err,
				"The span is below the watermark. That its segment survives because it "+
					"straddles the watermark is an implementation accident.")
		}
	})

	t.Run("segment still held by an earlier view", func(t *testing.T) {
		t.Parallel()
		c := New(strings.NewReader("0123456789abcdef"), WithSegmentSize(8))
		v, err := c.Acquire(0, 4)
		if err != nil {
			t.Fatalf("Acquire(0,4): %v", err)
		}
		defer v.Close() // stays open for the whole test: segment 0 cannot be freed
		if _, err := c.Ensure(0, 16); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		c.Release(c.Head())

		// Still readable through the view that owns it, so the bytes are there.
		if got, err := v.String(); err != nil || got != "0123" {
			t.Fatalf("control failed: the holding view reads %q, %v; want %q", got, err, "0123")
		}
		if _, err := c.Acquire(0, 1); !errors.Is(err, ErrReleased) {
			t.Fatalf("Acquire(0,1) after Release(Head) with the segment still held: "+
				"err=%v, want ErrReleased.\n%s", err,
				"A second caller must not reach released bytes through the retention of "+
					"an unrelated view.")
		}
	})
}

// F8 — AND IT MUST ACTUALLY FREE THE MEMORY.
//
// A control for the fix above: refusing the acquire is only half of it. One
// oldest view over a long stream must retain ITS OWN segment, not the suffix
// behind it. This is the memory bound stated in the ADR, measured rather than
// asserted.
func TestRelease_OldestViewDoesNotPinTheSuffix(t *testing.T) {
	t.Parallel()
	const segSize, segments = 64, 256
	src := strings.Repeat("x", segSize*segments)
	c := New(strings.NewReader(src), WithSegmentSize(segSize))

	v, err := c.Acquire(0, 1)
	if err != nil {
		t.Fatalf("Acquire(0,1): %v", err)
	}
	defer v.Close()
	if _, err := c.Ensure(0, len(src)); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	before, dirBefore := c.retainedBytes(), c.directoryLen()
	c.Release(c.Head())
	after, dirAfter := c.retainedBytes(), c.directoryLen()

	// One segment is legitimately retained: the one the view holds. Allow the
	// active write target as well, which the writer may not have finished.
	if max := int64(2 * segSize); after > max {
		t.Fatalf("after Release(%d) with one view on byte 0: %d bytes retained, want <= %d "+
			"(was %d before)\n%s", c.Head(), after, max, before,
			"An oldest view pins its own segment. If it pins the suffix, peak memory "+
				"is the whole stream and the bound in the ADR is not a bound.")
	}
	if before <= after {
		t.Fatalf("control failed: %d bytes retained before Release, %d after — the "+
			"probe cannot observe reclamation at all", before, after)
	}

	// THE DIRECTORY TOO. Freeing the buffers while the entries pile up behind a
	// held segment is still unbounded growth over a stream, just at a smaller
	// constant, and it is invisible to a byte count.
	if dirAfter > 2 {
		t.Fatalf("after Release(%d): %d directory entries, want <= 2 (was %d)\n%s",
			c.Head(), dirAfter, dirBefore,
			"Entries stranded behind a held segment must be compacted away, or the "+
				"directory grows with the stream even though the bytes are gone.")
	}
	if dirBefore <= dirAfter {
		t.Fatalf("control failed: %d directory entries before Release, %d after",
			dirBefore, dirAfter)
	}
}

// F10 — A FREED SEGMENT MUST NOT BECOME THE WRITE TARGET.
//
// Killing a segment leaves its entry behind with buf == nil and n unchanged: n
// is what bounds the entry for lookups, so it cannot be zeroed. The writer's
// "is the last segment full?" test is n == len(buf), and against a nil buffer
// that reads 4 == 0 — not full — so it reuses the entry and slices buf[4:] on
// nil.
//
// It needs the last segment to be FULL (an unfull one is protected as the
// active write target) and compaction not to have removed the entry, which is
// why the older held segments are there: they keep freed entries from being the
// strict majority.
func TestFill_DoesNotWriteIntoAFreedSegment(t *testing.T) {
	t.Parallel()
	c := New(strings.NewReader("0123456789ab"), WithSegmentSize(4))

	// Three older held entries, so compaction cannot rescue this by removing
	// the freed one — the defect must be reachable on its own terms.
	var holds []*View
	for _, off := range []int64{0, 1, 2} {
		v, err := c.Acquire(off, off+1)
		if err != nil {
			t.Fatalf("Acquire(%d,%d): %v", off, off+1, err)
		}
		holds = append(holds, v)
	}
	defer func() {
		for _, v := range holds {
			v.Close()
		}
	}()

	if _, err := c.Ensure(4, 4); err != nil { // segment 1 = [4,8), full, unheld
		t.Fatalf("Ensure(4,4): %v", err)
	}
	c.Release(c.Head()) // 8: segment 1 is wholly below it and nothing holds it

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Ensure(8,4) panicked: %v\n%s", r,
				"A segment whose buffer has been freed is not a write target. Its entry "+
					"survives for lookups, so the writer's fullness test sees n against a "+
					"nil buffer and reuses it.")
		}
	}()
	if _, err := c.Ensure(8, 4); err != nil {
		t.Fatalf("Ensure(8,4): %v", err)
	}
	// And the bytes must be right: reusing a dead entry would also corrupt
	// offsets, which a panic-only assertion would not notice.
	v, err := c.Acquire(8, 12)
	if err != nil {
		t.Fatalf("Acquire(8,12): %v", err)
	}
	defer v.Close()
	if got, _ := v.String(); got != "89ab" {
		t.Fatalf("Acquire(8,12) = %q, want %q", got, "89ab")
	}
}

// errSentinel is a source failure distinguishable from anything the cache
// raises on its own.
var errSentinel = errors.New("streamcache_test: the source failed on the second read")

// countingFailReader hands out one short chunk, then fails, and counts calls.
type countingFailReader struct {
	first []byte
	calls int
}

func (r *countingFailReader) Read(p []byte) (int, error) {
	r.calls++
	if r.first != nil {
		n := copy(p, r.first)
		r.first = nil
		return n, nil
	}
	return 0, errSentinel
}

// F11 — A RELEASED REQUEST MUST NOT TOUCH THE SOURCE.
//
// Acquire checked the watermark AFTER Ensure, so a request for released bytes
// read forward first. Two consequences, and the second is the one that matters:
// the caller is told their SOURCE broke when in fact their request was invalid,
// and the diagnosis points at the wrong component entirely.
func TestAcquire_ReleasedRequestDoesNotReadOrReportTheSource(t *testing.T) {
	t.Parallel()
	r := &countingFailReader{first: []byte("0123")}
	c := New(r, WithSegmentSize(8))

	if n, err := c.Ensure(0, 4); n != 4 || err != nil {
		t.Fatalf("Ensure(0,4) = %d, %v; want 4, nil", n, err)
	}
	c.Release(4)
	calls := r.calls

	_, err := c.Acquire(0, 8) // below the watermark, and needs bytes we do not have
	if !errors.Is(err, ErrReleased) {
		t.Fatalf("Acquire(0,8) after Release(4): err=%v, want ErrReleased.\n%s", err,
			"The span is released. Reading forward to discover that the source is also "+
				"broken reports the wrong component.")
	}
	if errors.Is(err, errSentinel) {
		t.Fatalf("Acquire returned the source's error for a released span: %v", err)
	}
	if r.calls != calls {
		t.Fatalf("Acquire on a released span made %d further reader call(s); want 0",
			r.calls-calls)
	}
}

// F12 — A WATERMARK AHEAD OF HEAD APPLIES AS THE BYTES ARRIVE.
//
// Release(off) is a statement about the stream, not about the bytes read so
// far. A caller skipping forward past input it does not want must not have to
// call Release a second time, once the bytes it already disowned have been
// read, to make them go.
func TestRelease_BeyondHeadDropsBytesAsTheyArrive(t *testing.T) {
	t.Parallel()
	const segSize, segments = 32, 64
	src := strings.Repeat("y", segSize*segments)
	c := New(strings.NewReader(src), WithSegmentSize(segSize))

	if _, err := c.Ensure(0, segSize); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	c.Release(int64(len(src))) // the whole stream, most of which is unread

	if _, err := c.Ensure(0, len(src)); err != nil { // now read all of it
		t.Fatalf("Ensure(all): %v", err)
	}
	if got := c.retainedBytes(); got > 2*segSize {
		t.Fatalf("%d bytes retained after reading a stream already released; want <= %d\n%s",
			got, 2*segSize,
			"A watermark set beyond head must apply to the bytes that arrive after it, "+
				"or a forward skip keeps everything it skipped.")
	}
	if _, err := c.Acquire(0, 1); !errors.Is(err, ErrReleased) {
		t.Fatalf("Acquire(0,1) = %v, want ErrReleased", err)
	}
}
