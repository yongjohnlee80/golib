# server

The shared, transport-agnostic core for golib's server subsystem. Zero external
dependencies — it imports no `net/http`. It provides the pieces every transport
reuses: a generic tree **router**, a builder-pattern **middleware chain**, a
**lifecycle** contract, and — for connection-oriented transports — an
**accept-loop scaffold** and a **drain-aware session registry**.

Transports build on top of this core:
[`server/http`](http/README.md) (HTTP) and [`server/ws`](ws/README.md)
(WebSocket) today; gRPC / SFTP / raw-TCP adapters slot in the same way.

```bash
go get github.com/yongjohnlee80/golib/server
```

```go
import "github.com/yongjohnlee80/golib/server"
```

## Two transport families

The core deliberately serves two shapes of transport, and it is honest about
which pieces each uses:

- **Request/response-with-path** (HTTP, WebSocket, anything mounted on an HTTP
  router). These use `Router[H]` + `Chain[H]` — `Handle(method, path)` and the
  wrap-the-handler middleware idiom fit them exactly.
- **Session / interceptor** (gRPC, SFTP, raw TCP). These implement the
  `Server` lifecycle interface and build on `Scaffold` + `Registry`, but
  **bypass `Router`/`Chain` by design** — their session/handshake/subsystem
  models don't share method+path semantics, and forcing a router onto them
  would make a worse API for everyone.

The one seam both families share is the `Server` lifecycle contract.

## The lifecycle contract

```go
type Server interface {
    Run(ctx context.Context) error       // serve until ctx cancel, then graceful shutdown
    Shutdown(ctx context.Context) error   // drain in-flight work, bounded by ctx
    Addr() string                          // resolved address (real port after ":0")
}
```

Both `httpserver.Server` and `server.Scaffold` satisfy it.

## Router[H]

A generic segment-tree router. `H` is the handler type — the core never
constrains it, so HTTP instantiates `Router[http.Handler]` while another
transport could use its own.

```go
r := server.NewRouter[http.Handler]()
r.Handle("GET", "/users/{id}", handler)         // static, {param}, {rest...} wildcard
sub := r.Group("/api")                            // shared prefix on the same tree
res := r.Match("GET", "/users/42")                // MatchResult[H]
```

`Match` returns a structured `MatchResult[H]`: `Found` (false → 404),
`MethodAllowed` (`Found && !MethodAllowed` → 405), `AllowedMethods` (sorted,
drives the `Allow` header), `Handler`, and `Context` (captured path params, nil
for a fully static route). Precedence is static > param > wildcard; a trailing
`{rest...}` wildcard must be the last segment. `Handle` returns an error on an
empty method, a non-final wildcard, or a duplicate route. `Match` is safe for
concurrent use once the tree is built.

## Chain[H] / Middleware[H]

An **immutable** builder for middleware. `Use`/`When`/`Extend` each return a new
`Chain` over a copied slice — they never mutate the receiver.

```go
type Middleware[H any] func(next H) H

c := server.NewChain[http.Handler](recover, logging).
    Use(requestID).
    When(cfg.Auth, auth) // conditional
h := c.Then(finalHandler) // first added is OUTERMOST
```

## RouteContext

A `map[string]string` of captured path params with a nil-safe `Param(name)` and
a defensive-copy `Params()`.

## Scaffold — the accept loop for connection transports

`Scaffold` owns the lifecycle plumbing that routing does not: bind (or accept an
injected listener), optional TLS, a per-connection goroutine + context,
structured accept-error handling with capped backoff, per-connection panic
isolation, and drain-aware shutdown. You supply a `ConnHandler`; you get a full
`Server`.

```go
type ConnHandler func(ctx context.Context, conn net.Conn)

sc := server.NewScaffold(handle,
    server.ScaffoldAddr(":9000"),
    server.WithListener(ln),          // inject a pre-bound listener (tests, socket activation)
    server.WithTLSConfig(tlsCfg),
    server.ScaffoldLogger(log),
    server.ScaffoldBaseContext(ctx),
    server.DrainTimeout(15*time.Second),
)
err := sc.Run(ctx) // bind synchronously (Addr resolves), serve, drain on cancel
```

