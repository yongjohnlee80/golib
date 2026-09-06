package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- test factors -----------------------------------------------------------

type fakeFactor struct {
	name    string
	kind    FactorKind
	subject string
	issued  time.Time
	expires time.Time
	err     error
	calls   *[]string
}

func (f fakeFactor) Kind() FactorKind { return f.kind }

func (f fakeFactor) Verify(context.Context, *Request) (Contribution, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name)
	}
	if f.err != nil {
		return Contribution{}, f.err
	}
	return Contribution{Method: f.name, Subject: f.subject, IssuedAt: f.issued, ExpiresAt: f.expires}, nil
}

func id(name, subject string) fakeFactor {
	return fakeFactor{name: name, kind: FactorIdentity, subject: subject}
}
func ctxf(name string) fakeFactor { return fakeFactor{name: name, kind: FactorContextual} }
func failing(name string, kind FactorKind) fakeFactor {
	return fakeFactor{name: name, kind: kind, err: errors.New("no")}
}

func req() *Request { return &Request{} }

// --- criterion 2c: tree validation on the FINISHED tree ---------------------

func TestNewPolicy_TreeValidation(t *testing.T) {
	t.Parallel()

	mtls, ticket, challenge, ipallow := id("mtls", "u"), id("ticket", "u"), id("challenge", "u"), ctxf("ipallow")

	cases := []struct {
		name    string
		root    Node
		wantErr error
	}{
		// The recommended policy and its optional wrapper: both valid.
		{"Any(mtls, All(ipallow, challenge))", Any(Leaf(mtls), All(Leaf(ipallow), Leaf(challenge))), nil},
		{"Any(ticket, mtls, challenge)", Any(Leaf(ticket), Leaf(mtls), Leaf(challenge)), nil},
		{"All(ipallow, Any(ticket, mtls))", All(Leaf(ipallow), Any(Leaf(ticket), Leaf(mtls))), nil},
		// A branch that admits on context alone can never be a root.
		{"Any(ipallow, mtls)", Any(Leaf(ipallow), Leaf(mtls)), ErrNoIdentityProof},
		{"contextual leaf root", Leaf(ipallow), ErrNoIdentityProof},
		{"All(ipallow)", All(Leaf(ipallow)), ErrNoIdentityProof},
		// Empty nodes are not identity-bearing, so they cannot be a root...
		{"empty Any", Any(), ErrNoIdentityProof},
		{"empty All", All(), ErrNoIdentityProof},
		// ...and a nil root is its own error, not a panic.
		{"nil root", nil, ErrEmptyPolicy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewPolicy(c.root)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if c.wantErr == nil && p == nil {
				t.Error("valid policy returned nil")
			}
			if c.wantErr != nil && p != nil {
				t.Error("invalid policy returned non-nil Policy")
			}
		})
	}
}

// An empty node nested below a valid root DENIES at evaluation — distinct from
// the construction error a bad root gives.
func TestPolicy_EmptyNodeDeniesAtEvaluation(t *testing.T) {
	t.Parallel()

	p, err := NewPolicy(All(Leaf(id("mtls", "u")), All()))
	if err != nil {
		t.Fatalf("construction: %v", err)
	}
	got, err := p.Authenticate(context.Background(), req())
	if !errors.Is(err, ErrUnauthenticated) || got != nil {
		t.Errorf("Authenticate = (%v, %v), want (nil, ErrUnauthenticated)", got, err)
	}
}

// --- order, short-circuit, uniform error ------------------------------------

func TestPolicy_OrderAndShortCircuit(t *testing.T) {
	t.Parallel()

	t.Run("All evaluates in order and stops at the first failure", func(t *testing.T) {
		var calls []string
		a := fakeFactor{name: "a", kind: FactorIdentity, subject: "u", calls: &calls}
		b := fakeFactor{name: "b", kind: FactorContextual, err: errors.New("no"), calls: &calls}
		c := fakeFactor{name: "c", kind: FactorContextual, calls: &calls}
		p, _ := NewPolicy(All(Leaf(a), Leaf(b), Leaf(c)))
		if _, err := p.Authenticate(context.Background(), req()); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("err = %v", err)
		}
		if strings.Join(calls, ",") != "a,b" {
			t.Errorf("calls = %v, want a,b (c must not run)", calls)
		}
	})

	t.Run("Any stops at the first success", func(t *testing.T) {
		var calls []string
		a := fakeFactor{name: "a", kind: FactorIdentity, err: errors.New("no"), calls: &calls}
		b := fakeFactor{name: "b", kind: FactorIdentity, subject: "u", calls: &calls}
		c := fakeFactor{name: "c", kind: FactorIdentity, subject: "u", calls: &calls}
		p, _ := NewPolicy(Any(Leaf(a), Leaf(b), Leaf(c)))
		if _, err := p.Authenticate(context.Background(), req()); err != nil {
			t.Fatalf("err = %v", err)
		}
		if strings.Join(calls, ",") != "a,b" {
			t.Errorf("calls = %v, want a,b (c must not run)", calls)
		}
	})
}

