package token

import (
	"sync"
	"time"
)

// MemStore is the bounded in-process Store: correct for a single-process
// deployment, and the default so nothing has to be configured to start.
//
// Consume holds one lock across fetch, validation and removal, so it is atomic
// by construction — exactly one concurrent redeemer of a single-use token can
// succeed.
type MemStore struct {
	mu   sync.Mutex
	recs map[Hash]Record
	max  int
}

// NewMemStore builds a store holding at most max live tokens (0 = 4096).
// Expired records are evicted lazily, and a Put that would exceed the cap first
// sweeps expired entries, then fails rather than growing without bound.
func NewMemStore(max int) *MemStore {
	if max <= 0 {
		max = 4096
	}
	return &MemStore{recs: make(map[Hash]Record), max: max}
}

// ErrFull is returned when the store is at capacity and nothing can be evicted.
var ErrFull = errStoreFull{}

type errStoreFull struct{}

func (errStoreFull) Error() string { return "token: store at capacity" }

func (s *MemStore) Put(h Hash, rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.recs) >= s.max {
		s.sweepLocked(time.Now())
		if len(s.recs) >= s.max {
			return ErrFull
		}
	}
	s.recs[h] = rec
	return nil
}

// Consume fetches, validates and — for a single-use record — removes, all under
// one lock. See Store.Consume for why that indivisibility matters.
func (s *MemStore) Consume(h Hash, now time.Time) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.recs[h]
	if !ok {
		return Record{}, ErrNotFound
	}
	if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
		delete(s.recs, h)
		return Record{}, ErrExpired
	}
	if rec.SingleUse {
		delete(s.recs, h)
	}
	return rec, nil
}

func (s *MemStore) Revoke(h Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recs, h)
	return nil
}

// Len reports the live record count, for tests and metrics.
func (s *MemStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.recs)
}

func (s *MemStore) sweepLocked(now time.Time) {
	for h, rec := range s.recs {
		if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
			delete(s.recs, h)
		}
	}
}
