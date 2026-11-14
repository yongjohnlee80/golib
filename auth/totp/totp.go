// Package totp computes and verifies time-based one-time passwords (RFC 6238,
// TOTP, over RFC 4226's HOTP) and issues the recovery codes that go with them.
//
// # Defaults
//
// SHA-1, 6 digits and a 30-second period: the common ground of the
// authenticator apps, and what an otpauth:// URI means when it names nothing
// else. The zero [Params] is that default, and the package-level [Code],
// [Verify] and [URI] use it. [New] builds other parameters (SHA-256 or SHA-512,
// 7 or 8 digits, another period) for completeness; an app that ignores the URI's
// parameters will then compute codes that never verify, so change them only for
// a client known to honour them.
//
// # Verifying
//
// [Verify] accepts the code of the current step or of the step either side of
// it, so a phone clock up to one period off still works. It never accepts the
// step a code was last accepted at, or an older one: the caller stores the step
// Verify returns and passes it back next time, which is what stops a code seen
// over a shoulder or in a log from being used again inside its window. That
// store must be updated atomically with the check (compare-and-set on the
// subject's last step), or two concurrent requests both succeed with one code.
// The comparison is constant-time, and a code that is not exactly the
// configured number of ASCII digits is refused before any is computed.
//
// # What stays the caller's
//
// This package is the arithmetic, not a login flow, and it is not an
// [auth.Factor]. The caller keeps:
//
//   - The secret. It is a symmetric key that the server must be able to read
//     back, so it cannot be hashed like a password: store it encrypted at rest,
//     and never log it.
//   - Rate limiting. Six digits are a million guesses and the window accepts
//     three of them at once; throttle failures per subject (auth.Throttle does
//     this), or the code falls to brute force in hours.
//   - Recovery codes once issued. [RecoveryCodes] only generates them: hash each
//     before storing it, compare a presented one (after [NormalizeRecoveryCode])
//     against the hashes, and delete it once it is used.
//   - Binding the code to the subject who presented the first factor.
//
// [auth.Factor]: https://pkg.go.dev/github.com/yongjohnlee80/golib/auth#Factor
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"hash"
	"strconv"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// Algorithm is the HMAC hash a code is computed with.
type Algorithm uint8

// The algorithms RFC 6238 names. The zero Algorithm is not one of them; [New]
// refuses it.
const (
	SHA1 Algorithm = iota + 1
	SHA256
	SHA512
)

// String returns the name an otpauth:// URI uses: "SHA1", "SHA256" or
// "SHA512".
func (a Algorithm) String() string {
	switch a {
	case SHA1:
		return "SHA1"
	case SHA256:
		return "SHA256"
	case SHA512:
		return "SHA512"
	}
	return "Algorithm(" + strconv.Itoa(int(a)) + ")"
}

func (a Algorithm) hash() func() hash.Hash {
	switch a {
	case SHA256:
		return sha256.New
	case SHA512:
		return sha512.New
	}
	return sha1.New
}

// The defaults: see the package documentation.
const (
	DefaultAlgorithm = SHA1
	DefaultDigits    = 6
	DefaultPeriod    = 30 * time.Second
)

// The digit counts allowed: RFC 4226 requires at least 6 and describes up to 8,
// which is as far as the authenticator apps go.
const (
	MinDigits = 6
	MaxDigits = 8
)

// Params is one choice of algorithm, digit count and period. The zero value is
// the default (SHA-1, 6 digits, 30 seconds); any other comes from [New], so a
// Params is always valid.
type Params struct {
	alg    Algorithm
	digits int
	period time.Duration
}

// Option configures [New].
type Option func(*Params)

// Hash sets the HMAC algorithm. Default [SHA1].
func Hash(a Algorithm) Option { return func(p *Params) { p.alg = a } }

// Digits sets how many digits a code has, [MinDigits] to [MaxDigits]. Default 6.
func Digits(n int) Option { return func(p *Params) { p.digits = n } }

// Period sets how long one code lasts: a whole number of seconds, at least
// one. Default 30 seconds.
func Period(d time.Duration) Option { return func(p *Params) { p.period = d } }

