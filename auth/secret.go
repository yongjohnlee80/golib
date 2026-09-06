package auth

import (
	"encoding/json"
	"fmt"
	"io"
)

// Secret wraps credential material so it cannot be printed by accident.
// Redaction is STRUCTURAL, not a convention: String, Format, MarshalJSON and
// MarshalText all return a placeholder, so %v, %+v, %s, log.Printf and JSON
// encoding are all safe (RULES.md #1).
//
// No claim is made that the bytes are erased from memory — Go offers no such
// guarantee. The claim is that Secret does not leak through formatting.
type Secret struct{ v string }

// NewSecret wraps credential material.
func NewSecret(s string) Secret { return Secret{v: s} }

// Reveal returns the material. Every call site is a place a reviewer should
// look; keep them few and short-lived.
func (s Secret) Reveal() string { return s.v }

// Len reports the material's length without revealing it — enough for a
// fixed-length precondition check.
func (s Secret) Len() int { return len(s.v) }

// IsZero reports whether no material was set.
func (s Secret) IsZero() bool { return s.v == "" }

const redacted = "auth.Secret(redacted)"

func (Secret) String() string               { return redacted }
func (Secret) GoString() string             { return redacted }
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

// Format implements fmt.Formatter so EVERY verb is redacted — including ones
// Stringer does not cover, such as %d or %x on the value. It must take
// fmt.State exactly: a locally-declared look-alike interface would not satisfy
// fmt.Formatter, and fmt would silently never call it.
func (s Secret) Format(f fmt.State, verb rune) { _, _ = io.WriteString(f, redacted) }

// compile-time: every formatting path fmt consults is implemented.
var (
	_ fmt.Formatter  = Secret{}
	_ fmt.Stringer   = Secret{}
	_ fmt.GoStringer = Secret{}
	_ json.Marshaler = Secret{}
)
