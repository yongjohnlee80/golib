package streamcache

import (
	"errors"
	"fmt"
	"io"
	"sort"
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

const (
	defaultSegmentSize = 32 << 10

	// maxEmptyReads bounds a run of legal (0, nil) reads before the Cache
	// reports io.ErrNoProgress, mirroring what the standard library does rather
	// than inventing a different tolerance.
	maxEmptyReads = 100
)

// Option configures a Cache. Configuration is immutable after New.
type Option func(*config)

type config struct{ segment int }

// WithSegmentSize sets the retained block size. Larger blocks mean fewer
// allocations and fewer spans that straddle a boundary; smaller ones release at
// a finer grain.
// A non-positive size is a programming error and panics at construction rather
// than silently becoming the default — a caller who wrote WithSegmentSize(0)
// meant something, and quietly substituting 32 KiB would hide it until a
// memory figure looked wrong.
func WithSegmentSize(n int) Option {
	return func(c *config) {
		if n <= 0 {
			panic(fmt.Sprintf("streamcache: WithSegmentSize(%d): size must be positive", n))
		}
		c.segment = n
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
	fixed bool // NewBytes-style: one immutable segment, never grown or dropped

	// released is the highest offset the caller has asked to drop. A Release
	// that finds a segment held cannot drop it THEN, so the request is recorded
	// and re-applied when the last view lets go. Without this, "released" meant
	// only "released if nobody happened to be holding it", the span stayed
	// acquirable afterwards, and the retained set only ever grew.
	released int64
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

	// (0, nil) is LEGAL per io.Reader, and a loop that reads it as "try again"
	// never terminates — while holding c.mu, so it stops every other goroutine
	// too. Go's own io.Copy tolerates a bounded run of empty reads and then
	// gives up; so does this.
	empties := 0
	for c.head < off+int64(n) && c.err == nil {
		before := c.head
		c.fillLocked()
		if c.head == before {
			empties++
			if empties >= maxEmptyReads {
				c.err = io.ErrNoProgress
				break
			}
			continue
		}
		empties = 0
	}

	avail := c.head - off
	switch {
	case avail < 0:
		avail = 0
	case avail > int64(n):
		avail = int64(n)
	}
	// A SHORT ANSWER CARRIES THE SOURCE'S REASON. Returning ErrRange here would
	// tell the caller their span was wrong when in fact their source broke, and
	// the real error would be lost at the only point it could be reported.
	if avail < int64(n) && c.err != nil && !errors.Is(c.err, io.EOF) {
		return int(avail), c.err
	}
	return int(avail), nil
}

// fillLocked reads one block. c.mu must be held.
//
// It appends a segment when the last one is full OR IS HELD BY A VIEW. The
// second condition is load-bearing twice over, and the first version of this
// function claimed it in a comment while checking only the first:
//
//   - it keeps retention from blocking the writer — a held segment is left
//     alone and a new one takes its place; and
//   - it is what makes a View's bytes IMMUTABLE for its lifetime. Writing into
//     a segment a view holds is a data race, because s.n advances under c.mu
//     while View.AppendTo reads it under v.mu. Reproduced with a one-byte
//     reader, which leaves a held segment PARTIAL and therefore still a write
//     target; the committed suite could not reach it because its reader
//     delivered full blocks and every held segment was already complete.
func (c *Cache) fillLocked() {
	last := len(c.segs) - 1
	if last < 0 || c.segs[last].n == len(c.segs[last].buf) || c.segs[last].refs > 0 {
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
	if off > c.released {
		c.released = off
	}
	c.dropLocked()
}

// dropLocked discards the reclaimable PREFIX of the directory. Called by
// Release and by the last View.Close on a segment, so a deferred release
// happens rather than being forgotten.
//
// PREFIX ONLY, AND O(DROPPED) — not a scan of the whole directory.
//
// The first version rebuilt the entire slice on every final Close, which moved
// F4's quadratic from access to reclamation: measured at ~4x per doubling —
// 512 segments ~0.19 ms, 4096 ~12.5 ms, all under c.mu. That is the same
// mistake as F4 in a second place, and it is the mistake of reasoning about the
// SIZE of a set instead of how often it is walked.
//
// Stopping at the first segment that cannot go is not a weakening: it is the
// semantics this cache already documents. A held early segment pins everything
// after it, so peak memory is governed by the OLDEST live view. Scanning past
// it to reclaim a later hole would contradict that and buy nothing a caller
// could rely on.
func (c *Cache) dropLocked() {
	i := 0
	for i < len(c.segs) {
		s := c.segs[i]
		if s.refs != 0 || s.base+int64(s.n) > c.released {
			break
		}
		c.segs[i] = nil // do not pin a dropped segment through the array
		i++
	}
	if i > 0 {
		c.segs = c.segs[i:]
	}
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
	// Acquiring a span must cost the segments it COVERS, not the segments the
	// cache holds — a reader taking one span out of a long stream must not get
	// slower as the stream grows behind it. So: one search to find the first
	// segment, then a cursor walk. Searching again per segment is O(k log n)
	// and reintroduces the dependency on n that the search was meant to remove.
	//
	// Arithmetic indexing is NOT available here: a segment held by a view is
	// left short when the writer moves on, so segment sizes vary.
	i := c.indexLocked(from)
	if i < 0 {
		return nil, fmt.Errorf("%w: offset %d", ErrReleased, from)
	}
	var held []*segment
	off := from
	for off < to {
		if i >= len(c.segs) || !c.segs[i].covers(off) {
			for _, h := range held {
				h.refs--
			}
			return nil, fmt.Errorf("%w: offset %d", ErrReleased, off)
		}
		s := c.segs[i]
		s.refs++
		held = append(held, s)
		off = s.base + int64(s.n)
		i++
	}
	return &View{c: c, from: from, to: to, held: held}, nil
}

// findLocked returns the segment covering off, or nil. c.mu must be held.
//
// BINARY SEARCH, not a scan. The first version scanned linearly and argued the
// retained set is "usually one or two" — but Acquire and AppendTo call it once
// per segment of a span, so the cost was O(k²) in the number of segments a span
// covers. Measured at 64-byte segments: 6 ms to acquire 256 KiB. Segments are
// contiguous and ascending by construction, so a search is available for free
// and the argument for the scan was simply wrong.
func (c *Cache) findLocked(off int64) *segment {
	if i := c.indexLocked(off); i >= 0 {
		return c.segs[i]
	}
	return nil
}

// indexLocked returns the index of the segment covering off, or -1. c.mu must
// be held. Segments are contiguous and ascending by construction, so a search
// is available; arithmetic is NOT, because a held partial segment is left short
// and the next one starts after it, so sizes vary.
func (c *Cache) indexLocked(off int64) int {
	i := sort.Search(len(c.segs), func(i int) bool {
		return c.segs[i].base+int64(c.segs[i].n) > off
	})
	if i < len(c.segs) && c.segs[i].covers(off) {
		return i
	}
	return -1
}
