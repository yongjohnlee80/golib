package web

import (
	"net/http"
	"net/netip"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
)

// authRequest builds the [auth.Request] for one attach.
//
// Credentials come from the FIRST WEBSOCKET MESSAGE, never from the URL: a URL
// lands in browser history, in a Referer header, and in every access log and
// proxy between the client and here. The ticket travels to the browser in a URL
// FRAGMENT — which is never transmitted — and the client scrubs it with
// history.replaceState before opening the socket (§2.8).
//
// Peer comes from the HTTP request's RemoteAddr, the transport's own view. A
// forwarded header is available to an ipallow factor under its own trusted-proxy
// rules and is never used as the peer here.
func authRequest(r *http.Request, m clientMessage) *auth.Request {
	req := &auth.Request{
		Peer:        peerFromRequest(r),
		Credentials: make(map[string]auth.Secret, 6),
		Metadata:    make(map[string][]string, 3),
	}

	// Empty fields are OMITTED rather than sent as empty secrets. A factor that
	// receives a present-but-empty credential has to decide whether that is a
	// failed attempt or an absence, and the two want different audit outcomes;
	// not sending it makes the absence unambiguous.
	set := func(k, v string) {
		if v != "" {
			req.Credentials[k] = auth.NewSecret(v)
		}
	}
	set(token.DefaultScheme, m.Ticket)
	set("ssh-identity", m.Identity)
	set("ssh-signature", m.Sig)
	set("ssh-challenge", m.Chal)
	// Password: permitted, weakest. See Config.Policy.
	set("subject", m.Subject)
	set("password", m.Password)
	// The session binding an sshkey challenge was issued for.
	set("session", m.Session)

	// Origin reaches the factors because an sshkey challenge is bound to it
	// (ADR-0001 §2.5). It is copied by allowlist, exactly as auth/authhttp does:
	// Metadata is a plain map that auth.Secret does not protect, so a
	// credential-bearing header must never land there.
	for _, name := range []string{"Origin", "User-Agent"} {
		if v := r.Header.Values(name); len(v) > 0 {
			req.Metadata[http.CanonicalHeaderKey(name)] = v
		}
	}
	return req
}

// peerFromRequest parses RemoteAddr, yielding the ZERO AddrPort when it cannot.
// Every address-keyed control must read that as "no address" rather than as a
// match: a plausible-looking fallback would be an allowlist bypass.
func peerFromRequest(r *http.Request) netip.AddrPort {
	if r == nil || r.RemoteAddr == "" {
		return netip.AddrPort{}
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
