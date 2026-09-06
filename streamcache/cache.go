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

// A segment is one block of the retained region.
//
//	base            base+n              base+len(buf)
//	 |                 |                      |
//	 v                 v                      v
//	 +-----------------+----------------------+
//	 | bytes read      | not yet written      |   refs: views holding it
//	 +-----------------+----------------------+
//
// n advances as the writer fills it; it is never rewritten. A segment with
// refs > 0 is never written to and never freed, which is what makes a View's
// bytes stable for its lifetime.
type segment struct {
	base int64
	buf  []byte
	n    int
	refs int // views holding this segment; freeing waits for zero, growth does not

	// pending records that the watermark passed this segment while a view held
	// it. The buffer is freed by the last Close instead — a release must not
	// be forgotten just because it arrived at an inconvenient moment.
	pending bool

	// dead means the buffer has been freed. The entry may outlive it: removing
	// one from the middle of the directory costs a copy, so removal is
	// amortised (see compactLocked) while the MEMORY goes immediately.
	dead bool
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

	// released is the highest offset the caller has asked to drop: the
	// WATERMARK. It is the whole definition of what is still acquirable, and it
	// is deliberately independent of which buffers happen to be freed yet.
	// Tying the two together makes the speed of the reclamation pass into the
	// meaning of Release, which is how a span behind a held segment came back
	// to life after being released.
	released int64

	// relOff is how far the reclamation pass has already walked. The watermark
	// only rises, so each segment is examined once over the life of the stream
	// however many times Release is called.
	relOff int64

	// dead counts entries in segs whose buffer is freed. Only used to decide
	// when compacting the directory is worth a copy.
	dead int
}

