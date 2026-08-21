// Package password verifies a password against a stored hash (ADR-0001 §2.4).
//
// # What is stored
//
// A self-describing PHC-style string, so a credential written years ago still
// says how to check it:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<digest>
//	$pbkdf2-sha256$i=600000$<salt>$<digest>
//
// Parameters travel WITH the hash rather than being read from configuration.
// Configuration is what today's policy is; the stored string is what actually
// produced that digest, and those diverge the moment anyone tunes a cost.
//
// # Argon2id is the default
//
// Memory-hardness is the property that matters against GPU and ASIC cracking
// (RFC 9106; OWASP Password Storage). [PBKDF2] ships too, but only for a caller
// with an explicit FIPS requirement — never as the default.
//
// # Upgrade on successful verify
//
// When a credential verifies but was written under different parameters than
// current policy, [Factor] rewrites it. That is the only moment the plaintext
// is available to rehash with, so a scheme without it can never strengthen its
// stored hashes without a password reset.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Internal errors. auth.Policy maps every failure to auth.ErrUnauthenticated;
// these exist for the audit record and for tests.
var (
	// ErrMismatch means the password does not match the stored hash. It is the
	// ONLY failure a caller may interpret as "wrong password" — every other
	// error below means the stored credential or the request was unusable.
	ErrMismatch = errors.New("password: does not match")

	ErrEncoding      = errors.New("password: malformed stored credential")
	ErrUnknownScheme = errors.New("password: unknown hash scheme")
	ErrParams        = errors.New("password: hash parameters out of range")
	ErrNoCredential  = errors.New("password: no credential for subject")
	ErrTooLong       = errors.New("password: presented password exceeds the length limit")
)

// MaxPasswordLen bounds what will be hashed.
//
// Argon2id does not truncate, so an unbounded password is an unauthenticated
// attacker choosing how much work the server does per attempt. 4 KiB is far
// past any human passphrase.
const MaxPasswordLen = 4096

// Hasher writes new credentials under current policy. Verification does NOT go
// through it — see [Verify] — because the stored string, not the current
// configuration, describes how a given digest was produced.
type Hasher interface {
	// Hash encodes a new credential for password.
	Hash(password string) (string, error)

	// scheme is the PHC identifier plus parameter segment this Hasher writes,
	// e.g. "$argon2id$v=19$m=65536,t=3,p=4". Comparing it against a stored
	// credential's prefix is how staleness is decided: an exact-match test over
	// the whole parameter set, rather than an ordering over parameters that are
	// not actually ordered (more memory but fewer passes — which is stronger?).
	scheme() string
}

// Verify reports whether password matches the stored credential, dispatching on
// what the credential itself declares.
//
// nil means match. Every other return is a failure, and only [ErrMismatch]
// means "wrong password".
func Verify(encoded, password string) error {
	if len(password) > MaxPasswordLen {
		return ErrTooLong
	}
	id, rest, ok := splitScheme(encoded)
	if !ok {
		return ErrEncoding
	}
	switch id {
	case "argon2id":
		return verifyArgon2id(rest, password)
	case "pbkdf2-sha256":
		return verifyPBKDF2(rest, password)
	default:
		// An unknown scheme is not a mismatch. Reporting it as one would let a
		// storage bug read as a wave of wrong passwords.
		return fmt.Errorf("%w: %q", ErrUnknownScheme, id)
	}
}

// Stale reports whether encoded was written under different parameters than h
// writes now, i.e. whether a successful verify should rewrite it.
//
// Any difference counts, in either direction. A deployment that lowers its cost
// parameters on purpose gets its stored hashes rewritten down to the new policy
// — which is what "policy" means, and is why lowering them is a decision, not a
// tuning knob to fiddle with.
func Stale(encoded string, h Hasher) bool {
	return !strings.HasPrefix(encoded, h.scheme()+"$")
}

// splitScheme peels the leading "$id$" off a PHC string.
func splitScheme(encoded string) (id, rest string, ok bool) {
	if !strings.HasPrefix(encoded, "$") {
		return "", "", false
	}
	i := strings.IndexByte(encoded[1:], '$')
	if i <= 0 {
		return "", "", false
	}
	return encoded[1 : 1+i], encoded[2+i:], true
}

var b64 = base64.RawStdEncoding

// randomSalt returns n cryptographically random bytes.
//
// Per-credential and never reused: a shared salt makes one precomputation
// attack serve every account at once.
func randomSalt(n int) ([]byte, error) {
	s := make([]byte, n)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	return s, nil
}

// sameDigest compares two digests in constant time.
//
// The length check is explicit and comes FIRST because
// subtle.ConstantTimeCompare returns early on a length mismatch — handing it
// variable-length material reintroduces exactly the leak it exists to prevent
// (ADR-0001 §2.6). Every caller here derives its candidate at the STORED
// digest's length, so a mismatch means a corrupt credential, not a wrong
// password.
func sameDigest(want, got []byte) error {
	if len(want) != len(got) {
		return ErrEncoding
	}
	if subtle.ConstantTimeCompare(want, got) != 1 {
		return ErrMismatch
	}
	return nil
}
