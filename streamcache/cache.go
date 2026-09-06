// Package streamcache retains a bounded region of a forward-only byte stream
// and hands out owning views into it.
//
// It knows nothing about tokens or syntax. "Read forward, keep what someone
// still needs, let go of the rest" describes a lexer, a protocol framer and a
// log tailer equally well, which is why it is its own package.
//
// # Lifetimes, not immutability
//
// Every access to retained bytes goes through [Cache.Acquire], which returns a
// [View] that OWNS the bytes until it is closed. There is deliberately no way
// to obtain bytes without also obtaining the lifetime that keeps them valid.
//
// An earlier design argued that append-only storage made readers safe because
// written bytes never change. That reasoning is wrong and worth recording:
// immutable payload bytes do not synchronise the lookup that FINDS a segment,
// nor its release and reuse, nor the lifetime of a view already handed out.
// Only an explicit lifetime does.
//
// # Retention never blocks the writer
//
// A held segment is never recycled — and when every segment is held and the
// writer needs space, it ALLOCATES. It does not wait.
//
// This is a correctness requirement, not a tuning choice. A consumer that holds
// a view on the first token of a statement while scanning forward for its
// terminator would otherwise deadlock: the writer needs a segment, all are
// held, the writer blocks, and the consumer never releases because it is
// waiting for the writer. A forward scan must always be able to make progress.
//
// # Memory
//
// Peak is O(buffer + retained views). A consumer that acquires nothing keeps
// one segment plus whatever it is mid-read on, whatever the size of the stream.
// Retention is the caller's choice; this package has no policy of its own,
// because it cannot know what a caller still needs.
package streamcache

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

var (
	// ErrReleased reports a span that is no longer retained: it was never
	// acquired, or its view was closed. Always a caller error — and always
	// reported, because returning bytes from a recycled segment instead would
	// be silent corruption.
	ErrReleased = errors.New("streamcache: span is no longer retained")

	// ErrRange reports a span that is not one: inverted, negative, or beyond
	// what the stream contains.
	ErrRange = errors.New("streamcache: span out of range")
)

const defaultSegmentSize = 32 << 10

// Option configures a Cache. Configuration is immutable after New.
type Option func(*config)

type config struct{ segment int }

// WithSegmentSize sets the retained block size. Larger blocks mean fewer
// allocations and fewer spans that straddle a boundary; smaller ones release at
// a finer grain.
func WithSegmentSize(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.segment = n
		}
	}
}

type segment struct {
	base int64
	buf  []byte
	n    int
	refs int // views holding this segment; recycling waits for zero, growth does not
}

func (s *segment) covers(off int64) bool {
	return off >= s.base && off < s.base+int64(s.n)
}

// Cache retains a region of a forward-only stream. The zero value is unusable;
// call New.
type Cache struct {
	r   io.Reader
	cfg config

	mu    sync.Mutex
	segs  []*segment
	head  int64
	err   error
	fixed bool // ScanBytes-style: one immutable segment, never grown or dropped
}

// New returns a Cache over r. It does not close r: ownership stays with the
// caller, so two caches can be built over one source.
func New(r io.Reader, opts ...Option) *Cache {
	cfg := config{segment: defaultSegmentSize}
	for _, o := range opts {
		o(&cfg)
	}
	return &Cache{r: r, cfg: cfg}
}

// NewBytes returns a Cache over b WITHOUT COPYING IT. The slice becomes one
// immutable segment that is never written to, never recycled, and never
// released, so Acquire is free and Close releases nothing.
//
// The caller must not mutate b for the life of the Cache.
func NewBytes(b []byte) *Cache {
	return &Cache{
		cfg:   config{segment: len(b)},
		segs:  []*segment{{base: 0, buf: b, n: len(b)}},
		head:  int64(len(b)),
		err:   io.EOF,
		fixed: true,
	}
}

// Head returns the offset one past the last byte read from the stream.
func (c *Cache) Head() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.head
}

// Ensure reads forward until n bytes are available at off, or the stream ends.
// It returns how many are available there, which may be fewer than n at EOF.
func (c *Cache) Ensure(off int64, n int) (int, error) {
	if off < 0 || n < 0 {
		return 0, fmt.Errorf("%w: Ensure(off=%d, n=%d)", ErrRange, off, n)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.head < off+int64(n) && c.err == nil {
		c.fillLocked()
	}
	if c.err != nil && !errors.Is(c.err, io.EOF) && c.head <= off {
		return 0, c.err
	}
	avail := c.head - off
	switch {
	case avail < 0:
		avail = 0
	case avail > int64(n):
		avail = int64(n)
	}
	return int(avail), nil
}

// fillLocked reads one block. c.mu must be held.
//
// It appends a segment when the last one is full OR is held by a view. That
// second condition is what keeps retention from blocking the writer: a held
// segment is simply left alone and a new one takes its place.
func (c *Cache) fillLocked() {
	last := len(c.segs) - 1
	if last < 0 || c.segs[last].n == len(c.segs[last].buf) {
		c.segs = append(c.segs, &segment{base: c.head, buf: make([]byte, c.cfg.segment)})
		last = len(c.segs) - 1
	}
	s := c.segs[last]
	n, err := c.r.Read(s.buf[s.n:])
	s.n += n
	c.head += int64(n)
	if err != nil {
		c.err = err
	}
}

// Release drops every segment lying entirely before off that no view holds.
// Segments still held are kept; nothing waits.
func (c *Cache) Release(off int64) {
	if c.fixed {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	keep := c.segs[:0]
	for _, s := range c.segs {
		if s.refs == 0 && s.base+int64(s.n) <= off {
			continue
		}
		keep = append(keep, s)
	}
	c.segs = keep
}

// Acquire resolves [from,to) and takes a reference on every segment covering
// it, in one step under one lock. There is no window between finding a segment
// and owning it, so a concurrent Release cannot recycle it underneath.
func (c *Cache) Acquire(from, to int64) (*View, error) {
	if from < 0 || to < from {
		return nil, fmt.Errorf("%w: Acquire(%d, %d)", ErrRange, from, to)
	}
	if _, err := c.Ensure(from, int(to-from)); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if to > c.head {
		return nil, fmt.Errorf("%w: [%d,%d) past end %d", ErrRange, from, to, c.head)
	}
	var held []*segment
	for off := from; off < to; {
		s := c.findLocked(off)
		if s == nil {
			for _, h := range held {
				h.refs--
			}
			return nil, fmt.Errorf("%w: offset %d", ErrReleased, off)
		}
		s.refs++
		held = append(held, s)
		off = s.base + int64(s.n)
	}
	return &View{c: c, from: from, to: to, held: held}, nil
}

// findLocked returns the segment covering off, or nil. c.mu must be held.
//
// Linear over RETAINED segments, which is the right complexity: that set is
// bounded by what callers hold, and is usually one or two. A tree would buy
// O(log n) over an almost-always-tiny set and charge every lookup for it.
func (c *Cache) findLocked(off int64) *segment {
	for _, s := range c.segs {
		if s.covers(off) {
			return s
		}
	}
	return nil
}
