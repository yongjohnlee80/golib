// Package authhttp adapts an [auth.Policy] to net/http (ADR-0001 §2.8).
//
// It exists so the core stays free of net/http: one policy value serves a web
// handler, an RPC server and a CLI prompt, and only this package knows what an
// HTTP request looks like.
package authhttp

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/mtls"
	"github.com/yongjohnlee80/golib/auth/token"
)

// defaultMetadataHeaders are the headers copied into [auth.Request.Metadata].
//
// This is an ALLOWLIST on purpose. Bulk-copying headers would put `Cookie` and
// `Authorization` into a plain map[string][]string — unwrapped by [auth.Secret],
// so nothing would stop them being printed by the first `%+v` that touched a
// request. Only headers a factor actually consults belong here.
//
// It is UNEXPORTED, and [DefaultMetadataHeaders] returns a copy, because an
// exported slice is mutable security configuration: any package in the build
// could append "Authorization" to it, at any time, including after a middleware
// was constructed.
var defaultMetadataHeaders = []string{
	"Origin",          // sshkey challenge binding; WebTUI's CSWSH allowlist
	"User-Agent",      // audit context only
	"X-Forwarded-For", // ipallow, and ONLY with TrustedProxies configured
	"X-Real-Ip",       //   ""
	"Sec-Websocket-Protocol",
}

// DefaultMetadataHeaders returns a copy of the default allowlist.
func DefaultMetadataHeaders() []string { return slices.Clone(defaultMetadataHeaders) }

// sensitiveHeaders may never be copied into Metadata: that map is not protected
// by [auth.Secret], so anything landing there can be printed by an ordinary
// `%+v`. Refusing is louder than documenting, and this is a programming error
// rather than a runtime condition.
var sensitiveHeaders = map[string]bool{
	"Authorization":       true,
	"Proxy-Authorization": true,
	"Cookie":              true,
	"Set-Cookie":          true,
}

// Option configures [FromRequest] and [Middleware].
type Option func(*config)

type config struct {
	headers      []string
	credentials  func(*http.Request) map[string]auth.Secret
	unauthorized func(http.ResponseWriter, *http.Request)
	correlation  string
}

// MetadataHeaders replaces the copied-header allowlist.
//
// The names are COPIED: retaining the caller's variadic backing array would let
// a later write to it change which headers a running middleware copies, and race
// while doing so.
//
// A credential-bearing header — `Authorization`, `Cookie`,
// `Proxy-Authorization`, `Set-Cookie` — PANICS. Metadata is not protected by
// [auth.Secret], so allowing one would mean a credential printable by any `%+v`;
// put credentials in Credentials, where they are wrapped.
func MetadataHeaders(names ...string) Option {
	cloned := slices.Clone(names)
	for _, n := range cloned {
		if sensitiveHeaders[http.CanonicalHeaderKey(n)] {
			panic("authhttp.MetadataHeaders: " + n + " carries credentials and Metadata is " +
				"not protected by auth.Secret — use Credentials(...) instead")
		}
	}
	return func(c *config) { c.headers = cloned }
}

// Credentials replaces how credentials are extracted. The default reads HTTP
// Basic (as "subject"/"password") and a Bearer token (as "token").
func Credentials(fn func(*http.Request) map[string]auth.Secret) Option {
	return func(c *config) { c.credentials = fn }
}

// Unauthorized replaces the rejection response. The default is a bare 401 with
// no body — every rejection identical, since a distinguishing message is the
// enumeration channel the whole package avoids.
func Unauthorized(fn func(http.ResponseWriter, *http.Request)) Option {
	return func(c *config) { c.unauthorized = fn }
}

func resolve(opts []Option) config {
	c := config{
		headers:     defaultMetadataHeaders,
		credentials: defaultCredentials,
		correlation: DefaultCorrelationHeader,
	}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	if c.headers == nil {
		c.headers = defaultMetadataHeaders
	}
	if c.credentials == nil {
		c.credentials = defaultCredentials
	}
	// Clone whatever we ended up with, so nothing outside can mutate the
	// allowlist a resolved config is using.
	c.headers = slices.Clone(c.headers)
	return c
}

// FromRequest projects an HTTP request into an [auth.Request].
//
// Peer comes from RemoteAddr — the transport's own view. A forwarded header is
// copied into Metadata for `ipallow` to consider under its own trusted-proxy
// rules, and is NEVER used as Peer: an attacker who picks their own peer address
// has defeated every address-keyed control at once.
func FromRequest(r *http.Request, opts ...Option) *auth.Request {
	return fromRequest(r, resolve(opts))
}

