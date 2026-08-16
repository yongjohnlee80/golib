# ADR-0006 — `golib/server`: Transport Scaffold, Session Registry & Honest Multi-Protocol Scope

- **Status:** **Accepted** (2026-07-04; lector r2 approved, see §7)
- **Date:** 2026-07-04
- **Module:** `github.com/yongjohnlee80/golib`
- **Amends:** golib-server-0001 (scope claims, see §1.2)
- **Related:** golib-server-0002 (router), 0003 (middleware), 0004 (HTTP
  transport), 0007 (WebSocket — consumes this ADR), golib conventions
  (capability honesty, ctx-first, zero-dep core)

> **Self-containment contract.** Implementable from this document alone:
> context restated, concrete Go signatures, files named, acceptance criteria
> listed.

---

## 1. Context

### 1.1 What the audit found

The 2026-07-04 audit tested ADR-0001's "shared core for many transports"
claim against the code. The core is genuinely `net/http`-free and well built,
but it is three generic helpers plus a 3-method interface (~590 lines):

- `Server{Run, Shutdown, Addr}` (`server/server.go`) — a **real, reusable
  lifecycle seam**; every transport fits it (HTTP today; gRPC/SFTP adapters
  wrap their own serve loops cleanly).
- `Router[H]` / `Chain[H]` — syntactically transport-free, but **semantically
  request/response-with-path**: `Handle(method, path)` and the
  wrap-the-handler middleware idiom fit HTTP and WebSocket; they do not fit
  gRPC (own HTTP/2 mux + interceptor signatures) or SFTP (SSH handshake +
  subsystem sessions, no verbs or paths).

What a *non-HTTP* transport actually needs is not routing — it is the
**lifecycle plumbing** the core does not provide: an accept loop, listener
injection, per-connection goroutines and contexts, structured accept-error
handling, and drain-aware shutdown. Today each new transport would hand-roll
all of it, and the HTTP transport itself cannot honestly drain long-lived
connections: `http.Server.Shutdown` explicitly ignores hijacked connections
(WebSocket upgrades), so "graceful shutdown" silently abandons them.

Adjacent operational gaps from the same audit: no `net.Listener` injection
anywhere (tests bind real ports; no systemd socket activation), TLS is
PEM-file-paths-only (no `*tls.Config` → no mTLS/min-version/autocert),
`http.Server.ErrorLog` is unwired (TLS handshake and parse errors bypass
`golib/logger` to stderr), no metrics seam, and no health/readiness helpers.

### 1.2 The scope amendment to ADR-0001

ADR-0001 §1 promised "start with HTTP, expand to WebSocket, SFTP, etc." with
the implication that the shared core carries each expansion. This ADR amends
that claim to what the design can honestly keep:

> `Router[H]` and `Chain[H]` serve the **request/response-with-path family**
> (HTTP, WebSocket, anything mounted on an HTTP router). Session/interceptor
> transports (gRPC, SFTP, raw TCP) implement `server.Server` — and, with this
> ADR, build on the transport **Scaffold** — but bypass routing and middleware
> **by design**. That is not a limitation to fix; generalizing the router to
> cover interceptor models would cost every user ergonomics to serve no one
> well (§4).

### 1.3 Goals

- **G1** — A reusable accept-loop scaffold so a new connection-oriented
  transport is a handler function, not a rewrite of lifecycle plumbing.
- **G2** — Honest graceful shutdown for long-lived connections everywhere: a
  session registry that `Shutdown` actually drains, shared by the scaffold
  and the HTTP transport (WebSocket needs it; ADR-0007 consumes it).
- **G3** — Listener injection (`WithListener`) for tests, socket activation,
  and zero-downtime restarts — scaffold and HTTP transport alike.
- **G4** — Close the operational gaps: `*tls.Config` injection, server-level
  error logging through `golib/logger`, a minimal metrics seam, and
  health/readiness handlers with a drain gate.
- **G5** — Zero new dependencies in the core (`net`, `crypto/tls`, `context`
  are stdlib); all changes additive — no public API breaks.

### 1.4 Non-goals

- Not a gRPC or SFTP implementation (those are future leaf packages that
  *consume* this scaffold; the heavy deps rule applies — nested modules).
- Not a connection pool, rate limiter, or proxy layer.
- Not a rework of `Router`/`Chain` (§1.2 scopes them instead).

