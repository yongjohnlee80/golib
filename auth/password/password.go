package password

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/errs"
)

// Store holds encoded credentials. It is an interface because a credential
// store is always the caller's — a database, an LDAP mirror, a config file — and
// this package only needs to read one string.
type Store interface {
	// Lookup returns the encoded credential for subject, or [ErrNoCredential].
	//
	// It MUST NOT distinguish "no such subject" from "subject with no password"
	// in the error it returns; both are ErrNoCredential. The distinction is an
	// enumeration oracle and this package cannot hide one it is handed.
	Lookup(ctx context.Context, subject string) (encoded string, err error)
}

// Rehasher is the optional half of [Store] that makes upgrade-on-verify
// possible. A Store that does not implement it simply never gets its hashes
// strengthened.
type Rehasher interface {
	// Rehash replaces subject's stored credential. It is called only after a
	// SUCCESSFUL verification.
	Rehash(ctx context.Context, subject, encoded string) error
}

// Factor verifies a password. It is identity-bearing.
type Factor struct {
	store  Store
	hasher Hasher
	dummy  string
	subKey string
	pwKey  string
	now    func() time.Time
	onErr  func(error)
}

// Option configures a Factor.
type Option func(*Factor)

// Hash sets the hasher used for NEW and upgraded credentials. Defaults to
// [Default] (Argon2id). Verification always follows the stored credential
// instead, so changing this never invalidates existing hashes.
func Hash(h Hasher) Option { return func(f *Factor) { f.hasher = h } }

// CredentialKeys sets the auth.Request credential keys the subject and password
// are read from. Defaults "subject" and "password".
func CredentialKeys(subject, pw string) Option {
	return func(f *Factor) { f.subKey, f.pwKey = subject, pw }
}

// Clock overrides the time source, for tests.
func Clock(fn func() time.Time) Option { return func(f *Factor) { f.now = fn } }

// OnError receives non-fatal problems that must not fail an authentication: a
// failed rehash, a corrupt stored credential. It never receives a password.
// Defaults to discarding them.
func OnError(fn func(error)) Option { return func(f *Factor) { f.onErr = fn } }

// New builds the factor.
//
// It hashes a random throwaway password up front to hold as a dummy credential
// — see [Factor.Verify]. That costs one hash at construction, which is the
// point: the cost has to be identical to a real one.
func New(store Store, opts ...Option) (*Factor, error) {
	if store == nil {
		return nil, fmt.Errorf("password.New: a Store is required (%w)", errs.ErrInvalidArgument)
	}
	f := &Factor{store: store, hasher: Default(), subKey: "subject", pwKey: "password", now: time.Now}
	for _, o := range opts {
		if o != nil {
			o(f)
		}
	}
	if f.hasher == nil {
		return nil, fmt.Errorf("password.New: Hash(nil) — pass a Hasher or omit the option (%w)", errs.ErrInvalidArgument)
	}
	throwaway, err := randomSalt(32)
	if err != nil {
		return nil, err
	}
	// Encoded with the SAME hasher a real credential would use, so the dummy
	// path and the real path cost the same by construction rather than by a
	// number someone has to keep in sync.
	dummy, err := f.hasher.Hash(string(throwaway))
	if err != nil {
		return nil, err
	}
	f.dummy = dummy
	return f, nil
}

// Kind reports auth.FactorIdentity.
func (f *Factor) Kind() auth.FactorKind { return auth.FactorIdentity }