// New returns Params with the defaults changed by opts, or an error wrapping
// errs.ErrInvalidArgument naming the option out of range.
func New(opts ...Option) (Params, error) {
	p := Params{alg: DefaultAlgorithm, digits: DefaultDigits, period: DefaultPeriod}
	for _, o := range opts {
		if o != nil {
			o(&p)
		}
	}
	switch {
	case p.alg != SHA1 && p.alg != SHA256 && p.alg != SHA512:
		return Params{}, fmt.Errorf("totp.New: unknown algorithm %v (%w)", p.alg, errs.ErrInvalidArgument)
	case p.digits < MinDigits || p.digits > MaxDigits:
		return Params{}, fmt.Errorf("totp.New: %d digits, want %d to %d (%w)", p.digits, MinDigits, MaxDigits, errs.ErrInvalidArgument)
	case p.period < time.Second || p.period%time.Second != 0:
		return Params{}, fmt.Errorf("totp.New: period %v is not a whole number of seconds of at least one (%w)", p.period, errs.ErrInvalidArgument)
	}
	return p, nil
}

// Algorithm reports the HMAC algorithm.
func (p Params) Algorithm() Algorithm {
	if p.alg == 0 {
		return DefaultAlgorithm
	}
	return p.alg
}

// Digits reports how many digits a code has.
func (p Params) Digits() int {
	if p.digits == 0 {
		return DefaultDigits
	}
	return p.digits
}

// Period reports how long one code lasts.
func (p Params) Period() time.Duration {
	if p.period == 0 {
		return DefaultPeriod
	}
	return p.period
}

// Step returns the time step t falls in: the whole periods since the Unix
// epoch, rounded down.
func (p Params) Step(t time.Time) int64 {
	sec := int64(p.Period() / time.Second)
	u := t.Unix()
	s := u / sec
	if u%sec != 0 && u < 0 {
		s--
	}
	return s
}

// Code returns the code for the step t falls in, zero-padded to the digit
// count.
func (p Params) Code(secret []byte, t time.Time) string {
	return p.hotp(secret, uint64(p.Step(t)))
}

// Verify reports whether code is the code for the step at falls in or for one
// step either side, and is newer than lastStep. On success it returns the step
// the code matched; the caller stores it and passes it as lastStep next time,
// so no code is ever accepted twice. Before any code has been accepted, pass a
// negative lastStep.
//
// A code must be exactly [Params.Digits] ASCII digits: trim spaces from user
// input first. If the code matches more than one step in the window, the latest
// is returned.
func (p Params) Verify(secret []byte, code string, at time.Time, lastStep int64) (step int64, ok bool) {
	if len(code) != p.Digits() {
		return 0, false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return 0, false
		}
	}
	now := p.Step(at)
	presented := []byte(code)
	matched := int64(-1)
	// Every step in the window is computed and compared, eligible or not, so the
	// time Verify takes does not depend on which one matched.
	for s := now - 1; s <= now+1; s++ {
		if s < 0 {
			continue
		}
		eq := subtle.ConstantTimeCompare([]byte(p.hotp(secret, uint64(s))), presented)
		if eq == 1 && s > lastStep {
			matched = s
		}
	}
	if matched < 0 {
		return 0, false
	}
	return matched, true
}

// pow10 holds 10^n for the digit counts HOTP allows.
var pow10 = [MaxDigits + 1]uint32{1, 10, 100, 1000, 10000, 100000, 1000000, 10000000, 100000000}

// hotp is RFC 4226's HOTP: an HMAC of the big-endian counter, dynamically
// truncated to 31 bits, reduced to the digit count.
func (p Params) hotp(secret []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(p.Algorithm().hash(), secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	d := p.Digits()
	return fmt.Sprintf("%0*d", d, bin%pow10[d])
}

// Code returns the default (SHA-1, 6 digits, 30 seconds) code for the step t
// falls in. See [Params.Code].
func Code(secret []byte, t time.Time) string { return Params{}.Code(secret, t) }

// Verify checks a default (SHA-1, 6 digits, 30 seconds) code. See
// [Params.Verify].
func Verify(secret []byte, code string, at time.Time, lastStep int64) (step int64, ok bool) {
	return Params{}.Verify(secret, code, at, lastStep)
}
