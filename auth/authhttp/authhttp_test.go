package authhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
)

type stubFactor struct {
	mu   sync.Mutex
	seen *auth.Request
	ok   bool
}

func (s *stubFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (s *stubFactor) Verify(_ context.Context, r *auth.Request) (auth.Contribution, error) {
	s.mu.Lock()
	s.seen = r
	s.mu.Unlock()
	if !s.ok {
		return auth.Contribution{}, errors.New("no")
	}
	return auth.Contribution{Method: "stub", Subject: "alice", IssuedAt: time.Now()}, nil
}

// lastSeen reads the recorded request under the lock, since the concurrency test
// drives 32 requests through one factor.
func (s *stubFactor) lastSeen() *auth.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen
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
	// The bearer token belongs in Credentials, where Secret redacts it — under
	// the key the token factor actually reads.
	tok, ok := got.Credentials[token.DefaultScheme]
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
	if _, ok := got.Credentials[token.DefaultScheme]; ok {
		t.Error("Basic must not also produce a bearer credential")
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

	seenReq := f.lastSeen()
	if seenReq == nil {
		t.Fatal("the factor never saw a request")
	}
	if v := seenReq.Metadata["X-Custom"]; len(v) != 1 || v[0] != "kept" {
		t.Errorf("custom header not copied: %v", seenReq.Metadata)
	}
	if _, ok := seenReq.Metadata["Origin"]; ok {
		t.Error("MetadataHeaders must REPLACE the allowlist, not extend it")
	}
}

// TLS must be projected through mtls.FromConnectionState, which refuses to carry
// PeerCertificates — any self-signed certificate lands there.
func TestFromRequest_TLSCarriesOnlyVerifiedChains(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := FromRequest(r); got.TLS != nil {
		t.Error("a plaintext request must not carry TLS state")
	}
}

// The adapter's bearer projection and the token factor's default MUST compose.
//
// They did not: the adapter wrote "token" and the factor read "ticket", so a
// real request 401'd with the credential unconsumed. A test that only inspected
// the projected map could not see it — this one runs the actual issuer, the
// actual factor and the actual middleware end to end.
func TestBearerComposesWithTheRealTokenFactor(t *testing.T) {
	t.Parallel()

	store := token.NewMemStore(64)
	issuer := token.NewIssuer(store)
	secret, err := issuer.Issue("alice", time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	minted := secret.Reveal()
	p, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}

	var handled bool
	var subject string
	h := Middleware(p)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = true
		if id, ok := IdentityFrom(r.Context()); ok {
			subject = id.Subject
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+minted)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204: the adapter's bearer key and the factor's "+
			"scheme do not compose", rec.Code)
	}
	if !handled || subject != "alice" {
		t.Errorf("handled = %v subject = %q", handled, subject)
	}
	// Single-use: the same bearer must not work twice.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("Authorization", "Bearer "+minted)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("replay code = %d, want 401", rec2.Code)
	}
}

// The correlation ID has to be per REQUEST. A policy-global observer cannot tell
// which of the requests in flight an attempt belongs to, and two from the same
// peer are indistinguishable to it.
func TestMiddleware_CorrelationIDIsPerRequest(t *testing.T) {
	t.Parallel()
	h := Middleware(policyFor(t, &stubFactor{ok: false}))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	const n = 32
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Same peer for every request, which is the case a global observer
			// cannot disambiguate.
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "203.0.113.1:5000"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			ids[i] = rec.Header().Get(DefaultCorrelationHeader)
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for i, id := range ids {
		if id == "" {
			t.Fatalf("request %d got no correlation header — the ID never reached the response", i)
		}
		if seen[id] {
			t.Fatalf("request %d reused correlation ID %q: the ID is not per-request", i, id)
		}
		seen[id] = true
	}
}

func TestMiddleware_CorrelationHeaderCanBeDisabled(t *testing.T) {
	t.Parallel()
	h := Middleware(policyFor(t, &stubFactor{ok: false}), CorrelationHeader(""))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get(DefaultCorrelationHeader); got != "" {
		t.Errorf("header = %q, want none", got)
	}
}

// An exported allowlist slice is mutable security configuration: any package in
// the build could append Authorization to it, at any time.
func TestDefaultMetadataHeaders_CannotBeMutated(t *testing.T) {
	t.Parallel()
	first := DefaultMetadataHeaders()
	first[0] = "Authorization"
	first = append(first, "Cookie")
	_ = first
	second := DefaultMetadataHeaders()
	for _, name := range second {
		if sensitiveHeaders[http.CanonicalHeaderKey(name)] {
			t.Fatalf("the default allowlist was mutated from outside: %v", second)
		}
	}
	if second[0] != "Origin" {
		t.Errorf("default allowlist = %v", second)
	}
}

// Refusing is louder than documenting: Metadata is not protected by Secret, so a
// credential-bearing header there is printable by any %+v.
func TestMetadataHeaders_RefusesSensitiveNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Authorization", "authorization", "Cookie", "Set-Cookie", "Proxy-Authorization"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("MetadataHeaders(%q) must panic", name)
				}
			}()
			_ = MetadataHeaders(name)
		}()
	}
	// A normal name is fine.
	if opt := MetadataHeaders("X-Whatever"); opt == nil {
		t.Error("a non-sensitive header must be accepted")
	}
}

// Retaining the caller's variadic backing array would let a later write change
// which headers a running middleware copies — and race while doing so.
func TestMetadataHeaders_ClonesItsInput(t *testing.T) {
	t.Parallel()
	names := []string{"X-Keep"}
	f := &stubFactor{ok: true}
	h := Middleware(policyFor(t, f), MetadataHeaders(names...))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	// Mutate the caller's slice AFTER the middleware exists.
	names[0] = "X-Swapped"

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Keep", "yes")
	r.Header.Set("X-Swapped", "no")
	h.ServeHTTP(httptest.NewRecorder(), r)

	seenReq := f.lastSeen()
	if seenReq == nil {
		t.Fatal("the factor never saw a request")
	}
	if _, ok := seenReq.Metadata["X-Keep"]; !ok {
		t.Error("the allowlist changed after construction — the option aliased the caller's slice")
	}
	if _, ok := seenReq.Metadata["X-Swapped"]; ok {
		t.Error("a header added to the caller's slice after construction was copied")
	}
}

// An invalid header name reaching http.Header.Set produces a silently malformed
// response header — discovered by a user who cannot report their attempt ID,
// which is the one thing this header exists to prevent.
func TestCorrelationHeader_ValidatedAtConstruction(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"X Auth", "X:Auth", "X\nAuth", "X\x00Auth", "Ünicode", "X(Auth)"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("CorrelationHeader(%q) must panic", bad)
				}
			}()
			_ = CorrelationHeader(bad)
		}()
	}
	// Valid names, including the RFC 9110 token punctuation, are accepted.
	for _, ok := range []string{"X-Auth-Attempt", "X_Auth", "Attempt-ID", "x-auth", "A1!#$%&'*+-.^_`|~"} {
		if opt := CorrelationHeader(ok); opt == nil {
			t.Errorf("CorrelationHeader(%q) must be accepted", ok)
		}
	}
	// Empty disables and must not panic.
	if opt := CorrelationHeader(""); opt == nil {
		t.Error(`CorrelationHeader("") must be accepted as "disabled"`)
	}
}
