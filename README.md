# golib

A collection of reusable, modular Go packages — general-purpose building blocks
for personal and work projects. The design goal is packages with **light,
adaptable interfaces** that drop into existing applications for easy migration
and give new applications good patterns to start from.

```bash
go get github.com/yongjohnlee80/golib
```

## Design principles

- **Zero-dependency core.** Every package imports only the standard library and
  other golib packages. Third-party dependencies are pushed to leaf subpackages
  — the `dao` database drivers and `server/ws` — and, when heavy (the GCP SDK),
  into their own nested module (`dao/bigquery`).
- **Small, adaptable seams.** Interfaces are minimal (`logger.Logger` is one
  method); consumers bridge their own backends rather than adopting a framework.
- **Data over code.** Per-entity/per-use differences are declarations, not
  boilerplate — the `dao` single-declaration `Schema` is the archetype.
- **Explicit over magic.** No struct-tag-driven behavior in core paths, no
  hidden global state.
- **Fail loud, fail typed.** Sentinel/typed errors compared with `errors.Is`/
  `As`; misconfiguration fails at construction; documented behavior claims
  (thread-safety, capabilities) are true or removed.
- **Ecosystem-normal shapes.** `context.Context` first on I/O calls,
  `io.Writer` destinations, `iter.Seq` iteration, `time.Duration` means a
  duration.

## Packages

| Package | Purpose | Docs |
|---|---|---|
| [`threadsafe`](threadsafe/README.md) | Generic thread-safe value containers (mutex, RWMutex, lock-free) behind one `Value[T]` interface | [README](threadsafe/README.md) |
| [`collections`](collections/README.md) | Generic `Set[T]` and stdlib-shaped `Map`/`Filter`/`Reduce` slice ops | [README](collections/README.md) |
| [`logger`](logger/README.md) | Small level-based logging seam; `Fields`/`Entry`, `Adapt`, and both `slog` bridges | [README](logger/README.md) |
| [`request`](request/README.md) | HTTP client: typed error decoding, functional options, multipart, history | [README](request/README.md) |
| [`ingestor`](ingestor/README.md) | Thread-safe buffer-and-flush pipelines to CSV/JSON with bounded background writes | [README](ingestor/README.md) |
| [`dao`](dao/README.md) | Generic, driver-agnostic data-access layer — declare an entity once | [README](dao/README.md) · [USAGE](dao/USAGE.md) |
| [`partial`](partial/README.md) | Three-state (value/absent/null) PATCH payloads, projecting onto `dao` updates | [README](partial/README.md) |
| [`msgpack`](msgpack/README.md) | Zero-dependency MessagePack value codec with hardened decode limits | [README](msgpack/README.md) |
| [`server`](server/README.md) | Transport-agnostic server core: router, middleware chain, lifecycle, scaffold, session registry | [README](server/README.md) |
| [`server/http`](server/http/README.md) | HTTP transport: chi-style routing, middleware, JSON helpers, mock server | [README](server/http/README.md) |
| [`server/ws`](server/ws/README.md) | WebSocket transport — endpoints as ordinary routes on the HTTP core | [README](server/ws/README.md) |
| [`server/rpc`](server/rpc/README.md) | RPC transport core over a pluggable wire codec: bounded dispatch, gate hook, polite drain | [README](server/rpc/README.md) |
| [`server/rpc/msgpackrpc`](server/rpc/msgpackrpc/README.md) | msgpack-RPC wire codec — the framing Neovim's `sockconnect` speaks natively | [README](server/rpc/msgpackrpc/README.md) |

### threadsafe

`SynchronizedValue[T]` (mutex), `MultiReadSyncValue[T]` (RWMutex),
`AtomicValue[T]` (lock-free) — all satisfy `Value[T]`, so you can swap the
locking strategy without changing call sites. The `Do`/`RDo` closure discipline
makes compound access race-free by construction.
→ [threadsafe/README.md](threadsafe/README.md)

### collections

