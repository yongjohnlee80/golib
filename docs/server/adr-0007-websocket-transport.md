# ADR-0007 — `golib/server/ws`: WebSocket Transport on the HTTP Core

- **Status:** Proposed (revision 2 — lector r1 amendment applied, see §7)
- **Date:** 2026-07-04
- **Module:** `github.com/yongjohnlee80/golib`
- **Depends on:** golib-server-0006 (session registry — REQUIRED for honest
  shutdown), the golib 02 `statusRecorder` interface-forwarding fix (landed:
  `wrapRecorder`, `server/http/middleware.go`)
- **Related:** golib-server-0001 (architecture), 0004 (HTTP transport),
  golib conventions (dependency policy, ctx-first, capability honesty)

> **Self-containment contract.** Implementable from this document alone.

---

## 1. Context

WebSocket is the request/response-with-path family's second member (ADR-0006
§1.2): an upgrade begins as a routed HTTP request, so `Router`, `Chain`,
groups, and the built-in middleware all apply as-is. Two blockers stood in
the way; one is fixed, one is fixed by ADR-0006:

1. ~~`statusRecorder` swallowed `http.Hijacker`~~ — fixed in golib 02:
   `wrapRecorder` forwards exactly the optional interfaces the underlying
   writer supports, proven by an end-to-end raw-hijack test through
   `RequestLogger`.
2. `http.Server.Shutdown` does not drain hijacked connections — a WebSocket
   server would "shut down gracefully" while abandoning every live socket.
   ADR-0006's `Registry` + registry-aware `httpserver.Shutdown` close this;
   this ADR's session type implements `Drainer` so drain sends proper close
   frames.

### 1.1 The dependency decision

Implementing RFC 6455 in-house (handshake, frame codec, masking, control
frames, fragmentation, close handshake, compression negotiation) is a large
correctness surface with well-known pitfalls. Per the conventions' dependency
policy — core stays zero-dep; a leaf subpackage may carry one moderate dep —
`golib/server/ws` is a **leaf subpackage in the root module** with exactly
one dependency: **`github.com/coder/websocket`** (the maintained successor
to `nhooyr.io/websocket`; pure Go, context-first API, zero transitive
dependencies of its own). The core `server` and `server/http` packages do
not import it.

*Alternatives:* gorilla/websocket (io-style, no ctx plumbing, maintenance
history wobbly) — rejected; in-house RFC 6455 (weeks of correctness work
duplicating a well-tested library, no consumer benefit) — rejected for v1,
revisitable if the zero-dep guarantee ever must become absolute.

### 1.2 Goals

- **G1** — WebSocket endpoints are ordinary routes: registered on the
  existing router, wrapped by existing middleware (`Recover`,
  `RequestLogger`, `RequestID`, `Auth`), grouped and mounted like any
  handler.
- **G2** — ctx-first session API; no raw `net.Conn` or frame types leak to
  the consumer.
- **G3** — Honest lifecycle: sessions auto-register in the server's
  `Registry`; shutdown sends close frames and waits, bounded (ADR-0006 §2.2).
- **G4** — Secure defaults: same-origin enforcement unless explicitly
  relaxed (cross-site WebSocket hijacking defense), a default read limit,
  and ping/pong keepalive with a dead-peer deadline.

### 1.3 Non-goals

Not a pub/sub or room framework; not a message-encoding layer (JSON helpers
are one thin convenience, not a protocol); no server-initiated reconnect
logic (client concern).

---

## 2. Decision

### 2.1 Surface (`server/ws`)

