package rpc

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// hidden strips a file of its deadlines, so StreamConn takes the goroutine path on every OS.
type hiddenR struct{ io.ReadCloser }
type hiddenW struct{ io.WriteCloser }

// streamFixture is a StreamConn and the far ends of its two pipes.
type streamFixture struct {
	conn      net.Conn
	peerWrite *os.File // what the conn reads
	peerRead  *os.File // what the conn writes
}

func newStreamFixture(t *testing.T, hide bool) *streamFixture {
	t.Helper()
	r, peerW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	peerR, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var cr io.ReadCloser = r
	var cw io.WriteCloser = w
	if hide {
		cr, cw = hiddenR{r}, hiddenW{w}
	}
	f := &streamFixture{conn: StreamConn(cr, cw), peerWrite: peerW, peerRead: peerR}
	// every call is bounded, so a defect fails its cell with a message rather than hanging the run
	if err := f.conn.SetDeadline(later()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = peerW.Close() // first, so a read stuck in the OS comes back
		_ = peerR.Close()
		_ = f.conn.Close()
	})
	return f
}

// later is a deadline no healthy call comes near: what a cell sets in place of none.
func later() time.Time { return time.Now().Add(5 * time.Second) }

// The contract, on both paths: on Unix the bare pipe has deadlines of its own, and on Windows it
// has none, so there it is the goroutine path on a real anonymous pipe.
func eachStream(t *testing.T, run func(t *testing.T, f *streamFixture)) {
	t.Run("the files as they are", func(t *testing.T) { run(t, newStreamFixture(t, false)) })
	t.Run("deadlines hidden", func(t *testing.T) { run(t, newStreamFixture(t, true)) })
}

func TestAStreamConnCarriesBytesBothWays(t *testing.T) {
	eachStream(t, func(t *testing.T, f *streamFixture) {
		if _, err := f.peerWrite.WriteString("in"); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 8)
		n, err := f.conn.Read(b)
		if err != nil || string(b[:n]) != "in" {
			t.Fatalf("Read = %q, %v", b[:n], err)
		}
		if _, err := f.conn.Write([]byte("out")); err != nil {
			t.Fatal(err)
		}
		n, err = f.peerRead.Read(b)
		if err != nil || string(b[:n]) != "out" {
			t.Fatalf("the peer read %q, %v", b[:n], err)
		}
	})
}

func TestAStreamConnsReadDeadlineFires(t *testing.T) {
	eachStream(t, func(t *testing.T, f *streamFixture) {
		if err := f.conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.conn.Read(make([]byte, 8)); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("Read past its deadline: %v", err)
		}
	})
}

// A read that has timed out loses nothing: what arrives after it, and what the goroutine had
// already read, comes to the next read.
func TestAStreamConnReadPastItsDeadlineLosesNothing(t *testing.T) {
	eachStream(t, func(t *testing.T, f *streamFixture) {
		if err := f.conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 64)
		if _, err := f.conn.Read(b); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("the first read: %v", err)
		}
		if _, err := f.peerWrite.WriteString("kept"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond) // the goroutine has it by now, and no read is waiting
		if err := f.conn.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.conn.Read(b); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("a read with its deadline already past: %v", err)
		}
		if err := f.conn.SetReadDeadline(later()); err != nil {
			t.Fatal(err)
		}
		n, err := f.conn.Read(b)
		if err != nil || string(b[:n]) != "kept" {
			t.Fatalf("the read after the timeouts = %q, %v; want what had arrived", b[:n], err)
		}
	})
}

// A chunk bigger than the reader's buffer comes out whole across reads.
func TestAStreamConnSmallReadsGetTheWholeChunk(t *testing.T) {
	eachStream(t, func(t *testing.T, f *streamFixture) {
		if _, err := f.peerWrite.WriteString("abcdefg"); err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		b := make([]byte, 3)
		for got.Len() < 7 {
			n, err := f.conn.Read(b)
			if err != nil {
				t.Fatalf("after %q the rest of the chunk never came: %v", got.String(), err)
			}
			got.Write(b[:n])
		}
		if got.String() != "abcdefg" {
			t.Fatalf("read %q", got.String())
		}
	})
}

