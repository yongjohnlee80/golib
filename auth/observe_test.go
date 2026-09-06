package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
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
	// A Reason, because that is what a factor uses to assert its text is
	// credential-free. An arbitrary error is recorded by type only — see
	// TestPolicy_FactorErrorTextNeverReachesTheLog.
	p, err := NewPolicy(Leaf(failFactor{err: Reason("no such credential")}), Log(sink))
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
	if !strings.Contains(sink.text(), "no such credential") {
		t.Error("the internal reason must reach the operator's log, since the caller never sees it")
	}
}

// A backoff refusal and a wrong credential are the SAME error to the caller and
// DIFFERENT lines in the log: a flood of "throttled" means something an operator
// must be able to see.
func TestPolicy_ThrottledOutcomeIsDistinctInTheLog(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{}
	mem := tracker(t, 16, Backoff{Threshold: 0, Base: time.Hour, Max: time.Hour, Forget: 2 * time.Hour})
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

// --- negative controls: these fixtures actually contain the secret -----------

const leaked = "hunter2-LEAKED-CREDENTIAL"

// leakyFactor is third-party code doing something entirely plausible:
// formatting the presented credential into its error. That error must never
// reach the audit trail.
type leakyFactor struct{}

func (leakyFactor) Kind() FactorKind { return FactorIdentity }
func (leakyFactor) Verify(_ context.Context, r *Request) (Contribution, error) {
	return Contribution{}, fmt.Errorf("rejected credential %q for subject %q",
		r.Credentials["password"].Reveal(), r.Credentials["subject"].Reveal())
}

// The previous test used a generic "bad credential" error, so it could not fail
// on this leak. This fixture's error text CONTAINS the secret, which is the only
// way the assertion means anything.
func TestPolicy_FactorErrorTextNeverReachesTheLog(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	var observed []Attempt
	p, err := NewPolicy(Leaf(leakyFactor{}), Log(sink), Observe(func(a Attempt) { observed = append(observed, a) }))
	if err != nil {
		t.Fatal(err)
	}
	r := &Request{Credentials: map[string]Secret{
		"password": NewSecret(leaked),
		"subject":  NewSecret("alice"),
	}}
	if _, err := p.Authenticate(context.Background(), r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}

	if strings.Contains(sink.text(), leaked) {
		t.Fatalf("the factor's error text carried a credential into the log: %q", sink.text())
	}
	if len(observed) != 1 {
		t.Fatalf("%d records, want 1", len(observed))
	}
	for _, reason := range observed[0].Reasons {
		if strings.Contains(reason.Detail, leaked) {
			t.Fatalf("the audit record carries the credential: %q", reason.Detail)
		}
	}
	// The record must still be USEFUL: an opaque marker naming the type, so an
	// operator knows a factor rejected rather than the tree being malformed.
	if !strings.Contains(sink.text(), "opaque") {
		t.Errorf("an unattributed error should be recorded by type: %q", sink.text())
	}
}

// An error that ASSERTS its text is credential-free contributes it verbatim,
// which is what keeps our own factors diagnosable.
func TestPolicy_SafeAuditDetailIsRecordedVerbatim(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	// Reason wraps: the dynamic half is dropped, the fixed half survives.
	const fixed = Reason("password: does not match")
	wrapped := fmt.Errorf("%w: subject %q", fixed, leaked)
	p, err := NewPolicy(Leaf(failFactor{err: wrapped}), Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), &Request{}); err == nil {
		t.Fatal("expected failure")
	}
	got := sink.text()
	if !strings.Contains(got, "password: does not match") {
		t.Errorf("the fixed reason must survive: %q", got)
	}
	if strings.Contains(got, leaked) {
		t.Errorf("the DYNAMIC half of a wrapped Reason must be dropped: %q", got)
	}
}

// A Method name comes from the factor, so it is third-party text that reaches a
// rendered log field.
func TestAttempt_ForgedMethodNameCannotInjectALogLine(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	p, err := NewPolicy(Leaf(okFactor{subject: "alice", method: "password\nauth attempt=00 outcome=success subject=root"}), Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), &Request{}); err != nil {
		t.Fatal(err)
	}
	got := sink.text()
	// The rendered record is one line; the trailing newline capture adds is the
	// only one permitted.
	if n := strings.Count(strings.TrimSuffix(got, "\n"), "\n"); n != 0 {
		t.Fatalf("a forged Method name produced %d extra log lines: %q", n, got)
	}
	// The fragment's TEXT legitimately survives — it is neutralized, not
	// censored. What must not survive is the line break that would have made it
	// a separate record.
	if !strings.Contains(got, "password?auth") {
		t.Errorf("the newline was not replaced: %q", got)
	}
}

// A subject is also factor-supplied on the success path.
func TestAttempt_ForgedSubjectCannotInjectALogLine(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	p, err := NewPolicy(Leaf(okFactor{subject: "alice\nauth attempt=00 outcome=success subject=root", method: "m"}), Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), &Request{}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSuffix(sink.text(), "\n"), "\n"); n != 0 {
		t.Fatalf("a forged subject produced %d extra log lines: %q", n, sink.text())
	}
}

