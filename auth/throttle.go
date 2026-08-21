package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Tracker records authentication failures and decides whether a key is
// currently in backoff (ADR-0001 §2.6b).
//
// Every method MUST be atomic with respect to the others. The seam is an
// interface so the multi-replica case — a shared Redis or SQL counter — does not
// require an interface change later; [MemTracker] is the single-process default.
//
// Keys are opaque strings. [Throttle] namespaces them so a subject can never
// collide with an address.
type Tracker interface {
	// Locked reports whether key is currently in backoff and how much of it
	// remains. It must not mutate state.
	Locked(key string, now time.Time) (locked bool, retryAfter time.Duration)

	// Fail records one failure against key and returns the resulting backoff.
	Fail(key string, now time.Time) time.Duration

	// Reset clears key's failure record.
	Reset(key string)
}

// ErrThrottled is the internal reason recorded when an attempt is refused for
// backoff rather than for a bad credential. Like every other reason it collapses
// to [ErrUnauthenticated] at the Policy boundary — telling a caller "you are
// locked out" confirms the account exists.
var ErrThrottled = errors.New("auth: too many failed attempts")

// Throttle wraps a Factor with per-subject and per-source-address failure
// counting (ADR-0001 §2.6).
//
// # Why it runs the inner factor even when locked
//
// A locked attempt still calls the wrapped factor and discards the verdict.
// Short-circuiting would be the obvious implementation and it is wrong: the
// locked path would return in microseconds while a wrong password takes tens of
// milliseconds, so an attacker could detect lockout by timing — and since only
// an EXISTING account can be locked out, detecting lockout enumerates users.
// That would hand back the oracle the dummy hash was added to close.
//
// The cost is real and is stated rather than hidden: a locked account still
// consumes a full verification's work per attempt. This wrapper defends
// CREDENTIALS, not CPU. Bounding total unauthenticated request volume is the
// transport's job — a connection rate limit in front — and no amount of counter
// logic here substitutes for it.
type Throttle struct {
	inner    Factor
	tracker  Tracker
	subjectF func(*Request) string
	addrF    func(*Request) string
}

// ThrottleOption configures [NewThrottle].
type ThrottleOption func(*Throttle)

// SubjectKey sets how the attempted principal is read from a request. It is the
// CLAIMED subject — unverified by definition, since counting has to happen
// before the credential is checked.
//
// Defaults to the "subject" credential, falling back to "ssh-identity".
func SubjectKey(fn func(*Request) string) ThrottleOption {
	return func(t *Throttle) { t.subjectF = fn }
}

// AddressKey sets how the source address is read. Defaults to [Request.Peer]'s
// address, which is the transport's own view.
//
// A forwarded header must NEVER be used here without a trusted-proxy set: an
// attacker who picks their own counter key has no counter at all.
func AddressKey(fn func(*Request) string) ThrottleOption {
	return func(t *Throttle) { t.addrF = fn }
}

// NewThrottle wraps inner.
func NewThrottle(inner Factor, tracker Tracker, opts ...ThrottleOption) (*Throttle, error) {
	if inner == nil {
		return nil, errors.New("auth.NewThrottle: no factor to wrap")
	}
	if tracker == nil {
		return nil, errors.New("auth.NewThrottle: a Tracker is required — pass auth.NewMemTracker(...)")
	}
	t := &Throttle{inner: inner, tracker: tracker, subjectF: claimedSubject, addrF: peerAddress}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	if t.subjectF == nil || t.addrF == nil {
		return nil, errors.New("auth.NewThrottle: a nil key function would disable counting")
	}
	return t, nil
}

// Kind reports the wrapped factor's kind: throttling changes when a factor
// admits, never what it proves.
func (t *Throttle) Kind() FactorKind { return t.inner.Kind() }

// Verify counts the attempt and delegates.
//
// The sequence is FIXED and identical on every path — two locked reads, the
// inner verification, then two writes — so the known, unknown, wrong, locked and
// tracker-full cases perform the same operations in the same order. That
// uniformity is the security property; the counting is just what it is for.
func (t *Throttle) Verify(ctx context.Context, r *Request) (Contribution, error) {
	subjectKey := "s:" + t.subjectF(r)
	addrKey := "a:" + t.addrF(r)
	now := time.Now()

	// Both reads, always, in this order. Reading only the subject counter when
	// the subject is known would itself be an enumeration signal.
	subjectLocked, _ := t.tracker.Locked(subjectKey, now)
	addrLocked, _ := t.tracker.Locked(addrKey, now)
	locked := subjectLocked || addrLocked

	// Runs even when locked — see the type comment. The verdict is discarded in
	// that case, but the work is not.
	contribution, err := t.inner.Verify(ctx, r)

	if err != nil || locked {
		// Both writes, always. A locked attempt counts as a failure: if a
		// correct guess arriving during backoff reset the counter, the lockout
		// would be bypassable by simply retrying, which is the entire thing it
		// exists to prevent.
		t.tracker.Fail(subjectKey, now)
		t.tracker.Fail(addrKey, now)
		if locked {
			return Contribution{}, fmt.Errorf("%w", ErrThrottled)
		}
		return Contribution{}, err
	}
	t.tracker.Reset(subjectKey)
	t.tracker.Reset(addrKey)
	return contribution, nil
}

// claimedSubject reads the unverified subject claim from the usual credential
// keys.
func claimedSubject(r *Request) string {
	if r == nil {
		return ""
	}
	for _, k := range []string{"subject", "ssh-identity"} {
		if s, ok := r.Credentials[k]; ok && !s.IsZero() {
			return s.Reveal()
		}
	}
	// An attempt with no claim still gets counted, under one shared key. It is
	// not attributable, but it must not be free.
	return ""
}

// peerAddress uses the transport's own view of the client, never a header.
func peerAddress(r *Request) string {
	if r == nil {
		return ""
	}
	if !r.Peer.IsValid() {
		return ""
	}
	// The address without the port: a port changes per connection, so keying on
	// it would give an attacker a fresh counter for every attempt.
	return r.Peer.Addr().Unmap().String()
}

var _ Factor = (*Throttle)(nil)
