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
func randomID() (string, error) {
	var b [sessionIDBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// RawURLEncoding: fixed length, no padding, safe in a URL path and in a log
	// line without escaping.
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
