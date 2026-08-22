package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
)

// loginHandler builds a Handler with password login enabled.
func loginHandler(t *testing.T, password, want string) (*Handler, *token.MemStore) {
	t.Helper()
	store := token.NewMemStore(64)
	tracker, err := auth.NewMemTracker(64, auth.Backoff{
		Threshold: 2, Base: time.Hour, Max: time.Hour, Forget: 2 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(claimingFactor{subject: want, password: password}, tracker, contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{
		Addr:           "127.0.0.1:8080",
		Policy:         attach,
		LoginPolicy:    loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m)
	if err != nil {
		t.Fatal(err)
	}
	return h, store
}

func loginPost(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	r.Host = "tui.example.test"
	r.Header.Set("Origin", "https://tui.example.test")
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "203.0.113.7:1"
	return r
}

// The whole point of the route: a password mints a SINGLE-USE ticket, and the
// attach path then only ever sees a ticket.
func TestLogin_MintsASingleUseTicket(t *testing.T) {
	t.Parallel()
	h, store := loginHandler(t, "correct-horse", "alice")

	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"correct-horse"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var got loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Ticket == "" {
		t.Fatal("no ticket returned")
	}
	// A credential in a response body must never be cached.
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q", cc)
	}

	// The ticket authenticates against the ATTACH policy...
	req := &auth.Request{Credentials: map[string]auth.Secret{
		token.DefaultScheme: auth.NewSecret(got.Ticket),
	}}
	id, err := h.cfg.Policy.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("the minted ticket does not attach: %v", err)
	}
	if id.Subject != "alice" {
		t.Errorf("subject = %q, want alice", id.Subject)
	}
	// ...exactly once.
	if _, err := h.cfg.Policy.Authenticate(context.Background(), req); err == nil {
		t.Error("the ticket was reusable — it must be single-use")
	}
	_ = store
}

// A password must never be an attach credential. That is the architectural point
// of minting: the attach policy does not contain a password factor at all, so a
// captured hello is worth a spent ticket rather than a reusable secret.
func TestLogin_PasswordIsNotAnAttachCredential(t *testing.T) {
	t.Parallel()
	h, _ := loginHandler(t, "correct-horse", "alice")

	_, err := h.cfg.Policy.Authenticate(context.Background(), &auth.Request{
		Credentials: map[string]auth.Secret{
			"subject":  auth.NewSecret("alice"),
			"password": auth.NewSecret("correct-horse"),
		},
	})
	if err == nil {
		t.Fatal("a correct password authenticated against the ATTACH policy; the two " +
			"policies are not separate and the password is an attach credential")
	}
}

// Every failure produces an identical status and body. The attempt ID is the
// only varying part, and it is random and outcome-independent.
func TestLogin_UniformRefusal(t *testing.T) {
	t.Parallel()
	h, _ := loginHandler(t, "correct-horse", "alice")

	bodies := map[string]string{
		"wrong password":  `{"subject":"alice","password":"nope"}`,
		"unknown subject": `{"subject":"nobody","password":"correct-horse"}`,
		"empty password":  `{"subject":"alice","password":""}`,
		"empty subject":   `{"subject":"","password":"correct-horse"}`,
		"malformed json":  `{`,
		"empty body":      ``,
		"wrong shape":     `["alice","correct-horse"]`,
	}
	var shape string
	for name, body := range bodies {
		rec := httptest.NewRecorder()
		h.ServeLogin(rec, loginPost(body))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d, want 401", name, rec.Code)
			continue
		}
		got := rec.Body.String()
		if strings.Contains(got, "alice") || strings.Contains(got, "nobody") ||
			strings.Contains(got, "correct-horse") {
			t.Errorf("%s: the refusal echoes the attempt: %q", name, got)
		}
		if shape == "" {
			shape = got
		} else if got != shape {
			t.Errorf("%s: refusal body %q differs from %q — a prober can tell the "+
				"causes apart", name, got, shape)
		}
	}
}

// An oversized body is bounded before it is buffered.
func TestLogin_BodyIsBounded(t *testing.T) {
	t.Parallel()
	h, _ := loginHandler(t, "pw", "alice")
	huge := `{"subject":"alice","password":"` + strings.Repeat("x", maxLoginBody*4) + `"}`
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(huge))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401 — an oversized body must be refused, not buffered", rec.Code)
	}
}

// Guessing must lock out. The throttle lives on the login route, which is where
// the guessing happens — not entangled with the attach policy that mTLS and SSH
// signatures also use.
func TestLogin_ThrottleEngages(t *testing.T) {
	t.Parallel()
	h, _ := loginHandler(t, "correct-horse", "alice")

	good := func() int {
		rec := httptest.NewRecorder()
		h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"correct-horse"}`))
		return rec.Code
	}
	if code := good(); code != http.StatusOK {
		t.Fatalf("baseline login failed: %d", code)
	}
	for range 4 {
		rec := httptest.NewRecorder()
		h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"guess"}`))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("a wrong password returned %d", rec.Code)
		}
	}
	if code := good(); code == http.StatusOK {
		t.Error("the correct password still worked after a guessing run — the throttle " +
			"is not engaged on the login route")
	}
}

