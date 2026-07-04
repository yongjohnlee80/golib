# server/ws

WebSocket transport on the golib HTTP core (golib-server ADR-0007): endpoints
are ordinary routes — routed, grouped, and wrapped by the same middleware as
any handler — with a ctx-first `Session` API and honest lifecycle.

## Install

The `ws` package is the only golib package with a WebSocket dependency
(`github.com/coder/websocket`); the server core stays dependency-free.

## Usage

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

## Lifecycle

`Handler` reserves a slot in the server's session registry **before** the
handshake: during shutdown, new upgrade attempts receive a plain HTTP 503 —
never a successful upgrade followed by an immediate close. Established
sessions drain politely on `Shutdown`: a `StatusGoingAway` close frame, a
bounded wait for the handler to finish, then force-close at the deadline.

## Options

| Option | Default | Purpose |
|---|---|---|
| `Subprotocols(...)` | none | advertise/negotiate subprotocols |
| `InsecureAllowOrigins(...)` | same-origin only | relax the CSWSH defense — deliberately loud name |
| `ReadLimit(n)` | 1 MiB | per-message cap; oversize closes with `StatusMessageTooBig` |
| `Keepalive(interval, timeout)` | 30s / 10s | ping/pong dead-peer detection; `0` disables |
| `WithLogger(l)` | Nop | upgrade/lifecycle errors |

## Concurrency contract

At most one concurrent reader and one concurrent writer per `Session` (the
protocol's constraint).

## License

See [LICENSE](../../LICENSE).
