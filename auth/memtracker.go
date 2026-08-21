package auth

import (
	"math"
	"sync"
	"time"
)

// Backoff describes how failures translate into lockout.
type Backoff struct {
	// Threshold is the number of failures before backoff begins. The first
	// Threshold failures are free, so a fat-fingered password is not a lockout.
	Threshold int

	// Base is the backoff after the first failure past the threshold; it
	// doubles per subsequent failure up to Max.
	Base time.Duration

	// Max caps the backoff.
	Max time.Duration

	// Forget is how long a failure record survives without new failures. It is
	// what makes the tracker's memory bounded in TIME as well as in size, and
	// what lets a locked-out user recover without operator involvement.
	Forget time.Duration
}

// DefaultBackoff is 5 free failures, then 1s doubling to 5m, forgotten after
// 15m of quiet.
func DefaultBackoff() Backoff {
	return Backoff{Threshold: 5, Base: time.Second, Max: 5 * time.Minute, Forget: 15 * time.Minute}
}

func (b Backoff) normalize() Backoff {
	if b.Threshold < 0 {
		b.Threshold = 0
	}
	if b.Base <= 0 {
		b.Base = time.Second
	}
	if b.Max < b.Base {
		b.Max = b.Base
	}
	if b.Forget <= 0 {
		b.Forget = 15 * time.Minute
	}
	return b
}

// delay is the backoff after n total failures.
func (b Backoff) delay(n int) time.Duration {
	over := n - b.Threshold
	if over <= 0 {
		return 0
	}
	// Shift rather than repeated multiplication, and cap the exponent before it
	// can overflow: 1<<63 nanoseconds is meaningless anyway, and an overflowed
	// duration going negative would read as "not locked".
	shift := min(over-1, 62)
	d := b.Base << shift
	if d <= 0 || d > b.Max {
		return b.Max
	}
	return d
}

// MemTracker is the bounded in-process [Tracker] (ADR-0001 §2.6b).
//
// # Why the bound matters
//
// Counters are keyed by attacker-supplied values — a claimed subject, a source
// address — so an unbounded map is a memory-exhaustion vector reachable WITHOUT
// authenticating. A defense that introduces a vulnerability is not a defense.
//
// # What the bound costs, stated plainly
//
// When the table is full, expired records are swept first; if that frees
// nothing, the least-recently-touched record is evicted. That means a flood of
// distinct keys can evict a real victim's counter and weaken per-subject
// lockout. The alternatives are worse: failing open on a full table removes
// lockout entirely, and failing closed lets an attacker lock out every user by
// filling the table. Eviction degrades one victim's protection; the per-ADDRESS
// counter still bites for a single-source flood, and a flood distributed widely
// enough to evict by address is a volume problem for the transport's rate limit,
// not something a counter can solve. Size the table for the deployment.
type MemTracker struct {
	mu      sync.Mutex
	records map[string]*failRecord
	backoff Backoff
	max     int
	now     func() time.Time
}

type failRecord struct {
	count    int
	last     time.Time
	until    time.Time // when backoff expires
	lastSeen time.Time // for eviction
}

// DefaultTrackerSize is the record cap when none is given.
const DefaultTrackerSize = 16384

// NewMemTracker builds a bounded tracker. A max of zero uses
// [DefaultTrackerSize]; a zero-value Backoff uses [DefaultBackoff].
func NewMemTracker(max int, b Backoff) *MemTracker {
	if max <= 0 {
		max = DefaultTrackerSize
	}
	if b == (Backoff{}) {
		b = DefaultBackoff()
	}
	return &MemTracker{records: make(map[string]*failRecord), backoff: b.normalize(), max: max, now: time.Now}
}

// Locked implements [Tracker].
func (m *MemTracker) Locked(key string, now time.Time) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[key]
	if !ok {
		return false, 0
	}
	if now.Sub(rec.last) >= m.backoff.Forget {
		// Expired: report unlocked, but leave the record for the sweep. Deleting
		// here would make a read mutate state, and Locked is documented not to.
		return false, 0
	}
	if now.Before(rec.until) {
		return true, rec.until.Sub(now)
	}
	return false, 0
}

// Fail implements [Tracker].
func (m *MemTracker) Fail(key string, now time.Time) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[key]
	if ok && now.Sub(rec.last) >= m.backoff.Forget {
		// Stale record: the previous run of failures is forgotten, so this
		// failure starts a new one rather than resuming an old lockout.
		rec.count = 0
		rec.until = time.Time{}
	}
	if !ok {
		if len(m.records) >= m.max {
			m.evictLocked(now)
		}
		rec = &failRecord{}
		m.records[key] = rec
	}
	rec.count++
	rec.last = now
	rec.lastSeen = now
	d := m.backoff.delay(rec.count)
	if d > 0 {
		rec.until = now.Add(d)
	}
	return d
}

// Reset implements [Tracker].
func (m *MemTracker) Reset(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, key)
}

// Len reports the number of tracked records, for tests and for a metric.
func (m *MemTracker) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

// evictLocked frees at least one slot. Caller holds the lock.
func (m *MemTracker) evictLocked(now time.Time) {
	// Sweep expired records first: they are dead weight, and clearing them
	// evicts nobody who is still being protected.
	for k, rec := range m.records {
		if now.Sub(rec.last) >= m.backoff.Forget {
			delete(m.records, k)
		}
	}
	if len(m.records) < m.max {
		return
	}
	// Still full: drop the least-recently-touched record. Dropping the OLDEST
	// rather than a random one keeps an active attack's counters alive, which is
	// the opposite of what a random eviction would do.
	var oldestKey string
	oldest := time.Unix(math.MaxInt32, 0)
	for k, rec := range m.records {
		if rec.lastSeen.Before(oldest) {
			oldest, oldestKey = rec.lastSeen, k
		}
	}
	if oldestKey != "" {
		delete(m.records, oldestKey)
	}
}

var _ Tracker = (*MemTracker)(nil)