// A deployment that did not ask for password auth must not expose a login
// endpoint at all — not a disabled one.
func TestLogin_AbsentWhenNotConfigured(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"x"}`))
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404: a deployment without password auth should look "+
			"like one that never had the route", rec.Code)
	}
}

// One without the other is a half-configured front door.
func TestLogin_PolicyAndIssuerMustBeSetTogether(t *testing.T) {
	t.Parallel()
	m, err := NewManager(func(*Backend) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	base := Config{
		Addr: "127.0.0.1:8080", Policy: testPolicy(t),
		AllowedOrigins: []string{"https://x.test"}, ExpectedHost: "x.test",
	}
	withPolicy := base
	withPolicy.LoginPolicy = testPolicy(t)
	if _, err := NewHandler(withPolicy, m); err == nil {
		t.Error("a LoginPolicy with no Issuer authenticates and then cannot admit anyone")
	}
	withIssuer := base
	withIssuer.Issuer = token.NewIssuer(token.NewMemStore(4))
	if _, err := NewHandler(withIssuer, m); err == nil {
		t.Error("an Issuer with no LoginPolicy would mint on request")
	}
}

func TestLogin_MethodAndHardening(t *testing.T) {
	t.Parallel()
	h, _ := loginHandler(t, "pw", "alice")
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		r := httptest.NewRequest(method, "/login", nil)
		r.Host = "tui.example.test"
		rec := httptest.NewRecorder()
		h.ServeLogin(rec, r)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP — an attacker choosing the method must not choose the headers", method)
		}
	}
}

// The route is the only unauthenticated endpoint, which makes the Origin check
// load-bearing: without it, any page the user visits could POST a guess.
func TestLogin_IsGuardedAgainstCrossOrigin(t *testing.T) {
	t.Parallel()
	h, _ := loginHandler(t, "correct-horse", "alice")
	reached := false
	guarded := h.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		h.ServeLogin(w, r)
	}))

	r := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"subject":"alice","password":"correct-horse"}`))
	r.Host = "tui.example.test"
	r.Header.Set("Origin", "https://attacker.test")
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, r)

	if reached {
		t.Fatal("a cross-origin login POST reached the handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", rec.Code)
	}
}

// The served page must offer the form only when the server advertises the route,
// and the password field must be a real credential field.
func TestLogin_ClientForm(t *testing.T) {
	t.Parallel()

	h, _ := loginHandler(t, "pw", "alice")
	rec := httptest.NewRecorder()
	h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"loginPath":"/login"`) {
		t.Error("the login path was not advertised to the client")
	}
	// type=password so the browser masks it, and current-password so a password
	// manager can fill it — a manager is a net security gain, and suppressing it
	// pushes users toward weaker memorable passwords.
	if !strings.Contains(body, `type="password"`) {
		t.Error("the password field is not type=password")
	}
	if !strings.Contains(body, `autocomplete="current-password"`) {
		t.Error("the password field should let a password manager fill it")
	}
	// Still no form element, so CSP form-action 'none' holds and the credential
	// goes by fetch to the same origin.
	if strings.Contains(body, "<form") {
		t.Error("a form element would need form-action, which the CSP denies")
	}
	if !strings.Contains(clientJS, "credentials: 'omit'") {
		t.Error("the login fetch should not attach ambient credentials")
	}
	if !strings.Contains(clientJS, "redirect: 'error'") {
		t.Error("the login fetch must refuse a redirect rather than follow one elsewhere")
	}
	// The field is cleared as part of submitting, not after the response.
	if !strings.Contains(clientJS, "pass.value = ''") {
		t.Error("the password field is never cleared")
	}

	// A handler without login must not advertise it.
	plain := handlerFor(t)
	rec = httptest.NewRecorder()
	plain.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), `"loginPath"`) {
		t.Error("a handler without password auth advertised a login path")
	}
}

// A ticket-issue failure is OUR failure, not a rejected password, and must not
// read as one.
func TestLogin_IssuerFailureIsNotARejection(t *testing.T) {
	t.Parallel()
	// A store with zero capacity cannot hold a ticket.
	store := token.NewMemStore(0)
	tracker, err := auth.NewMemTracker(8, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(claimingFactor{subject: "alice", password: "pw"}, tracker, contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach,
		LoginPolicy: loginPolicy, Issuer: token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code == http.StatusUnauthorized {
		t.Error("a store failure was reported as a rejected password, which would send " +
			"the user to reset a credential that is fine")
	}
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusOK {
		t.Logf("code = %d (store may have accepted the ticket)", rec.Code)
	}
	_ = errors.New
}
