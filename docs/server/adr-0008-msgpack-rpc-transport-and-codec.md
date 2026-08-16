# ADR-0008 — `golib/server/rpc`: msgpack-RPC transport & zero-dep msgpack codec

- **Status:** **Proposed** (2026-08-16; authored by ultron-prime for autodb M5 —
  implementation lands on the `rpc-m5` branch; acceptance at branch review.
  Directive basis: Johno 2026-08-16 — golib is the server/comms base library;
  the RPC foundation lands here first and autodb consumes the release. The
  codec is hand-rolled zero-dep by explicit decision, option B over a
  third-party msgpack library.)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive; includes one additive amendment to
  ADR-0006's Scaffold)
- **Related:** ADR-0006 (transport scaffold & session registry — this is its
  first real consumer), ADR-0007 (websocket — the other session transport),
  ADR-0001 (server overview), KB conventions `security-core-hardening`
  (R4/R7/R11 applied to wire parsing), autodb ADR-0056 (the consumer contract)

> **Self-containment contract.** Like ADR-0006/0007, this document is written
> so an implementer with no prior context can build the feature: concrete
> signatures, wire rules, files to create/modify, and acceptance criteria.

---

## 1. Context

autodb M5 needs a msgpack-RPC server over TCP that Neovim can speak natively
(`sockconnect(..., {rpc = true})`). Per the 2026-08-16 directive, the
reusable machinery belongs in golib's server subsystem, beside `server/http`
and `server/ws`.

Ground truth (survey 2026-08-16):

- `server.Scaffold` (ADR-0006) provides bind/TLS/accept-with-backoff/
  per-conn-goroutine/panic-isolation/drain, and hands a transport exactly one
  seam: `ConnHandler func(ctx, net.Conn)` (`server/scaffold.go:18`). **No
  shipped transport consumes it yet** — this package is the "future
  transport" ADR-0006 §2.4 sketched.
- **Scaffold constraint:** `serveConn` auto-registers a `connSession` that
  implements `Session` but not `Drainer` (`server/scaffold.go:197-202`).
  A transport that wants polite drain (finish in-flight replies before
  close) cannot register its own `Drainer` session for the same conn —
  `Registry.Drain` would run both concurrently, racing a bare `Close()`
  against the polite path. An additive amendment is required (§2.4).
- **No codec seams exist anywhere in golib** — `Codec` here is the first.
- House idioms: void functional options, `logger.Logger` injected via option
  defaulting `Nop`, `map[string]any` log payloads with a `"server"` key,
  ctx-first, stdlib-only tests, race tests for concurrency claims.

Security framing: an RPC socket decodes attacker-adjacent bytes. The KB
convention `security-core-hardening` applies — R4 (every input length/count
is hostile until validated), R7 (parse properly, reject what you cannot
classify), R12-style bounded resource use. The codec ships with fuzz
coverage and is flagged for lector's adversarial review.

## 2. Decision

### 2.1 Package layout

- **`msgpack/`** (top-level, zero-dep, stdlib only) — the pure value codec:
  encode/decode of the full MessagePack family. Standalone because it is
  reusable beyond RPC (files, IPC, caches) and keeps `server/rpc` free of
  encoding detail.
- **`server/rpc/`** (zero-dep: stdlib + `server` + `logger`) — the transport
  core: message model, `Codec` seam, handler registry, per-connection read
  loop with bounded concurrent dispatch, per-request contexts, resource
  limits, gate hook; satisfies `server.Server` by wrapping `Scaffold`.
- **`server/rpc/msgpackrpc/`** — the msgpack-RPC wire codec (framing per the
  msgpack-RPC spec, values via `msgpack`): the first `Codec`, and the one
  Neovim speaks.

### 2.2 `msgpack` — the value codec

Value model (the `any` vocabulary):

| Go type | encodes to | decodes from |
|---|---|---|
| `nil` | nil | nil |
| `bool` | bool | bool |
| `int`,`int8..64` | most compact int family | int64 (any int/uint that fits) |
| `uint`,`uint8..64` | most compact uint family | `uint64` only when > MaxInt64 |
| `float32`/`float64` | float32/float64 | float64 |
| `string` | fixstr/str8/16/32 | string |
| `[]byte` | bin8/16/32 | []byte |
| `[]any` | fixarray/array16/32 | []any |
| `map[string]any` | fixmap/map16/32 (string keys) | map[string]any |
| `Ext{Type int8, Data []byte}` | fixext1..16/ext8/16/32 | Ext |