// Verify checks the presented password against the stored credential.
//
// An unknown subject is hashed against a dummy credential and then rejected.
// Without that, "no such user" returns in microseconds while "wrong password"
// takes 60 milliseconds, and the difference is a user-enumeration oracle
// readable over the network (ADR-0001 §2.6). The dummy hash is not a
// nicety — it is the only reason the two paths are indistinguishable.
//
// It closes the hashing oracle only. Per-subject and per-address counters are a
// second oracle and are the throttle's responsibility, not this factor's.
func (f *Factor) Verify(ctx context.Context, r *auth.Request) (auth.Contribution, error) {
	if r == nil {
		return auth.Contribution{}, ErrNoCredential
	}
	subjectCred, hasSubject := r.Credentials[f.subKey]
	pwCred, hasPassword := r.Credentials[f.pwKey]
	if !hasSubject || !hasPassword || subjectCred.IsZero() || pwCred.IsZero() {
		// A request that omitted a field tells the attacker nothing they did not
		// already know, so there is nothing to equalize here.
		return auth.Contribution{}, ErrNoCredential
	}
	subject, presented := subjectCred.Reveal(), pwCred.Reveal()
	if len(presented) > MaxPasswordLen || len(subject) > MaxSubjectLen {
		return auth.Contribution{}, ErrTooLong
	}

	encoded, lookupErr := f.store.Lookup(ctx, subject)
	if lookupErr != nil || encoded == "" {
		// Same work, same shape, same outward error as a wrong password.
		_ = Verify(f.dummy, presented)
		if lookupErr != nil && !errors.Is(lookupErr, ErrNoCredential) {
			// A broken store is not a wrong password; the operator needs to see
			// it, and the caller still gets a uniform rejection.
			f.report(lookupErr)
		}
		return auth.Contribution{}, ErrMismatch
	}

	if err := Verify(encoded, presented); err != nil {
		if !errors.Is(err, ErrMismatch) {
			// A corrupt or unreadable stored credential is an operator problem
			// masquerading as a wrong password. Report it, reject uniformly.
			f.report(err)
			return auth.Contribution{}, ErrMismatch
		}
		return auth.Contribution{}, err
	}

	// Verified. This is the only moment the plaintext exists to rehash with.
	if Stale(encoded, f.hasher) {
		f.upgrade(ctx, subject, presented)
	}
	return auth.Contribution{
		Method:    "password",
		Subject:   subject,
		IssuedAt:  f.now(),
		ExpiresAt: time.Time{}, // a password proves a moment; session lifetime is elsewhere
	}, nil
}

// upgrade rewrites a verified credential under current policy.
//
// Best-effort by design: the user supplied the correct password, so a failed
// rehash must not turn a successful authentication into a rejection. The
// credential simply stays on its old parameters and the next login tries again.
func (f *Factor) upgrade(ctx context.Context, subject, presented string) {
	rh, ok := f.store.(Rehasher)
	if !ok {
		return
	}
	fresh, err := f.hasher.Hash(presented)
	if err != nil {
		f.report(err)
		return
	}
	if err := rh.Rehash(ctx, subject, fresh); err != nil {
		f.report(err)
	}
}

func (f *Factor) report(err error) {
	if f.onErr != nil {
		f.onErr(err)
	}
}

var _ auth.Factor = (*Factor)(nil)

// MaxSubjectLen bounds a claimed subject.
//
// Unlike the password, the subject is retained downstream — a throttle keys a
// counter by it, a log records it — so an unbounded value is not merely wasted
// hashing but retained memory chosen by an unauthenticated caller.
const MaxSubjectLen = 256

// Claim implements [auth.Claimant]: it names the principal this request targets,
// without verifying anything.
//
// This is what makes per-subject lockout possible for passwords. A factor that
// cannot answer it — an opaque bearer token — has to be throttled by address.
func (f *Factor) Claim(r *auth.Request) string {
	if r == nil {
		return ""
	}
	s, ok := r.Credentials[f.subKey]
	if !ok || s.IsZero() {
		return ""
	}
	v := s.Reveal()
	if len(v) > MaxSubjectLen {
		// An oversized claim is not a principal. Returning "" routes it to the
		// unattributed-per-address counter rather than creating a record keyed
		// by junk.
		return ""
	}
	return v
}

var _ auth.Claimant = (*Factor)(nil)