```go
package ws

// Session is one established WebSocket connection. Methods are safe for one
// concurrent reader and one concurrent writer (the underlying protocol's
// constraint); all take ctx and honor its cancellation.
type Session struct{ /* unexported */ }

func (s *Session) Read(ctx context.Context) (MessageType, []byte, error)
func (s *Session) Write(ctx context.Context, t MessageType, data []byte) error
func (s *Session) ReadJSON(ctx context.Context, v any) error
func (s *Session) WriteJSON(ctx context.Context, v any) error
func (s *Session) Close(code StatusCode, reason string) error
func (s *Session) Subprotocol() string
func (s *Session) Request() *http.Request // the upgrade request (auth material, params)

type MessageType int // Text, Binary (control frames are handled internally)

// Handler gates, upgrades, and runs fn with the established session on the
// handler goroutine; when fn returns the session closes with a normal close
// frame (if fn didn't already close it).
//
// Drain gate (ADR-0006 §2.2): Handler calls reg.Reserve() BEFORE the
// handshake. During shutdown Reserve reports ok=false and Handler responds
// HTTP 503 — a plain HTTP response, still possible because no 101 has been
// sent. On ok the handshake proceeds and the reservation is Completed with
// the established session (or Cancelled on handshake failure), so shutdown
// that races an in-flight upgrade waits for it and then drains it politely
// with StatusGoingAway — never accept-then-instant-close.
func Handler(reg *server.Registry, fn func(ctx context.Context, s *Session), opts ...Option) http.Handler

type Option func(*config)

func Subprotocols(names ...string) Option
// InsecureAllowOrigins relaxes the SAME-ORIGIN DEFAULT: patterns matched
// against the Origin header (e.g. "app.example.com"). The name says what it
// does — cross-origin browser access is a deliberate, visible decision.
func InsecureAllowOrigins(patterns ...string) Option
func ReadLimit(bytes int64) Option                  // default 1 MiB (matches http Decode default)
func Keepalive(interval, timeout time.Duration) Option // default 30s ping, 10s pong deadline; 0 disables
func WithLogger(l logger.Logger) Option              // upgrade/lifecycle errors; default Nop
```

Usage — a WS endpoint is just a route:

```go
srv := httpserver.New(httpserver.WithLogger(log),
	httpserver.Middlewares(httpserver.Recover(log), httpserver.RequestLogger(log)))

srv.Handle("GET /ws/updates", ws.Handler(srv.Sessions(), func(ctx context.Context, s *ws.Session) {
	for {
		var msg Update
		if err := s.ReadJSON(ctx, &msg); err != nil {
			return // peer closed, ctx cancelled (shutdown), or read error
		}
		if err := s.WriteJSON(ctx, process(msg)); err != nil {
			return
		}
	}
}, ws.Subprotocols("updates.v1")))
```

### 2.2 Semantics

- **Upgrade path.** `Handler` performs the RFC 6455 handshake via
  coder/websocket's `Accept` against the (hook-forwarding) response writer.
  Upgrade failure (bad Origin, missing headers) writes the appropriate 4xx —
  it is an ordinary HTTP response, visible to `RequestLogger` like any other.
- **Registry integration.** `Reserve` before the handshake (503 on refusal,
  §2.1); `Complete` binds the established session before fn runs; unregister
  when fn returns. The session's `Drain(ctx)` sends `StatusGoingAway`, then
  waits for fn to return (fn observes ctx cancellation and the read error),
  bounded by the drain ctx; expiry force-closes (ADR-0006 registry
  contract).
- **Keepalive.** A ticker pings; a missed pong past `timeout` fails the next
  Read with a timeout error — dead peers cannot hold shutdown hostage or leak
  goroutines.
- **fn's contract.** One reader + one writer concurrently at most (guarded
  misuse panics in dev builds are the library's behavior; the doc states the
  constraint). fn returning is the normal close path; `Recover` middleware
  catches panics above the upgrade, and the handler recovers panics inside
  fn, closing with `StatusInternalError`.

### 2.3 Testing strategy

Unit tests dial real connections over `httptest`-style servers (the ws
library provides a client — the same dependency, no new ones): echo
round-trip through the full middleware stack (proves golib 02's fix
end-to-end at the transport level), origin default-deny + allow-pattern,
read-limit enforcement, keepalive dead-peer detection (short intervals),
JSON helpers, and the headline lifecycle test: N live sessions,
`srv.Shutdown` → every client observes `StatusGoingAway` within the bound,
registry empty, no goroutine leaks (`goleak`-style manual check via runtime
counts). Race-enabled throughout.