---

## 2. Decision

### 2.1 `server.Scaffold` — the accept-loop seam (`server/scaffold.go`, new)

```go
package server

// ConnHandler serves one accepted connection until it returns. ctx is the
// per-connection context: it derives from the scaffold's base context and is
// cancelled when Shutdown begins, so long-lived handlers can wind down.
// Closing conn is the handler's responsibility (defer conn.Close()).
type ConnHandler func(ctx context.Context, conn net.Conn)

// Scaffold owns the accept-loop lifecycle every connection-oriented transport
// otherwise reimplements: bind (or accept an injected listener), optional TLS,
// per-connection goroutine + context, structured accept-error handling with
// backoff, and drain-aware graceful shutdown via a session Registry.
// Scaffold satisfies the Server lifecycle contract.
type Scaffold struct{ /* unexported */ }

func NewScaffold(handle ConnHandler, opts ...ScaffoldOption) *Scaffold

type ScaffoldOption func(*scaffoldConfig)

func ScaffoldAddr(addr string) ScaffoldOption          // default ":0"
func WithListener(ln net.Listener) ScaffoldOption      // injection; overrides Addr
func WithTLSConfig(cfg *tls.Config) ScaffoldOption     // wraps the listener in tls.NewListener
func ScaffoldLogger(l logger.Logger) ScaffoldOption    // lifecycle + accept errors; default Nop
func ScaffoldBaseContext(ctx context.Context) ScaffoldOption
func DrainTimeout(d time.Duration) ScaffoldOption      // extra bound inside Shutdown; default: ctx-only

func (s *Scaffold) Run(ctx context.Context) error      // bind (unless injected), serve, drain on cancel
func (s *Scaffold) Shutdown(ctx context.Context) error // stop accepting; cancel conn ctxs; drain Registry
func (s *Scaffold) Addr() string                       // resolved address (":0" supported)
func (s *Scaffold) Sessions() *Registry                // the scaffold's registry (auto-tracks conns)

// compile-time: Scaffold satisfies the lifecycle contract.
var _ Server = (*Scaffold)(nil)
```

Semantics:

- **Accept loop.** `Run` binds synchronously (bind errors return before any
  goroutine; `Addr` resolves), then accepts until the listener closes.
  `net.Error.Timeout()`-style temporary accept failures are logged and
  retried with capped exponential backoff (5ms → 1s); a terminal accept error
  cancels every connection context and is returned from `Run` (never
  swallowed — the golib 02 `errors.Join` rule applies here too).
- **Per-connection.** Each accepted conn runs `handle` on its own goroutine
  with a per-conn context; the conn is registered in the scaffold's
  `Registry` and unregistered when the handler returns. A panicking handler
  is recovered, logged at Error severity, and its conn closed — one broken
  connection never takes the server down.
- **Shutdown order.** Stop accepting → cancel per-conn contexts → drain the
  registry (bounded by the Shutdown ctx and `DrainTimeout`) → return. Conns
  still alive at the deadline are force-closed and counted in the returned
  error (`errors.Join`d), never silently abandoned.

### 2.2 `server.Registry` — drain-aware session tracking (`server/registry.go`, new)

```go
// Session is anything with a connection-scoped lifetime that graceful
// shutdown must account for: a raw conn, a WebSocket session, an SFTP
// channel. Close force-terminates it.
type Session interface{ Close() error }

// Drainer is optionally implemented by Sessions that can end politely
// (e.g. a WebSocket session sending a close frame and awaiting the peer).
// Drain must return when ctx is done.
type Drainer interface{ Drain(ctx context.Context) error }

// Registry tracks live Sessions for drain-aware shutdown. Safe for
// concurrent use. The zero value is ready.
type Registry struct{ /* unexported */ }

func (r *Registry) Register(s Session) (unregister func())
func (r *Registry) Len() int

// Reserve atomically claims a slot for a session that is ABOUT to be
// established (e.g. a WebSocket upgrade before its handshake). It returns
// ok=false once Drain has begun, letting the caller refuse the work while a
// normal protocol-level refusal is still possible (for HTTP upgrades: a 503
// BEFORE 101 Switching Protocols). Drain waits for open reservations exactly
// like live sessions, so establishment that won the race completes and is
// then drained politely — the accept-then-immediately-close window cannot
// occur.
func (r *Registry) Reserve() (res *Reservation, ok bool)

// Reservation is a claimed-but-not-yet-established session slot.
type Reservation struct{ /* unexported */ }

// Complete binds the established session to the reservation (it is now a
// live registry entry) and returns its unregister func.
func (res *Reservation) Complete(s Session) (unregister func())

// Cancel releases the reservation (establishment failed).
func (res *Reservation) Cancel()
// Drain asks every live session to end: Drainer.Drain where implemented,
// Close otherwise; then waits for unregisters AND open reservations, bounded
// by ctx. Sessions still live at the deadline are force-Closed; their count
// is reported in the returned error. After Drain begins, Reserve returns
// ok=false and Register (the established-session path) closes the session
// immediately — no new work during shutdown, and work that needs a
// pre-establishment gate uses Reserve (ADR-0007).
func (r *Registry) Drain(ctx context.Context) error
```

