# server/rpc

Connection-oriented RPC transport core over a pluggable wire codec
(golib-server ADR-0008), built on the [`server`](../README.md) scaffold: it
inherits bind/TLS/accept-backoff/drain and adds the RPC lifecycle — a
per-connection read loop, bounded concurrent dispatch, per-request contexts,
a pre-dispatch gate, and polite drain that flushes in-flight replies before
closing. The first codec is [`msgpackrpc`](msgpackrpc/), the framing Neovim
speaks natively.

```go
import (
    "github.com/yongjohnlee80/golib/server/rpc"
    "github.com/yongjohnlee80/golib/server/rpc/msgpackrpc"
)
```

## Usage

```go
srv := rpc.New(msgpackrpc.New(nil),
    rpc.Addr("127.0.0.1:9741"),
    rpc.WithLogger(log),
    rpc.WithGate(func(s *rpc.Session, method string) error {
        if method == "sys.hello" {
            return nil // the handshake itself is always reachable
        }
        if ok, _ := s.Value("authed").(bool); !ok {
            return &rpc.Error{Code: rpc.CodeAccessDenied, Message: "handshake required"}
        }
        return nil
    }),
)

srv.Handle("sys.hello", func(ctx context.Context, req *rpc.Request) (any, error) {
    // ...verify credentials in req.Params...
    req.Session.SetValue("authed", true)
    return "ok", nil
})
srv.Handle("db.query", func(ctx context.Context, req *rpc.Request) (any, error) {
    rows, err := run(ctx, req.Params)
    if err != nil {
        return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
    }
    return rows, nil
})

err := srv.Run(ctx) // serves until ctx cancels, then drains politely
```

## Semantics

- **Dispatch.** Requests and notifications run handlers on a per-connection
  worker pool (`MaxConcurrent`, default 8); the read loop blocks at the
  bound — backpressure, not goroutine growth. Responses serialize through a
  per-connection write lock and always echo the request's msgid; ordering
  across concurrent requests is not guaranteed (msgid matching is the
  contract).
- **Contexts.** Each handler's ctx derives from the connection's, which
  derives from `BaseContext`; `Shutdown` cancels them all, and the polite
  drain window then lets winding-down handlers flush their final replies.
- **Errors.** Return `*rpc.Error` for a structured wire error
  (`{code, message}`); any other error reaches the peer as `CodeInternal`
  with its `Error()` text. Handler panics are recovered, logged, and
  answered with a generic internal error — the panic value never reaches
  the wire, and the connection survives.
- **Gate.** Consulted before every dispatch with the connection's
  `*Session` — per-connection handshake state, not global. Gated requests
  answer the error without invoking the handler; gated notifications drop
  with a log line (no reply channel exists).
- **Protocol hygiene (R7).** Unknown methods answer `CodeMethodNotFound`
  and the connection survives. A frame that fails to decode, violates the
  codec's shape rules, or overruns `MaxMessageBytes` (default 16 MiB)
  closes the connection: a peer that cannot frame correctly is done.
  Unexpected responses (this server issues no requests in v1) drop with a
  log line.

## Not in v1

No Go client (lands with the TUI consumer), no streaming responses, no
jsonrpc codec — the `Codec` seam is where one would plug in.
