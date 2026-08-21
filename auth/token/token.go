// Package token issues and verifies opaque bearer tokens, including the
// single-use tickets ADR-0009's WebTUI handshake needs (ADR-0001 §2.3, §2.6).
//
// This package owns CREDENTIAL VALIDITY and CONSUMPTION. It does not own
// session lifecycle: a consumed ticket is a point-in-time proof, and its
// redemption deadline must never become a session's lifetime (ADR-0001 §2.2.1).
package token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

// Internal errors; auth.Policy maps every failure to auth.ErrUnauthenticated.
var (
	ErrMalformed = auth.Reason("token: malformed token")
	ErrNotFound  = auth.Reason("token: unknown token")
	ErrExpired   = auth.Reason("token: token expired")
	ErrConsumed  = auth.Reason("token: single-use token already consumed")
)

// rawLen is the token's entropy in bytes. 32 bytes = 256 bits, encoded as 43
// base64url characters — a FIXED length, which is what lets a presented token be
// length-checked before any comparison.
const rawLen = 32

// encodedLen is the exact length of a well-formed token string.
var encodedLen = base64.RawURLEncoding.EncodedLen(rawLen)

// Hash is a stored token's index key: the SHA-256 of the token string. The
// plaintext is never stored (ADR-0001 §2.6).
type Hash [sha256.Size]byte

// Record is what a store holds for one token. It never contains the plaintext.
type Record struct {
	Subject   string
	IssuedAt  time.Time
	ExpiresAt time.Time
	SingleUse bool
}

// Store holds token records.
//
// Consume MUST be ONE atomic operation: fetch, validate, and — for a single-use
// record — remove, indivisibly. A verify-then-delete pair races two concurrent
// redeemers into both succeeding, which for a WebTUI attach ticket means two
// sessions from one credential (ADR-0001 §2.6).
type Store interface {
	Put(h Hash, rec Record) error
	Consume(h Hash, now time.Time) (Record, error)
	Revoke(h Hash) error
}

// Factor verifies a presented token. It is identity-bearing.
type Factor struct {
	store  Store
	scheme string
	now    func() time.Time
}

// Option configures a Factor or an Issuer.
type Option func(*config)

type config struct {
	scheme string
	now    func() time.Time
}

// DefaultScheme is the auth.Request credential key a Factor reads by default.
//
// Exported so an adapter can PROJECT into the same key rather than guessing it:
// authhttp referenced a hand-written "token" while this package read "ticket",
// and the two silently did not compose — an end-to-end probe returned 401 with
// the credential unconsumed. A shared constant makes that drift impossible.
const DefaultScheme = "ticket"

// Scheme sets which auth.Request credential key is read. Default
// [DefaultScheme].
func Scheme(name string) Option { return func(c *config) { c.scheme = name } }

// Clock overrides the time source, for tests.
func Clock(fn func() time.Time) Option { return func(c *config) { c.now = fn } }

func resolve(opts []Option) config {
	c := config{scheme: DefaultScheme, now: time.Now}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// NewFactor builds the verifying factor.
func NewFactor(s Store, opts ...Option) *Factor {
	c := resolve(opts)
	return &Factor{store: s, scheme: c.scheme, now: c.now}
}

// Kind reports auth.FactorIdentity: a consumed token proves a principal.
func (f *Factor) Kind() auth.FactorKind { return auth.FactorIdentity }

// Verify decodes the presented token, hashes it, and consumes it atomically.
//
// The presented material is length-checked and decoded to a FIXED length before
// any comparison: subtle.ConstantTimeCompare returns early on a length
// mismatch, so handing it variable-length input reintroduces the very leak it
// exists to prevent (ADR-0001 §2.6).
func (f *Factor) Verify(_ context.Context, r *auth.Request) (auth.Contribution, error) {
	if r == nil {
		return auth.Contribution{}, ErrMalformed
	}
	presented, ok := r.Credentials[f.scheme]
	if !ok || presented.Len() != encodedLen {
		return auth.Contribution{}, ErrMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(presented.Reveal())
	if err != nil || len(raw) != rawLen {
		return auth.Contribution{}, ErrMalformed
	}
	rec, err := f.store.Consume(hashOf(presented.Reveal()), f.now())
	if err != nil {
		return auth.Contribution{}, err
	}
	// NOTE on constant time: there is deliberately no secret-dependent
	// comparison here to make constant. The store is indexed by the token's
	// SHA-256, so lookup is a fixed-size-key map hit rather than a comparison
	// against a stored secret — "constant-time lookup" would be a meaningless
	// claim (ADR-0001 §2.6). What matters, and is done above, is that the
	// presented material is length-checked and decoded to a FIXED length before
	// it reaches anything: subtle.ConstantTimeCompare returns early on a length
	// mismatch, so a variable-length path is what would leak. A method that
	// compares a presented proof against a STORED secret — auth/password — is
	// where subtle belongs.
	return auth.Contribution{
		Method:    "token",
		Subject:   rec.Subject,
		IssuedAt:  rec.IssuedAt,
		ExpiresAt: rec.ExpiresAt,
	}, nil
}

// Issuer mints tokens into a store.
type Issuer struct {
	store Store
	now   func() time.Time
}

// NewIssuer builds an issuer over the same store the Factor reads.
func NewIssuer(s Store, opts ...Option) *Issuer {
	c := resolve(opts)
	return &Issuer{store: s, now: c.now}
}

// Issue mints a token for subject, valid for ttl. singleUse makes it a ticket:
// redeemable exactly once.
//
// The returned Secret is the ONLY time the plaintext exists outside the
// caller — the store keeps a hash.
func (i *Issuer) Issue(subject string, ttl time.Duration, singleUse bool) (auth.Secret, error) {
	if subject == "" || ttl <= 0 {
		return auth.Secret{}, ErrMalformed
	}
	var raw [rawLen]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return auth.Secret{}, err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw[:])
	now := i.now()
	rec := Record{Subject: subject, IssuedAt: now, ExpiresAt: now.Add(ttl), SingleUse: singleUse}
	if err := i.store.Put(hashOf(tok), rec); err != nil {
		return auth.Secret{}, err
	}
	return auth.NewSecret(tok), nil
}

// Revoke invalidates a token by its plaintext.
func (i *Issuer) Revoke(tok string) error { return i.store.Revoke(hashOf(tok)) }

func hashOf(tok string) Hash { return sha256.Sum256([]byte(tok)) }
