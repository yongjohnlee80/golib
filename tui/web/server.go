package web

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
	"github.com/yongjohnlee80/golib/logger"
)

// Configuration errors. All of them are STARTUP errors: a misconfigured WebTUI
// must fail to start rather than serve something weaker than intended.
var (
	// ErrPlaintextExposed means a non-loopback bind was requested without TLS.
	// It is an error and never a warning: a warning in a log is not a control,
	// and "we told you" is not a mitigation for a terminal served in the clear.
	ErrPlaintextExposed = errors.New("web: refusing to serve a terminal in plaintext on a non-loopback address")

	// ErrNoPolicy means no auth.Policy was supplied. There is no
	// unauthenticated mode, not even on loopback.
	ErrNoPolicy = errors.New("web: an auth.Policy is required — there is no unauthenticated mode")

	// ErrNoOrigin means no allowed origin was configured. Origin validation
	// denies by default, so an empty allowlist is a configuration error rather
	// than a permissive default.
	ErrNoOrigin = errors.New("web: at least one allowed Origin must be configured")

	// ErrOriginDenied means the handshake's Origin is not allowed.
	ErrOriginDenied = errors.New("web: Origin not allowed")
)

// Config is the served WebTUI's configuration.
//
// Every field that could weaken security has to be set deliberately: there are
// no permissive defaults, and [Config.validate] refuses anything ambiguous at
// startup.
type Config struct {
	// Addr is the bind address, e.g. "127.0.0.1:8080".
	Addr string

	// TLS is the operator's certificate. No default certificate is ever
	// shipped, and none is auto-generated: browser UX for an ephemeral
	// localhost certificate is poor, and a certificate is not a substitute for
	// authentication, which is mandatory regardless.
	TLS *tls.Config

	// Policy authenticates every attach. Required — there is no
	// unauthenticated mode, not even on loopback.
	//
	// # A password factor does NOT belong here (rev 11)
	//
	// This is the ATTACH policy, and the attach protocol carries no password
	// fields at all — so a password factor placed here can never be satisfied.
	// Put it on [Config.LoginPolicy], where it authenticates once and mints a
	// ticket that this policy then accepts. See [Handler.ServeLogin] for why the
	// split is worth having.
	//
	// Use [RecommendedPolicy] to compose the mechanisms this policy does accept:
	// tickets, mTLS chains and SSH signatures.
	Policy auth.Policy

	// AllowedOrigins are the exact Origin header values permitted at the
	// WebSocket handshake. Required, and matched EXACTLY — no wildcards, no
	// suffix matching, because "*.example.com" matching
	// "evil.example.com.attacker.test" is a classic bypass.
	AllowedOrigins []string

	// LoginPolicy enables the password login route when set, together with
	// [Config.Issuer].
	//
	// It is a SEPARATE policy from [Config.Policy] on purpose. Password
	// authenticates to mint a ticket; the attach policy then only ever sees
	// tickets, mTLS chains and SSH signatures. Keeping them separate is what
	// stops a reusable secret from being an attach credential — see [Handler.ServeLogin].
	//
	// It MUST include a throttled password factor: see [PasswordPolicyExample].
	LoginPolicy auth.Policy

	// Issuer mints the single-use attach ticket a successful login returns.
	// Required when LoginPolicy is set.
	Issuer *token.Issuer

	// ExpectedHost is the exact Host header required. REQUIRED: §2.7 says Host
	// and Origin expectations come from configuration and are never inferred,
	// and an optional check is an inferred one with extra steps — the request
	// decides whether it is checked at all.
	//
	// Compared case-insensitively per RFC 9110, and it must include the port when
	// the deployment serves on a non-default one, because that is what a browser
	// sends.
	ExpectedHost string
}

// loopbackOnly reports whether addr binds only to a loopback interface.
//
// An unspecified address ("", ":8080", "0.0.0.0:8080", "[::]:8080") is NOT
// loopback: it binds every interface, which is the case this check exists to
// catch. Treating an empty host as local is exactly the mistake that puts a
// terminal on the internet.
func loopbackOnly(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port at all — cannot reason about it, so refuse rather than guess.
		return false, fmt.Errorf("web: cannot parse bind address %q: %w", addr, err)
	}
	if host == "" {
		return false, nil // all interfaces
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname. "localhost" is the only one that is loopback by
		// definition; anything else would need resolution, and resolving at
		// config time to decide a security question means DNS decides it.
		return strings.EqualFold(host, "localhost"), nil
	}
	if ip.IsUnspecified() {
		return false, nil
	}
	return ip.IsLoopback(), nil
}

