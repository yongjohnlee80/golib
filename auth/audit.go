package auth

import (
	"crypto/rand"
	"encoding/hex"
)

// Internal reasons. These never reach a caller — Authenticate returns
// ErrUnauthenticated — and exist so the audit record can say what happened
// (ADR-0001 §2.2).
// They are [Reason] values, not errors.New, so their text is compile-time and
// the audit trail may record it verbatim.
const (
	errKindViolation   = Reason("factor contribution disagrees with its declared kind")
	errEmptyNode       = Reason("empty policy node denies")
	errNoContributions = Reason("no contributions")
	errSubjectConflict = Reason("subject-bearing factors disagree")
	errExpired         = Reason("merged validity interval already expired")
	errNilRequest      = Reason("nil request")
)

// Audit is the private, structured record of one authentication attempt. It
// carries a correlation ID so an operator can tie a user's report to a specific
// attempt without the caller-visible error revealing anything (ADR-0001 §2.2).
//
// It never holds credential material.
type Audit struct {
	AttemptID string
	Reasons   []AuditReason
}

// AuditReason is one recorded step.
type AuditReason struct {
	Stage  string
	Detail string
}

func newAudit() *Audit {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return &Audit{AttemptID: hex.EncodeToString(b[:])}
}

func (a *Audit) note(stage, detail string) {
	if a == nil {
		return
	}
	a.Reasons = append(a.Reasons, AuditReason{Stage: stage, Detail: detail})
}
