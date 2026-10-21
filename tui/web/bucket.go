package web

import (
	"sync"
	"time"
)

// The input limiters: a token bucket over the input rate, and the overload tracker.

// bucket is a token bucket over the input rate.
//
// It BACKPRESSURES rather than dropping: the Backend contract promises an
// ordered un-coalesced stream, so silently discarding an event would make this
// backend behave differently from every other one. A client that outruns the
// bucket waits, and a client that keeps outrunning it eventually trips the
// overload close.
type bucket struct {
	mu     sync.Mutex
	tokens float64
	rate   float64
	burst  float64
	last   time.Time
	now    func() time.Time
}

func newBucket(rate, burst float64, now func() time.Time) *bucket {
	if now == nil {
		now = time.Now
	}
	return &bucket{tokens: burst, rate: rate, burst: burst, last: now(), now: now}
}

// take consumes one token, reporting how long the caller must wait first. A zero
// duration means "proceed now".
func (b *bucket) take() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens += elapsed.Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return 0
	}
	// Time until one token exists. Computed rather than a fixed sleep so a slow
	// rate does not become a busy loop.
	need := 1 - b.tokens
	return time.Duration(need / b.rate * float64(time.Second))
}

// overload tracks how long the event queue has been continuously full.
//
// Continuously is the operative word: a client that fills the queue and then
// lets it drain is bursty, not abusive, and closing on the first full queue
// would punish a momentarily busy App loop rather than a flood.
type overload struct {
	since time.Time
	now   func() time.Time
	grace time.Duration
}

func newOverload(grace time.Duration, now func() time.Time) *overload {
	if now == nil {
		now = time.Now
	}
	return &overload{now: now, grace: grace}
}

// full records that the queue is full and reports whether the grace period has
// been exceeded.
func (o *overload) full() bool {
	now := o.now()
	if o.since.IsZero() {
		o.since = now
		return false
	}
	return now.Sub(o.since) >= o.grace
}
