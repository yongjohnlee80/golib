# dao/postgres

The reference PostgreSQL driver for [`dao`](../README.md), implemented over
[`github.com/jackc/pgx/v5`](https://github.com/jackc/pgx). It plugs into the
`dao.DataConn`/`dao.Dialect` contract; the `dao` core and `logger` stay
zero-dependency (this driver and `dao/sqlite` are the two DB-backed packages in
the root module — `dao/bigquery` isolates the heavy GCP SDK in its own module).

```go
import (
    "github.com/yongjohnlee80/golib/dao"
    "github.com/yongjohnlee80/golib/dao/postgres"
)

conn, err := postgres.Open(ctx, "postgres://user:pass@localhost:5432/db?sslmode=disable")
if err != nil { /* ... */ }
defer conn.Close()

artists := BuildArtistSchema(conn, log) // conn is a dao.DataConn
```

## Opening

```go
postgres.Open(ctx, dsn, opts...)              // DataConn named "postgres"
postgres.OpenNamed(ctx, "postgres-gold", dsn) // explicit name (for multi-DB transactions)
```

Pool options map onto the pgx pool config:

```go
postgres.MaxOpenConns(n)   // pool MaxConns
postgres.MaxIdleConns(n)   // pool MinConns (warm-pool floor; pgx has no max-idle)
postgres.ConnMaxLifetime(d)
postgres.ConnMaxIdleTime(d)
```

## What `PostgresDialect` provides

It embeds `dao.GenericDialect` (whose conventions are already Postgres-shaped)
and overrides only the Postgres-specific parts:

- `$n` placeholders, 65535 bind-parameter limit, double-quoted identifiers,
  `RETURNING`, Postgres `ON CONFLICT` upserts (all inherited).
- **Native COPY fast-path** via pgx `CopyFrom` — `Batch()` uses it for large
  inserts with no conflict clause; chunked multi-row INSERT otherwise.
- **SQLSTATE → dao sentinel** translation: `23505` → `ErrDuplicate`, `23502` →
  `ErrNotNull`, `23503` → `ErrForeignKey`, carried as a `*dao.ConstraintError`
  with the constraint name.

Capability predicates: `SupportsReturning`, `CopySupported`, and
`TwoPhaseSupported` all report `true`; `SupportsTransactions`/`SupportsUpsert`
inherited `true`; `SupportsLastInsertID` `false` (RETURNING is preferred).

For `dao/deploy`: `TransactionalDDL` (DDL rolls back with the transaction) and
`DeployLock`, a transaction-level advisory lock the runner takes first, so two
processes applying schema scripts at once take turns.

**Full text** (`dao.FullTextMatcher`, `dao.FullTextQueryRanker`): the index is a
tsvector column on the content table, matched with
`@@ to_tsquery('<config>'::regconfig, $n)` and ranked with a negated `ts_rank_cd`,
weights placed by each column's class. It does not implement `dao.FullTexter`
(rank and snippet would need the query bound again); cut snippets in Go with
`search/chunk.Highlight`.

**Vectors** (`dao.VectorDistancer`): pgvector's `<=>`, `<->` and `<#>` against
`$n::vector`. `Vector` (`[]float32`) binds and scans pgvector's text form, so no
pgvector module is needed; it refuses NaN and infinities, and nil is NULL. COPY's
binary rows do not carry it: write vectors with INSERT or an upsert.

**Settings for one statement** (`dao.LocalSetter`, for `dao.WithLocalSettings`):
`set_config(name, value, true)`, the bound form of `SET LOCAL`, after reading each
name's value in force in a `MATERIALIZED` CTE; the restore puts the read values
back the same way, a name with none (a pgvector name before the library loads)
reset. Only these names, each with its domain:

| Name | Domain | Helper |
|---|---|---|
| `plan_cache_mode` | `auto`, `force_custom_plan`, `force_generic_plan` | `PlanCacheMode(string)` |
| `hnsw.iterative_scan` | `off`, `relaxed_order`, `strict_order` | `HNSWIterativeScan(string)` |
| `hnsw.ef_search` | integer, 1 to 1000 | `HNSWEfSearch(int)` |
| `hnsw.max_scan_tuples` | integer, 1 to 2147483647 | `HNSWMaxScanTuples(int)` |
| `hnsw.scan_mem_multiplier` | number, 1 to 1000 | `HNSWScanMemMultiplier(float64)` |

The bounds are pgvector 0.8's and PostgreSQL 16+'s; an integration cell checks
them against the server's `pg_settings`. Values are canonical text (an integer
in decimal, a number with no exponent). Adding a name needs its domain and a
cell.

## The index lifecycle (building and retiring an index while running)

