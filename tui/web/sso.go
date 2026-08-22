package web

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

// SSO wires the whole login-handoff workflow of ADR-0009 §2.12, so a consumer
// supplies only the two things that are genuinely theirs: how to allocate
// upstream state, and how to release it.
//
// # Why this exists rather than only documentation
//
// The raw seam has four paths — park on login, claim on create, release on
// reattach, release on failed attach — plus an expiry sweep. Every one of them
// must be wired or upstream state leaks, and the commonest of them (reattach) is
// the one a consumer is least likely to think of, because nothing in the happy
// path exercises it. A protocol with four obligations described in prose is a
// protocol someone implements three-quarters of.
//
// So the obligations are structural here:
//
//   - [SSO.Options] returns BOTH hooks together. A consumer cannot wire the login
//     side and forget the release side, because they arrive as one value.
//   - Release is REQUIRED. [NewSSO] fails without it, so "allocated and never
//     cleaned up" is not a state this type can be constructed in.
//   - The expiry sweep is internal and calls Release, so a client that abandons a
//     login cannot leave state behind by the consumer forgetting a timer.
//   - The park's capacity also sets [MaxPendingLogins], so the two bounds cannot
//     drift into disagreeing about how many logins may be in flight.
//
// # The shape of a consumer
//
//	sso, err := web.NewSSO(web.SSOConfig[*myUpstream]{
//	    Max: 8, TTL: 30 * time.Second,
//	    Release: func(u *myUpstream, r web.HandoffReason) { u.Logout(); u.Close() },
//	})
//
//	// In the login factor's Verify — the only place holding the credential:
//	sso.Stash(ctx, upstream)
//
//	// In the AppFactory:
//	upstream, ok := sso.Claim(info)
//
//	// Wiring, both hooks at once:
//	hOpt, mOpt := sso.Options()
type SSO[T any] struct {
	ttl     time.Duration
	max     int
	release func(T, HandoffReason)
	now     func() time.Time

	mu     sync.Mutex
	parked map[string]parkedEntry[T]
	closed bool
}

type parkedEntry[T any] struct {
	value   T
	expires time.Time
}

// SSOConfig configures [NewSSO].
type SSOConfig[T any] struct {
	// Release cleans up parked state. REQUIRED.
	//
	// Called exactly once per parked value, on whichever path applies: a reattach,
	// a failed attach, or expiry. It is the consumer's teardown — typically a
	// logout followed by a close, in that order, because closing a transport does
	// not usually revoke a credential.
	//
	// It must not block for long: it runs on the attach path for the first two
	// reasons, and on the sweeper for the third.
	Release func(T, HandoffReason)

	// Max is the capacity of the park, and also becomes [MaxPendingLogins] so the
	// two cannot disagree. Zero uses [DefaultMaxPendingLogins].
	Max int

	// TTL bounds how long a parked value waits for an attach. Zero uses
	// [DefaultHandoffTTL].
	//
	// It exists because this package cannot know that a client walked away — it
	// never saw a connection — so an abandoned login is only detectable by time.
	TTL time.Duration

	// Clock overrides the time source, for tests.
	Clock func() time.Time
}

// DefaultHandoffTTL is how long a parked login waits for its attach.
//
// Short on purpose: a real client attaches within a round trip of receiving its
// ticket, and every second beyond that is a window in which allocated upstream
// state sits unused.
const DefaultHandoffTTL = 30 * time.Second

// Expired is the [HandoffReason] for a login that was never attached.
const Expired HandoffReason = 2

