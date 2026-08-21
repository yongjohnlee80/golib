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
	"strings"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/mtls"
)

// DefaultMetadataHeaders are the headers copied into [auth.Request.Metadata].
//
// This is an ALLOWLIST on purpose. Bulk-copying headers would put `Cookie` and
// `Authorization` into a plain map[string][]string — unwrapped by [auth.Secret],
// so nothing would stop them being printed by the first `%+v` that touched a
// request. Only headers a factor actually consults belong here.
var DefaultMetadataHeaders = []string{
	"Origin",          // sshkey challenge binding; WebTUI's CSWSH allowlist
	"User-Agent",      // audit context only
	"X-Forwarded-For", // ipallow, and ONLY with TrustedProxies configured
	"X-Real-Ip",       //   ""
	"Sec-Websocket-Protocol",
}

// Option configures [FromRequest] and [Middleware].
type Option func(*config)

type config struct {
	headers      []string
	credentials  func(*http.Request) map[string]auth.Secret
	unauthorized func(http.ResponseWriter, *http.Request)
}

// MetadataHeaders replaces the copied-header allowlist.
//
// Adding `Cookie` or `Authorization` here defeats [auth.Secret]. Put credentials
// in Credentials, where they are wrapped.
func MetadataHeaders(names ...string) Option {
	return func(c *config) { c.headers = names }
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
	c := config{headers: DefaultMetadataHeaders, credentials: defaultCredentials}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	if c.headers == nil {
		c.headers = DefaultMetadataHeaders
	}
	if c.credentials == nil {
		c.credentials = defaultCredentials
	}
	return c
}

// FromRequest projects an HTTP request into an [auth.Request].
//
// Peer comes from RemoteAddr — the transport's own view. A forwarded header is
// copied into Metadata for `ipallow` to consider under its own trusted-proxy
// rules, and is NEVER used as Peer: an attacker who picks their own peer address
// has defeated every address-keyed control at once.
func FromRequest(r *http.Request, opts ...Option) *auth.Request {
	c := resolve(opts)
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
		creds["token"] = auth.NewSecret(strings.TrimSpace(h[len(bearer):]))
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

// Middleware authenticates every request and rejects the ones that fail.
//
// A nil policy is a programming error and panics at construction rather than
// admitting everything: a middleware that silently stops authenticating is the
// worst possible failure mode for this package.
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
			id, err := p.Authenticate(r.Context(), FromRequest(r, opts...))
			if err != nil || id == nil {
				reject(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}
