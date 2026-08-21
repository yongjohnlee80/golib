// Package ipallow authenticates nothing: it constrains an identity proved
// elsewhere by checking the client address against an allowlist (ADR-0001
// §2.3, §2.6).
//
// An address is a FACTOR, never an identity. This factor reports
// auth.FactorContextual, so auth.NewPolicy structurally refuses any policy that
// could be satisfied by an address alone — the rule is enforced by the type,
// not by this comment.
package ipallow

import (
	"context"
	"errors"
	"net/netip"
	"strings"

	"github.com/yongjohnlee80/golib/auth"
)

// Errors are internal: auth.Policy maps every failure to
// auth.ErrUnauthenticated. They exist so the audit record can be specific.
var (
	ErrNotAllowed  = errors.New("ipallow: client address not in the allowlist")
	ErrNoAddress   = errors.New("ipallow: no usable client address")
	ErrEmptyPolicy = errors.New("ipallow: empty allowlist denies")
)

// Factor checks the client address against allowed prefixes.
type Factor struct {
	allowed []netip.Prefix
	// trustedProxies, when non-empty, are the only peers whose forwarded
	// headers are believed at all.
	trustedProxies []netip.Prefix
	header         string
}

// Option configures a Factor.
type Option func(*Factor)

// TrustedProxies enables forwarded-header parsing, and ONLY for these peers.
//
// Without this, forwarded headers are ignored entirely: `X-Forwarded-For` is
// attacker-controlled on any directly reachable listener, so believing it by
// default would turn the allowlist into a decoration (ADR-0001 §2.6).
func TrustedProxies(prefixes ...netip.Prefix) Option {
	return func(f *Factor) { f.trustedProxies = append(f.trustedProxies, prefixes...) }
}

// ForwardedHeader sets which header carries the chain. Default
// "X-Forwarded-For". Only consulted when TrustedProxies is configured.
func ForwardedHeader(name string) Option {
	return func(f *Factor) { f.header = name }
}

// New builds the factor. An EMPTY allowlist denies everything — deny by
// default, never allow-everything.
func New(allowed []netip.Prefix, opts ...Option) *Factor {
	f := &Factor{allowed: allowed, header: "X-Forwarded-For"}
	for _, o := range opts {
		if o != nil {
			o(f)
		}
	}
	return f
}

// Kind reports auth.FactorContextual: this factor proves no identity.
func (f *Factor) Kind() auth.FactorKind { return auth.FactorContextual }

// Verify resolves the client address and checks it against the allowlist. The
// Contribution carries NO Subject (it proves no identity) and NO expiry: a
// static observation about one request imposes no post-authentication bound
// (ADR-0001 §2.2.1).
func (f *Factor) Verify(_ context.Context, r *auth.Request) (auth.Contribution, error) {
	if len(f.allowed) == 0 {
		return auth.Contribution{}, ErrEmptyPolicy
	}
	client, err := f.clientAddr(r)
	if err != nil {
		return auth.Contribution{}, err
	}
	for _, p := range f.allowed {
		if p.Contains(client) {
			return auth.Contribution{Method: "ipallow"}, nil
		}
	}
	return auth.Contribution{}, ErrNotAllowed
}

// clientAddr returns the address to check.
//
// The peer address is authoritative unless the peer is itself a configured
// trusted proxy, in which case the forwarded chain is walked from the RIGHT —
// the end the infrastructure appended — skipping trusted hops. The first
// untrusted address from that end is the client. Taking the leftmost value
// instead is the classic spoof: a client can prepend anything it likes.
func (f *Factor) clientAddr(r *auth.Request) (netip.Addr, error) {
	if r == nil || !r.Peer.IsValid() {
		return netip.Addr{}, ErrNoAddress
	}
	peer := r.Peer.Addr().Unmap()
	if len(f.trustedProxies) == 0 || !containsAny(f.trustedProxies, peer) {
		return peer, nil
	}
	hops := forwardedHops(r, f.header)
	for i := len(hops) - 1; i >= 0; i-- {
		if !containsAny(f.trustedProxies, hops[i]) {
			return hops[i], nil
		}
	}
	// Every hop was a trusted proxy (or there were none): the peer is the best
	// answer available.
	return peer, nil
}

func forwardedHops(r *auth.Request, header string) []netip.Addr {
	var out []netip.Addr
	for name, values := range r.Metadata {
		if !strings.EqualFold(name, header) {
			continue
		}
		for _, v := range values {
			for _, part := range strings.Split(v, ",") {
				if a, err := netip.ParseAddr(strings.TrimSpace(part)); err == nil {
					out = append(out, a.Unmap())
				} else if ap, err := netip.ParseAddrPort(strings.TrimSpace(part)); err == nil {
					out = append(out, ap.Addr().Unmap())
				}
			}
		}
	}
	return out
}

func containsAny(prefixes []netip.Prefix, a netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
