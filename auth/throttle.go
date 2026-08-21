package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// Tracker records authentication failures and decides whether a key is
// currently in backoff (ADR-0001 §2.6b).
//
// Every method MUST be atomic with respect to the others. The seam is an
// interface so the multi-replica case — a shared Redis or SQL counter — does not
// require an interface change later, which is why every method takes a context
// and returns an error: a network-backed tracker has to honor the caller's
// deadline and has to be able to say "I could not answer". [MemTracker] is the
// single-process default.
//
// Keys are opaque fixed-width strings produced by [Throttle]; an implementation
// must treat them as bytes and must not parse them.
type Tracker interface {
	// Locked reports whether key is currently in backoff and how much of it
	// remains. It must not mutate state.
	Locked(ctx context.Context, key string, now time.Time) (locked bool, retryAfter time.Duration, err error)

	// Fail records one failure against key and returns the resulting backoff.
	Fail(ctx context.Context, key string, now time.Time) (time.Duration, error)

	// Reset clears key's failure record.
	Reset(ctx context.Context, key string) error
}

// Claimant is a [Factor] that can name the principal an attempt is FOR before
// verifying anything.
//
// Per-subject lockout is only possible for a factor that has this: the counter
// has to be keyed before the credential is checked. A password factor can do it
// (the subject is presented alongside the password); an opaque bearer token
// cannot, because the credential *is* the identity and nothing names a principal
// until it verifies.
type Claimant interface {
	Factor

	// Claim returns the UNVERIFIED principal this request targets, or "" when
	// the request names none. It must not have side effects and must not
	// verify anything.
	Claim(*Request) string
}

var (
	// ErrThrottled is the internal reason recorded when an attempt is refused
	// for backoff rather than for a bad credential. Like every other reason it
	// collapses to [ErrUnauthenticated] at the Policy boundary — telling a
	// caller "you are locked out" confirms the account exists.
	ErrThrottled = Reason("auth: too many failed attempts")

	// ErrTrackerUnavailable is recorded when the Tracker itself failed. By
	// default it denies: see [FailOpen].
	ErrTrackerUnavailable = Reason("auth: failure tracker unavailable")
)

// Throttle wraps a Factor with failure counting and backoff (ADR-0001 §2.6).
//
// # Two modes, because one contract cannot cover both
//
// A factor that implements [Claimant] is throttled per-subject AND
// per-source-address. A factor that cannot name a principal before verifying —
// an opaque token, a client certificate whose subject only exists after chain
// validation — must be wrapped with [AddressOnly]. There is no third option:
// silently falling back to a shared "no subject" key would put every such client
// on ONE global counter, so any one attacker could lock out everybody while
// their own address counter stayed clean.
//
// # Why a locked attempt still does the work, in subject mode
//
// In subject mode a locked attempt still calls the wrapped factor and discards
// the verdict. Short-circuiting would be the obvious implementation and it is
// wrong: the locked path would return in microseconds while a wrong password
// takes tens of milliseconds, so an attacker could detect lockout by timing —
// and since only an EXISTING account can be locked out, detecting lockout
// enumerates users. That would hand back the oracle the dummy hash was added to
// close.
//
// The cost is real and is stated rather than hidden: a locked account still
// consumes a full verification's work per attempt. This wrapper defends
// CREDENTIALS, not CPU. Bounding total unauthenticated request volume is the
// transport's job — a connection rate limit in front — and no amount of counter
// logic here substitutes for it.
//
// # Why address-only mode does NOT do the work
//
// It short-circuits instead, for two reasons that both point the same way. There
// is no principal to enumerate — the credential is the identity — and an address
// learning about its own backoff reveals nothing it did not already know. And
// short-circuiting is REQUIRED for correctness here: a single-use token factor
// consumes its credential atomically on presentation, so running it on a denied
// attempt would destroy a valid ticket and throw the proof away.
//
// # Permitted side effects of a wrapped Verify
//
// In subject mode the wrapped factor's Verify MAY consume state the client
// presented in THIS attempt (an SSH challenge nonce is spent on presentation by
// design). It MUST NOT consume a credential the client holds across attempts — a
// bearer token, a ticket. Such a factor belongs in [AddressOnly] mode.
type Throttle struct {
	inner      Factor
	tracker    Tracker
	claim      func(*Request) string // nil means address-only mode
	addr       func(*Request) string
	failOpen   bool
	onTrackErr func(error)
}

// ThrottleOption configures [NewThrottle].
type ThrottleOption func(*throttleConfig)

