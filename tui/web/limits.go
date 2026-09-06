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

	// MaxPending caps connections that have been accepted but not yet
	// authenticated.
	//
	// MaxSessions bounds only what exists AFTER a successful hello, so without
	// this a responsive non-browser that forges Host and Origin can hold
	// arbitrarily many sockets and goroutines while consuming no session slot at
	// all. This is the bound on the unauthenticated waiting room.
	MaxPending int

	// HelloTimeout bounds how long a connection may sit before its first
	// message.
	//
	// Without it a client that connects and says nothing is held forever: the
	// read simply blocks. A pre-auth connection has to prove it is going
	// somewhere, quickly.
	HelloTimeout time.Duration
}

// Defaults from §2.9's table.
const (
	DefaultMaxMessage      = 64 << 10
	DefaultEventsPerSecond = 500
	DefaultBurst           = 2000
	DefaultQueueDepth      = 1024
	DefaultOverloadGrace   = 2 * time.Second

	// DefaultMaxPending is generous relative to DefaultMaxSessions: a burst of
	// legitimate reconnects after a network blip should not be refused, while an
	// unbounded waiting room should not exist.
	DefaultMaxPending = 64

	// DefaultHelloTimeout is short because a real client sends its hello in the
	// same tick it opens the socket.
	DefaultHelloTimeout = 10 * time.Second
)

