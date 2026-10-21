package web

import (
	"crypto/rand"
	"encoding/base64"
)

// sessionIDBytes is the entropy in a session id.
//
// 32 bytes, matching auth/token's floor. A session id is a handle, not a
// credential — an attach still re-runs the full policy — but a GUESSABLE handle
// plus any credential is a shortcut to somebody else's screen, so it is treated
// as if it mattered.
const sessionIDBytes = 32

// randomID returns a URL-safe high-entropy identifier.
func randomID() (string, error) { return randomToken(sessionIDBytes) }

// randomToken is n random bytes, URL-safe: RawURLEncoding is fixed length, has no padding, and is
// safe in a URL path and in a log line without escaping. A session ID and a CSP nonce are both one.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
