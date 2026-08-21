package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// Internal reasons. These never reach a caller — Authenticate returns
// ErrUnauthenticated — and exist so the audit record can say what happened
// (ADR-0001 §2.2).
var (
	errKindViolation   = errors.New("factor contribution disagrees with its declared kind")
	errEmptyNode       = errors.New("empty policy node denies")
	errNoContributions = errors.New("no contributions")
	errSubjectConflict = errors.New("subject-bearing factors disagree")
	errExpired         = errors.New("merged validity interval already expired")
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