For a consumer that builds indexes at run time (AutoDoc's HNSW index per embedding model):
closed operations over a declared index, which golib renders, quotes and allowlists. Nothing here
takes SQL text.

- **Declare** an `IndexSpec`:
  - the table and the name are lowercase identifiers (`^[a-z_][a-z0-9_]{0,62}$`);
  - the method is `hnsw` or `btree`;
  - the element comes from `HNSWIndexElement(col, metric, type)` (the same cast `dao.DistanceAs`
    orders by) or `ColumnElement(col)`;
  - `WhereEq` holds partial-predicate terms, each value an escape-string literal (`E'…'`) that
    holds whatever `standard_conforming_strings` is. Control characters are refused.
- **On a pinned session** (`PinSessionConn`), outside any transaction:
  - `CreateIndexConcurrently` and `DropIndexConcurrently` (`IF EXISTS`), with explicit
    `BuildOptions` timeouts (zero is none), so a pool's `statement_timeout` never cuts a build
    short;
  - `ReferenceDefinition`: the spec built on an empty temporary copy of the table, to compare
    with;
  - `MarkSession`: the session's backend, marked as the lifecycle's.

  These leave session state, so the member is destroyed at `Discard`, never recycled.
- **From any executor:**
  - `ReadIndexState`: whether the index exists and is valid, and its canonical definition (access
    method, expression, operator class, predicate). Compare it with `==`;
  - `BackendAlive`, `CancelBackend`, `TerminateBackend`, for a `Backend` that `MarkSession`
    answered only. The pid, the start and the mark must all match, so no other session is ever
    signalled.

## Transactions

Single-DB and multi-DB **ordered** transactions work via `dao.RunTx` /
`schema.On(tx)` (see the [dao README](../README.md)).

**True two-phase commit is supported.** `tx.TwoPhase()` prepares every
participating database under a generated global id (`PREPARE TRANSACTION`) and
commits them only once all prepares succeed (`COMMIT PREPARED`), rolling back
prepared transactions if any prepare fails. Two safety details:

- After a successful `PREPARE TRANSACTION` the pgx connection is released back
  to the pool (the prepared transaction lives server-side, decoupled from the
  session).
- Postgres treats `PREPARE TRANSACTION` in an *aborted* transaction like a
  silent rollback (success with a `ROLLBACK` command tag, no error). The driver
  checks the command tag and turns that into a phase-one failure — a poisoned
  participant can never masquerade as prepared.

The PostgreSQL server must be started with `max_prepared_transactions > 0` (its
default is `0`); otherwise the prepare fails and `dao.RunTx` reports it. A
`COMMIT PREPARED` that fails after a successful prepare is surfaced in
`CommitError.PreparedPending` (name → gid) for operator resolution.

## Driver transaction options, context finalizers, raw rows (ADR-0017)

Postgres is the one driver that opts into every ADR-0017 capability, because
pgx can honor every one of them natively:

- **`dao.TxBeginner` + `dao.SessionTxBeginner`** — `BEGIN` carries the full
  option set: `READ ONLY` / explicit `READ WRITE`, all four isolation levels
  (`READ UNCOMMITTED` is accepted and reported, and behaves as `READ
  COMMITTED` — Postgres has no dirty-read mode), and `DEFERRABLE` /
  `NOT DEFERRABLE` under `SERIALIZABLE READ ONLY`. Nothing in the matrix is
  refused. A write inside a `READ ONLY` transaction fails with SQLSTATE
  **25006** — the server enforces it, not the client.
- **`dao.ContextTxConn`** — `CommitContext`/`RollbackContext` take their own
  context, so cleanup after the transaction's original context is cancelled
  still has a deadline. pgx's finalizers take a context natively; this is why
  the capability is honest here and absent on the `database/sql` drivers.
- **`dao.RawRows`** — `RawValues()` and `Fields()` hand back pgx's own receive
  buffers and the server's `RowDescription`. The byte slices are **borrowed
  until the next `Next`**.

A failed `CommitContext` is classified rather than passed through raw:
`ErrTxRolledBack` when the transaction is proven not to have committed (a
server-confirmed rollback, `pgx.ErrTxCommitRollback`, or a pgconn error proving
nothing was written), `ErrTxOutcomeUnknown` when the COMMIT went out and the
answer did not come back. The pgx/pgconn cause stays reachable with
`errors.As`; a context cancelled *before* dispatch returns the raw context error
and leaves the handle open.

## Session-pinned connections & raw extended-protocol execution (ADR-0018)

`SessionPinner` is a Postgres-only capability for a consumer (autodb's pgwire
front door) that must run a *relayed* client's extended-protocol frames on the
**same** connection that hosts the session's transaction — a guarantee no pool
can give:

```go
pc, err := postgres.PinSessionConn(ctx, conn) // dao.ErrUnsupported on a non-pg conn
if err != nil { /* … */ }
defer pc.Discard() // idempotent terminal cleanup — always relinquishes the lease

tx, err := pc.BeginSessionTx(ctx, dao.TxOptions{Access: dao.TxReadOnly})
// … relay the client's frames on the SAME wire, inside that transaction:
_ = pc.Send(ctx, postgres.ParseOp("", clientSQL, nil))
_ = pc.Send(ctx, postgres.BindOp("", "", clientParams, formats, nil))
_ = pc.Send(ctx, postgres.ExecuteOp("", 0))
_ = pc.Flush(ctx)
for {
    m, err := pc.Receive(ctx) // one message at a time; DataRow buffers are BORROWED
    if err != nil { /* transport error → the handle is poisoned; Discard */ }
    if m.Kind == "ErrorResponse" { /* protocol data, not a Go error — classify + reframe */ break }
    if m.Kind == "CommandComplete" { break }
}
status, _ := pc.Sync(ctx) // the ONLY call that returns the wire to quiescent
```

The contract in one paragraph. `Send` queues one frontend frame (`Parse`,
`Bind`, `Describe`, `Execute`, `Close` — a **closed** vocabulary; the
simple-`Query` frame is excluded so a relay's gate at `Parse` is never bypassed);
`Flush` writes them plus a protocol `Flush` so the server answers without ending
the exchange; `Receive` returns one backend message at a time with borrowed
`DataRow` buffers and surfaces a server `ErrorResponse` as **protocol data**, not
a Go error; `Sync` is the single call that consumes to the terminal
`ReadyForQuery` and reopens the wire. The state is **two orthogonal tracks**
(outbound: idle→building→flushed; inbound: none→receiving→discarding) plus a
poison flag; a guarded call made mid-segment is refused **immediately** with
`ErrSegmentInFlight`, never serialized. `BeginSessionTx` returns a guarded
`dao.ContextTxConn` whose finalizers run over the raw wire and reuse the ADR-0017
`classifyCommit` outcome contract, and whose `QueryContext`/`ExecContext` run a
private unnamed extended sequence (or a simple `Query` for a no-arg `Exec`) that
cleans up its own unnamed objects. `Release` returns a quiescent, transaction-
closed member to the pool; `Discard` is the unconditional terminal that closes
the physical connection whenever safe reuse cannot be proven — so a poisoned or
mid-flight member is never recycled dirty. Nor is one on which a command left
**session state** (a `SET` or `RESET`, `PREPARE` or `DEALLOCATE`, `LISTEN` or
`UNLISTEN`, a `DISCARD`): every face reads its command tags, and `Release` or
`Discard` then closes that member so the pool destroys it, rather than handing
the next borrower a search_path, a role or a prepared name it never asked for.
`Release` still succeeds; the cost is a reconnect. `DECLARE` counts too (a cursor
`WITH HOLD` outlives its transaction). What no command tag names is the
borrower's to know: a session-changing function (`set_config(…, false)`,
`pg_advisory_lock`) or a temporary object (`CREATE TEMP …`) leaves state the tags
cannot see, so a borrower that uses one must `Destroy` the member, not `Release`
it. `Discard` judges reuse from **wire
mechanics alone**, which is all a driver can see; a consumer that knows the
*session* is unfit (a reset the server refused, sanitation it could not prove)
asserts the optional `Destroyer` capability and calls `Destroy()`, which poisons,
interrupts and barriers behind in-flight I/O, closes the socket and relinquishes
the lease **unconditionally** — a quiescent, perfectly reusable wire included.
`Destroyer` is a separate optional interface, like `SimpleQuerier` and
`ParameterStatusReporter`, so `PinnedConn`'s published method set is unchanged and
external implementations keep compiling. Because the handle never exposes pgx's
high-level `Conn`, pgx's statement cache is **structurally unreachable** on a
pinned member and can never disagree with it.

## Integration tests

Build-tagged; require a reachable Postgres:

```bash
TEST_PGURL='postgres://user:pass@localhost:5432/golib?sslmode=disable' \
  go test -tags integration ./dao/postgres/
```

They (re)create and drop scratch tables and cover CRUD + RETURNING,
duplicate→`ErrDuplicate`, upsert, native-COPY and chunked batches, `RunTx`
commit/rollback, a two-process integration test, and the ADR-0017 suite (the
option matrix live against the server, the 25006 read-only proof through
`BeginSessionTx`, fresh-context finalizers, all four commit fault states, and
raw rows on both the pool and transaction paths), and the ADR-0018 suite — one
cell per acceptance criterion (`-run TestPinned_`): the async queue-then-stream
contract in bounded memory, the error/poison/premature-ReadyForQuery state
machine, the fault-state matrix on the pinned transaction, one-wire-one-backend-
transaction via `txid_current()`, immediate mid-segment refusal proven with a
watchdog, no lease leak across poison/discard cycles, portal resume/abandonment
with exact state tuples, the private query/exec path with object-lifetime
observation, the closed-vocabulary boundary, arg encoding vs the pool path by
decoded equality, the legacy finalizer shape, and the ErrorResponse cleanup tail.
The two-phase-commit tests
additionally require `max_prepared_transactions > 0` and **skip** with an
explanatory message when the server has prepared transactions disabled:

```bash
# e.g. a throwaway container with 2PC enabled:
docker run --rm -p 5433:5432 -e POSTGRES_PASSWORD=x postgres:16-alpine \
  -c max_prepared_transactions=10
```