// validate refuses a configuration that would serve something weaker than
// intended.
func (c Config) validate() error {
	if c.Policy == nil {
		return ErrNoPolicy
	}
	if len(c.AllowedOrigins) == 0 {
		return ErrNoOrigin
	}
	for _, o := range c.AllowedOrigins {
		if o == "" {
			return fmt.Errorf("%w: an empty Origin entry would match a request that sends none", ErrNoOrigin)
		}
		if o == "*" {
			return fmt.Errorf("%w: %q is not an Origin, it is the absence of one", ErrNoOrigin, o)
		}
	}
	if c.ExpectedHost == "" {
		return errors.New("web: ExpectedHost is required — §2.7 forbids inferring it " +
			"from the request, and an optional check is one the request decides")
	}
	if (c.LoginPolicy == nil) != (c.Issuer == nil) {
		// One without the other is a half-configured front door: a policy with no
		// issuer authenticates and then cannot admit anyone, and an issuer with no
		// policy would mint on request.
		return errors.New("web: LoginPolicy and Issuer must be set together")
	}
	if c.Addr == "" {
		return errors.New("web: no bind address")
	}
	local, err := loopbackOnly(c.Addr)
	if err != nil {
		return err
	}
	if !local && c.TLS == nil {
		// Criterion 9. Plaintext is permitted ONLY for a loopback bind,
		// including inside the documented SSH local-forward, where the tunnel is
		// the boundary.
		return fmt.Errorf("%w: %s has no TLS configured. Bind loopback and use "+
			"an SSH local-forward, or supply a certificate", ErrPlaintextExposed, c.Addr)
	}
	return nil
}

// originAllowed reports whether an Origin header value is permitted.
//
// Exact match only, and an ABSENT Origin is denied. A WebSocket handshake is an
// HTTP request that carries the browser's ambient credentials but is NOT subject
// to the Same-Origin Policy, so this check is the thing standing between a
// terminal and any page the user happens to visit. It is mandatory even under
// mTLS, because a browser may re-present a selected client certificate to a
// cross-origin request just as automatically as a cookie.
func (c Config) originAllowed(origin string) bool {
	if origin == "" {
		// A non-browser client can omit Origin, and so can an attacker. Denying
		// is the only answer that does not require guessing which one this is.
		return false
	}
	return slices.Contains(c.AllowedOrigins, origin)
}

// hostAllowed reports whether the Host header matches the configured
// expectation.
//
// An empty expectation denies. validate() refuses to construct that state, so
// this is the belt to its braces: the failure mode of "accept anything" is a
// terminal reachable under any Host a proxy or attacker chooses, which is worth
// two independent refusals.
func (c Config) hostAllowed(host string) bool {
	if c.ExpectedHost == "" {
		return false
	}
	return strings.EqualFold(host, c.ExpectedHost)
}

// hardeningHeaders sets the response headers of §2.7 on every response.
//
// The CSP is restrictive on purpose. The served client needs an inline script
// and inline styles — the cell attributes ARE inline styles — so those are
// permitted by nonce and by 'unsafe-inline' for style only, and nothing else is:
// no external script, no framing, no plugins, no form submission, and no
// connection anywhere except back to this origin.
func hardeningHeaders(h http.Header, scriptNonce string) {
	csp := []string{
		"default-src 'none'",
		"script-src 'nonce-" + scriptNonce + "'",
		// Cell attributes are inline styles, so style-src cannot be nonce-only
		// without rewriting every cell through a stylesheet.
		"style-src 'self' 'unsafe-inline'",
		"font-src 'self'",
		"img-src 'none'",
		"connect-src 'self'",
		"base-uri 'none'",
		"form-action 'none'",
		// The terminal must never be framed: a framed terminal is a clickjacking
		// surface, and 'none' is stronger than X-Frame-Options.
		"frame-ancestors 'none'",
		"object-src 'none'",
	}
	h.Set("Content-Security-Policy", strings.Join(csp, "; "))
	// No intermediary or browser may retain a terminal's contents.
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	h.Set("Pragma", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	// The ticket travels in a URL fragment, which is never sent to a server —
	// but a Referer would leak the rest of the URL, and there is no reason to
	// send one at all.
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Cross-Origin-Embedder-Policy", "require-corp")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
}

// checkHandshake applies every handshake-level control before anything is
// upgraded or authenticated.
//
// Order is deliberate: the cheap, credential-free checks come first, so a
// cross-origin probe is refused without touching the auth machinery at all.
func (c Config) checkHandshake(r *http.Request) error {
	if !c.hostAllowed(r.Host) {
		return fmt.Errorf("%w: unexpected Host", ErrOriginDenied)
	}
	if !c.originAllowed(r.Header.Get("Origin")) {
		return ErrOriginDenied
	}
	return nil
}

// logHandshakeDenial records a refused handshake WITHOUT the credential.
//
// The Origin is logged because it is what the operator needs, and it is
// sanitized on the way: it is attacker-controlled and a newline in it would forge
// a log line.
func logHandshakeDenial(l logger.Logger, r *http.Request, err error) {
	logger.Notice(l, handshakeDenial{
		Origin: sanitizeHeader(r.Header.Get("Origin")),
		Host:   sanitizeHeader(r.Host),
		Reason: err.Error(),
	})
}

type handshakeDenial struct {
	Origin, Host, Reason string
}

func (h handshakeDenial) String() string {
	return "web handshake denied origin=" + quoteEmpty(h.Origin) +
		" host=" + quoteEmpty(h.Host) + " reason=" + h.Reason
}

func quoteEmpty(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// sanitizeHeader bounds and de-controls an attacker-supplied header value.
func sanitizeHeader(s string) string {
	const max = 200
	if len(s) > max {
		s = s[:max] + "…"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}