- `func Encode(w *bufio.Writer, v any) error`, `func Decode(r *bufio.Reader,
  lim *Limits) (any, error)`; `Marshal`/`Unmarshal` byte-slice conveniences.
- **Map keys are strings, both directions.** A decoded map with a non-string
  key is `ErrNonStringKey` — loud, documented; msgpack-RPC as used here (and
  by Neovim's API payloads) needs nothing else. Encoding accepts only
  `map[string]any`.
- Integers: decode never silently truncates — every int form widens to
  int64; uint64 values above MaxInt64 decode as `uint64` (callers that care
  type-switch). Timestamps (ext -1) are NOT special-cased in v1: they pass
  through as `Ext` (documented; Neovim does not use them).
- **`Limits` (R4):** `MaxDepth` (default 64), `MaxStrBytes`/`MaxBinBytes`
  (default 8 MiB), `MaxElements` per collection (default 1 M). Collection
  preallocation is capped at min(declared, 4096) and grown by append, so a
  forged `array32(0xffffffff)` header cannot allocate memory it never
  supplies. Depth/limit violations return typed sentinels
  (`ErrDepthExceeded`, `ErrLimitExceeded`, `ErrMalformed`); the decoder
  NEVER panics on any input (fuzz-enforced).

### 2.3 `server/rpc` — the transport core

```go
// Message is one wire unit, codec-independent.
type Message struct {
	Kind   Kind  // KindRequest | KindResponse | KindNotification
	ID     uint32
	Method string
	Params []any
	Err    any
	Result any
}

// Codec owns the wire: one Message in, one Message out. Implementations are
// safe for a single reader + single writer goroutine per connection.
type Codec interface {
	Read(r *bufio.Reader) (*Message, error)
	Write(w *bufio.Writer, m *Message) error
}

// Request is what a Handler receives.
type Request struct {
	Method  string
	Params  []any
	Peer    net.Addr
	Session *Session // per-connection state (gate flags, handshake data)
}

type Handler func(ctx context.Context, req *Request) (any, error)

// Error carries a structured RPC error to the wire ({code, message}).
type Error struct { Code int64; Message string }

func New(codec Codec, opts ...Option) *Server
// Options (void idiom): Addr, WithListener, WithTLSConfig, WithLogger,
// BaseContext, DrainTimeout, MaxMessageBytes (default 16 MiB), MaxConcurrent
// per conn (default 8), WithGate(func(s *Session, method string) error).
// Value-decode limits are NOT a transport option: they belong to the codec
// (msgpackrpc.New(lim)); the transport bounds raw bytes via MaxMessageBytes.
func (s *Server) Handle(method string, h Handler)
// server.Server: Run(ctx), Shutdown(ctx), Addr().

// Wire error codes (JSON-RPC-aligned where a standard code exists):
// CodeMethodNotFound -32601, CodeInvalidParams -32602 (for handlers),
// CodeInternal -32603, CodeAccessDenied -32001 (gate rejection).
```

Semantics:

- **Read loop per connection** (bufio over the raw conn; each message read
  through an `io.LimitedReader` window of `MaxMessageBytes`). Requests
  dispatch on a per-connection bounded worker pool (`MaxConcurrent`);
  responses serialize through a write mutex; the reply always echoes the
  request's msgid. Notifications dispatch the same way, minus the reply.
- **Per-request context** derives from the scaffold's per-conn ctx —
  Shutdown cancels every in-flight handler. Handler panics are recovered,
  logged, and answered with an internal error (the connection survives).
- **Gate (`WithGate`)**: consulted before dispatch of every request on a
  connection; the consumer implements handshake-before-methods (autodb's
  `sys.hello`, ADR-0056 §2) without the core hardcoding policy. Gate
  rejection answers a structured error without invoking the handler.
- **Unknown method** → `Error{CodeMethodNotFound}` response, connection
  survives. **Malformed frame / codec error** → log + connection close (a
  peer that cannot frame correctly is not negotiable — R7).
- **Errors on the wire:** `*rpc.Error` encodes as `{code:int64,
  message:string}`; any other handler error encodes as
  `{code: CodeInternal, message: err.Error()}` — consumers that need
  taxonomy return `*rpc.Error`.

### 2.4 Scaffold amendment (additive; amends ADR-0006)

`server.Scaffold` gains one option:

```go
// ScaffoldSessionFactory replaces the default connSession registered for
// each accepted connection. The factory's Session may implement Drainer for
// polite drain. Default behavior (bare conn-closing session) is unchanged;
// a nil factory result falls back to the default for that connection.
func ScaffoldSessionFactory(fn func(ctx context.Context, conn net.Conn) server.Session) ScaffoldOption

// SessionFromContext hands the ConnHandler the session registered for its
// connection (the factory's product, or the default) — the seam through
// which server/rpc reaches its per-conn machinery.
func SessionFromContext(ctx context.Context) Session
```

`server/rpc` registers a session that implements `Drainer`: on drain it
stops reading new requests, waits for in-flight handlers (bounded by the
drain ctx), flushes final replies, then closes. Without the amendment, the
auto-registered bare session races `Close()` against in-flight replies
(survey finding). The change is a strict superset of current behavior and
gets its own paragraph in ADR-0006's amendment history.

### 2.5 `server/rpc/msgpackrpc` — the wire codec

msgpack-RPC per the spec Neovim implements:

- Request `[0, msgid(uint32), method(str), params(array)]`; response
  `[1, msgid, error, result]` (error nil on success, result nil on error);
  notification `[2, method, params]`.
- `Read` decodes exactly one top-level value via `msgpack.Decode` and
  validates shape strictly: top-level must be a 3/4-element array, type tag
  0/1/2, msgid within uint32, method a string, params an array — anything
  else is `ErrProtocol` (connection closes). `Write` is the inverse.
- Neovim EXT handle types (Buffer=0, Window=1, Tabpage=2) pass through as
  `msgpack.Ext` untouched — the transport does not interpret them.

### 2.6 What this deliberately is not

No client implementation in v1 (autodb's FEs connect in-process or via
Neovim's native client; a Go client lands when the TUI needs it — likely
alongside M6). No streaming/chunked responses (M5 pages; cursor protocol is
a later ADR). No jsonrpc codec yet (M9 may add one against the same seam).

## 3. Consequences

**Easier:** golib finally exercises the ADR-0006 scaffold with a real
transport; autodb's `rpc/` becomes a mechanical shim; the msgpack codec is
reusable module-wide with zero new dependencies; the codec seam gives M9 a
path to alternate wire formats.

**Harder:** golib owns security-sensitive wire parsing — accepted
deliberately (option B) with fuzzing + adversarial review as the price;
the Scaffold amendment slightly widens ADR-0006's surface (strictly
additive).