`Set[T]` with the full algebra (union, intersect, diff, subset) plus `iter.Seq`
iteration, and `Map`/`Filter`/`Reduce` (+ `-Indexed` variants) shaped like the
stdlib `slices` conventions.
→ [collections/README.md](collections/README.md)

### logger

A one-method `Logger` seam (`Log(Severity, any)`) that golib packages accept for
optional logging. `Fields` for structured payloads, `Entry` that keeps error
chains `errors.Is`-able, `Adapt` to bridge any external logger without importing
it, and `FromSlog`/`NewSlogHandler` for both `log/slog` directions.
→ [logger/README.md](logger/README.md)

### request

`Request`/`Do(ctx, …)` run an HTTP cycle into a `Params` carrier — transport
errors only, status codes are data. `DecodeResponse[T]` maps a response into
typed success/error, `FormWriter` builds multipart, `Histories` keeps a debug
trail.
→ [request/README.md](request/README.md)

### ingestor

Buffer items in memory and flush them in batches to CSV/JSON files (or any
`io.Writer` you supply) with bounded, drain-aware background writes.
`Ingestor[T]` is context-first; embed `MemoryLoader[T]` to build a custom
backend.
→ [ingestor/README.md](ingestor/README.md)

### dao

A generic, driver-agnostic data-access layer. Declare each entity **once**
(fields, columns, scan targets, joins, sort, search) and that drives
column-aware reads, scanning, query building, auto-chunked batch writes,
multi-database transactions (incl. two-phase commit), query-time hooks
(tenant scoping, soft delete, metrics), and partial (PATCH) updates. Not an ORM
— explicit columns, explicit joins, no struct-tag magic.

- Core: zero external dependencies.
- Drivers: [`dao/postgres`](dao/postgres/README.md) (pgx, native COPY, 2PC),
  [`dao/sqlite`](dao/sqlite/README.md) (pure-Go modernc; the new-driver
  template), [`dao/bigquery`](dao/bigquery/README.md) (read-mostly OLAP,
  separate module).

→ [dao/README.md](dao/README.md) for the reference, [dao/USAGE.md](dao/USAGE.md)
for a worked cookbook (hooks, partial updates, transactions).

### partial

Turns a three-state JSON PATCH body (a field carries a value / is absent / is
`null`) into a Write/Skip/Clear disposition that `dao` applies directly — with
zero per-entity code. Bind a `Patch[T]`, shape it server-side, and
`partial.ApplyRules(dao, patch)`.
→ [partial/README.md](partial/README.md)

### msgpack

A zero-dependency MessagePack value codec over a fixed Go vocabulary
(string-keyed maps, `Ext` passthrough for Neovim handle types). Decoding is
built for attacker-adjacent input: per-item limits plus whole-decode
aggregate budgets, capped preallocation, typed errors, panic-free by fuzz
contract.
→ [msgpack/README.md](msgpack/README.md)

### server

A transport-agnostic core (`net/http`-free) shared by every transport: a
generic tree router, an immutable middleware chain, a lifecycle contract, an
accept-loop `Scaffold`, and a drain-aware session `Registry`.
[`server/http`](server/http/README.md), [`server/ws`](server/ws/README.md),
and [`server/rpc`](server/rpc/README.md) build on it; gRPC/SFTP adapters
slot in the same way.
→ [server/README.md](server/README.md)

### server/rpc

A connection-oriented RPC transport over a pluggable wire `Codec`:
per-connection read loop with size windows, bounded concurrent dispatch with
backpressure, per-request contexts cancelled on disconnect/shutdown, a
pre-dispatch gate for handshake-before-methods, staged size-capped replies,
and polite drain. [`server/rpc/msgpackrpc`](server/rpc/msgpackrpc/README.md)
is the first codec — msgpack-RPC, which Neovim speaks natively over
`sockconnect(..., {rpc = true})`.
→ [server/rpc/README.md](server/rpc/README.md)

## Conventions

Development conventions and the project philosophy are maintained alongside the
codebase; new code follows the zero-dep, small-seam, fail-loud-and-typed rules
above. Structural changes are ADR-first — design records live under `docs/`.

## License

See [LICENSE](LICENSE).