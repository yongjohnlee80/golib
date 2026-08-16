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
  worker pool (`MaxConcurrent`, default 8). The read loop acquires its slot
  BEFORE decoding the next message, so decoded-message retention is bounded
  too — while saturated, the connection simply isn't read (backpressure,
  not goroutine growth; the documented trade-off is that a disconnect is
  observed when a slot frees). Responses serialize through a per-connection
  write lock and always echo the request's msgid; ordering across
  concurrent requests is not guaranteed (msgid matching is the contract).
- **Contexts.** Each handler's ctx derives from a connection-scoped context
  cancelled when the connection ends for ANY reason — peer disconnect,
  drain, or `Shutdown`. Honor it: a handler waiting on `ctx.Done()` always
  winds down when its client disappears, and the polite drain window lets
  winding-down handlers flush their final replies before the socket closes.
- **Errors (deny-before-disclose).** Only `*rpc.Error` text is public: it
  crosses the wire as `{code, message}`. Any other handler error is logged
  server-side with full detail and reaches the peer as a generic
  `CodeInternal` "internal error" — raw error text can carry paths,
  hostnames, query fragments, or credentials. Handler panics likewise
  answer generically; the connection survives.
- **Reply integrity.** Every response is fully encoded into a staging
  buffer before any byte touches the socket. An unencodable or oversized
  handler result never corrupts the stream — it's logged and replaced by a
  generic internal-error reply; only socket-level failures close the
  connection.
- **Gate.** Consulted before every dispatch with the connection's
  `*Session` — per-connection handshake state, not global. A `*rpc.Error`
  from the gate is the public rejection; any other gate error is logged and
  answered with a stable generic "access denied". Gated notifications drop
  with a log line (no reply channel exists).
- **Protocol hygiene (R7).** Unknown methods answer `CodeMethodNotFound`
  and the connection survives. A frame that fails to decode, violates the
  codec's shape rules, or overruns `MaxMessageBytes` (default 16 MiB)
  closes the connection: a peer that cannot frame correctly is done. A
  clean `io.EOF` between frames logs as a normal close. Unexpected
  responses (this server issues no requests in v1) drop with a log line.
- **Construction.** `New` validates options up front: nil codec,
  non-positive `MaxConcurrent`/`MaxMessageBytes`/`DrainTimeout` panic at
  construction rather than misbehaving per connection.

## Not in v1

No Go client (lands with the TUI consumer), no streaming responses, no
jsonrpc codec — the `Codec` seam is where one would plug in.