// Every failure cause presents the SAME outward error: no oracle.
func TestPolicy_UniformOutwardError(t *testing.T) {
	t.Parallel()

	roots := map[string]Node{
		"factor failed":     All(Leaf(failing("x", FactorIdentity))),
		"kind violation":    All(Leaf(fakeFactor{name: "liar", kind: FactorIdentity})), // identity with no subject
		"subject conflict":  All(Leaf(id("a", "alice")), Leaf(id("b", "bob"))),
		"empty nested node": All(Leaf(id("a", "u")), Any()),
		"expired":           All(Leaf(fakeFactor{name: "old", kind: FactorIdentity, subject: "u", expires: time.Now().Add(-time.Minute)})),
	}
	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			p, err := NewPolicy(root)
			if err != nil {
				t.Fatalf("construction: %v", err)
			}
			got, err := p.Authenticate(context.Background(), req())
			if got != nil {
				t.Errorf("Identity = %+v, want nil", got)
			}
			if err == nil || err.Error() != ErrUnauthenticated.Error() {
				t.Errorf("err = %v, want exactly %v", err, ErrUnauthenticated)
			}
		})
	}
}

// --- criterion 2b: identity merging ----------------------------------------

func TestPolicy_IdentityMerging(t *testing.T) {
	t.Parallel()

	t.Run("disagreeing subjects fail", func(t *testing.T) {
		p, _ := NewPolicy(All(Leaf(id("a", "alice")), Leaf(id("b", "bob"))))
		if got, err := p.Authenticate(context.Background(), req()); err == nil || got != nil {
			t.Errorf("got (%v, %v), want (nil, error)", got, err)
		}
	})

	t.Run("agreeing subjects accumulate proofs in order", func(t *testing.T) {
		p, _ := NewPolicy(All(Leaf(ctxf("ipallow")), Leaf(id("mtls", "u")), Leaf(id("totp", "u"))))
		got, err := p.Authenticate(context.Background(), req())
		if err != nil {
			t.Fatal(err)
		}
		if got.Subject != "u" {
			t.Errorf("Subject = %q", got.Subject)
		}
		var names []string
		for _, pr := range got.Proofs {
			names = append(names, fmt.Sprintf("%s/%s", pr.Method, pr.Kind))
		}
		want := "ipallow/contextual,mtls/identity,totp/identity"
		if strings.Join(names, ",") != want {
			t.Errorf("proofs = %v, want %s", names, want)
		}
	})

	t.Run("a contextual factor may not carry a subject", func(t *testing.T) {
		liar := fakeFactor{name: "liar", kind: FactorContextual, subject: "sneaky"}
		p, _ := NewPolicy(All(Leaf(id("ok", "u")), Leaf(liar)))
		if _, err := p.Authenticate(context.Background(), req()); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("a contextual contribution with a Subject must fail; err = %v", err)
		}
	})

	t.Run("an identity factor must carry a subject", func(t *testing.T) {
		liar := fakeFactor{name: "liar", kind: FactorIdentity}
		p, _ := NewPolicy(All(Leaf(liar)))
		if _, err := p.Authenticate(context.Background(), req()); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("an identity contribution without a Subject must fail; err = %v", err)
		}
	})
}

// --- criterion 2d: the validity interval is an intersection ----------------

