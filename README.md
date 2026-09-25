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
| [`covercheck`](covercheck/README.md) | CI-neutral Go coverage profile comparison, changed-block measurement, and policy enforcement | [README](covercheck/README.md) |
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
| [`parse`](parse/README.md) | A streaming lexer core, plus the scanner and positions golib's hand-written parsers share | [README](parse/README.md) |
| [`parse/js`](parse/js/README.md) | The C-family expression and statement grammar, dialects as data, embeddable | [README](parse/js/README.md) |
| [`parse/qml`](parse/qml/README.md) | A faithful QML parser into a plain data tree | [README](parse/qml/README.md) |
| [`parse/sql`](parse/sql/README.md) | SQL dialects as values, over the streaming lexer | [README](parse/sql/README.md) |
| [`highlight`](highlight/README.md) | Syntax highlighting's contract: `Highlighter` (Qt's QSyntaxHighlighter, line + carried state), KSyntaxHighlighting's 31 styles, tree-sitter capture mapping | [README](highlight/README.md) |
| [`decl`](decl/README.md) | Instantiate a declarative (QML) UI through an adapter — identity, reloads, bindings, modules, signals | [README](decl/README.md) |
| [`tui/decl`](tui/decl/README.md) | golib/tui screens written in QML: the vocabulary, themes, dialogs, your own widgets, `NewProgram` | [README](tui/decl/README.md) · [USAGE](tui/decl/USAGE.md) |
| [`tui/decl/controls`](tui/decl/controls/README.md) | Qt Quick Controls' `TextField` and `Popup` for QML screens — written with the public widget contract alone | [README](tui/decl/controls/README.md) |
| [`tui/decl/decltest`](tui/decl/decltest/README.md) | Test a QML program in `go test`: `Check` every file it can load, `Run` it on a test backend | [README](tui/decl/decltest/README.md) |
| [`tui`](tui/README.md) | Cell-buffer terminal UI: component tree, constraint layout, focus routing, async tasks, and a widget set (vim Editor, lazy Tree, Table, Split, Float…) | [README](tui/README.md) · [TUTORIAL](tui/tutorial/README.md) |

### tui

A terminal UI framework, not a widget grab-bag: a mounted component tree
with constraint-based layout, target-then-bubble event routing, focus
scopes and traps, async tasks that post results back onto the loop, and
a cell buffer that diffs frames.

The widget set covers what an IDE-shaped app needs — `Box`, `Split`,
`Dock`, `Tabs`, `Table[T]`, `List[T]`, `BufferView`, `StatusBar`,
`Float` (modal, anchored, fraction-sized), text inputs, a lazy
generation-tokened `Tree`, and a vim-modal `Editor` that doubles as a
read-only viewer (motions, visual selection and yank; edits refused).

**Debugging is first-class.** Interactive bugs are timing bugs, and the
state that explains them is runtime-owned: `tui.WithTrace` emits focus
moves and repairs, mounts, modal scopes, and key routing including which
node CONSUMED each key. Reach for it before theorising —
[tutorial chapter 8](tui/tutorial/08-debugging.md) shows how to read one.

Start with the [tutorial](tui/tutorial/README.md); each chapter leads
with the mistake that cost an afternoon. Design records live in
[docs/tui/](docs/tui/), including a
[scored incident register](docs/tui/incident-register-2026-08-autodb-m6.md)
of every defect a real consumer hit and how each was fixed.
→ [tui/README.md](tui/README.md)

### tui/decl — QML screens

Write a golib/tui screen in QML and keep the Go for behaviour. `NewProgram`
builds the whole app in one call; a theme is a QML file the layout imports, so
switching theme is one line; dialogs, file dialogs (over any `fs.FS`, remote
included) and component files are QML; your own Go widgets join the vocabulary
as one `tuidecl.Type` each. The [editor-qml example](tui/examples/editor-qml/README.md)
is a complete text editor built this way.
→ [tui/decl/README.md](tui/decl/README.md) · [USAGE.md](tui/decl/USAGE.md)

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

- Declarations are built from your own constants: `Field.Expr` with
  `dao.T`/`dao.C`/`dao.Coalesce` resolves identifier quoting through the
  connection's dialect at construction, so a reserved word or a
  schema-qualified name is correct on every driver.
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

Several of those conventions are not left to good intentions. `internal/audit`
holds repo-wide guards that assert properties of the source tree itself — the
panic budget, the comment budget, the promotion self-call guard and others — and
they run as part of `go test ./...`. See [docs/audit/](docs/audit/) for what
each one checks, what its ledger means, and what it cannot see;
[docs/audit/verification-discipline.md](docs/audit/verification-discipline.md)
covers the mutation matrix and the gates a change passes before it is submitted.

Error identity, wrapping and comparison are covered separately in
[docs/error_handling.md](docs/error_handling.md).

## License

See [LICENSE](LICENSE).