// A deadline set while a read waits applies to it: this is how a session wakes its reader.
func TestAStreamConnsDeadlineWakesAWaitingRead(t *testing.T) {
	eachStream(t, func(t *testing.T, f *streamFixture) {
		done := make(chan error, 1)
		go func() {
			_, err := f.conn.Read(make([]byte, 8))
			done <- err
		}()
		time.Sleep(30 * time.Millisecond)
		if err := f.conn.SetReadDeadline(time.Now()); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatalf("the woken read: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a deadline set during the read did not wake it")
		}
	})
}

func TestAStreamConnsEOFAndDataBeforeIt(t *testing.T) {
	eachStream(t, func(t *testing.T, f *streamFixture) {
		if _, err := f.peerWrite.WriteString("last"); err != nil {
			t.Fatal(err)
		}
		_ = f.peerWrite.Close()
		got, err := io.ReadAll(f.conn)
		if err != nil || string(got) != "last" {
			t.Fatalf("ReadAll = %q, %v", got, err)
		}
		if _, err := f.conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Fatalf("a read after the end: %v", err)
		}
	})
}

// Writing past what the pipe holds with nothing reading blocks, and the deadline ends it.
func TestAStreamConnsWriteDeadlineFires(t *testing.T) {
	eachStream(t, func(t *testing.T, f *streamFixture) {
		if err := f.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := f.conn.Write(bytes.Repeat([]byte("x"), 16<<20))
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatalf("a write past its deadline: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the write's deadline did not end it")
		}
	})
}

// On the goroutine path a write that timed out can't be taken back, so the side stays broken, and
// a write that never started breaks nothing.
func TestAPumpedWriteThatTimedOutBreaksTheSide(t *testing.T) {
	f := newStreamFixture(t, true)
	if err := f.conn.SetWriteDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Write([]byte("x")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("a write with its deadline already past: %v", err)
	}
	if err := f.conn.SetWriteDeadline(later()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Write([]byte("y")); err != nil {
		t.Fatalf("a write that never started broke the side: %v", err)
	}
	if err := f.conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Write(bytes.Repeat([]byte("x"), 16<<20)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the blocked write: %v", err)
	}
	if err := f.conn.SetWriteDeadline(later()); err != nil {
		t.Fatal(err)
	}
	_, err := f.conn.Write([]byte("z"))
	if !errors.Is(err, os.ErrDeadlineExceeded) || !strings.Contains(err.Error(), "earlier write") {
		t.Fatalf("a write after one timed out: %v", err)
	}
}

func TestClosingAStreamConnEndsAWaitingRead(t *testing.T) {
	f := newStreamFixture(t, true)
	done := make(chan error, 1)
	go func() {
		_, err := f.conn.Read(make([]byte, 8))
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	_ = f.peerWrite.Close() // Windows can't interrupt a read in the OS on the Close alone
	if err := f.conn.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the read returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not end the read")
	}
	if err := f.conn.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("a second Close: %v", err)
	}
}

// Reads, writes, deadlines and Close from different goroutines, for the race detector.
func TestAStreamConnTakesConcurrentCallers(t *testing.T) {
	f := newStreamFixture(t, true)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(3)
		go func() { defer wg.Done(); _, _ = f.conn.Read(make([]byte, 8)) }()
		go func() { defer wg.Done(); _, _ = f.conn.Write([]byte("x")) }()
		go func() { defer wg.Done(); _ = f.conn.SetDeadline(time.Now().Add(5 * time.Millisecond)) }()
	}
	wg.Wait()
	_ = f.peerWrite.Close()
	_ = f.conn.Close()
}
