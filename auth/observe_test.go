package auth

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// capture is a logger sink that keeps every record.
type capture struct {
	records  []any
	severity []logger.Severity
}

func (c *capture) Log(s logger.Severity, payload any) {
	c.severity = append(c.severity, s)
	c.records = append(c.records, payload)
}

func (c *capture) text() string {
	var b strings.Builder
	for _, r := range c.records {
		b.WriteString(logRender(r))
		b.WriteString("\n")
	}
	return b.String()
}

func logRender(v any) string {
	if s, ok := v.(interface{ String() string }); ok {
		return s.String()
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}

// okFactor succeeds for a fixed subject.
type okFactor struct {
	subject string
	method  string
}

func (o okFactor) Kind() FactorKind { return FactorIdentity }
func (o okFactor) Verify(context.Context, *Request) (Contribution, error) {
	return Contribution{Method: o.method, Subject: o.subject, IssuedAt: time.Now()}, nil
}

type failFactor struct{ err error }

func (f failFactor) Kind() FactorKind { return FactorIdentity }
func (f failFactor) Verify(context.Context, *Request) (Contribution, error) {
	return Contribution{}, f.err
}

// The correlation ID is the only way an operator can act on a uniform
// ErrUnauthenticated. Before this existed the audit record was built and thrown
// away, which made the diagnosability promise false.
func TestPolicy_EmitsCorrelatedAttempts(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	var observed []Attempt

	p, err := NewPolicy(Leaf(okFactor{subject: "alice", method: "fake"}),
		Log(sink), Observe(func(a Attempt) { observed = append(observed, a) }))
	if err != nil {
		t.Fatal(err)
	}
	r := &Request{Peer: netip.MustParseAddrPort("10.0.0.4:5000")}
	id, err := p.Authenticate(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 {
		t.Fatalf("%d records, want 1", len(observed))
	}
	got := observed[0]
	if got.Outcome != "success" || got.Subject != "alice" || got.ID == "" {
		t.Errorf("record = %+v", got)
	}
	if len(got.Methods) != 1 || got.Methods[0] != "fake" {
		t.Errorf("methods = %v", got.Methods)
	}
	if got.Peer != "10.0.0.4:5000" {
		t.Errorf("peer = %q", got.Peer)
	}
	if id.Subject != "alice" {
		t.Errorf("identity = %+v", id)
	}
	if len(sink.severity) != 1 || sink.severity[0] != logger.SeverityInfo {
		t.Errorf("severity = %v, want Info for a success", sink.severity)
	}
}

// A failed login is the system working, so it must not log at an error level —
// that trains operators to ignore the level that matters.
func TestPolicy_FailureLogsAtNotice(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	p, err := NewPolicy(Leaf(failFactor{err: errors.New("nope")}), Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), &Request{}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
	if len(sink.severity) != 1 || sink.severity[0] != logger.SeverityNotice {
		t.Errorf("severity = %v, want Notice for a failure", sink.severity)
	}
	if !strings.Contains(sink.text(), "outcome=failure") {
		t.Errorf("log = %q", sink.text())
	}
	if !strings.Contains(sink.text(), "nope") {
		t.Error("the internal reason must reach the operator's log, since the caller never sees it")
	}
}

// A backoff refusal and a wrong credential are the SAME error to the caller and
// DIFFERENT lines in the log: a flood of "throttled" means something an operator
// must be able to see.
func TestPolicy_ThrottledOutcomeIsDistinctInTheLog(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{}
	mem := NewMemTracker(16, Backoff{Threshold: 0, Base: time.Hour, Max: time.Hour, Forget: 2 * time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	sink := &capture{}
	p, err := NewPolicy(Leaf(th), Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	r := attempt("alice", "10.0.0.5:1")

	// First attempt fails on the credential and locks the key.
	if _, err := p.Authenticate(context.Background(), r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
	// Second is refused for backoff.
	if _, err := p.Authenticate(context.Background(), r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
	text := sink.text()
	if !strings.Contains(text, "outcome=failure") {
		t.Errorf("no credential-failure line: %q", text)
	}
	if !strings.Contains(text, "outcome=throttled") {
		t.Errorf("a backoff refusal must be distinguishable in the log: %q", text)
	}
}

// RULES.md #1: no credential material in logs, ever.
func TestAttempt_NeverCarriesCredentialMaterial(t *testing.T) {
	t.Parallel()
	const secret = "hunter2-do-not-log-me"
	sink := &capture{}
	p, err := NewPolicy(Leaf(failFactor{err: errors.New("bad credential")}), Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	r := &Request{
		Peer:        netip.MustParseAddrPort("10.0.0.6:1"),
		Credentials: map[string]Secret{"password": NewSecret(secret)},
	}
	if _, err := p.Authenticate(context.Background(), r); err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(sink.text(), secret) {
		t.Fatal("the credential reached the log")
	}
}

// A newline in a logged field is how a log file gets forged entries.
func TestAttempt_StripsControlCharacters(t *testing.T) {
	t.Parallel()
	a := Attempt{
		ID:      "abc",
		Outcome: "failure",
		Subject: "alice\nauth attempt=fake outcome=success subject=root",
		Peer:    "10.0.0.1\x00:1",
		Reasons: []AuditReason{{Stage: "leaf", Detail: "line1\nline2"}},
	}
	got := a.String()
	if strings.Contains(got, "\n") || strings.Contains(got, "\x00") {
		t.Errorf("control characters survived: %q", got)
	}
	if !strings.Contains(got, "attempt=abc") {
		t.Errorf("rendering lost the correlation ID: %q", got)
	}
	// Long fields are truncated so one enormous claim cannot flood the log.
	long := Attempt{ID: "x", Outcome: "failure", Subject: strings.Repeat("a", 5000)}
	if len(long.String()) > 1000 {
		t.Errorf("an oversized field was not truncated: %d bytes", len(long.String()))
	}
}

// Omitting the option must be safe: the default sink discards.
func TestPolicy_DefaultLoggerIsNop(t *testing.T) {
	t.Parallel()
	p, err := NewPolicy(Leaf(okFactor{subject: "alice", method: "fake"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), &Request{}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolicy(Leaf(okFactor{subject: "a", method: "m"}), Log(nil), Observe(nil)); err != nil {
		t.Errorf("nil options must be ignored, not fatal: %v", err)
	}
}
