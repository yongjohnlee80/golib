package auth

import (
	"context"
	"fmt"
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

// validate rejects an unusable configuration.
//
// It does NOT normalize. Silently repairing a security parameter means an
// operator who typo'd their lockout threshold gets a working system with
// different behavior than they configured, and never finds out.
func (b Backoff) validate() error {
	switch {
	case b.Threshold < 0:
		return fmt.Errorf("negative Threshold %d", b.Threshold)
	case b.Base <= 0:
		return fmt.Errorf("Base must be positive, got %s", b.Base)
	case b.Max < b.Base:
		return fmt.Errorf("Max %s is below Base %s", b.Max, b.Base)
	case b.Forget <= 0:
		return fmt.Errorf("Forget must be positive, got %s", b.Forget)
	}
	return nil
}

// delay is the backoff after n total failures.
//
// It saturates MONOTONICALLY at Max. Testing the product for overflow after the
// fact is not enough: a large Base shifted far enough wraps to a small POSITIVE
// duration that is legitimately below Max, so the backoff would reach Max and
// then start coming back down — the most persistent attacker getting the
// shortest wait. The comparison is therefore done on the exponent, before any
// shift happens.
func (b Backoff) delay(n int) time.Duration {
	over := n - b.Threshold
	if over <= 0 {
		return 0
	}
	shift := over - 1
	if shift >= 63 {
		return b.Max
	}
	// b.Max>>shift cannot overflow, and b.Base <= that implies
	// b.Base<<shift <= b.Max, so the shift below is safe by construction.
	if b.Base > b.Max>>shift {
		return b.Max
	}
	return b.Base << shift
}

// MemTracker is the bounded in-process [Tracker] (ADR-0001 §2.6b).
//
// # Why the bound matters
//
// Counters are keyed by attacker-supplied values — a claimed subject, a source
// address — so an unbounded map is a memory-exhaustion vector reachable WITHOUT
// authenticating. A defense that introduces a vulnerability is not a defense.
// [Throttle] hashes its keys to a fixed width, so an entry cap here bounds
// memory rather than merely bounding a count of unbounded strings.
//
// # Eviction is bounded work, not a full scan
//
// Making room samples a small fixed number of records, drops any expired ones it
// sees, and otherwise evicts the least-recently-touched of the sample. It is
// O(sample), not O(cap). A full scan per insert would be O(cap) work an attacker
// triggers with every miss — at a 16k cap that measured three orders of
// magnitude slower than a normal insert, which is both a DoS amplifier and a
// timing signal separating "new key at capacity" from every other path.
// Sampling costs approximation: the record evicted is the oldest of a sample,
// not the oldest overall.
//
// # What the bound costs, stated plainly
//
// A flood of distinct keys can still evict a real victim's counter and weaken
// per-subject lockout. The alternatives are worse: failing open on a full table
// removes lockout entirely, and failing closed lets an attacker lock out every
// user by filling the table. Eviction degrades one victim's protection; the
// per-ADDRESS counter still bites for a single-source flood, and a flood
// distributed widely enough to evict by address is a volume problem for the
// transport's rate limit, not something a counter can solve. Size the table for
// the deployment.
type MemTracker struct {
	mu      sync.Mutex
	records map[string]*failRecord
	backoff Backoff
	max     int
}

type failRecord struct {
	count    int
	last     time.Time // last failure, for Forget
	until    time.Time // when backoff expires
	lastSeen time.Time // for eviction
}

// DefaultTrackerSize is the record cap when none is given.
const DefaultTrackerSize = 16384

// evictionSample is how many records making-room may examine.
//
// Go randomizes map iteration order, so the first N entries of a range are an
// effectively random sample — which is what makes "oldest of a sample" a usable
// approximation of "oldest overall" without an O(cap) scan or a second index.
const evictionSample = 8

// NewMemTracker builds a bounded tracker.
//
// A max of zero means [DefaultTrackerSize] and a zero-value Backoff means
// [DefaultBackoff]; anything else invalid is an ERROR rather than a silent
// repair, because these are security parameters.
func NewMemTracker(max int, b Backoff) (*MemTracker, error) {
	if max < 0 {
		return nil, fmt.Errorf("auth.NewMemTracker: negative size %d", max)
	}
	if max == 0 {
		max = DefaultTrackerSize
	}
	if b == (Backoff{}) {
		b = DefaultBackoff()
	}
	if err := b.validate(); err != nil {
		return nil, fmt.Errorf("auth.NewMemTracker: %w", err)
	}
	return &MemTracker{records: make(map[string]*failRecord), backoff: b, max: max}, nil
}

// Locked implements [Tracker]. It never fails and never mutates state.
func (m *MemTracker) Locked(_ context.Context, key string, now time.Time) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[key]
	if !ok {
		return false, 0, nil
	}
	if now.Sub(rec.last) >= m.backoff.Forget {
		// Expired: report unlocked, but leave the record for eviction to sweep.
		// Deleting here would make a read mutate state.
		return false, 0, nil
	}
	if now.Before(rec.until) {
		return true, rec.until.Sub(now), nil
	}
	return false, 0, nil
}

// Fail implements [Tracker]. It never fails.
func (m *MemTracker) Fail(_ context.Context, key string, now time.Time) (time.Duration, error) {
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
			m.makeRoomLocked(now)
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
	return d, nil
}

// Reset implements [Tracker]. It never fails.
func (m *MemTracker) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, key)
	return nil
}

// Len reports the number of tracked records, for tests and for a metric.
func (m *MemTracker) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

// makeRoomLocked frees at least one slot using bounded work. Caller holds the
// lock.
func (m *MemTracker) makeRoomLocked(now time.Time) {
	var (
		oldestKey  string
		oldestSeen time.Time
		haveOldest bool
		freed      int
		examined   int
	)
	for k, rec := range m.records {
		if now.Sub(rec.last) >= m.backoff.Forget {
			// Expired records are dead weight; dropping them evicts nobody who
			// is still being protected.
			delete(m.records, k)
			freed++
		} else if !haveOldest || rec.lastSeen.Before(oldestSeen) {
			// An explicit "have we seen one yet" flag, NOT an absolute-time
			// sentinel. A sentinel like time.Unix(math.MaxInt32, 0) selects
			// nothing once real records are dated past 2038, so the cap silently
			// stops being enforced.
			oldestKey, oldestSeen, haveOldest = k, rec.lastSeen, true
		}
		examined++
		if examined >= evictionSample {
			break
		}
	}
	if freed > 0 {
		return
	}
	if haveOldest {
		delete(m.records, oldestKey)
	}
}

var _ Tracker = (*MemTracker)(nil)
