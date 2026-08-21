package web

import (
	"sync"
	"time"
)

// Limits are the resource bounds of ADR-0009 §2.9. Every one has a concrete
// default and is configurable.
//
// They exist because every resource here is allocated on behalf of a remote
// party: the message buffer, the event queue, the CPU that decodes. Without
// bounds, how much the server spends is the client's decision.
type Limits struct {
	// MaxMessage is the largest accepted WebSocket message. On breach the
	// connection closes with 1009 Message Too Big.
	MaxMessage int64

	// EventsPerSecond is the sustained input rate.
	EventsPerSecond float64

	// Burst is a token-bucket CREDIT, not a promise that this many events are
	// absorbed. The event queue holds [Limits.QueueDepth]; anything beyond it
	// applies backpressure and may reach the overload close.
	Burst float64

	// QueueDepth is the un-coalesced event queue capacity.
	QueueDepth int

	// OverloadGrace is how long the queue may stay continuously full before the
	// connection is closed with 1008 Policy Violation. A brief burst is
	// tolerated; a sustained flood is not.
	OverloadGrace time.Duration
}

// Defaults from §2.9's table.
const (
	DefaultMaxMessage      = 64 << 10
	DefaultEventsPerSecond = 500
	DefaultBurst           = 2000
	DefaultQueueDepth      = 1024
	DefaultOverloadGrace   = 2 * time.Second
)

// DefaultLimits returns §2.9's defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxMessage:      DefaultMaxMessage,
		EventsPerSecond: DefaultEventsPerSecond,
		Burst:           DefaultBurst,
		QueueDepth:      DefaultQueueDepth,
		OverloadGrace:   DefaultOverloadGrace,
	}
}

// normalize fills zero fields with defaults and refuses nothing: a zero Limits
// is the documented way to ask for the defaults, and a caller who sets one field
// should not have to restate the rest.
func (l Limits) normalize() Limits {
	d := DefaultLimits()
	if l.MaxMessage <= 0 {
		l.MaxMessage = d.MaxMessage
	}
	if l.EventsPerSecond <= 0 {
		l.EventsPerSecond = d.EventsPerSecond
	}
	if l.Burst <= 0 {
		l.Burst = d.Burst
	}
	if l.QueueDepth <= 0 {
		l.QueueDepth = d.QueueDepth
	}
	if l.OverloadGrace <= 0 {
		l.OverloadGrace = d.OverloadGrace
	}
	return l
}

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

// clear records that the queue drained, resetting the timer.
func (o *overload) clear() { o.since = time.Time{} }
