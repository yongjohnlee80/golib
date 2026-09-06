package password

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id hashes with Argon2id. It is the default.
type Argon2id struct {
	// Memory is the memory cost in KiB.
	Memory uint32
	// Time is the number of passes.
	Time uint32
	// Parallelism is the number of lanes.
	Parallelism uint8
	// SaltLen and KeyLen are in bytes.
	SaltLen int
	KeyLen  int
}

// Default is RFC 9106's second recommended configuration: 64 MiB, 3 passes, 4
// lanes.
//
// Note what this costs: every verification — INCLUDING a failed one, and
// including the dummy hash performed for an unknown subject — allocates 64 MiB
// and holds it for the duration. Unauthenticated attempt volume times 64 MiB is
// the number to bound, which is the throttle's job, not this package's. A
// memory-constrained server should select [Interactive] deliberately rather than
// discover this under load.
func Default() Argon2id {
	return Argon2id{Memory: 64 * 1024, Time: 3, Parallelism: 4, SaltLen: 16, KeyLen: 32}
}

// Interactive is OWASP's minimum recommended configuration: 19 MiB, 2 passes, 1
// lane. Weaker than [Default] and chosen on purpose, for a server that cannot
// spend 64 MiB per attempt.
func Interactive() Argon2id {
	return Argon2id{Memory: 19 * 1024, Time: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32}
}

// Parameter bounds, applied on BOTH write and read.
//
// The read side is the security-relevant one: a stored credential is data, and
// if an attacker can write to the credential store, an "m=4194304" line turns
// every login attempt into a 4 GiB allocation. Refusing out-of-range parameters
// makes that a failed credential rather than a downed process.
const (
	minMemory      = 8 * 1024 // 8 MiB
	maxMemory      = 1 << 20  // 1 GiB, in KiB
	minTime        = 1
	maxTime        = 16
	maxParallelism = 16
	minSaltLen     = 8
	maxSaltLen     = 64
	minKeyLen      = 16
	maxKeyLen      = 64
)

func (a Argon2id) validate() error {
	switch {
	case a.Memory < minMemory || a.Memory > maxMemory:
		return fmt.Errorf("%w: m=%d outside [%d,%d] KiB", ErrParams, a.Memory, minMemory, maxMemory)
	case a.Time < minTime || a.Time > maxTime:
		return fmt.Errorf("%w: t=%d outside [%d,%d]", ErrParams, a.Time, minTime, maxTime)
	case a.Parallelism < 1 || a.Parallelism > maxParallelism:
		return fmt.Errorf("%w: p=%d outside [1,%d]", ErrParams, a.Parallelism, maxParallelism)
	case a.SaltLen < minSaltLen || a.SaltLen > maxSaltLen:
		return fmt.Errorf("%w: salt length %d outside [%d,%d]", ErrParams, a.SaltLen, minSaltLen, maxSaltLen)
	case a.KeyLen < minKeyLen || a.KeyLen > maxKeyLen:
		return fmt.Errorf("%w: key length %d outside [%d,%d]", ErrParams, a.KeyLen, minKeyLen, maxKeyLen)
	}
	return nil
}

// scheme is the identifier plus parameter segment, which is also the staleness
// key. v=19 is Argon2's version constant, not ours.
func (a Argon2id) scheme() string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d", argon2.Version, a.Memory, a.Time, a.Parallelism)
}

// Hash produces a new credential.
func (a Argon2id) Hash(password string) (string, error) {
	if len(password) > MaxPasswordLen {
		return "", ErrTooLong
	}
	if err := a.validate(); err != nil {
		return "", err
	}
	salt, err := randomSalt(a.SaltLen)
	if err != nil {
		return "", err
	}
	digest := argon2.IDKey([]byte(password), salt, a.Time, a.Memory, a.Parallelism, uint32(a.KeyLen))
	return a.scheme() + "$" + b64.EncodeToString(salt) + "$" + b64.EncodeToString(digest), nil
}

// verifyArgon2id re-derives using the STORED parameters and salt.
//
// The key length comes from the stored digest, so the candidate and the stored
// value are the same length by construction — which is what lets the comparison
// be constant-time (see sameDigest).
func verifyArgon2id(rest, password string) error {
	// rest: "v=19$m=..,t=..,p=..$<salt>$<digest>"
	parts := strings.Split(rest, "$")
	if len(parts) != 4 {
		return ErrEncoding
	}
	version, err := parseKV(parts[0], "v")
	if err != nil {
		return err
	}
	if version != uint64(argon2.Version) {
		// A different Argon2 version is a different function. Guessing would be
		// worse than refusing.
		return fmt.Errorf("%w: argon2 version %d", ErrUnknownScheme, version)
	}
	m, t, p, err := parseCosts(parts[1])
	if err != nil {
		return err
	}
	salt, err := b64.DecodeString(parts[2])
	if err != nil {
		return ErrEncoding
	}
	stored, err := b64.DecodeString(parts[3])
	if err != nil {
		return ErrEncoding
	}
	cfg := Argon2id{Memory: m, Time: t, Parallelism: p, SaltLen: len(salt), KeyLen: len(stored)}
	if err := cfg.validate(); err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(stored)))
	return sameDigest(stored, got)
}

// parseCosts reads "m=<n>,t=<n>,p=<n>" in that exact order. A lenient parser
// here would accept a credential whose real parameters differ from the ones a
// reader would report.
func parseCosts(s string) (m, t uint32, p uint8, err error) {
	fields := strings.Split(s, ",")
	if len(fields) != 3 {
		return 0, 0, 0, ErrEncoding
	}
	mv, err := parseKV(fields[0], "m")
	if err != nil {
		return 0, 0, 0, err
	}
	tv, err := parseKV(fields[1], "t")
	if err != nil {
		return 0, 0, 0, err
	}
	pv, err := parseKV(fields[2], "p")
	if err != nil {
		return 0, 0, 0, err
	}
	if mv > maxMemory || tv > maxTime || pv > maxParallelism {
		return 0, 0, 0, fmt.Errorf("%w: m=%d,t=%d,p=%d", ErrParams, mv, tv, pv)
	}
	return uint32(mv), uint32(tv), uint8(pv), nil
}

// parseKV reads "<key>=<uint>", requiring the exact key.
func parseKV(s, key string) (uint64, error) {
	name, value, ok := strings.Cut(s, "=")
	if !ok || name != key {
		return 0, ErrEncoding
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, ErrEncoding
	}
	return n, nil
}

var _ Hasher = Argon2id{}
