package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
)

// contextualFactor is the shape of an IP allowlist: it proves something about
// the request, never who made it.
type contextualFactor struct{ allow bool }

func (contextualFactor) Kind() auth.FactorKind { return auth.FactorContextual }
func (c contextualFactor) Verify(context.Context, *auth.Request) (auth.Contribution, error) {
	if !c.allow {
		return auth.Contribution{}, auth.Reason("test: not allowed")
	}
	return auth.Contribution{Method: "ipallow"}, nil
}

// claimingFactor names its principal before verifying, so auth.Throttle can key
// per-subject — the shape auth/password has.
type claimingFactor struct{ subject, password string }

func (claimingFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (c claimingFactor) Claim(r *auth.Request) string {
	if r == nil {
		return ""
	}
	return r.Credentials["subject"].Reveal()
}
func (c claimingFactor) Verify(_ context.Context, r *auth.Request) (auth.Contribution, error) {
	if r.Credentials["subject"].Reveal() != c.subject || r.Credentials["password"].Reveal() != c.password {
		return auth.Contribution{}, auth.Reason("test: wrong password")
	}
	return auth.Contribution{Method: "password", Subject: c.subject, IssuedAt: time.Now()}, nil
}

func TestRecommendedPolicy(t *testing.T) {
	t.Parallel()

	t.Run("no mechanism is a construction error", func(t *testing.T) {
		if _, err := RecommendedPolicy(nil); err == nil {
			t.Error("an empty policy must be an error, not a permissive default")
		}
		if _, err := RecommendedPolicy([]auth.Factor{nil, nil}); err == nil {
			t.Error("nil mechanisms must not compose into an empty Any")
		}
	})

	t.Run("any one mechanism admits", func(t *testing.T) {
		p, err := RecommendedPolicy([]auth.Factor{
			alwaysFactor{subject: "alice"},
			claimingFactor{subject: "bob", password: "pw"},
		})
		if err != nil {
			t.Fatal(err)
		}
		id, err := p.Authenticate(context.Background(), &auth.Request{})
		if err != nil {
			t.Fatal(err)
		}
		if id.Subject != "alice" {
			t.Errorf("subject = %q", id.Subject)
		}
	})

	t.Run("a contextual factor narrows but cannot admit", func(t *testing.T) {
		// The whole point: ipallow can refuse, but a policy satisfiable by it
		// alone must be impossible to construct.
		if _, err := RecommendedPolicy([]auth.Factor{contextualFactor{allow: true}}); err == nil {
			t.Fatal("a contextual-only policy was accepted — ADR-0001 §2.2.2 must refuse it")
		}

		p, err := RecommendedPolicy(
			[]auth.Factor{alwaysFactor{subject: "alice"}},
			contextualFactor{allow: false},
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Authenticate(context.Background(), &auth.Request{}); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Errorf("err = %v: a failing constraint must deny even a valid identity", err)
		}

		p, err = RecommendedPolicy(
			[]auth.Factor{alwaysFactor{subject: "alice"}},
			contextualFactor{allow: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Authenticate(context.Background(), &auth.Request{}); err != nil {
			t.Errorf("a satisfied constraint denied a valid identity: %v", err)
		}
	})
}

// Password is permitted and is the weakest option. The example policy must
// actually work, and must actually throttle.
func TestPasswordPolicyExample(t *testing.T) {
	t.Parallel()

	tracker, err := auth.NewMemTracker(64, auth.Backoff{
		Threshold: 2, Base: time.Hour, Max: time.Hour, Forget: 2 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := PasswordPolicyExample(
		claimingFactor{subject: "alice", password: "correct"}, tracker,
		contextualFactor{allow: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	// The helper builds a LOGIN policy and nothing else: a request carrying no
	// password is refused rather than falling through to some other arm. It takes
	// no "stronger mechanisms" parameter, because ServeLogin projects only a
	// subject and a password — a ticket or certificate placed here could never be
	// presented to it (lector r3).
	if _, err := p.Authenticate(context.Background(), &auth.Request{}); err == nil {
		t.Error("an empty request authenticated against the login policy")
	}

	pwOnly := p
	attempt := func(pw string) error {
		_, err := pwOnly.Authenticate(context.Background(), &auth.Request{
			Credentials: map[string]auth.Secret{
				"subject":  auth.NewSecret("alice"),
				"password": auth.NewSecret(pw),
			},
		})
		return err
	}
	if err := attempt("correct"); err != nil {
		t.Fatalf("the correct password was refused: %v", err)
	}
	// Guessing must lock out: a reusable secret with no backoff is an online
	// guessing attack waiting to happen, which is precisely why the throttle is
	// a requirement rather than advice.
	for range 3 {
		if err := attempt("wrong"); err == nil {
			t.Fatal("a wrong password was accepted")
		}
	}
	if err := attempt("correct"); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	locked, _, lerr := tracker.Locked(context.Background(), "", time.Now())
	_ = locked
	if lerr != nil {
		t.Fatal(lerr)
	}
	// The subject counter must have engaged; the exact key is auth's business,
	// so this asserts the observable outcome: the correct password now fails.
	if err := attempt("correct"); err == nil {
		t.Error("the throttle did not engage: the correct password succeeded after a guessing run")
	}

	// A nil tracker is a construction error — auth.NewThrottle refuses it, so
	// password without backoff cannot be built through this helper.
	if _, err := PasswordPolicyExample(claimingFactor{}, nil, contextualFactor{allow: true}); err == nil {
		t.Error("a password policy with no Tracker must not be constructible here")
	}
	// And the constraint is REQUIRED: the helper's name promises the recommended
	// shape, so it must not hand back a weaker one.
	if _, err := PasswordPolicyExample(claimingFactor{}, tracker, nil); err == nil {
		t.Error("PasswordPolicyExample with no constraint must be refused — use " +
			"RecommendedPolicy to build that deliberately")
	}
	if _, err := PasswordPolicyExample(claimingFactor{}, tracker, nil); err == nil {
		t.Error("a nil constraint must be refused rather than silently dropped")
	}
}

// The ticket must never be in the URL: a URL lands in history, in a Referer, and
// in every access log between the client and here.
func TestAuthRequest_CredentialsComeFromTheMessage(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/attach", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	r.Header.Set("Origin", "https://tui.example.test")
	r.Header.Set("Cookie", "session=leak-me")
	r.Header.Set("Authorization", "Bearer leak-me-too")

	m := clientMessage{
		T: msgHello, Ticket: "tkt", Identity: "alice@host", Sig: "-----BEGIN...",
		Chal: "c1", Session: "s1",
	}
	req := authRequest(r, m)

	for key, want := range map[string]string{
		token.DefaultScheme: "tkt",
		"ssh-identity":      "alice@host",
		"ssh-signature":     "-----BEGIN...",
		"ssh-challenge":     "c1",
		"session":           "s1",
	} {
		got, ok := req.Credentials[key]
		if !ok {
			t.Errorf("credential %q missing", key)
			continue
		}
		if got.Reveal() != want {
			t.Errorf("credential %q = %q, want %q", key, got.Reveal(), want)
		}
	}
	if req.Peer.String() != "203.0.113.7:44321" {
		t.Errorf("Peer = %v, want the transport's view", req.Peer)
	}
	// Origin reaches the factors, because an sshkey challenge is bound to it.
	if v := req.Metadata["Origin"]; len(v) != 1 || v[0] != "https://tui.example.test" {
		t.Errorf("Origin metadata = %v", v)
	}
	// Credential-bearing headers must NOT land in Metadata, which auth.Secret
	// does not protect.
	for _, banned := range []string{"Cookie", "Authorization"} {
		if _, ok := req.Metadata[banned]; ok {
			t.Errorf("%s reached Metadata, where Secret does not protect it", banned)
		}
	}
}

// An absent credential must be ABSENT, not present-and-empty: a factor receiving
// an empty secret has to guess whether that is a failed attempt or no attempt.
func TestAuthRequest_OmitsEmptyCredentials(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/attach", nil)
	r.RemoteAddr = "127.0.0.1:1"
	req := authRequest(r, clientMessage{T: msgHello, Ticket: "only-this"})

	if len(req.Credentials) != 1 {
		t.Fatalf("%d credentials, want 1: %v", len(req.Credentials), req.Credentials)
	}
	for _, absent := range []string{"password", "subject", "ssh-identity", "ssh-signature"} {
		if _, ok := req.Credentials[absent]; ok {
			t.Errorf("%q was sent as an empty secret", absent)
		}
	}
}

func TestPeerFromRequest(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"203.0.113.7:44321":  "203.0.113.7:44321",
		"[2001:db8::1]:8080": "[2001:db8::1]:8080",
		"garbage":            "",
		"":                   "",
		"203.0.113.7":        "",
	}
	for in, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = in
		got := peerFromRequest(r)
		if want == "" {
			if got.IsValid() {
				t.Errorf("%q produced %v, want the zero AddrPort — a plausible fallback is an allowlist bypass", in, got)
			}
			continue
		}
		if got.String() != want {
			t.Errorf("%q -> %v, want %v", in, got, want)
		}
	}
	if peerFromRequest(nil).IsValid() {
		t.Error("a nil request must produce no peer")
	}
}