func fromRequest(r *http.Request, c config) *auth.Request {
	out := &auth.Request{
		Peer:        peerOf(r),
		Credentials: c.credentials(r),
		Metadata:    make(map[string][]string, len(c.headers)),
	}
	for _, name := range c.headers {
		if v := r.Header.Values(name); len(v) > 0 {
			out.Metadata[http.CanonicalHeaderKey(name)] = v
		}
	}
	if r.TLS != nil {
		out.TLS = projectTLS(r.TLS)
	}
	return out
}

// peerOf parses RemoteAddr. An unparsable value yields the zero AddrPort, which
// every address-keyed factor must treat as "no address" rather than as a match.
func peerOf(r *http.Request) netip.AddrPort {
	if r.RemoteAddr == "" {
		return netip.AddrPort{}
	}
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return ap
	}
	// httptest and some proxies produce host:port with a non-numeric host.
	host, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.AddrPort{}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.AddrPort{}
	}
	p, err := netip.ParseAddrPort(net.JoinHostPort(addr.String(), port))
	if err != nil {
		return netip.AddrPort{}
	}
	return p
}

// projectTLS carries across only what a factor may authenticate from.
//
// It deliberately routes through mtls.FromConnectionState, which refuses to
// project PeerCertificates: any self-signed certificate lands there, so a later
// reader must not be able to reach one (ADR-0001 §2.6a).
func projectTLS(st *tls.ConnectionState) *auth.TLSState {
	return mtls.FromConnectionState(st)
}

// defaultCredentials reads HTTP Basic and Bearer.
func defaultCredentials(r *http.Request) map[string]auth.Secret {
	creds := make(map[string]auth.Secret, 2)
	if user, pass, ok := r.BasicAuth(); ok {
		creds["subject"] = auth.NewSecret(user)
		creds["password"] = auth.NewSecret(pass)
		return creds
	}
	const bearer = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(bearer) && strings.EqualFold(h[:len(bearer)], bearer) {
		// token.DefaultScheme, not a hand-written string: the two were "token"
		// here and "ticket" there, so the adapter and the factor silently did
		// not compose and every bearer request 401'd with the credential
		// unconsumed.
		creds[token.DefaultScheme] = auth.NewSecret(strings.TrimSpace(h[len(bearer):]))
	}
	return creds
}

// identityKey is unexported so nothing outside this package can plant an
// identity in a context and have a downstream handler trust it.
type identityKey struct{}

// IdentityFrom returns the identity [Middleware] established.
func IdentityFrom(ctx context.Context) (*auth.Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(*auth.Identity)
	return id, ok && id != nil
}

// WithIdentity attaches an identity, for tests and for adapters that
// authenticate outside the middleware.
func WithIdentity(ctx context.Context, id *auth.Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// DefaultCorrelationHeader carries the attempt ID back to the client.
//
// The value is random per attempt and reveals nothing about the outcome or the
// account, which is what makes it safe to hand out — and it is the only way a
// user can quote something an operator can find, given that every rejection is
// byte-identical.
const DefaultCorrelationHeader = "X-Auth-Attempt"

// CorrelationHeader sets the response header carrying the attempt ID. An empty
// name disables it.
func CorrelationHeader(name string) Option {
	return func(c *config) { c.correlation = name }
}

// Middleware authenticates every request and rejects the ones that fail.
//
// A nil policy is a programming error and panics at construction rather than
// admitting everything: a middleware that silently stops authenticating is the
// worst possible failure mode for this package.
//
// Configuration is resolved ONCE here, not per request: re-resolving would run
// the option functions on every call, so a mutable option input could change
// behavior mid-flight and race while doing so.
func Middleware(p auth.Policy, opts ...Option) func(http.Handler) http.Handler {
	if p == nil {
		panic("authhttp.Middleware: nil policy — refusing to build a middleware that admits everything")
	}
	c := resolve(opts)
	reject := c.unauthorized
	if reject == nil {
		reject = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A PER-REQUEST sink. auth.Observe is policy-global and cannot tell
			// which of the requests in flight an attempt belongs to — two from
			// the same peer are indistinguishable to it — so the ID that reaches
			// this response has to be captured on this request's context.
			var attemptID string
			ctx := r.Context()
			if c.correlation != "" {
				ctx = auth.WithAttemptSink(ctx, func(a auth.Attempt) { attemptID = a.ID })
			}
			id, err := p.Authenticate(ctx, fromRequest(r, c))
			if c.correlation != "" && attemptID != "" {
				w.Header().Set(c.correlation, attemptID)
			}
			if err != nil || id == nil {
				reject(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}