The **HTTP transport** gains a registry too (`httpserver.Server.Sessions()
*server.Registry`): `Shutdown` becomes `http.Server.Shutdown` **plus**
`Registry.Drain` — hijacked/long-lived connections (WebSocket, SSE) finally
drain honestly. Handlers that hijack register their session; ADR-0007's
upgrade helper does this automatically.

### 2.3 HTTP transport polish (`server/http`, all additive options)

```go
func WithListener(ln net.Listener) Option   // skip bind; serve the injected listener
func WithTLSConfig(cfg *tls.Config) Option  // full TLS control (mTLS, min version, autocert); TLS(cert,key) remains
func Healthz() http.HandlerFunc             // liveness: always 200
func (s *Server) Readyz() http.HandlerFunc  // readiness: 200 until Shutdown begins, then 503 (drain gate)
```

- **Server-level error log**: when a logger is configured, `Listen` wires
  `http.Server.ErrorLog` to a `log.Logger` adapter over `golib/logger`
  (severity Error) — TLS handshake failures and malformed requests stop
  bypassing structured logging. No option needed; it follows `WithLogger`.
- **Metrics seam**: one option, deliberately minimal —
  `WithConnMetrics(fn func(state http.ConnState, active int))` wired to
  `http.Server.ConnState` with an internally-maintained active-connection
  gauge. Request-level metrics remain middleware territory (ADR-0009-style
  hooks belong to dao, not here).
- `Readyz` flips before drain starts, so load balancers stop routing while
  in-flight work completes.

### 2.4 What a future transport looks like (illustrative, not part of this ADR)

```go
// An SFTP adapter: no Router, no Chain — Scaffold + Registry + the sftp lib.
scaffold := server.NewScaffold(func(ctx context.Context, conn net.Conn) {
    defer conn.Close()
    sess, err := sshHandshake(conn, hostKey, auth)      // transport-owned
    if err != nil { return }
    unreg := scaffold.Sessions().Register(sess)          // drains on shutdown
    defer unreg()
    sess.ServeSubsystem(ctx, "sftp", sftpHandler)        // transport-owned
}, server.ScaffoldAddr(":22"), server.ScaffoldLogger(log))
err := scaffold.Run(ctx) // full lifecycle: bind, accept, drain — zero rewrites
```

---

## 3. Consequences

**Positive.** Adding a connection-oriented transport shrinks from "rewrite
accept/lifecycle/drain" to "write a ConnHandler"; graceful shutdown becomes
honest for every long-lived connection (the WebSocket blocker falls);
listener injection unlocks port-free tests and socket activation; the
operational gaps (TLS config, error log, metrics, readiness) close without
any public API break. ADR-0001's multi-transport promise becomes true in the
form it can be true.

**Negative / costs.** The core gains its first non-trivial runtime code
(scaffold + registry, still stdlib-only) and owns its tests; two shutdown
paths exist in `httpserver` (`http.Server.Shutdown` + registry drain) whose
ordering must be specified and tested; `Registry` is deliberately minimal —
no priorities, no per-session deadlines (YAGNI until a real transport needs
them).

---

## 4. Alternatives considered

- **Generalize `Router`/`Chain` to cover gRPC/SFTP.** Interceptor and
  subsystem models don't share the method+path shape; a unifying abstraction
  would be a worse API for all parties. Rejected — scope honestly (§1.2).
- **Per-transport bespoke lifecycles (status quo).** Every future transport
  re-implements accept/drain subtly differently; the WS drain hole stays.
  Rejected.
