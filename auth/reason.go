package auth

import (
	"errors"
	"fmt"
	"strings"
)

// Reason is an error whose text is fixed at COMPILE TIME and is therefore safe
// to record in an audit trail verbatim.
//
// It exists because the audit record cannot trust an arbitrary error. A factor
// is third-party code by design, and `fmt.Errorf("bad token %q", presented)`
// puts a credential into `err.Error()`; recording that text would write the
// secret straight into the operator's log. Reason marks the errors whose text
// contains no runtime data.
//
// Declare sentinels with it instead of [errors.New], and wrap freely — a
// `fmt.Errorf("%w: %s", ErrSomething, detail)` chain still yields the FIXED text
// of the Reason it wraps, and the dynamic half is dropped rather than logged.
//
// One caveat follows from it being a string type: two Reason values with
// IDENTICAL text are `==`, so [errors.Is] cannot tell them apart, where
// [errors.New] would have produced distinct pointers. Package-prefixing every
// sentinel — as this module does — makes an ACCIDENTAL cross-package collision
// very unlikely, but it does not make collision impossible: the same text can be
// declared twice inside one package, and an exported Reason can be reconstructed
// anywhere from its string. What actually guards the property is a test that
// compares recorded reason DETAILS for uniqueness; see
// TestBuiltInReasonsAreDistinguishable. Reach for a struct with a unique
// identity field only if collision-proof [errors.Is] identity becomes an API
// invariant rather than a testable convention.
type Reason string

// Error implements error.
func (r Reason) Error() string { return string(r) }

// AuditDetail implements [SafeAuditDetail]: the text is a compile-time constant.
func (r Reason) AuditDetail() string { return string(r) }

// SafeAuditDetail is implemented by an error that asserts its own text carries
// no credential material and may be recorded verbatim.
//
// A factor outside this module implements it to get useful audit detail. An
// error that does not implement it is recorded by TYPE only — see [auditDetail]
// — because a type name is compile-time and an error string is not.
type SafeAuditDetail interface {
	error

	// AuditDetail returns a credential-free description.
	AuditDetail() string
}

// maxAuditDetail bounds one recorded reason.
const maxAuditDetail = 200

// auditDetail extracts a loggable description of err.
//
// The default is deliberately uninformative: an error that has not asserted
// safety contributes only its Go TYPE, which is fixed at compile time and so
// cannot carry a credential. Losing detail on a third-party factor is the right
// trade — the alternative is a package that writes secrets to disk whenever
// somebody else's factor formats one into an error.
func auditDetail(err error) string {
	if err == nil {
		return ""
	}
	var safe SafeAuditDetail
	if errors.As(err, &safe) {
		return clampDetail(safe.AuditDetail())
	}
	return clampDetail(fmt.Sprintf("opaque error of type %T", err))
}

func clampDetail(s string) string {
	s = sanitize(s)
	if len(s) > maxAuditDetail {
		return s[:maxAuditDetail] + "…"
	}
	return s
}

// sanitize replaces control characters and bounds length, so a value that
// reached a rendered field cannot inject a line into a log.
func sanitize(s string) string {
	const maxField = 256
	if len(s) > maxField {
		s = s[:maxField] + "…"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}
