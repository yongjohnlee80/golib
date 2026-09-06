package streamcache

import (
	"fmt"
	"io"
	"sync"
)

// A View owns the bytes of one span and keeps them valid until it is closed.
//
// It is the ONLY way to reach retained bytes, which is deliberate: an accessor
// that returned bytes without a lifetime would leave the caller holding
// something that could be recycled underneath them, and no amount of care at
// the call site can fix that.
//
// A View is safe for concurrent reads of its own bytes. Close is idempotent and
// safe to call from any goroutine.
type View struct {
	c    *Cache
	from int64
	to   int64

	mu     sync.Mutex
	held   []*segment
	closed bool
}

// Len is the span's length in bytes.
func (v *View) Len() int64 { return v.to - v.from }

// Reader returns a reader over the span. NO COPY is made: it walks the segments
// the span covers, which is why the span may straddle a boundary — a single
// contiguous []byte could not represent that at all.
//
// The reader is valid until the View is closed.
func (v *View) Reader() io.Reader {
	return &viewReader{v: v, off: v.from}
}

// AppendTo appends the span to dst and returns it. It copies only where it must
// and reuses the caller's buffer, so a loop over many spans allocates once.
func (v *View) AppendTo(dst []byte) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return dst, fmt.Errorf("%w: view closed", ErrReleased)
	}
	for off := v.from; off < v.to; {
		s := v.segmentFor(off)
		if s == nil {
			return dst, fmt.Errorf("%w: offset %d", ErrReleased, off)
		}
		start := off - s.base
		end := int64(s.n)
		if s.base+end > v.to {
			end = v.to - s.base
		}
		dst = append(dst, s.buf[start:end]...)
		off = s.base + end
	}
	return dst, nil
}

// Bytes returns the span as a string. Convenience over AppendTo for the
// minority of spans whose text is kept.
func (v *View) String() (string, error) {
	b, err := v.AppendTo(make([]byte, 0, v.Len()))
	return string(b), err
}

// segmentFor finds the held segment covering off. v.mu must be held.
func (v *View) segmentFor(off int64) *segment {
	for _, s := range v.held {
		if s.covers(off) {
			return s
		}
	}
	return nil
}

// Close releases the span. It is idempotent: releasing twice is a no-op rather
// than a double-decrement that would let a live segment be recycled.
func (v *View) Close() error {
	v.mu.Lock()
	held := v.held
	already := v.closed
	v.closed = true
	v.held = nil
	v.mu.Unlock()
	if already {
		return nil
	}
	v.c.mu.Lock()
	for _, s := range held {
		s.refs--
	}
	v.c.mu.Unlock()
	return nil
}

type viewReader struct {
	v   *View
	off int64
}

func (r *viewReader) Read(p []byte) (int, error) {
	if r.off >= r.v.to {
		return 0, io.EOF
	}
	r.v.mu.Lock()
	defer r.v.mu.Unlock()
	if r.v.closed {
		return 0, fmt.Errorf("%w: view closed", ErrReleased)
	}
	s := r.v.segmentFor(r.off)
	if s == nil {
		return 0, fmt.Errorf("%w: offset %d", ErrReleased, r.off)
	}
	start := r.off - s.base
	end := int64(s.n)
	if s.base+end > r.v.to {
		end = r.v.to - s.base
	}
	n := copy(p, s.buf[start:end])
	r.off += int64(n)
	return n, nil
}