- **Adopt a third-party server framework for the scaffold.** Violates the
  zero-dep core rule for ~300 lines of stdlib plumbing. Rejected.
- **context.Context-only shutdown (no registry).** Cancelling ctx tells
  handlers to stop but gives shutdown no way to *wait* for them or report
  stragglers; drain stays dishonest. Rejected.

---

## 5. Acceptance criteria

1. `NewScaffold` with an injected listener serves connections without
   binding; with `ScaffoldAddr(":0")` it binds and `Addr()` reports the real
   port. `var _ server.Server = (*Scaffold)(nil)` compiles.
2. Cancelling `Run`'s ctx stops the accept loop, cancels every per-conn
   context, drains registered sessions, and returns nil on a clean drain; a
   session still alive past the deadline is force-closed and reported.
3. A handler panic is recovered and logged; other connections are unaffected;
   the accept loop continues.
4. Temporary accept errors back off and recover; a terminal accept error is
   returned from `Run` (test with a closed-then-failing fake listener).
5. `Registry.Drain` prefers `Drainer.Drain` over `Close`, waits bounded, and
   closes late registrations immediately during drain (race-tested).
   `Reserve` returns ok=false once drain has begun; a reservation taken
   before drain is awaited by `Drain` and its completed session drained
   politely (race-tested: no accept-then-instant-close window).
6. `httpserver.Shutdown` drains registry sessions in addition to
   `http.Server.Shutdown`; a registered hijacked connection blocks shutdown
   until drained or deadline (this is ADR-0007's enabling test).
7. `WithListener` on `httpserver` serves an injected listener (no bind);
   `WithTLSConfig` performs an mTLS handshake in an integration-style test;
   `TLS(cert, key)` behavior is unchanged.
8. With `WithLogger` set, an `http.Server`-level error (e.g. a TLS handshake
   failure) lands in the logger at Error severity.
9. `Readyz` returns 200 while serving and 503 once Shutdown begins;
   `WithConnMetrics` observes state transitions with a correct active gauge.
10. The full pre-ADR server test suite passes unmodified.

## 6. File plan

| File | Change |
|---|---|
| `server/scaffold.go` | new — `Scaffold`, `ConnHandler`, `ScaffoldOption` set |
| `server/registry.go` | new — `Session`, `Drainer`, `Registry` |
| `server/scaffold_test.go`, `server/registry_test.go` | new — criteria 1–5 |
| `server/http/server.go` | `WithListener`, `WithTLSConfig`, ErrorLog wiring, `WithConnMetrics`, registry-aware `Shutdown`, `Sessions()` |
| `server/http/health.go` | new — `Healthz`, `Readyz` + drain gate |
| `server/http/server_test.go` | criteria 6–10 |
| `docs/server/adr-0001…` | unchanged (this ADR records the scope amendment) |
---

## 7. Review history

- **r1 (2026-07-04, lector, combined 0006/0007 review):** `change_requested` —
  review doc `agents/lector/reviews/2026-07-04-golib-server-adr-0006-0007-review.md`.
  The must-fix targeted ADR-0007's drain-gate promise; the amendment lands
  here as the atomic `Reserve`/`Reservation` registry API (revision 2), which
  0007 consumes pre-handshake. Split/scope/zero-dep judged sound.
- **r2 (2026-07-04, lector): `approved`** — review doc:
  `agents/lector/reviews/2026-07-04-golib-server-adr-0006-0007-rereview.md`.
  The r1 blocker is closed by the Reserve/Reservation pre-establishment gate.
  **Accepted 2026-07-04.**

## 8. Amendments

- **A1 (2026-08-16, via ADR-0008): `ScaffoldSessionFactory` +
  `SessionFromContext`.** The scaffold auto-registered a bare conn-closing
  `connSession` for every accepted connection, with no way for a transport to
  substitute a `Drainer`-capable session — registering a second session for
  the same conn would make `Registry.Drain` race a bare `Close()` against the
  polite path. The additive option `ScaffoldSessionFactory(func(ctx, conn)
  Session)` replaces the registered session (nil result falls back to the
  default), and `SessionFromContext(ctx)` hands the ConnHandler the session
  registered for its connection. Default behavior is unchanged; first
  consumer is `server/rpc` (ADR-0008 §2.4).
