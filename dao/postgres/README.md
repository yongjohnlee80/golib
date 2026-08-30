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
raw rows on both the pool and transaction paths). The two-phase-commit tests
additionally require `max_prepared_transactions > 0` and **skip** with an
explanatory message when the server has prepared transactions disabled:

```bash
# e.g. a throwaway container with 2PC enabled:
docker run --rm -p 5433:5432 -e POSTGRES_PASSWORD=x postgres:16-alpine \
  -c max_prepared_transactions=10
```