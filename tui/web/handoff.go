package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"

	"github.com/yongjohnlee80/golib/auth"
)

// HandoffID is the opaque, single-claim correlation between one successful login
// and the session that login authorizes (ADR-0009 §2.12).
//
// It is DERIVED from the ticket rather than generated and stored, which buys
// three things:
//
//   - this package holds no state. A map from ticket to handoff would mean
//     keeping credential-derived keys at rest and expiring them correctly, a
//     second lifetime beside the token store's.
//   - only the ticket holder can compute it, so the value is useless to anyone
//     who did not already have the credential.
//   - it is domain-separated from the token store's own sha256(ticket) index. Two
//     hashes of one secret for two purposes must not be the same hash, or a leak
//     of one becomes a lookup key for the other.
func HandoffID(ticket string) string {
	if ticket == "" {
		return ""
	}
	sum := sha256.Sum256(append([]byte(handoffDomain), ticket...))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const handoffDomain = "webtui-handoff\x00"

// HandoffReason says why a handoff will never be claimed by an [AppFactory].
type HandoffReason uint8

const (
	// ReattachedExisting: the attach resumed an existing session, so no factory
	// ran. The commonest case and the one that leaked before §2.12.
	ReattachedExisting HandoffReason = iota

	// AttachFailed: authentication succeeded but the attach did not — the session
	// limit, an unusable hello, a transport error.
	AttachFailed

	// Expired, SessionEnded and LoginFailed are declared in sso.go, where the park
	// that produces them lives.
)

// String names the reason for a log line.
func (r HandoffReason) String() string {
	switch r {
	case ReattachedExisting:
		return "reattached-existing"
	case AttachFailed:
		return "attach-failed"
	case Expired:
		return "expired"
	case SessionEnded:
		return "session-ended"
	case LoginFailed:
		return "login-failed"
	}
	return "unknown"
}

// SessionInfo is what an [AppFactory] needs to build an App for a specific user.
//
// It exists because the factory previously received only a [Backend], so it could
// not know WHICH user the session was for — which made single sign-on
// unimplementable by any consumer (ADR-0009 §2.12).
type SessionInfo struct {
	// Identity is the authenticated principal. Never nil when a factory is
	// called: [Manager.Create] refuses without one.
	Identity *auth.Identity

	// Handoff is the [HandoffID] derived from the ticket this attach presented, or
	// "" when it presented none — an mTLS or SSH-challenge attach carries no
	// ticket at all.
	//
	// A NON-EMPTY handoff does not mean a login parked anything. Every presented
	// ticket derives one, including a ticket minted out of band by an SSH tool, so
	// the question "did a login park state for this session?" is answered by
	// LOOKING IN THE PARK, not by the handoff being set (lector r1 on PR #14).
	// [SSO.Session] is built that way: it claims, and provisions when the claim
	// misses.
	Handoff string

	// Peer is the transport's view of the client that created the session, or the
	// zero value when unknown. It is never a forwarded header: see §2.13.
	Peer string
}

// stashKey carries the per-request slot through the login's context.
type stashKey struct{}

// Stash is a per-REQUEST slot for state produced during credential
// verification.
//
// # Why request-scoped rather than keyed by subject
//
// A consumer's Verify holds the credential and is therefore the only place that
// can allocate upstream state — but the ticket, and so the handoff, is minted
// afterwards. Parking by subject in the meantime cannot separate two concurrent
// logins by one user: whichever [OnLogin] ran second would claim the other's
// session. One slot per request removes the shared key entirely, so concurrent
// same-user logins are deterministic.
//
// It is valid only for the duration of one login request. A consumer puts state
// in during Verify and moves it into its own park during OnLogin, where the
// handoff is known.
type Stash struct {
	mu      sync.Mutex
	value   any
	release func()
	taken   bool
}

// Set records state produced during verification. The last write wins; a factor
// that verifies more than once per request is doing something the login route
// does not ask for.
//
// Nothing cleans up a value set this way if the login later fails — use
// [SSO.Stash], which registers the cleanup at the same moment it records the
// value.
func (s *Stash) Set(v any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = v
}

// setOwned records a value TOGETHER with the cleanup that must run if it never
// reaches its destination.
//
// The two arrive together because every step between them is a place the login
// can fail: another factor in the policy can refuse after this one succeeded, the
// ticket can fail to mint, the park can be full. Each of those returns from the
// request having allocated an upstream session that nothing will ever close
// (lector r1 on PR #14 reproduced two of them).
//
// A second call is REFUSED rather than overwriting: the first value would become
// unreachable with its cleanup already registered, and last-write-wins is not a
// sensible answer when the thing being overwritten is a live connection. The
// caller owns the value it could not park.
func (s *Stash) setOwned(v any, release func()) error {
	if s == nil {
		return errors.New("web: no login request to stash into")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value != nil || s.taken {
		return errors.New("web: this login already stashed upstream state — the " +
			"second value cannot be parked and remains the caller's to release")
	}
	s.value, s.release = v, release
	return nil
}

// Take removes and returns the stashed value, leaving the slot empty. Taking
// twice yields nil the second time, which is what makes the move to a park
// single-claim even if a hook runs more than once.
//
// Taking also transfers OWNERSHIP: after a Take, [Stash.discard] does nothing, so
// whoever took the value is responsible for it.
func (s *Stash) Take() any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.value
	s.value, s.release = nil, nil
	if v != nil {
		s.taken = true
	}
	return v
}

// discard releases a value that was never taken. Called on every login path that
// returns without parking, and a no-op after a Take or a bare Set.
func (s *Stash) discard() {
	if s == nil {
		return
	}
	s.mu.Lock()
	v, rel := s.value, s.release
	s.value, s.release = nil, nil
	s.mu.Unlock()
	// Outside the lock: a consumer's cleanup may make network calls.
	if v != nil && rel != nil {
		rel()
	}
}

// claimed reports whether the stashed value was taken, which is what tells the
// login route that a handoff now owns an admission slot.
func (s *Stash) claimed() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taken
}

// StashFromContext returns the login request's slot, or nil outside one.
//
// A nil return is not an error to guard against with a panic: a factor may
// legitimately be used by a caller that has no per-login state, and [Stash]'s
// methods are nil-safe so such a factor needs no special case.
func StashFromContext(ctx context.Context) *Stash {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(stashKey{}).(*Stash)
	return s
}

// withStash attaches a fresh slot for one login request.
func withStash(ctx context.Context, s *Stash) context.Context {
	return context.WithValue(ctx, stashKey{}, s)
}
