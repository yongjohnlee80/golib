# server/http

The HTTP transport for golib's [`server`](../README.md) core — package
`httpserver`. A `net/http` server with chi-style routing, builder middleware,
JSON helpers, health checks, a drain-aware graceful lifecycle, and a loopback
mock for tests. Depends only on the stdlib, [`golib/logger`](../../logger),
and [`golib/server`](../README.md).

```go
import httpserver "github.com/yongjohnlee80/golib/server/http"
```

## Quick start

```go
srv := httpserver.New(
    httpserver.Addr(":8080"),
    httpserver.WithLogger(log),
    httpserver.Middlewares(
        httpserver.Recover(log),
        httpserver.RequestLogger(log),
        httpserver.RequestID(),
    ),
)

srv.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
    id := httpserver.URLParam(r, "id")
    httpserver.JSON(w, 200, findUser(id))
})

srv.Get("/healthz", httpserver.Healthz())
srv.Get("/readyz", srv.Readyz())

log.Fatal(srv.RunUntilSignal(context.Background())) // serve until SIGINT/SIGTERM, then drain
```

## Construction

`New(opts...)` applies conservative defaults; a `0` duration on a timeout option
means "no timeout" explicitly.

| Option | Default | Effect |
|---|---|---|
| `Addr(a)` | `:8080` | listen address |
| `ReadTimeout(d)` | 15s | max time to read the whole request |
| `ReadHeaderTimeout(d)` | 5s | header read cap (Slowloris guard) |
| `WriteTimeout(d)` | 15s | end-of-headers to end-of-response |
| `IdleTimeout(d)` | 60s | keep-alive idle cap |
| `ShutdownTimeout(d)` | 10s | grace period `Run` grants on ctx cancel |
| `MaxHeaderBytes(n)` | 1 MiB | request header cap |
| `BaseContext(ctx)` | — | parent context for every request |
| `WithLogger(l)` | Nop | lifecycle + (via middleware) request logging; also wires `http.Server.ErrorLog` |
| `Middlewares(mw...)` | — | global middleware, outermost, accumulates |
| `TLS(cert, key)` | — | HTTPS from PEM files |
| `WithTLSConfig(cfg)` | — | full TLS control (mTLS, min version, ACME); precedence over `TLS` |
| `WithListener(ln)` | — | serve an injected listener (tests, socket activation); overrides `Addr` |
| `WithConnMetrics(fn)` | — | `func(http.ConnState, active int)` — the metrics seam |
| `NotFound(h)` / `MethodNotAllowed(h)` | JSON envelope | override 404 / 405 handlers |

## Lifecycle

```go
srv.Listen(ctx)  // bind synchronously (Addr resolves; idempotent)
srv.Serve()      // block serving the bound listener
srv.Start()      // Listen + Serve (blocking)
srv.Run(ctx)     // bind, serve on a goroutine, graceful shutdown on ctx cancel
srv.RunUntilSignal(ctx) // Run + SIGINT/SIGTERM
srv.Shutdown(ctx)
srv.Addr()       // resolved address (real port after ":0")
```

`Shutdown` flips the readiness gate first (so `Readyz` returns 503 and load
balancers stop routing), then runs `http.Server.Shutdown` (in-flight requests)
**and** `Registry.Drain` (hijacked/long-lived connections — WebSocket, SSE)
concurrently on the shared deadline. Register such connections with
`srv.Sessions()`.

## Routing

chi-style, on the shared `server.Router`. Patterns are `"METHOD /path"` with
`{param}` and `{rest...}` wildcard segments.

```go
srv.Get("/a", h); srv.Post("/a", h); srv.Put(...); srv.Patch(...); srv.Delete(...); srv.Head(...); srv.Options(...)
srv.Handle("GET /a", h)        // explicit method+pattern
srv.HandleFunc("GET /a", fn)
srv.Mount("/static/", fs)      // sub-handler, prefix stripped

api := srv.Group("/api", authMiddleware) // prefix + baked-in middleware
api.Get("/users/{id}", h)
v2 := api.Group("/v2")                     // nestable
scoped := srv.With(rateLimit)              // extra middleware, same prefix

httpserver.URLParam(r, "id") // read a captured path param inside a handler
```

Registering a bad/duplicate pattern, or **any** route after the server has
started, panics — a late registration would be a data race against the running
router (register all routes before `Run`).

## JSON helpers

```go
httpserver.JSON(w, 201, v)                     // encode v as JSON with status
httpserver.Error(w, 404, "not found")          // {"status":404,"message":"not found"}
u, err := httpserver.Decode[User](r, 1<<20)    // decode body, reject unknown fields, bound size

// Error-returning handlers: *StatusError → its status, anything else → 500.
srv.Handle("POST /users", httpserver.Handler(func(w http.ResponseWriter, r *http.Request) error {
    u, err := httpserver.Decode[User](r, httpserver.DefaultMaxBodyBytes)
    if err != nil {
        return httpserver.Status(400, "invalid body")
    }
    return httpserver.JSON(w, 201, create(u))
}))
```

The error envelope is JSON-schema-compatible with
[`request.Error`](../../request), so a golib client decodes a golib server's
errors directly.

## Built-in middleware

```go
httpserver.Recover(log)        // panic → 500 JSON, logged at Error (nil logger = Nop)
httpserver.RequestLogger(log)  // one structured Info line per request
httpserver.RequestID()         // honor/generate X-Request-ID, echo it, stash in ctx
httpserver.RequestIDFrom(ctx)  // read it back
httpserver.Auth(verify)        // *StatusError → its status, any other error → 401
```

`RequestLogger` wraps the `ResponseWriter` to record the status **while
correctly forwarding** `http.Hijacker`/`Flusher`/`Pusher`/`io.ReaderFrom` — so
WebSocket upgrades and SSE keep working behind the middleware (a common
foot-gun in naive status-recording wrappers).

## Health

```go
httpserver.Healthz()  // liveness — always 200 "ok"
srv.Readyz()          // readiness — 200 until Shutdown begins, then 503 "draining"
```

## Testing with MockServer

`NewMock(opts...)` is a loopback double — a real `httptest` server in front of
the same routing core — with request recording and canned stubs:

```go
m := httpserver.NewMock()
defer m.Close()
m.Get("/users/{id}", handler).
  Stub("GET", "/ping", 200, map[string]string{"ok": "yes"}) // canned JSON, chainable

resp, _ := m.Client().Get(m.URL() + "/ping")
recorded := m.Recorded() // []RecordedRequest snapshot; m.Reset() clears records
```

## File layout

| File | Contents |
|---|---|
| `server.go` | `Server`, `New`, options, lifecycle |
| `routing.go` | `Group`, method sugar, `Mount`, `URLParam` |
| `json.go` | `JSON`, `Error`, `Decode[T]`, `Handler`, `StatusError`, `Status` |
| `middleware.go` | `Recover`, `RequestLogger`, `RequestID`, `Auth` |
| `health.go` | `Healthz`, `Readyz` |
| `mock.go` | `MockServer`, `NewMock`, `RecordedRequest` |

## License

See [LICENSE](../../LICENSE).