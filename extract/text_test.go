package extract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestText_CopiesAsIs(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"", "x", "# heading\n\nbody ëë\n", strings.Repeat("0123456789", textChunk/10*3+7)} {
		var out bytes.Buffer
		info, err := Text{}.Extract(context.Background(), strings.NewReader(src), int64(len(src)), &out)
		if err != nil || out.String() != src || info != (Info{}) {
			t.Errorf("%d bytes: copied %d, %v, %+v", len(src), out.Len(), err, info)
		}
	}
}

// Text reads only the size it was given, so a reader longer than the container is not overread.
func TestText_ReadsOnlySize(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if _, err := (Text{}).Extract(context.Background(), strings.NewReader("abcdef"), 3, &out); err != nil || out.String() != "abc" {
		t.Errorf("copied %q, %v; want the first 3 bytes", out.String(), err)
	}
}

// cancelOnRead cancels the extraction during the first read, so cancellation lands mid-copy.
type cancelOnRead struct {
	r      io.ReaderAt
	cancel context.CancelFunc
}

func (c cancelOnRead) ReadAt(p []byte, off int64) (int, error) {
	c.cancel()
	return c.r.ReadAt(p, off)
}

func TestText_StopsWhenCancelled(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("x", 3*textChunk)
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	_, err := Text{}.Extract(ctx, cancelOnRead{strings.NewReader(src), cancel}, int64(len(src)), &out)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if out.Len() != textChunk {
		t.Errorf("copied %d bytes after cancellation, want only the chunk already read (%d)", out.Len(), textChunk)
	}
}

func TestText_ReturnsWriteAndReadErrors(t *testing.T) {
	t.Parallel()
	failed := errors.New("disk full")
	if _, err := (Text{}).Extract(context.Background(), strings.NewReader("abc"), 3, failingWriter{failed}); !errors.Is(err, failed) {
		t.Errorf("write error: %v", err)
	}
	broken := errors.New("bad sector")
	if _, err := (Text{}).Extract(context.Background(), failingReaderAt{broken}, 10, io.Discard); !errors.Is(err, broken) {
		t.Errorf("read error: %v", err)
	}
}

type failingReaderAt struct{ err error }

func (f failingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, f.err }

// Text holds one chunk at most, however large the container: the memory claim behind streaming. It
// is measured in bytes allocated, since a whole-file buffer is still a single allocation.
func TestText_MemoryIsOneChunk(t *testing.T) {
	src := bytes.Repeat([]byte("x"), 64*textChunk) // 4 MiB
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := (Text{}).Extract(context.Background(), bytes.NewReader(src), int64(len(src)), io.Discard); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 2*textChunk {
		t.Errorf("allocated %d bytes to copy %d; want no more than about one %d-byte chunk", got, len(src), textChunk)
	}
}

// A source that ends before its declared size is an error: a file that shrank after its size was
// read must not be indexed as if it were complete.
func TestText_SourceShorterThanSize(t *testing.T) {
	t.Parallel()
	s := New(Register(Text{}, ".md"))
	_, err := s.Extract(context.Background(), "a.md", strings.NewReader("abc"), 5, io.Discard)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

// A destination that accepts fewer bytes than it was given, without an error, is a short write.
func TestText_ShortWrite(t *testing.T) {
	t.Parallel()
	_, err := Text{}.Extract(context.Background(), strings.NewReader("abcdef"), 6, shortWriter{})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("err = %v, want io.ErrShortWrite", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