// EXACTLY ONE audit record per authentication is promised, and a nil request
// was a silent hole in that trail.
func TestPolicy_NilRequestStillEmitsOneAttempt(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	var observed []Attempt
	p, err := NewPolicy(Leaf(okFactor{subject: "a", method: "m"}), Log(sink),
		Observe(func(a Attempt) { observed = append(observed, a) }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), nil); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
	if len(observed) != 1 {
		t.Fatalf("%d records for a nil request, want exactly 1", len(observed))
	}
	if observed[0].ID == "" || observed[0].Outcome != "failure" {
		t.Errorf("record = %+v", observed[0])
	}
}

// The per-request sink is what makes correlation possible under concurrency;
// the global observer cannot attribute an attempt to a caller.
func TestWithAttemptSink_IsPerCall(t *testing.T) {
	t.Parallel()
	p, err := NewPolicy(Leaf(okFactor{subject: "alice", method: "m"}))
	if err != nil {
		t.Fatal(err)
	}
	const n = 24
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := WithAttemptSink(context.Background(), func(a Attempt) { ids[i] = a.ID })
			if _, err := p.Authenticate(ctx, &Request{}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, id := range ids {
		if id == "" {
			t.Fatalf("call %d received no attempt ID", i)
		}
		if seen[id] {
			t.Fatalf("call %d reused ID %q — the sink is not per-call", i, id)
		}
		seen[id] = true
	}
	// A nil sink must be ignored, not panic.
	if got := WithAttemptSink(context.Background(), nil); got == nil {
		t.Error("WithAttemptSink(ctx, nil) must return a usable context")
	}
}

// The other half of the contract: a DYNAMIC error from a backend must stay
// opaque, or the migration would have traded a leak for a different leak.
func TestBackendErrorTextStaysOpaque(t *testing.T) {
	t.Parallel()
	sink := &capture{}
	// The shape of a store failure: a driver error carrying a DSN with a
	// password in it.
	backend := fmt.Errorf("dial tcp: postgres://admin:%s@db:5432 refused", leaked)
	p, err := NewPolicy(Leaf(failFactor{err: backend}), Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), &Request{}); err == nil {
		t.Fatal("expected failure")
	}
	got := sink.text()
	if strings.Contains(got, leaked) {
		t.Fatalf("a backend error's text reached the log: %q", got)
	}
	if !strings.Contains(got, "opaque error of type") {
		t.Errorf("an unattributed error must be recorded by type: %q", got)
	}
}

// A backoff refusal is operationally distinct from a wrong credential, and must
// stay so wherever it sits in the tree. Keeping only the last branch error made
// the outcome depend on DECLARATION ORDER — so a fallback policy, the exact
// topology where Any is used, could hide it.
func TestPolicy_ThrottledSurvivesAnyInEitherOrder(t *testing.T) {
	t.Parallel()

	orders := map[string][]Node{
		"throttled first": {Leaf(failFactor{err: ErrThrottled}), Leaf(failFactor{err: Reason("plain failure")})},
		"throttled last":  {Leaf(failFactor{err: Reason("plain failure")}), Leaf(failFactor{err: ErrThrottled})},
	}
	for name, children := range orders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sink := &capture{}
			p, err := NewPolicy(Any(children...), Log(sink))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Authenticate(context.Background(), &Request{}); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(sink.text(), "outcome=throttled") {
				t.Errorf("a backoff refusal inside Any logged as %q — the outcome depends "+
					"on which branch happened to be last", sink.text())
			}
			// Both branch reasons must still be recorded.
			for _, want := range []string{"too many failed attempts", "plain failure"} {
				if !strings.Contains(sink.text(), want) {
					t.Errorf("branch reason %q missing from %q", want, sink.text())
				}
			}
		})
	}
}

// A single context key meant the middleware's sink silently REPLACED an outer
// one, so a caller's request-scoped observer just stopped firing.
func TestWithAttemptSink_Composes(t *testing.T) {
	t.Parallel()
	p, err := NewPolicy(Leaf(okFactor{subject: "alice", method: "m"}))
	if err != nil {
		t.Fatal(err)
	}
	var outer, inner []Attempt
	ctx := WithAttemptSink(context.Background(), func(a Attempt) { outer = append(outer, a) })
	ctx = WithAttemptSink(ctx, func(a Attempt) { inner = append(inner, a) })

	if _, err := p.Authenticate(ctx, &Request{}); err != nil {
		t.Fatal(err)
	}
	if len(outer) != 1 {
		t.Fatalf("the outer sink fired %d times, want 1 — an inner sink shadowed it", len(outer))
	}
	if len(inner) != 1 {
		t.Fatalf("the inner sink fired %d times, want 1", len(inner))
	}
	if outer[0].ID != inner[0].ID {
		t.Errorf("sinks saw different attempts: %q vs %q", outer[0].ID, inner[0].ID)
	}

	// Three deep, and a nil in the middle must not break the chain.
	var third []Attempt
	ctx = WithAttemptSink(ctx, nil)
	ctx = WithAttemptSink(ctx, func(a Attempt) { third = append(third, a) })
	if _, err := p.Authenticate(ctx, &Request{}); err != nil {
		t.Fatal(err)
	}
	if len(outer) != 2 || len(inner) != 2 || len(third) != 1 {
		t.Errorf("chain fired outer=%d inner=%d third=%d, want 2/2/1", len(outer), len(inner), len(third))
	}
}