func TestPolicy_ValidityInterval(t *testing.T) {
	t.Parallel()

	early, late := time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
	soon, later := time.Now().Add(time.Minute), time.Now().Add(time.Hour)

	t.Run("latest IssuedAt, minimum finite ExpiresAt", func(t *testing.T) {
		a := fakeFactor{name: "a", kind: FactorIdentity, subject: "u", issued: early, expires: later}
		b := fakeFactor{name: "b", kind: FactorIdentity, subject: "u", issued: late, expires: soon}
		p, _ := NewPolicy(All(Leaf(a), Leaf(b)))
		got, err := p.Authenticate(context.Background(), req())
		if err != nil {
			t.Fatal(err)
		}
		if !got.IssuedAt.Equal(late) {
			t.Errorf("IssuedAt = %v, want the LATEST (%v)", got.IssuedAt, late)
		}
		if !got.ExpiresAt.Equal(soon) {
			t.Errorf("ExpiresAt = %v, want the MINIMUM (%v)", got.ExpiresAt, soon)
		}
	})

	t.Run("a zero expiry imposes no bound", func(t *testing.T) {
		// A static contextual observation (ipallow) contributes no expiry, so it
		// must not shorten — or unbound — the interval.
		static := ctxf("ipallow")
		bounded := fakeFactor{name: "ticket", kind: FactorIdentity, subject: "u", expires: soon}
		p, _ := NewPolicy(All(Leaf(static), Leaf(bounded)))
		got, err := p.Authenticate(context.Background(), req())
		if err != nil {
			t.Fatal(err)
		}
		if !got.ExpiresAt.Equal(soon) {
			t.Errorf("ExpiresAt = %v, want %v — a zero expiry must not win the minimum", got.ExpiresAt, soon)
		}
	})

	t.Run("all-zero expiries leave the identity unbounded", func(t *testing.T) {
		p, _ := NewPolicy(All(Leaf(ctxf("ipallow")), Leaf(id("mtls", "u"))))
		got, err := p.Authenticate(context.Background(), req())
		if err != nil {
			t.Fatal(err)
		}
		if !got.ExpiresAt.IsZero() {
			t.Errorf("ExpiresAt = %v, want zero (unbounded)", got.ExpiresAt)
		}
	})
}

// --- structural redaction ---------------------------------------------------

func TestSecret_RedactedEverywhere(t *testing.T) {
	t.Parallel()

	const material = "hunter2-super-secret"
	s := NewSecret(material)

	// Every formatting path fmt consults, plus both marshalers, plus a verb
	// Stringer alone would not cover (%d).
	var rendered []string
	rendered = append(rendered,
		s.String(),
		fmt.Sprint(s),
		fmt.Sprintf("%v", s),
		fmt.Sprintf("%+v", s),
		fmt.Sprintf("%#v", s),
		fmt.Sprintf("%s", s),
		fmt.Sprintf("%q", s),
		fmt.Sprintf("%d", s),
		fmt.Sprintf("%x", s),
	)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	rendered = append(rendered, string(b))
	tb, err := s.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	rendered = append(rendered, string(tb))

	// ...and through a nested struct, a pointer, and a wrapped error.
	type creds struct{ Password Secret }
	c := creds{Password: s}
	rendered = append(rendered,
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%+v", c),
		fmt.Sprintf("%#v", c),
		fmt.Sprintf("%v", &c),
		fmt.Errorf("login failed for %v", s).Error(),
	)
	nested, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	rendered = append(rendered, string(nested))

	for i, out := range rendered {
		if strings.Contains(out, material) {
			t.Errorf("rendering %d leaked the secret: %s", i, out)
		}
	}
	// Reveal is the one deliberate exit.
	if s.Reveal() != material {
		t.Error("Reveal must return the material")
	}
	if s.Len() != len(material) {
		t.Error("Len must report the length without revealing")
	}
}

// A failed Verify must yield the ZERO Contribution even if the factor tries to
// return one alongside the error (the Factor invariant).
func TestPolicy_ErrorImpliesZeroContribution(t *testing.T) {
	t.Parallel()

	p, _ := NewPolicy(Any(Leaf(sneaky{}), Leaf(id("fallback", "u"))))
	got, err := p.Authenticate(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "u" {
		t.Errorf("Subject = %q, want the fallback's — a failing factor must contribute nothing", got.Subject)
	}
	for _, pr := range got.Proofs {
		if pr.Method == "sneaky" {
			t.Error("a failing factor's contribution was merged")
		}
	}
}

type sneaky struct{}

func (sneaky) Kind() FactorKind { return FactorIdentity }
func (sneaky) Verify(context.Context, *Request) (Contribution, error) {
	return Contribution{Method: "sneaky", Subject: "attacker"}, errors.New("failed")
}
