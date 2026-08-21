package auth

import (
	"strings"

	"github.com/yongjohnlee80/golib/logger"
)

// Attempt is the record emitted for every authentication attempt (ADR-0001
// §2.7).
//
// It carries NO credential material — not the password, not the token, not the
// signature. What it does carry is the correlation ID, which is the whole point:
// the caller-visible error is a uniform [ErrUnauthenticated] that says nothing,
// so an operator diagnosing a user's report needs a handle that ties the two
// together without the error itself revealing anything.
type Attempt struct {
	// ID correlates this record with one attempt. It is random per attempt, so
	// it distinguishes attempts without distinguishing causes.
	ID string

	// Outcome is "success", "failure", or "throttled".
	Outcome string

	// Subject is set ONLY on success, where it has been proven. On failure the
	// claim is unverified and belongs to the factor's own reasons, not to a
	// field that reads like an established fact.
	Subject string

	// Methods are the factors that contributed, in order. Present on success.
	Methods []string

	// Peer is the transport's view of the client, or empty.
	Peer string

	// Reasons is the internal failure detail. Present on failure.
	Reasons []AuditReason
}

// String renders the record without any credential material.
//
// Reasons are internal strings, but Subject and Peer derive from request data,
// so control characters are stripped: a newline in a logged field is how a log
// file gets forged entries.
func (a Attempt) String() string {
	var b strings.Builder
	b.WriteString("auth attempt=")
	b.WriteString(a.ID)
	b.WriteString(" outcome=")
	b.WriteString(a.Outcome)
	if a.Subject != "" {
		b.WriteString(" subject=")
		b.WriteString(sanitize(a.Subject))
	}
	if len(a.Methods) > 0 {
		b.WriteString(" methods=")
		b.WriteString(strings.Join(a.Methods, ","))
	}
	if a.Peer != "" {
		b.WriteString(" peer=")
		b.WriteString(sanitize(a.Peer))
	}
	for _, r := range a.Reasons {
		b.WriteString(" [")
		b.WriteString(r.Stage)
		b.WriteString(": ")
		b.WriteString(sanitize(r.Detail))
		b.WriteString("]")
	}
	return b.String()
}

// sanitize replaces control characters, so a value taken from a request cannot
// inject a line into a log.
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

// PolicyOption configures [NewPolicy].
type PolicyOption func(*policy)

// Log sends one [Attempt] per authentication to l. Defaults to logger.Nop{}, so
// omitting it is safe and allocation-free.
//
// Severity is by outcome: success at Info, failure at Notice. A failed login is
// not an error — it is the system working — and logging it at Error trains
// operators to ignore the level that matters.
func Log(l logger.Logger) PolicyOption {
	return func(p *policy) {
		if l != nil {
			p.log = l
		}
	}
}

// Observe receives the same record as a callback, for a caller that wants to
// put the correlation ID in a response header or a CLI message.
//
// It runs on the authenticating goroutine, so it must not block.
func Observe(fn func(Attempt)) PolicyOption {
	return func(p *policy) { p.observe = fn }
}

// emit records one attempt.
func (p *policy) emit(a Attempt) {
	switch a.Outcome {
	case "success":
		logger.Info(p.log, a)
	default:
		logger.Notice(p.log, a)
	}
	if p.observe != nil {
		p.observe(a)
	}
}
