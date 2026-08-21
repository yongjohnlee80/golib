package auth

import (
	"context"
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
		// Method names come from the factor, which is third-party code: a
		// Method of "password\nforged=true" would otherwise write a second,
		// fabricated log line. Every rendered field is sanitized, and the count
		// is bounded so one policy with a thousand leaves cannot flood a line.
		const maxMethods = 16
		for i, m := range a.Methods {
			if i == maxMethods {
				b.WriteString(",…")
				break
			}
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(sanitize(m))
		}
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

// attemptSinkKey carries a PER-REQUEST observer.
type attemptSinkKey struct{}

// WithAttemptSink attaches a sink that receives the [Attempt] for
// authentications performed under ctx.
//
// [Observe] is policy-global and therefore cannot answer "which of the twelve
// requests in flight was that?" — under concurrency, two attempts from the same
// peer are indistinguishable to it. This is the per-call channel: an adapter
// installs a sink on the request's context, and the ID it captures belongs to
// that request and no other.
//
// The sink runs on the authenticating goroutine, so it must not block.
func WithAttemptSink(ctx context.Context, fn func(Attempt)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, attemptSinkKey{}, fn)
}

// attemptSinkFrom returns the per-request sink, if any.
func attemptSinkFrom(ctx context.Context) func(Attempt) {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(attemptSinkKey{}).(func(Attempt))
	return fn
}

// emit records one attempt.
func (p *policy) emit(ctx context.Context, a Attempt) {
	switch a.Outcome {
	case "success":
		logger.Info(p.log, a)
	default:
		logger.Notice(p.log, a)
	}
	if p.observe != nil {
		p.observe(a)
	}
	if sink := attemptSinkFrom(ctx); sink != nil {
		sink(a)
	}
}