// NewSSO builds the handoff workflow.
//
// A nil Release is refused rather than defaulted to a no-op: a park whose entries
// are dropped without cleanup is precisely the leak this type exists to prevent,
// and defaulting would make the dangerous case the quiet one.
func NewSSO[T any](cfg SSOConfig[T]) (*SSO[T], error) {
	if cfg.Release == nil {
		return nil, errors.New("web.NewSSO: Release is required — parked state that is " +
			"dropped without cleanup is the leak this type exists to prevent")
	}
	if cfg.Max < 0 {
		return nil, fmt.Errorf("web.NewSSO: negative Max %d", cfg.Max)
	}
	if cfg.TTL < 0 {
		return nil, fmt.Errorf("web.NewSSO: negative TTL %s", cfg.TTL)
	}
	s := &SSO[T]{
		ttl:     cfg.TTL,
		max:     cfg.Max,
		release: cfg.Release,
		now:     cfg.Clock,
		parked:  make(map[string]parkedEntry[T]),
	}
	if s.ttl == 0 {
		s.ttl = DefaultHandoffTTL
	}
	if s.max == 0 {
		s.max = DefaultMaxPendingLogins
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s, nil
}

// Stash records state produced during credential verification.
//
// Call it from the login factor's Verify, which is the only place holding the
// credential. The value moves into the park when the ticket is minted; a consumer
// never touches the handoff itself.
//
// Returns an error outside a login request, so a factor used in the wrong place
// fails loudly instead of silently discarding the state it just allocated.
func (s *SSO[T]) Stash(ctx context.Context, v T) error {
	slot := StashFromContext(ctx)
	if slot == nil {
		return errors.New("web: Stash called outside a login request — the value would " +
			"be discarded and its upstream state leaked")
	}
	slot.Set(v)
	return nil
}

// Claim takes the state parked by the login that created this session.
//
// Call it from the [AppFactory]. ok is false when the attach carried no login —
// an mTLS or SSH-challenge attach parks nothing — which is a normal case a
// consumer must handle rather than an error.
func (s *SSO[T]) Claim(info *SessionInfo) (T, bool) {
	var zero T
	if info == nil || info.Handoff == "" {
		return zero, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.parked[info.Handoff]
	if !ok {
		return zero, false
	}
	// Single-claim: removed as it is handed over, so a repeated call cannot give
	// two sessions the same upstream state.
	delete(s.parked, info.Handoff)
	return e.value, true
}

// Options returns the handler and manager wiring, together.
//
// Together on purpose. Returning them separately would let a consumer wire the
// login side and forget the release side, which is the leak of §2.12 with extra
// steps.
func (s *SSO[T]) Options() (HandlerOption, ManagerOption) {
	hOpt := func(h *Handler) {
		OnLogin(func(handoff string, _ *auth.Identity, slot *Stash) error {
			v, ok := slot.Take().(T)
			if !ok {
				// The factor did not stash, or stashed the wrong type. Failing the
				// login is correct: a ticket for state that does not exist is a
				// credential for nothing.
				return errors.New("web: no upstream state was stashed during verification")
			}
			return s.hold(handoff, v)
		})(h)
		// The park's capacity IS the pending-login budget, so the two cannot
		// drift into disagreeing.
		MaxPendingLogins(s.max)(h)
	}
	mOpt := OnHandoffUnused(func(handoff string, r HandoffReason) {
		s.Release(handoff, r)
	})
	return hOpt, mOpt
}

// hold parks a value, sweeping expired entries first so an abandoned login cannot
// occupy capacity indefinitely.
func (s *SSO[T]) hold(handoff string, v T) error {
	if handoff == "" {
		return errors.New("web: empty handoff")
	}
	now := s.now()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStopped
	}
	stale := s.expiredLocked(now)
	if len(s.parked) >= s.max {
		s.mu.Unlock()
		s.releaseAll(stale, Expired)
		return ErrPendingLogins
	}
	s.parked[handoff] = parkedEntry[T]{value: v, expires: now.Add(s.ttl)}
	s.mu.Unlock()
	// Released outside the lock: a consumer's cleanup may make network calls, and
	// holding the park's lock across one would block every other login.
	s.releaseAll(stale, Expired)
	return nil
}

// Release cleans up one parked entry. Idempotent: an entry already claimed or
// released is a no-op, so the hook firing twice cannot double-release.
func (s *SSO[T]) Release(handoff string, r HandoffReason) {
	s.mu.Lock()
	e, ok := s.parked[handoff]
	if ok {
		delete(s.parked, handoff)
	}
	s.mu.Unlock()
	if ok {
		s.release(e.value, r)
	}
}

// Sweep releases expired entries. Call it periodically, or rely on the sweep that
// [SSO.Options]'s login path performs; a consumer with long idle periods between
// logins should call it from a ticker so abandoned state does not wait for the
// next login to be noticed.
func (s *SSO[T]) Sweep() {
	now := s.now()
	s.mu.Lock()
	stale := s.expiredLocked(now)
	s.mu.Unlock()
	s.releaseAll(stale, Expired)
}

// Close releases every parked entry. For process shutdown.
func (s *SSO[T]) Close() {
	s.mu.Lock()
	s.closed = true
	all := make([]T, 0, len(s.parked))
	for h, e := range s.parked {
		all = append(all, e.value)
		delete(s.parked, h)
	}
	s.mu.Unlock()
	s.releaseAll(all, Expired)
}

// Len reports how many entries are parked, for tests and a metric.
func (s *SSO[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.parked)
}

// expiredLocked removes and returns expired values. Caller holds the lock.
func (s *SSO[T]) expiredLocked(now time.Time) []T {
	var stale []T
	for h, e := range s.parked {
		if now.After(e.expires) {
			stale = append(stale, e.value)
			delete(s.parked, h)
		}
	}
	return stale
}

func (s *SSO[T]) releaseAll(vals []T, r HandoffReason) {
	for _, v := range vals {
		s.release(v, r)
	}
}
