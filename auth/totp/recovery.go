package totp

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

// RecoveryCodeLen is the length of a recovery code: 10 characters of 5 bits
// each, 50 bits.
const RecoveryCodeLen = 10

// RecoveryAlphabet is the 32 characters a recovery code is made of: the digits
// 2 to 9 and the upper-case letters without I and O, so no two can be misread
// as each other (0 and O, 1 and I and l).
const RecoveryAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// ErrRecoveryCode means a string given to [NormalizeRecoveryCode] is not a
// recovery code: the wrong length, or a character outside [RecoveryAlphabet].
var ErrRecoveryCode = errors.New("totp: malformed recovery code")

// RecoveryCodes returns n distinct recovery codes of [RecoveryCodeLen]
// characters from [RecoveryAlphabet], from crypto/rand. Show them to the user
// once, grouped however reads best ("ABCDE-FGHJK"): [NormalizeRecoveryCode]
// undoes the grouping.
//
// Storing them is the caller's job, and they must not be stored as they are:
// keep only a hash of each (auth/password's Hasher, or an HMAC under a server
// key), compare a presented code's normalised form against the hashes, and
// delete the one that matched so it works once. An n below 1 is an error
// wrapping errs.ErrInvalidArgument.
func RecoveryCodes(n int) ([]string, error) {
	if n < 1 {
		return nil, fmt.Errorf("totp.RecoveryCodes: n = %d, want at least 1 (%w)", n, errs.ErrInvalidArgument)
	}
	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	var raw [RecoveryCodeLen]byte
	var code [RecoveryCodeLen]byte
	for len(out) < n {
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, err
		}
		// 256 is a multiple of 32, so the low 5 bits of a uniform byte are a
		// uniform index: no modulo bias.
		for i, r := range raw {
			code[i] = RecoveryAlphabet[r&31]
		}
		c := string(code[:])
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out, nil
}

// NormalizeRecoveryCode returns a recovery code as a user typed it in the form
// [RecoveryCodes] made it: upper case, with spaces, tabs and hyphens removed.
// What is left must be [RecoveryCodeLen] characters of [RecoveryAlphabet], or
// it is [ErrRecoveryCode].
func NormalizeRecoveryCode(s string) (string, error) {
	clean := gather(s)
	if len(clean) != RecoveryCodeLen {
		return "", ErrRecoveryCode
	}
	for i := 0; i < len(clean); i++ {
		if strings.IndexByte(RecoveryAlphabet, clean[i]) < 0 {
			return "", ErrRecoveryCode
		}
	}
	return clean, nil
}
