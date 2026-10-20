package web

import (
	"net/http"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/authhttp"
	"github.com/yongjohnlee80/golib/auth/token"
)

// authRequest builds the [auth.Request] for one attach: authhttp's projection of the HTTP request
// (the peer from RemoteAddr, the allowlisted headers, the TLS state through mtls), with the
// credentials of the first websocket message.
//
// Credentials come from the FIRST WEBSOCKET MESSAGE, never from the URL: a URL
// lands in browser history, in a Referer header, and in every access log and
// proxy between the client and here. The ticket travels to the browser in a URL
// FRAGMENT — which is never transmitted — and the client scrubs it with
// history.replaceState before opening the socket.
func authRequest(r *http.Request, m clientMessage) *auth.Request {
	return authhttp.FromRequest(r, authhttp.Credentials(func(*http.Request) map[string]auth.Secret {
		return attachCredentials(m)
	}))
}

// attachCredentials are the first message's credentials.
//
// Empty fields are OMITTED rather than sent as empty secrets. A factor that
// receives a present-but-empty credential has to decide whether that is a
// failed attempt or an absence, and the two want different audit outcomes;
// not sending it makes the absence unambiguous.
//
// NO password credentials are projected here, and the protocol carries none.
// A password authenticates at the login route and yields a ticket; the attach
// path sees only tickets, certificates and signatures.
func attachCredentials(m clientMessage) map[string]auth.Secret {
	creds := make(map[string]auth.Secret, 5)
	set := func(k, v string) {
		if v != "" {
			creds[k] = auth.NewSecret(v)
		}
	}
	set(token.DefaultScheme, m.Ticket)
	set("ssh-identity", m.Identity)
	set("ssh-signature", m.Sig)
	set("ssh-challenge", m.Chal)
	// The session binding an sshkey challenge was issued for.
	set("session", m.Session)
	return creds
}
