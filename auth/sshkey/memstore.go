package sshkey

import (
	"sync"
	"time"
)

// MemStore is the bounded in-process ChallengeStore. Consume holds one lock
// across fetch, validation and removal, so a nonce cannot be redeemed twice
// even by simultaneous attempts.
type MemStore struct {
	mu   sync.Mutex
	recs map[string]ChallengeRecord
	max  int
}

// NewMemStore builds a store holding at most max outstanding challenges
// (0 = 1024). At capacity it sweeps expired entries first and then refuses,
// rather than growing — an unauthenticated caller can ask for challenges, so an
// unbounded store would be a memory-exhaustion vector reachable before any
// authentication.
func NewMemStore(max int) *MemStore {
	if max <= 0 {
		max = 1024
	}
	return &MemStore{recs: make(map[string]ChallengeRecord), max: max}
}

// ErrFull is returned when the store is at capacity with nothing to evict.
var ErrFull = errFull{}

type errFull struct{}

func (errFull) Error() string { return "sshkey: challenge store at capacity" }

func (s *MemStore) Put(id string, rec ChallengeRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.recs) >= s.max {
		s.sweepLocked(time.Now())
		if len(s.recs) >= s.max {
			return ErrFull
		}
	}
	s.recs[id] = rec
	return nil
}

// Consume fetches, validates and removes under one lock — atomic by
// construction, and unconditional: a challenge is single-use whether or not the
// signature that follows turns out to be valid, so a failed attempt cannot be
// retried against the same nonce.
func (s *MemStore) Consume(id string, now time.Time) (ChallengeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.recs[id]
	if !ok {
		return ChallengeRecord{}, ErrChallengeUnknown
	}
	delete(s.recs, id)
	if !rec.Expires.IsZero() && !rec.Expires.After(now) {
		return ChallengeRecord{}, ErrChallengeExpired
	}
	return rec, nil
}

// Len reports the outstanding challenge count.
func (s *MemStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.recs)
}

func (s *MemStore) sweepLocked(now time.Time) {
	for id, rec := range s.recs {
		if !rec.Expires.IsZero() && !rec.Expires.After(now) {
			delete(s.recs, id)
		}
	}
}
