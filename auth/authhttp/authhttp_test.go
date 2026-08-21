package authhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

type stubFactor struct {
	seen *auth.Request
	ok   bool
}

func (s *stubFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (s *stubFactor) Verify(_ context.Context, r *auth.Request) (auth.Contribution, error) {
	s.seen = r
	if !s.ok {
		return auth.Contribution{}, errors.New("no")
	}
	return auth.Contribution{Method: "stub", Subject: "alice", IssuedAt: time.Now()}, nil
}

func policyFor(t *testing.T, f auth.Factor) auth.Policy {
	t.Helper()
	p, err := auth.NewPolicy(auth.Leaf(f))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Peer must be the transport's view. An attacker who can pick their own peer
// address has defeated every address-keyed control at once, so a forwarded
// header may inform ipallow but must never BE the address.
func TestFromRequest_PeerIsTheTransportsView(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:44321"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")

	got := FromRequest(r)
	if got.Peer.String() != "203.0.113.9:44321" {
		t.Errorf("Peer = %v, want the RemoteAddr", got.Peer)
	}
	if v := got.Metadata["X-Forwarded-For"]; len(v) != 1 || v[0] != "10.0.0.1" {
		t.Errorf("the forwarded header must still be AVAILABLE to ipallow: %v", v)
	}

	// An unparsable RemoteAddr must yield "no address", not a wrong one.
	for _, bad := range []string{"", "garbage", "notanip:80", "203.0.113.9"} {
		r.RemoteAddr = bad
		if p := FromRequest(r).Peer; p.IsValid() {
			t.Errorf("RemoteAddr %q produced Peer %v, want the zero value", bad, p)
		}
	}
}

// Bulk-copying headers would put Cookie and Authorization into a plain map that
// auth.Secret does not protect, so the first %+v prints them.
func TestFromRequest_SensitiveHeadersAreNotCopiedToMetadata(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Cookie", "session=super-secret")
	r.Header.Set("Authorization", "Bearer tok-do-not-copy")
	r.Header.Set("Origin", "https://example.test")

	got := FromRequest(r)
	rendered := fmt.Sprintf("%+v", got.Metadata)
	for _, leak := range []string{"super-secret", "tok-do-not-copy"} {
		if strings.Contains(rendered, leak) {
			t.Errorf("Metadata carries credential material: %q", rendered)
		}
	}
	if v := got.Metadata["Origin"]; len(v) != 1 || v[0] != "https://example.test" {
		t.Errorf("Origin must be copied — sshkey bindings and the CSWSH allowlist need it: %v", v)
	}
	// The bearer token belongs in Credentials, where Secret redacts it.
	tok, ok := got.Credentials["token"]
	if !ok || tok.Reveal() != "tok-do-not-copy" {
		t.Errorf("the bearer token must arrive as a Credential: %v", got.Credentials)
	}
	if strings.Contains(fmt.Sprintf("%+v", got.Credentials), "tok-do-not-copy") {
		t.Error("Credentials rendered the token — Secret is not redacting")
	}
}

func TestFromRequest_BasicAuth(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetBasicAuth("alice", "s3cret")
	got := FromRequest(r)
	if got.Credentials["subject"].Reveal() != "alice" {
		t.Errorf("subject = %v", got.Credentials["subject"])
	}
	if got.Credentials["password"].Reveal() != "s3cret" {
		t.Error("password was not extracted")
	}
	if _, ok := got.Credentials["token"]; ok {
		t.Error("Basic must not also produce a token credential")
	}
}

func TestMiddleware_AdmitsAndRejects(t *testing.T) {
	t.Parallel()

	t.Run("admits and exposes the identity", func(t *testing.T) {
		var seen *auth.Identity
		h := Middleware(policyFor(t, &stubFactor{ok: true}))(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, ok := IdentityFrom(r.Context())
				if !ok {
					t.Error("the handler cannot see the identity")
				}
				seen = id
				w.WriteHeader(http.StatusNoContent)
			}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("code = %d", rec.Code)
		}
		if seen == nil || seen.Subject != "alice" {
			t.Errorf("identity = %+v", seen)
		}
	})

	t.Run("rejects without reaching the handler", func(t *testing.T) {
		reached := false
		h := Middleware(policyFor(t, &stubFactor{ok: false}))(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if reached {
			t.Fatal("the handler ran for an unauthenticated request")
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("code = %d, want 401", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("the rejection has a body (%q) — every rejection must be identical", rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Error("a 401 must not be cacheable")
		}
	})
}

// A handler downstream of the middleware must not be able to be fooled by an
// identity planted in the context by unrelated code.
func TestIdentityFrom_KeyIsUnforgeable(t *testing.T) {
	t.Parallel()
	type lookalike struct{}
	ctx := context.WithValue(context.Background(), lookalike{}, &auth.Identity{Subject: "root"})
	if id, ok := IdentityFrom(ctx); ok {
		t.Errorf("a foreign context key was accepted: %+v", id)
	}
	ctx = context.WithValue(context.Background(), identityKey{}, "not an identity")
	if _, ok := IdentityFrom(ctx); ok {
		t.Error("a wrongly-typed value was accepted")
	}
	if _, ok := IdentityFrom(WithIdentity(context.Background(), nil)); ok {
		t.Error("a nil identity must not read as authenticated")
	}
}

// A middleware that silently stops authenticating is the worst failure mode
// available to this package, so a nil policy must not build one.
func TestMiddleware_NilPolicyPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("Middleware(nil) must panic rather than admit everything")
		}
	}()
	_ = Middleware(nil)
}

// Options must reach the request projection the middleware performs, not just a
// direct FromRequest call.
func TestMiddleware_HonorsOptions(t *testing.T) {
	t.Parallel()
	f := &stubFactor{ok: true}
	h := Middleware(policyFor(t, f), MetadataHeaders("X-Custom"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Custom", "kept")
	r.Header.Set("Origin", "dropped")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if f.seen == nil {
		t.Fatal("the factor never saw a request")
	}
	if v := f.seen.Metadata["X-Custom"]; len(v) != 1 || v[0] != "kept" {
		t.Errorf("custom header not copied: %v", f.seen.Metadata)
	}
	if _, ok := f.seen.Metadata["Origin"]; ok {
		t.Error("MetadataHeaders must REPLACE the allowlist, not extend it")
	}
}

// TLS must be projected through mtls.FromConnectionState, which refuses to carry
// PeerCertificates — any self-signed certificate lands there (§2.6a).
func TestFromRequest_TLSCarriesOnlyVerifiedChains(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := FromRequest(r); got.TLS != nil {
		t.Error("a plaintext request must not carry TLS state")
	}
}