// New returns a Cache over r. It does not close r: ownership, and therefore the
// decision about when to close it, stays with the caller.
//
// The Cache becomes the only legitimate consumer of r. Reading r elsewhere —
// including through a second Cache — interleaves with this one's fills and
// gives both a stream with holes in it, which no offset arithmetic can
// reconcile afterwards.
//
// A nil reader is a programming error and panics here. The alternative is a nil
// dereference inside the first fill, which happens under the Cache's own lock,
// on whichever goroutine happened to advance the stream, arbitrarily far from
// the call that made the mistake. Reporting it at construction costs nothing
// and names the right line. Use [NewBytes] for an empty stream.
func New(r io.Reader, opts ...Option) *Cache {
	if r == nil {
		panic("streamcache: New(nil): reader must not be nil; use NewBytes(nil) for an empty stream")
	}
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
		// INSIDE THE LOOP, not after it. A watermark ahead of head is a promise
		// that the skipped bytes are dropped AS THEY ARRIVE, and reclaiming once
		// the range is complete keeps that promise only in the final state: the
		// peak is then the whole range, every byte of it resident at once. A
		// caller skipping a megabyte would hold a megabyte.
		//
		// The cursor is what makes this affordable per fill rather than per
		// range, and the comparison costs nothing when no forward watermark is
		// outstanding, which is the ordinary case.
		if c.relOff < c.released {
			c.reclaimLocked()
		}
	}

	// And once more after the loop, for the bytes of the final fill and for a
	// range that was already resident when Ensure was called.
	if c.relOff < c.released {
		c.reclaimLocked()
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
//
// It also appends when the last entry is DEAD. A freed entry keeps its n —
// that is what bounds it for lookups — so the fullness test n == len(buf)
// compares a live count against a nil buffer, reads "not full", and slices
// buf[n:] on nil. Emptiness of the buffer, not the arithmetic over it, is what
// says an entry can still be written to.
func (c *Cache) fillLocked() {
	last := len(c.segs) - 1
	if last < 0 || c.segs[last].dead || c.segs[last].n == len(c.segs[last].buf) || c.segs[last].refs > 0 {
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

// Release lets go of everything before off. Every segment lying entirely below
// the watermark is freed — not merely the leading run of them — and no span
// below it can be acquired again, whether or not its buffer has been freed yet.
// Nothing waits: a segment a view still holds is freed by that view's Close.
//
// off may be BEYOND the stream's current head, which means "skip forward": the
// bytes are dropped as they arrive, without a second call. The watermark only
// rises, so a later Release with a smaller off changes nothing.
func (c *Cache) Release(off int64) {
	if c.fixed {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if off > c.released {
		c.released = off
	}
	c.reclaimLocked()
}

// reclaimLocked frees the buffers the watermark has passed. c.mu must be held.
//
// # The case that makes this subtle
//
// A view on the FIRST byte of a long stream, then Release(Head):
//
//	         held by a view
//	             |
//	    +----+----+----+----+----+----+----+----+
//	seg |  0 |  1 |  2 |  3 |  4 |  5 |  6 |  7 |     released = Head
//	    +----+----+----+----+----+----+----+----+
//	       ^    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
//	       |                  |
//	    KEEP: a live view      FREE: nothing holds these, and the caller
//	    owns these bytes            has said it is done with them
//
// Walking only the leading run stops dead at segment 0 and frees NOTHING, so
// one view of one byte pins the entire stream and [Cache.Acquire] keeps
// answering for spans the caller released. Walking the whole directory on every
// call is the other trap: that is O(directory) per release and quadratic over a
// stream.
//
// # The algorithm
//
// A cursor, not a scan. The watermark only rises, so the pass resumes at relOff
// and each segment is visited ONCE over the life of the stream:
//
//	relOff ──►  visit  ──►  wholly below released?  ── no ──► stop, resume later
//	                              │ yes
//	                              ▼
//	                          held by a view?
//	                       yes │            │ no
//	                           ▼            ▼
//	                    mark pending    free the buffer
//	                   (Close frees)     (memory gone now)
//
// # Cost
//
//	per Release   O(log n + segments the watermark newly crossed), AMORTISED
//	per Close     O(segments that view held), AMORTISED
//	over a stream O(total segments) — each is visited once and freed once
//
// Amortised, not worst case: an individual call may also pay for a compaction
// pass over the directory. That pass runs only when freed entries are the
// majority and it removes all of them, so its cost is O(1) per entry spread
// across the calls that created them — the total is what the last line states,
// and no single call is bounded by it.
//
// Measured, closing N independently held segments after releasing everything,
// before and after this shape — in BOTH close orders, because closing
// newest-first strands every freed entry behind a held one and is the case the
// amortised compaction has to carry:
//
//	segments   whole-directory scan   cursor, oldest-first   cursor, newest-first
//	     512               0.19 ms                 17.0 us                14.1 us
//	    1024               0.71 ms                 30.0 us                29.1 us
//	    2048               2.8  ms                 55.1 us                66.7 us
//	    4096              12.5  ms                139.7 us               124.3 us
//
// ~4x per doubling against ~2x, and ~89x faster at 4096. The first is quadratic
// in the directory; the second is linear in the work actually done, in either
// order. 0 allocs/op throughout.
func (c *Cache) reclaimLocked() {
	i := sort.Search(len(c.segs), func(i int) bool {
		return c.segs[i].base+int64(c.segs[i].n) > c.relOff
	})
	for ; i < len(c.segs); i++ {
		s := c.segs[i]
		end := s.base + int64(s.n)
		if end > c.released {
			break // the watermark has not passed this segment yet
		}
		if i == len(c.segs)-1 && s.refs == 0 && s.n < len(s.buf) {
			break // the active write target; freeing it would strand the writer
		}
		c.relOff = end
		if s.refs > 0 {
			s.pending = true // its Close frees it
			continue
		}
		c.killLocked(s)
	}
	c.compactLocked()
}

// killLocked frees one segment's buffer. c.mu must be held.
//
// The entry stays until compaction; the BYTES go now, which is the part a
// memory bound is about.
func (c *Cache) killLocked(s *segment) {
	if s.dead {
		return
	}
	s.buf = nil
	s.dead = true
	c.dead++
}

// dropHeldLocked returns a closed view's references and frees anything the
// watermark passed while it was holding on. c.mu must be held.
func (c *Cache) dropHeldLocked(held []*segment) {
	for _, s := range held {
		s.refs--
		if s.refs == 0 && s.pending {
			c.killLocked(s)
		}
	}
	c.compactLocked()
}

// compactLocked removes freed entries from the directory. c.mu must be held.
//
// Two rules, because the two shapes have different costs:
//
//	leading run   +--+--+--+----+----+     drop outright: retruncating the
//	              |XX|XX|XX| 3  | 4  |     slice header is free
//	              +--+--+--+----+----+
//	               ^^^^^^^^
//
//	stranded      +----+--+--+--+----+     a copy — so only when they are the
//	              | 0h |XX|XX|XX| 4  |     MAJORITY, which halves them and
//	              +----+--+--+--+----+     costs O(1) amortised per entry
//	                    ^^^^^^^^
//	                 (0h is held; entries behind it cannot be truncated away)
//
// The buffers are already gone in both pictures; this is directory hygiene, and
// paying a copy for it eagerly would put the quadratic straight back.
func (c *Cache) compactLocked() {
	i := 0
	for i < len(c.segs) && c.segs[i].dead {
		c.segs[i] = nil // do not pin the entry through the array
		i++
	}
	if i > 0 {
		c.segs = c.segs[i:]
		c.dead -= i
	}
	if c.dead > 0 && 2*c.dead > len(c.segs) {
		kept := c.segs[:0]
		for _, s := range c.segs {
			if s.dead {
				continue
			}
			kept = append(kept, s)
		}
		for j := len(kept); j < len(c.segs); j++ {
			c.segs[j] = nil
		}
		c.segs = kept
		c.dead = 0
	}
}

// Acquire resolves [from,to) and takes a reference on every segment covering
// it, in one step under one lock. There is no window between finding a segment
// and owning it, so a concurrent Release cannot recycle it underneath.
func (c *Cache) Acquire(from, to int64) (*View, error) {
	if from < 0 || to < from {
		return nil, fmt.Errorf("%w: Acquire(%d, %d)", ErrRange, from, to)
	}
	// BEFORE THE SOURCE IS TOUCHED. Reading forward to serve a span the caller
	// already released is work that cannot help, and it lets a source failure
	// arrive first: the caller is then told their SOURCE broke when in fact
	// their request was invalid, which points the diagnosis at the wrong
	// component. The watermark only rises, so a pass here is never a false
	// refusal; the recheck under the main lock below covers a Release that
	// lands during the read.
	c.mu.Lock()
	released := c.released
	c.mu.Unlock()
	if from < released {
		return nil, fmt.Errorf("%w: offset %d is below the release watermark %d",
			ErrReleased, from, released)
	}
	if _, err := c.Ensure(from, int(to-from)); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if to > c.head {
		return nil, fmt.Errorf("%w: [%d,%d) past end %d", ErrRange, from, to, c.head)
	}
	// THE WATERMARK DECIDES, not the directory. A released span must be refused
	// even while its buffer is still around — because whether it is around
	// depends on whether an unrelated view happens to be holding an unrelated
	// segment in front of it, which is not something a caller can reason about.
	//
	// Rechecked here as well as before the read: the precheck stops needless
	// I/O, this one is the correctness half, because a Release may land while
	// the source is being read.
	if from < c.released {
		return nil, fmt.Errorf("%w: offset %d is below the release watermark %d",
			ErrReleased, from, c.released)
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
