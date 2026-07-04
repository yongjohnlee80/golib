# server/ws

WebSocket transport on the golib HTTP core (golib-server ADR-0007): endpoints
are ordinary routes — routed, grouped, and wrapped by the same middleware as
any handler — with a ctx-first `Session` API and an honest lifecycle. Built on
[`github.com/coder/websocket`](https://github.com/coder/websocket), the only
WebSocket dependency in golib; the [`server`](../README.md) core stays
dependency-free.

```go
import (
    httpserver "github.com/yongjohnlee80/golib/server/http"
    "github.com/yongjohnlee80/golib/server/ws"
)
```

## Usage

A WebSocket endpoint is just a route. `ws.Handler` returns an `http.Handler`
you register on the server; it upgrades the request and runs your function with
the established session:

```go
srv := httpserver.New(
    httpserver.WithLogger(log),
    httpserver.Middlewares(httpserver.Recover(log), httpserver.RequestLogger(log)),
)

srv.Handle("GET /ws/updates", ws.Handler(srv.Sessions(), func(ctx context.Context, s *ws.Session) {
    for {
        var msg Update
        if err := s.ReadJSON(ctx, &msg); err != nil {
            return // peer closed, shutdown, or read error
        }
        if err := s.WriteJSON(ctx, process(msg)); err != nil {
            return
        }
    }
}, ws.Subprotocols("updates.v1")))
```

`fn` runs on the handler goroutine; returning from it closes the socket with a
normal close frame. Its `ctx` is cancelled at shutdown or on a keepalive
timeout. A panic in `fn` is recovered, logged, and closed as
`StatusInternalError`.

## Session API

All methods take a `ctx` and honor its cancellation:

```go
func (s *Session) Read(ctx) (MessageType, []byte, error)
func (s *Session) Write(ctx, MessageType, []byte) error
func (s *Session) ReadJSON(ctx, v any) error
func (s *Session) WriteJSON(ctx, v any) error       // sends a text message
func (s *Session) Close(code StatusCode, reason string) error
func (s *Session) Subprotocol() string               // negotiated subprotocol, "" if none
func (s *Session) Request() *http.Request            // the upgrade request (auth, path params)
```

`MessageType` is `ws.Text` or `ws.Binary` (control frames are handled
internally). `StatusCode` constants: `StatusNormalClosure`, `StatusGoingAway`,
`StatusInternalError`, `StatusMessageTooBig`.

## Lifecycle

`Handler(reg *server.Registry, fn, opts...)` reserves a slot in the server's
session registry **before** the handshake: during shutdown, new upgrade
attempts receive a plain HTTP 503 — never a successful upgrade followed by an
immediate close (a 503 is impossible after `101 Switching Protocols`, which is
why the gate is pre-handshake). Established sessions drain politely on
`Shutdown`: a `StatusGoingAway` close frame, a bounded wait for the handler to
return, then force-close at the deadline. Pass `srv.Sessions()` as the registry.

## Options

| Option | Default | Purpose |
|---|---|---|
| `Subprotocols(...)` | none | advertise/negotiate subprotocols |
| `InsecureAllowOrigins(...)` | same-origin only | relax the cross-site-WebSocket-hijacking defense — deliberately loud name |
| `ReadLimit(n)` | 1 MiB | per-message cap; oversize closes with `StatusMessageTooBig` |
| `Keepalive(interval, timeout)` | 30s / 10s | ping/pong dead-peer detection; `0` disables |
| `WithLogger(l)` | Nop | upgrade/lifecycle errors |

Origin is enforced same-origin by default; a cross-origin upgrade is refused
with HTTP 403 (visible to `RequestLogger`). Relax it only deliberately via
`InsecureAllowOrigins`.

## Concurrency contract

At most one concurrent reader and one concurrent writer per `Session` (the
protocol's constraint).

## License

See [LICENSE](../../LICENSE).