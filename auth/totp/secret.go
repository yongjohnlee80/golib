package totp

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
)

// SecretLen is the length of a secret [NewSecret] makes: 20 bytes, 160 bits,
// the length RFC 4226 recommends (and SHA-1's output size).
const SecretLen = 20

// ErrSecret means a string given to [Decode] is not a base32 secret.
var ErrSecret = errors.New("totp: malformed secret")

// b32 is RFC 4648 base32 without padding, the form authenticator apps show and
// accept.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns [SecretLen] random bytes from crypto/rand.
func NewSecret() ([]byte, error) {
	s := make([]byte, SecretLen)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	return s, nil
}

// Encode returns secret as upper-case base32 without padding, the text a user
// types into an authenticator app.
func Encode(secret []byte) string { return b32.EncodeToString(secret) }

// Decode reads a base32 secret as a user may have copied it: upper or lower
// case, with spaces, tabs or hyphens between groups, padded or not. A string
// that holds no base32 characters, or is not valid base32 once they are
// gathered, is [ErrSecret].
func Decode(s string) ([]byte, error) {
	clean := strings.TrimRight(gather(s), "=")
	// Unpadded base32 ends a group of 8 after 2, 4, 5 or 7 characters (1 to 4
	// bytes); any other remainder is a truncated string, which the decoder
	// would otherwise accept by dropping its last characters.
	switch len(clean) % 8 {
	case 1, 3, 6:
		return nil, ErrSecret
	}
	if clean == "" {
		return nil, ErrSecret
	}
	out, err := b32.DecodeString(clean)
	if err != nil {
		return nil, ErrSecret
	}
	return out, nil
}

// gather removes the separators a user may type between groups (spaces, tabs,
// newlines, hyphens) and upper-cases ASCII letters.
func gather(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '-':
			return -1
		}
		if 'a' <= r && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, s)
}
