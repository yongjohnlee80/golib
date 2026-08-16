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
- **`Limits` (R4):** per-item bounds — `MaxDepth` (default 64),
  `MaxStrBytes`/`MaxBinBytes` (default 8 MiB), `MaxElements` per collection
  (default 1 M) — PLUS whole-decode aggregate budgets (r2, review finding):
  `MaxTotalElements` (default 1 M decoded values) and `MaxTotalBytes`
  (default 16 MiB payload bytes). Per-item limits alone don't bound a
  message packed with many maximal siblings: sixteen 1M-element nil arrays
  fit one 16 MiB frame but decode to ~256 MiB. The aggregate budgets make
  decoded footprint LINEAR in the budgets and tunable — as an ESTIMATE (r3),
  not a byte ceiling: `MaxTotalElements×(per-value overhead) +
  MaxTotalBytes`, where the per-value overhead is one interface word
  (16 bytes) for scalars but several times that for container-heavy shapes
  (an empty map costs an interface word + hmap header + allocator
  overhead). Consumers size the budgets for the shapes they expect;
  defaults land in the tens of MiB per decode. Zero/negative
  Limits fields fall back to defaults — a partial `Limits` can tighten or
  explicitly loosen bounds but never silently disable one. Collection
  preallocation is capped at min(declared, 4096) and grown by append, so a
  forged `array32(0xffffffff)` header cannot allocate memory it never
  supplies. Depth/limit violations return typed sentinels
  (`ErrDepthExceeded`, `ErrLimitExceeded`, `ErrMalformed`); the decoder
  NEVER panics on any input (fuzz-enforced).
- **Clean EOF (r2):** end-of-stream before ANY byte of a value is `io.EOF`
  (polite hang-up); truncation inside a value is `ErrMalformed`. Transports
  log the two differently. `Unmarshal` keeps its one-shot contract (empty
  input is malformed).
- **Encoder depth bound (r2):** `Encode` refuses nesting beyond a fixed
  depth (256) with `ErrDepthExceeded`, so a cyclic container value —
  server-side bug, not attacker input — fails loudly instead of exhausting
  the stack.

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

// Codec owns the wire: one Message in, one Message out. ONE instance is
// shared across every live connection: the transport serializes reads and
// writes per connection, but different connections call concurrently, so
// implementations must be stateless or internally synchronized (r2 —
// msgpackrpc is stateless and copies its Limits at construction).
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
// BaseContext, DrainTimeout, MaxMessageBytes (default 16 MiB, BOTH
// directions), MaxConcurrent per conn (default 8),
// WithGate(func(s *Session, method string) error).
// Construction validates options (r2): nil codec, non-positive
// MaxConcurrent/MaxMessageBytes/DrainTimeout panic in New instead of
// misbehaving per connection at runtime; a nil logger falls back to Nop.
// Value-decode limits are NOT a transport option: they belong to the codec
// (msgpackrpc.New(lim)); the transport bounds raw bytes via MaxMessageBytes.
func (s *Server) Handle(method string, h Handler)
// server.Server: Run(ctx), Shutdown(ctx), Addr().