type throttleConfig struct {
	claim       func(*Request) string
	addr        func(*Request) string
	addressOnly bool
	failOpen    bool
	onTrackErr  func(error)
}

// SubjectClaim sets how the attempted principal is read from a request,
// overriding a [Claimant] implementation. It is the CLAIMED subject —
// unverified by definition, since counting has to happen before the credential
// is checked.
func SubjectClaim(fn func(*Request) string) ThrottleOption {
	return func(c *throttleConfig) { c.claim = fn }
}

// AddressOnly throttles by source address alone.
//
// Required for a factor that cannot name a principal before verifying, and it
// must be chosen EXPLICITLY: per-subject lockout silently degrading to a shared
// global counter is the failure this option exists to make impossible.
func AddressOnly() ThrottleOption {
	return func(c *throttleConfig) { c.addressOnly = true }
}

// AddressKey sets how the source address is read. Defaults to [Request.Peer]'s
// address, which is the transport's own view.
//
// A forwarded header must NEVER be used here without a trusted-proxy set: an
// attacker who picks their own counter key has no counter at all.
func AddressKey(fn func(*Request) string) ThrottleOption {
	return func(c *throttleConfig) { c.addr = fn }
}

// FailOpen admits attempts when the Tracker itself fails.
//
// The default is fail-CLOSED: a tracker outage denies. That is the deny-by-
// default house rule, and it is the safe side — a tracker that cannot answer
// cannot protect anything, so continuing without it means running with
// brute-force protection silently switched off.
//
// Choosing FailOpen trades that for availability, and is legitimate when a
// shared tracker's outage would otherwise take down every login. It is a
// deliberate decision, never a default.
func FailOpen() ThrottleOption {
	return func(c *throttleConfig) { c.failOpen = true }
}

// OnTrackerError reports a Tracker failure to the operator. The caller still
// receives a uniform rejection; without this hook a tracker outage is invisible.
func OnTrackerError(fn func(error)) ThrottleOption {
	return func(c *throttleConfig) { c.onTrackErr = fn }
}

// NewThrottle wraps inner.
//
// The subject-key source must be DETERMINED, not guessed: inner implements
// [Claimant], or [SubjectClaim] is supplied, or [AddressOnly] is chosen. Anything
// else is a construction error naming the three options.
func NewThrottle(inner Factor, tracker Tracker, opts ...ThrottleOption) (*Throttle, error) {
	if inner == nil {
		return nil, Reason("auth.NewThrottle: no factor to wrap")
	}
	if tracker == nil {
		return nil, Reason("auth.NewThrottle: a Tracker is required — pass auth.NewMemTracker(...)")
	}
	var c throttleConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	t := &Throttle{inner: inner, tracker: tracker, failOpen: c.failOpen, onTrackErr: c.onTrackErr}

	t.addr = c.addr
	if t.addr == nil {
		t.addr = peerAddress
	}

	switch {
	case c.addressOnly:
		if c.claim != nil {
			return nil, Reason("auth.NewThrottle: AddressOnly and SubjectClaim are contradictory")
		}
		t.claim = nil
	case c.claim != nil:
		t.claim = c.claim
	default:
		claimant, ok := inner.(Claimant)
		if !ok {
			return nil, fmt.Errorf("auth.NewThrottle: %T cannot name a principal before "+
				"verifying, so per-subject lockout is impossible for it. Choose one: "+
				"implement auth.Claimant, pass auth.SubjectClaim(fn), or pass "+
				"auth.AddressOnly(). Defaulting would put every client of this factor "+
				"on one shared counter", inner)
		}
		t.claim = claimant.Claim
	}
	return t, nil
}

// Kind reports the wrapped factor's kind: throttling changes when a factor
// admits, never what it proves.
func (t *Throttle) Kind() FactorKind { return t.inner.Kind() }

