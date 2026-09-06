package web

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/auth"
)

// alwaysPolicy authenticates everything. Used only to satisfy Config.validate in
// tests about OTHER controls; the auth behaviour has its own suite in auth/.
type alwaysFactor struct{ subject string }

func (alwaysFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (a alwaysFactor) Verify(context.Context, *auth.Request) (auth.Contribution, error) {
	return auth.Contribution{Method: "test", Subject: a.subject}, nil
}

func testPolicy(t *testing.T) auth.Policy {
	t.Helper()
	p, err := auth.NewPolicy(auth.Leaf(alwaysFactor{subject: "alice"}))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func baseConfig(t *testing.T) Config {
	return Config{
		Addr:           "127.0.0.1:8080",
		Policy:         testPolicy(t),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}
}

// A non-loopback bind without TLS FAILS TO START. An error, never a
// warning — a warning in a log is not a control.
func TestConfig_NonLoopbackPlaintextIsAStartupError(t *testing.T) {
	t.Parallel()

	exposed := []string{
		"0.0.0.0:8080",
		"[::]:8080",
		":8080",
		"192.168.1.10:8080",
		"10.0.0.5:443",
		"[2001:db8::1]:8080",
		"example.test:8080",
	}
	for _, addr := range exposed {
		t.Run("plaintext "+addr, func(t *testing.T) {
			t.Parallel()
			c := baseConfig(t)
			c.Addr = addr
			err := c.validate()
			if !errors.Is(err, ErrPlaintextExposed) {
				t.Fatalf("addr %q: err = %v, want ErrPlaintextExposed", addr, err)
			}
			// The message must tell the operator what to do instead.
			if !strings.Contains(err.Error(), "SSH local-forward") {
				t.Errorf("the error should name the documented alternative: %v", err)
			}
		})
		t.Run("with TLS "+addr, func(t *testing.T) {
			t.Parallel()
			c := baseConfig(t)
			c.Addr = addr
			c.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			if err := c.validate(); err != nil {
				t.Errorf("addr %q with TLS was refused: %v", addr, err)
			}
		})
	}

	// Plaintext loopback is permitted: this is the documented SSH-forward
	// deployment, where the tunnel is the boundary.
	for _, addr := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080", "127.0.0.2:9000"} {
		c := baseConfig(t)
		c.Addr = addr
		if err := c.validate(); err != nil {
			t.Errorf("loopback %q was refused: %v", addr, err)
		}
	}
}

// An unparsable address is refused rather than guessed at, because guessing
// means guessing about a security question.
func TestConfig_UnparsableAddress(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{"127.0.0.1", "garbage", "[::1"} {
		c := baseConfig(t)
		c.Addr = addr
		if err := c.validate(); err == nil {
			t.Errorf("addr %q was accepted", addr)
		}
	}
	c := baseConfig(t)
	c.Addr = ""
	if err := c.validate(); err == nil {
		t.Error("an empty bind address was accepted")
	}
}

// There is no unauthenticated mode, not even on loopback.
func TestConfig_PolicyIsMandatory(t *testing.T) {
	t.Parallel()
	c := baseConfig(t)
	c.Policy = nil
	if err := c.validate(); !errors.Is(err, ErrNoPolicy) {
		t.Errorf("err = %v, want ErrNoPolicy", err)
	}
}

// Origin validation denies by default, so an empty allowlist is a configuration
// error rather than a permissive default.
func TestConfig_OriginAllowlistIsMandatoryAndExact(t *testing.T) {
	t.Parallel()

	for name, origins := range map[string][]string{
		"nil":         nil,
		"empty":       {},
		"empty entry": {""},
		// A wildcard is not an Origin, it is the absence of one.
		"wildcard":      {"*"},
		"with wildcard": {"https://ok.test", "*"},
	} {
		c := baseConfig(t)
		c.AllowedOrigins = origins
		if err := c.validate(); !errors.Is(err, ErrNoOrigin) {
			t.Errorf("%s: err = %v, want ErrNoOrigin", name, err)
		}
	}
}

// Exact matching only. Suffix matching is the classic bypass:
// "*.example.com" matches "evil.example.com.attacker.test".
func TestConfig_OriginMatchingIsExact(t *testing.T) {
	t.Parallel()
	c := baseConfig(t)
	c.AllowedOrigins = []string{"https://tui.example.test", "https://alt.example.test:8443"}

	allowed := []string{"https://tui.example.test", "https://alt.example.test:8443"}
	for _, o := range allowed {
		if !c.originAllowed(o) {
			t.Errorf("%q was denied", o)
		}
	}

	denied := []string{
		"",                                       // absent: an attacker can omit it too
		"http://tui.example.test",                // scheme differs
		"https://tui.example.test:443",           // explicit port differs textually
		"https://tui.example.test/",              // trailing slash
		"https://TUI.EXAMPLE.TEST",               // case differs; Origin is compared verbatim
		"https://evil.tui.example.test",          // subdomain
		"https://tui.example.test.attacker.test", // the suffix-match bypass
		"https://attacker.test",
		"null", // sandboxed iframe / file:// origin
	}
	for _, o := range denied {
		if c.originAllowed(o) {
			t.Errorf("%q was ALLOWED", o)
		}
	}
}

// Host comes from configuration, never inference: inferring it means an attacker
// who controls the Host header controls the check.
func TestConfig_HostExpectation(t *testing.T) {
	t.Parallel()
	c := baseConfig(t)
	// An empty expectation DENIES. validate() refuses to construct that state,
	// and this is the belt to its braces: "accept anything" would mean a terminal
	// reachable under any Host a proxy or an attacker chooses.
	c.ExpectedHost = ""
	if c.hostAllowed("anything.test") {
		t.Error("an unset expectation must deny, not accept every host")
	}
	c.ExpectedHost = "tui.example.test:8443"
	if !c.hostAllowed("tui.example.test:8443") {
		t.Error("the configured host was rejected")
	}
	if !c.hostAllowed("TUI.EXAMPLE.TEST:8443") {
		t.Error("host comparison is case-insensitive per RFC 9110")
	}
	for _, bad := range []string{"", "attacker.test", "tui.example.test", "tui.example.test:9999"} {
		if c.hostAllowed(bad) {
			t.Errorf("host %q was allowed", bad)
		}
	}
}

// Criterion 10a: Origin validation is enforced at the handshake, before any
// authentication runs, so a cross-origin probe never touches the auth machinery.
func TestCheckHandshake(t *testing.T) {
	t.Parallel()
	c := baseConfig(t)
	c.ExpectedHost = "tui.example.test"

	req := func(origin, host string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/attach", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}

	if err := c.checkHandshake(req("https://tui.example.test", "tui.example.test")); err != nil {
		t.Errorf("a matching handshake was refused: %v", err)
	}
	for name, r := range map[string]*http.Request{
		"cross origin":  req("https://attacker.test", "tui.example.test"),
		"absent origin": req("", "tui.example.test"),
		"wrong host":    req("https://tui.example.test", "attacker.test"),
	} {
		if err := c.checkHandshake(r); !errors.Is(err, ErrOriginDenied) {
			t.Errorf("%s: err = %v, want ErrOriginDenied", name, err)
		}
	}
}

// Criterion 10c: the hardening headers are present and restrictive.
func TestHardeningHeaders(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	hardeningHeaders(h, "abc123")

	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'none'",
		"script-src 'nonce-abc123'",
		// A framed terminal is a clickjacking surface.
		"frame-ancestors 'none'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"connect-src 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
	// No script source other than the nonce.
	if strings.Contains(csp, "script-src") && strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP allows unsafe-eval: %s", csp)
	}
	if idx := strings.Index(csp, "script-src"); idx >= 0 {
		seg := csp[idx:]
		if end := strings.Index(seg, ";"); end >= 0 {
			seg = seg[:end]
		}
		if strings.Contains(seg, "unsafe-inline") {
			t.Errorf("script-src allows unsafe-inline: %q", seg)
		}
	}

	// No intermediary or browser may retain a terminal's contents.
	if cc := h.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	} {
		if got := h.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if pp := h.Get("Permissions-Policy"); !strings.Contains(pp, "camera=()") {
		t.Errorf("Permissions-Policy = %q", pp)
	}
}

// An attacker-controlled header value must not be able to forge a log line.
func TestSanitizeHeader(t *testing.T) {
	t.Parallel()
	got := sanitizeHeader("https://ok.test\nweb handshake denied origin=forged")
	if strings.Contains(got, "\n") {
		t.Errorf("a newline survived: %q", got)
	}
	if sanitizeHeader("a\x00b") != "a?b" {
		t.Errorf("NUL was not replaced: %q", sanitizeHeader("a\x00b"))
	}
	long := strings.Repeat("x", 500)
	if len(sanitizeHeader(long)) > 210 {
		t.Errorf("an oversized header was not truncated: %d bytes", len(sanitizeHeader(long)))
	}
}

func TestHandshakeDenial_Rendering(t *testing.T) {
	t.Parallel()
	got := handshakeDenial{Origin: "https://attacker.test", Host: "tui.test", Reason: "Origin not allowed"}.String()
	want := "web handshake denied origin=https://attacker.test host=tui.test reason=Origin not allowed"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// An absent origin must read as absent, not as an empty string that looks
	// like a match.
	if got := (handshakeDenial{Reason: "x"}).String(); !strings.Contains(got, "origin=(none)") {
		t.Errorf("got %q", got)
	}
}

func TestLoopbackOnly(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"127.0.0.1:80":    true,
		"127.0.0.2:80":    true,
		"[::1]:80":        true,
		"localhost:80":    true,
		"LOCALHOST:80":    true,
		"0.0.0.0:80":      false,
		"[::]:80":         false,
		":80":             false,
		"192.168.0.1:80":  false,
		"example.test:80": false,
		// An unspecified address is NOT loopback: treating an empty or wildcard
		// host as local is exactly the mistake that puts a terminal online.
		"[::0]:80": false,
	}
	for addr, want := range cases {
		got, err := loopbackOnly(addr)
		if err != nil {
			t.Errorf("%q: %v", addr, err)
			continue
		}
		if got != want {
			t.Errorf("loopbackOnly(%q) = %v, want %v", addr, got, want)
		}
	}
}