---

## 3. Consequences

**Positive.** WebSocket endpoints inherit the entire HTTP surface (routing,
groups, middleware, auth, request IDs, logging) instead of a parallel stack;
shutdown is honest end-to-end (the first consumer proving ADR-0006's registry
in anger); secure-by-default origin policy makes CSWSH an explicit opt-out.

**Negative / costs.** First third-party dependency under `server/` (leaf-only,
zero transitive deps — the root `go.sum` gains one line); the
one-reader/one-writer constraint is real and documented rather than hidden
behind internal locking (matching the library's model and keeping the hot
path lock-free); JSON helpers imply encoding/json coupling (accepted — it is
the stdlib).

---

## 4. Alternatives considered

- **gorilla/websocket** — no context plumbing (deadline juggling instead),
  historical maintenance gaps. Rejected.
- **In-house RFC 6455** — large correctness surface for zero consumer
  benefit in v1. Rejected; revisit only if the absolute zero-dep guarantee
  becomes a requirement.
- **A separate ws server (own listener) instead of router-mounted upgrades**
  — loses middleware/auth reuse, doubles ports and lifecycle; the
  request/response-with-path family exists precisely to avoid this. Rejected.
- **Default-allow origins (gorilla's historical default)** — CSWSH by
  default violates secure-defaults; the option name carries the word
  `Insecure` for a reason. Rejected.

---

## 5. Acceptance criteria

1. An echo endpoint registered with `srv.Handle` behind `Recover` +
   `RequestLogger` + `RequestID` completes a client round-trip (text, binary,
   JSON) — end-to-end through the wrapRecorder path.
2. A cross-origin upgrade is refused by default; the same request succeeds
   with `InsecureAllowOrigins` matching; the refusal is an HTTP 403 visible
   to `RequestLogger`.
3. `srv.Shutdown` with live sessions: every client receives
   `StatusGoingAway`, handler ctxs cancel, shutdown returns within the bound,
   `Sessions().Len() == 0`; a handler that ignores cancellation is
   force-closed at the deadline and reported.
4. A message exceeding `ReadLimit` fails the read and closes with
   `StatusMessageTooBig`; the server goroutine exits.
5. Keepalive detects a non-responsive peer within `interval+timeout`; the
   session unregisters; no goroutine leak (before/after goroutine-count
   check across the suite).
6. The user-observable drain boundary: once shutdown/drain has started, a
   fresh WebSocket client attempting to connect receives an HTTP 503 — never
   a successful upgrade followed by an immediate close (verified with a
   client dialing concurrently with `srv.Shutdown`; the race is closed by
   `Reserve`, ADR-0006 §2.2). `Readyz` flips to 503 before sessions drain.
7. The `ws` package is the only golib package importing the websocket
   dependency (`go list -deps` check in CI-style test); `server` and
   `server/http` remain dependency-free.

## 6. File plan

| File | Change |
|---|---|
| `server/ws/ws.go` | new — `Handler`, `Session`, options, registry + keepalive integration |
| `server/ws/doc.go` | new — package doc: family scope, 1R/1W constraint, security defaults |
| `server/ws/ws_test.go` | new — criteria 1–7 |
| `server/ws/README.md` | new — usage, options, shutdown semantics |
| `go.mod` | + `github.com/coder/websocket` (leaf-only) |
---

## 7. Review history

- **r1 (2026-07-04, lector, combined 0006/0007 review):** `change_requested` —
  must-fix: criterion 6's 503-during-drain promise had no pre-handshake API
  (post-`Accept` there is no HTTP response left to send). Revision 2 adopts
  ADR-0006 r2's atomic `Reserve`/`Complete`/`Cancel` registry API: Handler
  reserves before the handshake, 503s on refusal, and completed reservations
  are awaited + drained politely. Dependency isolation, secure defaults, and
  `InsecureAllowOrigins` naming judged sound.