// Verify counts the attempt and delegates.
//
// Within a mode the sequence is FIXED and identical on every path, so the known,
// unknown, wrong, locked and tracker-full cases perform the same operations in
// the same order. That uniformity is the security property; the counting is just
// what it is for.
func (t *Throttle) Verify(ctx context.Context, r *Request) (Contribution, error) {
	keys := t.keysFor(r)
	now := time.Now()

	// Every key is read, always, in a fixed order. Reading only the subject
	// counter when the subject is known would itself be an enumeration signal.
	locked, outage := t.lockedAny(ctx, keys, now)
	if outage != nil {
		return Contribution{}, outage
	}

	if locked && t.claim == nil {
		// Address-only: refuse without running the factor. See the type comment
		// — there is no principal to enumerate, and running a single-use
		// credential factor here would destroy a valid ticket.
		t.failAll(ctx, keys, now)
		return Contribution{}, ErrThrottled
	}

	// Subject mode runs the factor even when locked, so the locked path cannot
	// be told from a wrong credential by timing.
	contribution, err := t.inner.Verify(ctx, r)

	if err != nil || locked {
		// A locked attempt counts as a failure: if a correct guess arriving
		// during backoff reset the counter, the lockout would be bypassable by
		// simply retrying, which is the entire thing it exists to prevent.
		t.failAll(ctx, keys, now)
		if locked {
			return Contribution{}, ErrThrottled
		}
		return Contribution{}, err
	}
	for _, k := range keys {
		if rerr := t.tracker.Reset(ctx, k); rerr != nil {
			// The credential was correct, so a tracker write failure must not
			// turn a success into a rejection. The stale counter simply expires.
			t.report(rerr)
		}
	}
	return contribution, nil
}

// keysFor builds the fixed-width tracker keys for one attempt.
func (t *Throttle) keysFor(r *Request) []string {
	addr := t.addr(r)
	if t.claim == nil {
		return []string{trackerKey("a", addr)}
	}
	subject := t.claim(r)
	if subject == "" {
		// An attempt that names nobody is counted against its ADDRESS in the
		// subject slot, never against a shared global key. A global key would
		// let one attacker lock out every client of this factor at once.
		return []string{trackerKey("u", addr), trackerKey("a", addr)}
	}
	return []string{trackerKey("s", subject), trackerKey("a", addr)}
}

func (t *Throttle) failAll(ctx context.Context, keys []string, now time.Time) {
	for _, k := range keys {
		if _, err := t.tracker.Fail(ctx, k, now); err != nil {
			t.report(err)
		}
	}
}

// lockedAny reads every key and applies the configured outage policy.
//
// A failing key is recorded and the loop CONTINUES rather than returning early:
// the number of tracker calls stays the same on every path, which is the
// property the whole design rests on.
func (t *Throttle) lockedAny(ctx context.Context, keys []string, now time.Time) (bool, error) {
	locked := false
	var outage error
	for _, k := range keys {
		l, _, err := t.tracker.Locked(ctx, k, now)
		if err != nil {
			outage = errors.Join(outage, err)
			continue
		}
		locked = locked || l
	}
	if outage != nil {
		t.report(outage)
		if !t.failOpen {
			// Deny. A tracker that cannot answer cannot protect anything, and
			// continuing would mean running with brute-force protection
			// silently switched off.
			return false, fmt.Errorf("%w: %v", ErrTrackerUnavailable, outage)
		}
		// FailOpen was chosen deliberately: proceed as if nothing were locked.
		return false, nil
	}
	return locked, nil
}

func (t *Throttle) report(err error) {
	if t.onTrackErr != nil {
		t.onTrackErr(err)
	}
}

// maxKeyMaterial bounds how much of a claimed value is hashed.
//
// Beyond it, two distinct subjects sharing a prefix share one counter. That only
// makes lockout STRONGER, never weaker, and it keeps the per-attempt hashing
// cost bounded by something other than the attacker's input length.
const maxKeyMaterial = 4096

// trackerKey produces a FIXED-WIDTH key.
//
// The value is attacker-controlled and unbounded — a claimed subject, a
// forwarded address — and a tracker retains its keys. Without hashing, an entry
// cap bounds the NUMBER of records but not the memory they hold: 16,384
// arbitrarily long strings stay inside any count-based cap. The namespace is
// inside the hash, so keys from different slots cannot collide.
func trackerKey(namespace, value string) string {
	if len(value) > maxKeyMaterial {
		value = value[:maxKeyMaterial]
	}
	sum := sha256.Sum256([]byte(namespace + "\x00" + value))
	// Base64, not raw digest bytes. A raw digest is not valid UTF-8 and contains
	// NUL, which the SQL and Redis trackers this seam exists for would have to
	// escape or would silently truncate. Still fixed width.
	return namespace + ":" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// peerAddress uses the transport's own view of the client, never a header.
func peerAddress(r *Request) string {
	if r == nil || !r.Peer.IsValid() {
		return ""
	}
	// The address without the port: a port changes per connection, so keying on
	// it would give an attacker a fresh counter for every attempt.
	return r.Peer.Addr().Unmap().String()
}

var _ Factor = (*Throttle)(nil)