// Wire error codes (JSON-RPC-aligned where a standard code exists):
// CodeMethodNotFound -32601, CodeInvalidParams -32602 (for handlers),
// CodeInternal -32603, CodeAccessDenied -32001 (gate rejection).
```

Semantics (r2 revisions marked):

- **Read loop per connection** (bufio over the raw conn; each message read
  through a resettable window of `MaxMessageBytes`). **Slot-before-read
  (r2):** the loop acquires its `MaxConcurrent` execution slot BEFORE
  decoding the next message, so per-connection retention of decoded
  messages is itself capped at `MaxConcurrent` — no message is decoded
  without a bounded home. Responses serialize through a write mutex and
  always echo the request's msgid. Notifications dispatch the same way,
  minus the reply. **Saturation liveness policy (r2, documented
  trade-off):** while all slots are busy the connection is not read, so a
  peer disconnect is observed only when a slot frees — bounded by handler
  completion, which Shutdown bounds via context cancellation.
- **Connection-scoped cancellation (r2):** each connection owns a child
  context cancelled when its read loop exits for ANY reason — peer
  disconnect, malformed frame, drain, Shutdown. Per-request contexts derive
  from it, so a handler waiting on ctx.Done() always winds down when its
  client disappears; the connection then waits for in-flight handlers to
  flush final replies before closing (the polite-drain window).
- **Drain-safe admission (r2):** request admission (`WaitGroup.Add`) and the
  draining flag share one mutex, so drain's wait can never race a
  just-admitted request; a message decoded after drain begins is refused,
  not half-served. Drain waits on the connection's owned completion signal
  (no per-drain watcher goroutine), bounded by the drain context.
- **Staged replies (r2, streaming bound r3):** every response is encoded
  COMPLETELY into a per-connection staging buffer before any byte reaches
  the socket, and the `MaxMessageBytes` bound is enforced WHILE the reply
  encodes (a capping writer under the staging bufio) — an over-bound
  handler result is refused as it streams, so staging memory never exceeds
  the bound plus one bufio flush chunk regardless of the value's size. An
  unencodable or over-bound result therefore never poisons the stream: it
  is logged and replaced by a generic internal-error reply (the msgid still
  gets answered); a socket-level write failure closes the connection. The
  panic boundary covers the whole request task — handler, response
  construction, encode, write; a reply-path panic closes the connection
  (write-stream state unknown).
- **Gate (`WithGate`)**: consulted before dispatch of every request on a
  connection; the consumer implements handshake-before-methods (autodb's
  `sys.hello`, ADR-0056 §2) without the core hardcoding policy. Gate
  rejection answers without invoking the handler.
- **Unknown method** → `Error{CodeMethodNotFound}` response, connection
  survives. **Malformed frame / codec error** → log + connection close (a
  peer that cannot frame correctly is not negotiable — R7). A clean
  `io.EOF` at a frame boundary logs as a normal close, not a drop (r2).
- **Errors on the wire (r2 — deny-before-disclose):** ONLY `*rpc.Error`
  text is public, encoding as `{code:int64, message:string}`. Any other
  handler or gate error is logged server-side with full detail and crosses
  the wire as a generic `{CodeInternal, "internal error"}` (handlers) or
  `{CodeAccessDenied, "access denied"}` (gates) — raw error text can carry
  paths, hostnames, query fragments, or credentials. Handler panic detail
  likewise never reaches the wire.

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

## 6. Review history

- **r1 (2026-08-16, lector): `change_requested`** — review doc
  `agents/lector/reviews/2026-08-16-golib-rpc-m5-review.md` (KB). Six
  must-fixes, all verified CONFIRMED by the author: (1) per-container
  limits permitted ~16× decoded amplification → aggregate
  `MaxTotalElements`/`MaxTotalBytes` budgets + slot-before-read; (2) drain
  raced admission through an invalid WaitGroup lifecycle → mutex-guarded
  admission gate + owned completion signal; (3) peer disconnect didn't
  cancel handler contexts → connection-scoped cancellation on read-loop
  exit; (4) reply encoding could poison the persistent stream → staged
  frames, substitute error replies, outbound bounds, task-wide panic
  boundary; (5) untyped errors disclosed internals → *Error-only public
  text; (6) session factory ran outside the scaffold's panic boundary →
  defers installed first. Should-fixes folded: cross-connection Codec
  contract documented + Limits copied; construction-time option
  validation; clean-EOF classification (io.EOF vs ErrMalformed). The r2
  markers in §2.2/§2.3 are this fold.

- **r2 (2026-08-16, lector): `change_requested`** — five of six r1
  must-fixes confirmed closed, all r1 should-fixes folded; the substitute-
  reply refinement to finding 4 accepted. One residual blocker: the
  outbound bound was checked only AFTER the full reply had accumulated in
  staging, so a huge handler value could exhaust memory before refusal →
  fixed with a capping writer that refuses bytes DURING encoding (staging
  never exceeds the bound + one bufio chunk). Should-fix: the "~32 MiB
  worst-case footprint" arithmetic overstated precision (empty-map
  counterexample) → reworded as a linear, shape-dependent estimate here,
  in the package docs, and in the README. The r3 markers are this fold.