`Run` returns bind errors before spawning anything, and a terminal accept error
is returned (never swallowed). The per-connection context is cancelled when
`Shutdown` begins, so long-lived handlers wind down.

## Registry — drain-aware sessions

`Registry` tracks live sessions so graceful shutdown can actually drain them —
`http.Server.Shutdown` ignores hijacked connections (WebSocket, SSE), so a
transport registers its sessions here instead.

```go
type Session interface{ Close() error }
type Drainer interface{ Drain(ctx context.Context) error } // optional: end politely

unregister := reg.Register(session)   // established session
defer unregister()

// Drain (during Shutdown) prefers Drainer.Drain over Close, waits bounded by
// ctx, and force-closes + counts stragglers at the deadline.
```

### The pre-establishment gate

`Reserve` claims a slot **before** establishment, and it exists for two reasons —
a transport needs it if *either* applies:

1. **Refusal must happen before a protocol handshake commits.** A WebSocket
   upgrade can't send an HTTP 503 after `101 Switching Protocols`.
2. **An establishment that hasn't registered yet is invisible to `Drain`.** A
   drain starting mid-establishment finds nothing to wait for and reports a
   clean shutdown while the connection is still being set up. This applies to
   **every** transport that establishes sessions, including ones with no
   refuse-before-commit problem at all.

Reason 2 is easy to miss, and missing it is not hypothetical: `Scaffold` itself
registered only after the session existed and could abandon an accepted
connection while returning `nil` (fixed in #25). If you build on `Scaffold` you
get the reservation for free — it reserves at accept, before your `ConnHandler`
runs. **If you run your own accept loop against a bare `Registry`, reserving is
your responsibility.**

```go
res, ok := reg.Reserve()
if !ok {
    // Drain has begun — refuse now (cleanly, if your protocol still can)
    return
}
// ... establish (handshake) ...
unregister := res.Complete(session) // or res.Cancel() on failure
```

`Drain` waits for open reservations exactly like live sessions, so an
in-flight establishment that won the race completes and is then drained
politely — never accepted-then-abandoned. `Reserve` returns `ok=false` once the
drain has begun; release a failed establishment with `res.Cancel()`, or the
drain waits out its whole budget for a session that never arrives.

## Adding a transport

A session transport is a `ConnHandler` on a `Scaffold` plus its own sessions in
the `Registry` — no `Router`, no `Chain`:

```go
sc := server.NewScaffold(func(ctx context.Context, conn net.Conn) {
    defer conn.Close()
    sess, err := handshake(conn, hostKey, auth) // transport-owned
    if err != nil {
        return
    }
    unreg := sc.Sessions().Register(sess) // drains on shutdown
    defer unreg()
    sess.Serve(ctx) // transport-owned loop
}, server.ScaffoldAddr(":22"), server.ScaffoldLogger(log))
err := sc.Run(ctx) // full lifecycle: bind, accept, drain — zero rewrites
```

An HTTP-family transport instead instantiates `Router[YourHandler]` +
`Chain[YourHandler]` and satisfies `Server` however it serves (see
[`server/http`](http/README.md), and [`server/ws`](ws/README.md) which is just
routes on the HTTP core).

## File layout

| File | Contents |
|---|---|
| `server.go` | `Server` lifecycle interface |
| `router.go` | `Router[H]`, `MatchResult[H]` |
| `middleware.go` | `Chain[H]`, `Middleware[H]` |
| `context.go` | `RouteContext` |
| `scaffold.go` | `Scaffold`, `ConnHandler`, scaffold options |
| `registry.go` | `Registry`, `Session`, `Drainer`, `Reservation` |

## License

See [LICENSE](../LICENSE).