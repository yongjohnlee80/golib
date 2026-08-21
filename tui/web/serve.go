package web

import (
	"context"
	"net/http"
	"time"

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

// Mount registers the page and the WebSocket endpoint on srv.
//
// This is the only supported way to serve a WebTUI, because it is the only way
// the validated [Config] and the listener are the same decision. A caller that
// builds its own listener can still mount [Handler.Guard] and [Handler.ServeWS],
// and the documented guarantee narrows accordingly: see [Handler.Serve].
func (h *Handler) Mount(srv *golibhttp.Server) {
	srv.Get("/", h.ServePage)
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
// The bind address and TLS settings that [Config.validate] checked are the ones
// used here. Previously validate() inspected fields that nothing consumed, so a
// config claiming loopback-plus-TLS could be mounted on a plaintext 0.0.0.0
// listener and pass every check (lector r1) — the validation was describing an
// intention rather than constraining anything.
func (h *Handler) Serve(ctx context.Context) error {
	opts := []golibhttp.Option{golibhttp.Addr(h.cfg.Addr)}
	if h.cfg.TLS != nil {
		opts = append(opts, golibhttp.WithTLSConfig(h.cfg.TLS))
	}
	srv := golibhttp.New(opts...)
	h.Mount(srv)

	stopSweep := h.mgr.Start()
	defer stopSweep()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		_ = h.mgr.Shutdown(shutdownCtx)
	}()
	return srv.Run(ctx)
}

// shutdownGrace bounds how long sessions get to exit on shutdown.
const shutdownGrace = 10 * time.Second

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
