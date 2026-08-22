package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/yongjohnlee80/golib/logger"
	golibhttp "github.com/yongjohnlee80/golib/server/http"
	"github.com/yongjohnlee80/golib/server/ws"
)

// Guard wraps a handler with the handshake controls, applied BEFORE anything is
// upgraded.
//
// # Why this exists and why it wraps rather than checks inside
//
// The Origin and Host checks previously ran inside the WebSocket session loop —
// which is after server/ws has already called websocket.Accept, so the upgrade
// had happened and a 101 had been sent before the request was judged (lector r1).
// A refusal after the upgrade is still a refusal, but it means the connection
// existed, the client saw a successful handshake, and any per-connection cost was
// already paid.
//
// As an http.Handler wrapper the decision happens while a plain 403 is still
// possible, and the WS handler is simply never reached.
func (h *Handler) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h.cfg.checkHandshake(r); err != nil {
			logHandshakeDenial(h.log, r, err)
			hardeningHeaders(w.Header(), "-")
			// Deliberately uninformative: a refusal that explained itself would
			// tell a prober which control it tripped.
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Mount registers the page and the WebSocket endpoint on srv, with [Handler.Guard]
// composed onto every SENSITIVE route.
//
// The page itself is served directly: it carries no credential, processes no
// input, and refusing it on Origin would break a legitimate first visit, which
// has no Origin to send. The WebSocket and login routes are guarded.
//
// # What this does NOT check
//
// It accepts a server somebody else built, and it does NOT inspect that server's
// address or TLS settings. So mounting on a server bound to plaintext
// 0.0.0.0 succeeds even when the Config says loopback-plus-TLS: the routes are
// safe, the BIND is the caller's. An earlier version of this comment said Mount
// binds the validated config to the listener, which was simply untrue
// (lector r2).
//
// [Handler.Serve] is the path where the validated Config and the listener are
// the same decision. Use Mount when you are composing this into a larger server
// and are taking responsibility for where it listens.
func (h *Handler) Mount(srv *golibhttp.Server) {
	srv.Get("/", h.ServePage)
	if h.cfg.LoginPolicy != nil {
		// Guarded like everything else. It is the only unauthenticated endpoint
		// in the package, which makes the Origin check load-bearing rather than
		// defensive: without it, any page the user visits could POST a guess.
		srv.Handle("POST "+h.loginPath, h.Guard(http.HandlerFunc(h.ServeLogin)))
	}
	srv.Handle("GET "+h.wsPath, h.Guard(ws.Handler(
		srv.Sessions(),
		h.ServeWS,
		ws.ReadLimit(h.limits.MaxMessage),
		// The allowlist is enforced by Guard above, before the upgrade. It is
		// ALSO passed here so server/ws's own same-origin default cannot admit
		// something the config does not name: two independent checks agreeing is
		// the point, not redundancy.
		ws.InsecureAllowOrigins(originHosts(h.cfg.AllowedOrigins)...),
	)))
}

// Serve builds the listener FROM the validated config and serves until ctx ends.
//
// This is the path where the §2.5 guarantees actually hold: the bind address and
// TLS settings [Config.validate] checked are the ones used here, so a
// non-loopback plaintext bind cannot happen. Previously validate() inspected
// fields nothing consumed, so it described an intention rather than constraining
// anything (lector r1).
//
// Prefer this over [Handler.Mount] unless you are deliberately composing into a
// server whose bind you own.
func (h *Handler) Serve(ctx context.Context) (err error) {
	opts := []golibhttp.Option{golibhttp.Addr(h.cfg.Addr)}
	if h.cfg.TLS != nil {
		opts = append(opts, golibhttp.WithTLSConfig(h.cfg.TLS))
	}
	srv := golibhttp.New(opts...)
	h.Mount(srv)

	stopSweep := h.mgr.Start()
	defer stopSweep()
	// A NAMED result, because `return shutdownErr` evaluated the variable before
	// the deferred function assigned it — so a failed shutdown returned nil and
	// the error I had just been told not to discard was discarded anyway
	// (lector r3). errors.Join keeps both causes when the server also failed.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.grace)
		defer cancel()
		// A Shutdown error means a session did not exit, which is exactly §2.8's
		// guaranteed-teardown promise failing. Hiding it behind a clean return is
		// worse than the leak it conceals.
		if serr := h.mgr.Shutdown(shutdownCtx); serr != nil {
			logger.Warning(h.log, serr, sessionAudit{Kind: "shutdown",
				Reason: "sessions did not exit"})
			err = errors.Join(err, serr)
		}
	}()
	return srv.Run(ctx)
}

// DefaultShutdownGrace bounds how long sessions get to exit when Serve returns.
const DefaultShutdownGrace = 10 * time.Second

// originHosts extracts the host portion of each allowed origin, which is the form
// server/ws matches against.
//
// A parse failure yields NOTHING for that entry rather than a permissive
// fallback: an origin this package cannot parse must not become an origin the
// transport accepts.
func originHosts(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		host := stripScheme(o)
		if host != "" {
			out = append(out, host)
		}
	}
	return out
}

func stripScheme(origin string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if len(origin) > len(prefix) && origin[:len(prefix)] == prefix {
			return origin[len(prefix):]
		}
	}
	return ""
}
