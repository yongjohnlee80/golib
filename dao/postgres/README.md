# dao/postgres

The reference PostgreSQL driver for [`dao`](../README.md), implemented over
[`github.com/jackc/pgx/v5`](https://github.com/jackc/pgx). This is the only golib
package that carries a database dependency; the `dao` core and `logger` stay
zero-dependency.

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
postgres.MaxOpenConns(n)        // pool MaxConns
postgres.MaxIdleConns(n)        // pool MinConns (warm-pool floor; pgx has no max-idle)
postgres.ConnMaxLifetime(d)
postgres.ConnMaxIdleTime(d)
```

## What `PostgresDialect` provides

It embeds `dao.GenericDialect` (whose conventions are already Postgres-shaped) and
overrides only the Postgres-specific parts:

- `$n` placeholders, 65535 bind-parameter limit, double-quoted identifiers,
  `RETURNING`, Postgres `ON CONFLICT` upserts (all inherited).
- **Native COPY fast-path** via pgx `CopyFrom` — `Batch()` uses it for large
  inserts (no conflict clause); chunked multi-row INSERT otherwise.
- **SQLSTATE → dao sentinel** translation: `23505` → `ErrDuplicate`, `23502` →
  `ErrNotNull`, `23503` → `ErrForeignKey`, carried as a `*dao.ConstraintError` with
  the constraint name.

## Transactions

Single-DB and multi-DB **ordered** transactions work via `dao.RunTx` / `schema.On(tx)`
(see the dao README). **True two-phase commit is not yet enabled**:
`TwoPhaseSupported()` returns `false`, so `tx.TwoPhase()` fails fast rather than
silently degrading. Safe `PREPARE TRANSACTION` / `COMMIT PREPARED` (with correct pgx
connection release and orphan reaping) is a tracked follow-up.

## Integration tests

The package's integration tests are build-tagged and require a reachable Postgres:

```bash
TEST_PGURL='postgres://user:pass@localhost:5432/golib?sslmode=disable' \
  go test -tags integration ./dao/postgres/
```

They (re)create and drop a single scratch table and cover CRUD + RETURNING,
duplicate→`ErrDuplicate`, upsert, native-COPY and chunked batches, and `RunTx`
commit/rollback.