// DefaultLimits returns §2.9's defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxMessage:      DefaultMaxMessage,
		EventsPerSecond: DefaultEventsPerSecond,
		Burst:           DefaultBurst,
		QueueDepth:      DefaultQueueDepth,
		OverloadGrace:   DefaultOverloadGrace,
		MaxPending:      DefaultMaxPending,
		HelloTimeout:    DefaultHelloTimeout,
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
	if l.MaxPending <= 0 {
		l.MaxPending = d.MaxPending
	}
	if l.HelloTimeout <= 0 {
		l.HelloTimeout = d.HelloTimeout
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

// gate is a counting semaphore bounding unauthenticated connections.
//
// It is deliberately non-blocking: a connection that cannot get a slot is
// REFUSED rather than queued. Queueing would move the unbounded waiting room one
// level down instead of removing it, and a client that has to wait to be let in
// has no way to tell that from a hung server.
// A slot is either ANONYMOUS — held for the duration of one request — or KEYED,
// held by a parked login handoff that outlives its request. A keyed slot is
// returned when its handoff settles, and expires on its own if it never does: a
// login whose ticket is never presented cannot be settled by anyone, so without an
// expiry the slot would be lost for the process's lifetime (lector r1 on PR #14
// reproduced exactly that — the ninth login 503'd forever).
type gate struct {
	mu   sync.Mutex
	n    int
	max  int
	held map[string]slot // handoff -> the slot it owns
	now  func() time.Time
}

// slot is one keyed admission lease.
//
// committed distinguishes a RESERVATION — taken before the login hook runs, when
// nothing is parked yet — from a slot that actually accounts for a parked entry.
// The distinction is what tells the login route whether to return the slot when the
// hook is done: a hook that parked committed, and one that did not did not. Asking
// the stash whether its value was taken was the wrong question, because a
// hand-rolled park need not take anything from the stash to park (lector r5 found
// the docs gap; the test for it found this).
type slot struct {
	expires   time.Time
	committed bool
}

func newGate(max int) *gate {
	return &gate{max: max, held: make(map[string]slot), now: time.Now}
}

// enter takes an anonymous slot, reporting whether one was available.
func (g *gate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweepLocked()
	if g.n >= g.max {
		return false
	}
	g.n++
	return true
}

// leave returns an anonymous slot. Safe to call once per successful enter.
func (g *gate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.n > 0 {
		g.n--
	}
}

// hold converts this request's anonymous slot into one keyed by handoff, so the
// caller must NOT also call leave.
//
// It reports whether the transfer happened: an empty key, or a key already
// holding a slot, leaves the caller owning an anonymous slot to return.
func (g *gate) hold(key string, until time.Time) bool {
	if key == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, dup := g.held[key]; dup {
		return false
	}
	g.held[key] = slot{expires: until}
	return true
}

// commit re-stamps a keyed slot's deadline and reports whether the slot is STILL
// THERE.
//
// It exists because a reservation and the park entry it accounts for are created
// at two different moments, by two different components, on two different clocks.
// The reservation goes in before the consumer's hook runs; the park entry appears
// inside it. A hook slow enough to outlive its reservation — a real dial to an
// upstream is exactly that — came back to find its slot swept, and parked anyway:
// an entry with no accounting, so the budget under-counted and more logins could
// park than the cap allows (lector r4 on PR #14, probe: parked=1, held=0).
//
// So parking is now conditional on this returning true, which makes the pair
// atomic in the only sense that matters: the entry exists only if a slot accounts
// for it. false means the caller must not park — or must undo the park it cannot
// account for.
// The publish callback is why this takes one: returning a bool and letting the
// caller mutate afterwards is still a time-of-check-to-time-of-use gap, because
// this lock is released as the function returns and the key can be swept in
// between (lector r5 on PR #14 reproduced exactly that after the first repair).
// The callback runs while g.mu is STILL HELD, so membership and publication are
// indivisible.
//
// It is called with both locks held, in the established s.mu -> g.mu order — the
// caller holds s.mu, this holds g.mu — so the callback must only touch state the
// caller already protects, and must not block.
func (g *gate) commit(key string, until time.Time, publish func()) bool {
	if key == "" || publish == nil {
		// A nil publisher would mark the slot as accounting for an entry nobody
		// wrote: a slot with no entry, the mirror of the defect this prevents.
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweepLocked()
	held, ok := g.held[key]
	if !ok {
		return false
	}
	if held.committed {
		// Defensive: one reservation accounts for one entry, so a second commit
		// would either publish a second entry against one slot or re-stamp a slot
		// whose entry is already live. Callers are single-use; this is the backstop
		// for a caller that is not.
		return false
	}
	// Re-stamped from NOW, at the same moment the park's own deadline is set, so
	// the two cannot be skewed by however long the hook took.
	g.held[key] = slot{expires: until, committed: true}
	// A PANICKING publish keeps the commitment.
	//
	// The earlier version rolled it back on the reasoning that nothing had been
	// written — which is only true for a callback that panics before it mutates.
	// A consumer's callback can insert into its own park and then panic, and
	// undoing that is not something this package can do: the mutation is in the
	// consumer's data structure (lector r7 on PR #14 reproduced parked=1,
	// committed=0 that way).
	//
	// So the accounting is preserved CONSERVATIVELY. The two possible errors are
	// not symmetric: keeping a commitment for an entry that was never written
	// leaves a slot held until the backstop expiry reclaims it, while dropping one
	// for an entry that WAS written breaks the cap invariant outright and stays
	// broken. The bounded error is the one to take.
	publish()
	return true
}

// committed reports whether this key's slot now accounts for a parked entry.
func (g *gate) committed(key string) bool {
	if key == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.held[key]
	return ok && s.committed
}

// release returns the slot keyed by handoff. Idempotent, because a handoff can be
// settled by a claim, a release or an expiry and those paths do not coordinate.
func (g *gate) release(key string) {
	if key == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.held[key]; !ok {
		return
	}
	delete(g.held, key)
	if g.n > 0 {
		g.n--
	}
}

// sweepLocked drops keyed slots whose handoff can no longer be claimed. Lazy on
// purpose: the only moment the occupancy matters is when someone wants in, so a
// timer would buy nothing a check at the door does not.
func (g *gate) sweepLocked() {
	if len(g.held) == 0 {
		return
	}
	now := g.now()
	for k, s := range g.held {
		if now.After(s.expires) {
			delete(g.held, k)
			if g.n > 0 {
				g.n--
			}
		}
	}
}

// pending reports the current occupancy, for tests and a metric.
func (g *gate) pending() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweepLocked()
	return g.n
}
