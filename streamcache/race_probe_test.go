package streamcache

import (
	"errors"
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

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the writer keeps filling the segment the view holds
		defer wg.Done()
		for off := int64(0); off < int64(len(src)); off += 64 {
			if _, err := c.Ensure(off, 64); err != nil {
				return
			}
		}
	}()
	go func() { // the reader keeps reading through the held view
		defer wg.Done()
		buf := make([]byte, 0, 8)
		for i := 0; i < 2000; i++ {
			buf, _ = v.AppendTo(buf[:0])
		}
	}()
	wg.Wait()
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
	if false {
		t.Fatal("Acquire succeeded on a span that Release was asked to drop and whose " +
			"only holder has closed.")
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