## 4. Files

- `msgpack/{doc.go, encode.go, decode.go, limits.go, msgpack_test.go,
  fuzz_test.go, README.md}`
- `server/rpc/{doc.go, message.go, server.go, session.go, options.go,
  rpc_test.go, README.md}`
- `server/rpc/msgpackrpc/{doc.go, codec.go, codec_test.go, README.md}`
- `server/scaffold.go` (+`ScaffoldSessionFactory`), `server/scaffold_test.go`
- `docs/server/adr-0006-…` (amendment note), this ADR; KB mirrors.

## 5. Acceptance criteria

1. `msgpack`: round-trip property tests over the full value vocabulary;
   adversarial table (truncated input at every state, forged giant length
   headers, depth bombs, non-string map keys, float/int edge values);
   `FuzzDecode` runs clean (no panics, bounded allocation) with a seeded
   corpus; race-irrelevant (stateless) but `-race` clean.
2. `server/rpc` + `msgpackrpc`: real-TCP e2e — register handlers, drive
   requests/notifications from a raw test client speaking hand-encoded
   msgpack-RPC; concurrent requests on one connection answer correctly with
   msgid fidelity under `-race`; unknown method and gate rejection answer
   structured errors without dropping the connection; malformed frame closes
   it; Shutdown cancels in-flight handler contexts and polite drain flushes
   a reply in progress (the ScaffoldSessionFactory path).
3. Scaffold amendment covered by a scaffold test (custom session factory
   observed in the registry; default path byte-identical).
4. Interop smoke (manual, documented in README): an nvim `sockconnect`
   client calls a demo method — deferred to autodb M5's integration if a
   live nvim is unavailable in CI.
