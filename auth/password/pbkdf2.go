package password

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"fmt"
	"strings"
)

// PBKDF2 hashes with PBKDF2-HMAC-SHA256, from the standard library.
//
// This exists for ONE reason: a caller with an explicit FIPS requirement.
// PBKDF2 is not memory-hard, so it is materially weaker than [Argon2id] against
// GPU and ASIC cracking, and selecting it to "avoid a dependency" would be a
// bad trade — `golang.org/x/crypto` is in this module's graph regardless
// (ADR-0001 §2.4). Never the default.
type PBKDF2 struct {
	// Iterations is the work factor. OWASP's 2023 floor for
	// PBKDF2-HMAC-SHA256 is 600,000.
	Iterations int
	SaltLen    int
	KeyLen     int
}

// FIPS returns the OWASP-floor configuration: 600,000 iterations.
func FIPS() PBKDF2 { return PBKDF2{Iterations: 600_000, SaltLen: 16, KeyLen: 32} }

const (
	minIterations = 100_000
	maxIterations = 10_000_000 // read-side cap: a stored "i=" is untrusted data
)

func (p PBKDF2) validate() error {
	switch {
	case p.Iterations < minIterations || p.Iterations > maxIterations:
		return fmt.Errorf("%w: i=%d outside [%d,%d]", ErrParams, p.Iterations, minIterations, maxIterations)
	case p.SaltLen < minSaltLen || p.SaltLen > maxSaltLen:
		return fmt.Errorf("%w: salt length %d outside [%d,%d]", ErrParams, p.SaltLen, minSaltLen, maxSaltLen)
	case p.KeyLen < minKeyLen || p.KeyLen > maxKeyLen:
		return fmt.Errorf("%w: key length %d outside [%d,%d]", ErrParams, p.KeyLen, minKeyLen, maxKeyLen)
	}
	return nil
}

// scheme is the identifier plus parameters. PBKDF2 has no registered PHC
// identifier, so this encoding is ours; it never leaves the credential store.
func (p PBKDF2) scheme() string {
	return fmt.Sprintf("$pbkdf2-sha256$i=%d", p.Iterations)
}

// Hash produces a new credential.
func (p PBKDF2) Hash(password string) (string, error) {
	if len(password) > MaxPasswordLen {
		return "", ErrTooLong
	}
	if err := p.validate(); err != nil {
		return "", err
	}
	salt, err := randomSalt(p.SaltLen)
	if err != nil {
		return "", err
	}
	digest, err := pbkdf2.Key(sha256.New, password, salt, p.Iterations, p.KeyLen)
	if err != nil {
		return "", err
	}
	return p.scheme() + "$" + b64.EncodeToString(salt) + "$" + b64.EncodeToString(digest), nil
}

// verifyPBKDF2 re-derives at the stored digest's length, for the same
// constant-time reason as verifyArgon2id.
func verifyPBKDF2(rest, password string) error {
	// rest: "i=<n>$<salt>$<digest>"
	parts := strings.Split(rest, "$")
	if len(parts) != 3 {
		return ErrEncoding
	}
	iter, err := parseKV(parts[0], "i")
	if err != nil {
		return err
	}
	salt, err := b64.DecodeString(parts[1])
	if err != nil {
		return ErrEncoding
	}
	stored, err := b64.DecodeString(parts[2])
	if err != nil {
		return ErrEncoding
	}
	cfg := PBKDF2{Iterations: int(iter), SaltLen: len(salt), KeyLen: len(stored)}
	if err := cfg.validate(); err != nil {
		return err
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, int(iter), len(stored))
	if err != nil {
		return ErrEncoding
	}
	return sameDigest(stored, got)
}

var _ Hasher = PBKDF2{}
