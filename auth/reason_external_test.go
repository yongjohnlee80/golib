// This file is package auth_test, not package auth: it imports the method
// subpackages, and those import auth. An internal test file cannot.
package auth_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/ipallow"
	"github.com/yongjohnlee80/golib/auth/mtls"
	"github.com/yongjohnlee80/golib/auth/password"
	"github.com/yongjohnlee80/golib/auth/sshkey"
	"github.com/yongjohnlee80/golib/auth/token"
	"github.com/yongjohnlee80/golib/logger"
)

// lineSink keeps the rendered text of every record.
type lineSink struct{ lines []string }

func (l *lineSink) Log(_ logger.Severity, payload any) {
	if s, ok := payload.(interface{ String() string }); ok {
		l.lines = append(l.lines, s.String())
		return
	}
	if e, ok := payload.(error); ok {
		l.lines = append(l.lines, e.Error())
	}
}

func (l *lineSink) text() string { return strings.Join(l.lines, "\n") }

// rejecting is a factor that always fails with a fixed error.
type rejecting struct{ err error }

func (rejecting) Kind() auth.FactorKind { return auth.FactorIdentity }
func (r rejecting) Verify(context.Context, *auth.Request) (auth.Contribution, error) {
	return auth.Contribution{}, r.err
}

// TestBuiltInReasonsAreDistinguishable is the cross-package half of the
// safe-reason contract.
//
// §2.2 promises the private record says WHAT happened. A migration that missed a
// package collapses every failure in it to one opaque line, so a malformed
// token, an expired one and a consumed one become indistinguishable to the
// operator — losing exactly the value §2.2 exists to provide. The first cut of
// the migration missed four packages, and only a test that reaches across them
// can see that.
func TestBuiltInReasonsAreDistinguishable(t *testing.T) {
	t.Parallel()

	cases := map[string]error{
		"token malformed":     token.ErrMalformed,
		"token expired":       token.ErrExpired,
		"token consumed":      token.ErrConsumed,
		"token not found":     token.ErrNotFound,
		"throttled":           auth.ErrThrottled,
		"tracker down":        auth.ErrTrackerUnavailable,
		"password mismatch":   password.ErrMismatch,
		"password encoding":   password.ErrEncoding,
		"password no cred":    password.ErrNoCredential,
		"sshkey namespace":    sshkey.ErrNamespace,
		"sshkey identity":     sshkey.ErrIdentity,
		"sshkey bad sig":      sshkey.ErrBadSignature,
		"mtls no chain":       mtls.ErrNoVerifiedChain,
		"mtls expired":        mtls.ErrExpired,
		"mtls not clientauth": mtls.ErrNotClientAuth,
		"ipallow not allowed": ipallow.ErrNotAllowed,
		"ipallow no address":  ipallow.ErrNoAddress,
		"ipallow empty":       ipallow.ErrEmptyPolicy,
	}

	seen := make(map[string]string, len(cases))
	for name, sentinel := range cases {
		if sentinel == nil {
			continue
		}
		sink := &lineSink{}
		p, err := auth.NewPolicy(auth.Leaf(rejecting{err: sentinel}), auth.Log(sink))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Authenticate(context.Background(), &auth.Request{}); err == nil {
			t.Fatalf("%s: expected failure", name)
		}
		got := sink.text()
		if strings.Contains(got, "opaque error of type") {
			t.Errorf("%s: recorded as opaque — this sentinel is still errors.New, so every "+
				"failure in its package logs identically: %q", name, got)
			continue
		}
		if !strings.Contains(got, sentinel.Error()) {
			t.Errorf("%s: the sentinel's own text is missing: %q", name, got)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s and %s produce the same log line %q", name, prev, got)
		}
		seen[got] = name
	}
}

// The wrapping case, cross-package: a sentinel wrapped with dynamic detail
// contributes its FIXED text and drops the rest.
func TestWrappedBuiltInDropsDynamicDetail(t *testing.T) {
	t.Parallel()
	const secret = "hunter2-LEAKED-CREDENTIAL"
	sink := &lineSink{}
	// The exact shape auth/password uses internally.
	wrapped := wrapWith(password.ErrParams, secret)
	p, err := auth.NewPolicy(auth.Leaf(rejecting{err: wrapped}), auth.Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate(context.Background(), &auth.Request{}); err == nil {
		t.Fatal("expected failure")
	}
	got := sink.text()
	if !strings.Contains(got, password.ErrParams.Error()) {
		t.Errorf("the wrapped sentinel's fixed text must survive: %q", got)
	}
	if strings.Contains(got, secret) {
		t.Errorf("the dynamic half of a wrapped sentinel reached the log: %q", got)
	}
}

// An end-to-end check through a REAL factor, rather than a stub returning a
// sentinel: a consumed single-use token must record why.
func TestRealTokenFactorRecordsWhy(t *testing.T) {
	t.Parallel()
	store := token.NewMemStore(16)
	secret, err := token.NewIssuer(store).Issue("alice", time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	sink := &lineSink{}
	p, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)), auth.Log(sink))
	if err != nil {
		t.Fatal(err)
	}
	req := func() *auth.Request {
		return &auth.Request{Credentials: map[string]auth.Secret{
			token.DefaultScheme: auth.NewSecret(secret.Reveal()),
		}}
	}
	if _, err := p.Authenticate(context.Background(), req()); err != nil {
		t.Fatalf("first use must succeed: %v", err)
	}
	if _, err := p.Authenticate(context.Background(), req()); err == nil {
		t.Fatal("a single-use token must not verify twice")
	}
	got := sink.text()
	if strings.Contains(got, "opaque error of type") {
		t.Errorf("a real token failure recorded as opaque: %q", got)
	}
	if !strings.Contains(got, "outcome=failure") {
		t.Errorf("log = %q", got)
	}
}

// wrapWith reproduces the fmt.Errorf("%w: ...") shape the method packages use.
func wrapWith(sentinel error, detail string) error {
	return fmt.Errorf("%w: subject %q", sentinel, detail)
}
