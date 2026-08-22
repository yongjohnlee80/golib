package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"

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

	// Expired is declared in sso.go, where the park that produces it lives.
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

	// Handoff is the [HandoffID] of the login that created this session, or ""
	// when the attach carried no login — an mTLS or SSH-challenge attach
	// authenticates directly and parks nothing.
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
	value any
}

// Set records state produced during verification. The last write wins; a factor
// that verifies more than once per request is doing something the login route
// does not ask for.
func (s *Stash) Set(v any) {
	if s != nil {
		s.value = v
	}
}

// Take removes and returns the stashed value, leaving the slot empty. Taking
// twice yields nil the second time, which is what makes the move to a park
// single-claim even if a hook runs more than once.
func (s *Stash) Take() any {
	if s == nil {
		return nil
	}
	v := s.value
	s.value = nil
	return v
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
