package web

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// upstream models a consumer's per-login state: a thing that must be logged out
// and closed, in that order.
type upstream struct {
	id           string
	loggedOut    bool
	closed       bool
	releaseCount int
	reason       HandoffReason
}

func ssoFor(t *testing.T, max int, ttl time.Duration) (*SSO[*upstream], *[]*upstream, func() time.Time, *time.Time) {
	t.Helper()
	var mu sync.Mutex
	var released []*upstream
	now := time.Now()
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	s, err := NewSSO(SSOConfig[*upstream]{
		Max: max, TTL: ttl, Clock: clock,
		Release: func(u *upstream, r HandoffReason) {
			mu.Lock()
			defer mu.Unlock()
			// The order a consumer must use: revoke, then close. Closing a
			// transport does not revoke a credential.
			u.loggedOut = true
			u.closed = true
			u.releaseCount++
			u.reason = r
			released = append(released, u)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, &released, clock, &now
}

// The whole point of the type: the dangerous state cannot be constructed.
func TestNewSSO_ReleaseIsRequired(t *testing.T) {
	t.Parallel()
	if _, err := NewSSO(SSOConfig[*upstream]{Max: 4}); err == nil {
		t.Fatal("a park with no Release was accepted — dropping entries without cleanup " +
			"is the leak this type exists to prevent, so it must not be constructible")
	}
	if _, err := NewSSO(SSOConfig[*upstream]{
		Release: func(*upstream, HandoffReason) {}, Max: -1,
	}); err == nil {
		t.Error("a negative Max was accepted")
	}
	if _, err := NewSSO(SSOConfig[*upstream]{
		Release: func(*upstream, HandoffReason) {}, TTL: -time.Second,
	}); err == nil {
		t.Error("a negative TTL was accepted")
	}
	// The documented shorthands work.
	s, err := NewSSO(SSOConfig[*upstream]{Release: func(*upstream, HandoffReason) {}})
	if err != nil {
		t.Fatal(err)
	}
	if s.max != DefaultMaxPendingLogins || s.ttl != DefaultHandoffTTL {
		t.Errorf("defaults not applied: max=%d ttl=%s", s.max, s.ttl)
	}
}

// Options must hand back BOTH hooks, because returning them separately is what
// lets a consumer wire the login side and forget the release side.
func TestSSO_OptionsWiresBothHooks(t *testing.T) {
	t.Parallel()
	s, _, _, _ := ssoFor(t, 4, time.Minute)
	hOpt, mOpt := s.Options()
	if hOpt == nil || mOpt == nil {
		t.Fatal("Options returned a nil hook")
	}

	var h Handler
	hOpt(&h)
	if h.onLogin == nil {
		t.Error("the handler option did not install OnLogin")
	}
	// The park's capacity IS the pending budget, so the two cannot drift.
	if h.maxPendingLogins != s.max {
		t.Errorf("MaxPendingLogins = %d, park Max = %d — they must not disagree",
			h.maxPendingLogins, s.max)
	}

	var m Manager
	mOpt(&m)
	if m.unused == nil {
		t.Error("the manager option did not install OnHandoffUnused")
	}
}

// Stash outside a login request must FAIL, not silently discard state the factor
// has already allocated.
func TestSSO_StashOutsideALoginRequestFails(t *testing.T) {
	t.Parallel()
	s, _, _, _ := ssoFor(t, 4, time.Minute)
	if err := s.Stash(context.Background(), &upstream{id: "a"}); err == nil {
		t.Error("Stash outside a login request succeeded, so the value would be dropped " +
			"and its upstream state leaked with no sign of it")
	}
	// Inside one it works.
	ctx := withStash(context.Background(), &Stash{})
	if err := s.Stash(ctx, &upstream{id: "a"}); err != nil {
		t.Errorf("Stash inside a login request: %v", err)
	}
}

// Claim is single-use, so two sessions cannot receive the same upstream state.
func TestSSO_ClaimIsSingleUse(t *testing.T) {
	t.Parallel()
	s, released, _, _ := ssoFor(t, 4, time.Minute)
	u := &upstream{id: "a"}
	if err := s.hold("h1", u); err != nil {
		t.Fatal(err)
	}

	got, ok := s.Claim(&SessionInfo{Handoff: "h1"})
	if !ok || got != u {
		t.Fatalf("Claim = %v %v", got, ok)
	}
	if _, ok := s.Claim(&SessionInfo{Handoff: "h1"}); ok {
		t.Error("a second Claim succeeded — two sessions would share one upstream session")
	}
	// A claimed entry is NOT released: the session owns it now, and releasing here
	// would log out a live session.
	if len(*released) != 0 {
		t.Errorf("a claimed entry was released: %v", *released)
	}
	// An attach with no login parks nothing, which is normal, not an error.
	if _, ok := s.Claim(&SessionInfo{Handoff: ""}); ok {
		t.Error("an attach with no handoff claimed something")
	}
	if _, ok := s.Claim(nil); ok {
		t.Error("a nil SessionInfo claimed something")
	}
}

func TestSSO_ReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	s, released, _, _ := ssoFor(t, 4, time.Minute)
	u := &upstream{id: "a"}
	if err := s.hold("h1", u); err != nil {
		t.Fatal(err)
	}
	s.Release("h1", ReattachedExisting)
	s.Release("h1", ReattachedExisting)
	s.Release("unknown", AttachFailed)

	if u.releaseCount != 1 {
		t.Errorf("released %d times, want exactly 1 — the hook firing twice must not "+
			"double-release", u.releaseCount)
	}
	if !u.loggedOut || !u.closed {
		t.Error("the consumer's cleanup did not run")
	}
	if u.reason != ReattachedExisting {
		t.Errorf("reason = %v", u.reason)
	}
	if len(*released) != 1 {
		t.Errorf("%d releases recorded", len(*released))
	}
}

// An abandoned login is only detectable by time, so expiry must clean up — and
// the sweep must not depend on the consumer remembering a ticker.
func TestSSO_ExpirySweeps(t *testing.T) {
	t.Parallel()
	s, released, _, now := ssoFor(t, 4, time.Minute)
	abandoned := &upstream{id: "abandoned"}
	if err := s.hold("h1", abandoned); err != nil {
		t.Fatal(err)
	}

	// Not yet expired.
	*now = now.Add(30 * time.Second)
	s.Sweep()
	if s.Len() != 1 {
		t.Fatal("an unexpired entry was swept")
	}

	*now = now.Add(2 * time.Minute)
	s.Sweep()
	if s.Len() != 0 {
		t.Fatal("an expired entry survived the sweep")
	}
	if abandoned.releaseCount != 1 || abandoned.reason != Expired {
		t.Errorf("expiry cleanup: count=%d reason=%v", abandoned.releaseCount, abandoned.reason)
	}
	_ = released
}

// A login also sweeps, so a consumer that never calls Sweep still cannot
// accumulate abandoned state indefinitely.
func TestSSO_HoldSweepsFirst(t *testing.T) {
	t.Parallel()
	s, _, _, now := ssoFor(t, 2, time.Minute)
	old1, old2 := &upstream{id: "1"}, &upstream{id: "2"}
	if err := s.hold("h1", old1); err != nil {
		t.Fatal(err)
	}
	if err := s.hold("h2", old2); err != nil {
		t.Fatal(err)
	}
	// The park is full. Without a sweep, the next login would be refused forever.
	*now = now.Add(2 * time.Minute)
	if err := s.hold("h3", &upstream{id: "3"}); err != nil {
		t.Fatalf("hold after expiry: %v — a full park of abandoned logins must not "+
			"block new ones until someone remembers to sweep", err)
	}
	if old1.releaseCount != 1 || old2.releaseCount != 1 {
		t.Errorf("expired entries were dropped without cleanup: %d %d",
			old1.releaseCount, old2.releaseCount)
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}
}

func TestSSO_CapacityIsEnforced(t *testing.T) {
	t.Parallel()
	s, _, _, _ := ssoFor(t, 2, time.Minute)
	if err := s.hold("h1", &upstream{id: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.hold("h2", &upstream{id: "2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.hold("h3", &upstream{id: "3"}); !errors.Is(err, ErrPendingLogins) {
		t.Errorf("err = %v, want ErrPendingLogins", err)
	}
	if err := s.hold("", &upstream{id: "x"}); err == nil {
		t.Error("an empty handoff was accepted")
	}
}

// Shutdown must not leave upstream state logged in.
func TestSSO_CloseReleasesEverything(t *testing.T) {
	t.Parallel()
	s, released, _, _ := ssoFor(t, 8, time.Minute)
	var held []*upstream
	for i := range 5 {
		u := &upstream{id: string(rune('a' + i))}
		held = append(held, u)
		if err := s.hold("h"+u.id, u); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if s.Len() != 0 {
		t.Errorf("%d entries survived Close", s.Len())
	}
	for _, u := range held {
		if u.releaseCount != 1 || !u.loggedOut {
			t.Errorf("%s was not cleaned up on Close", u.id)
		}
	}
	if len(*released) != 5 {
		t.Errorf("%d releases, want 5", len(*released))
	}
	// After Close, nothing new is accepted.
	if err := s.hold("late", &upstream{id: "late"}); !errors.Is(err, ErrStopped) {
		t.Errorf("hold after Close = %v, want ErrStopped", err)
	}
}

// The consumer's cleanup may make network calls, so it must not run under the
// park's lock — otherwise one slow logout blocks every other login.
func TestSSO_ReleaseDoesNotHoldTheLock(t *testing.T) {
	t.Parallel()
	blocked := make(chan struct{})
	proceed := make(chan struct{})
	s, err := NewSSO(SSOConfig[int]{
		Max: 4, TTL: time.Minute,
		Release: func(int, HandoffReason) {
			close(blocked)
			<-proceed
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.hold("h1", 1); err != nil {
		t.Fatal(err)
	}

	go s.Release("h1", AttachFailed)
	<-blocked
	// While the cleanup is blocked, other park operations must still work.
	done := make(chan error, 1)
	go func() { done <- s.hold("h2", 2) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("hold during a slow release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("a slow consumer cleanup blocked the park — one hung logout would stall " +
			"every other login")
	}
	close(proceed)
}

func TestHandoffReason_String(t *testing.T) {
	t.Parallel()
	for r, want := range map[HandoffReason]string{
		ReattachedExisting: "reattached-existing",
		AttachFailed:       "attach-failed",
		Expired:            "expired",
		HandoffReason(99):  "unknown",
	} {
		if got := r.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", r, got, want)
		}
	}
}
