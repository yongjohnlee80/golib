# dao/sqlite

A SQLite driver for [`dao`](../README.md), implemented over the standard library
`database/sql` and backed by the **pure-Go** [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite)
driver (no cgo). Importing this package registers that driver.

```go
import (
    "github.com/yongjohnlee80/golib/dao"
    "github.com/yongjohnlee80/golib/dao/sqlite"
)

conn, err := sqlite.Open(ctx, "/path/to/app.db")
if err != nil { /* ... */ }
defer conn.Close()

widgets := BuildWidgetSchema(conn, log) // conn is a dao.DataConn
```

## Opening

```go
sqlite.Open(ctx, dsn, opts...)            // DataConn named "sqlite"
sqlite.OpenNamed(ctx, "sqlite-cache", dsn)

sqlite.MaxOpenConns(n)
sqlite.MaxIdleConns(n)
sqlite.ConnMaxLifetime(d)
sqlite.ConnMaxIdleTime(d)
```

`dsn` is a modernc.org/sqlite DSN: a file path, or `:memory:`. For an in-memory
database pair it with `MaxOpenConns(1)` so every query hits the same connection:

```go
conn, _ := sqlite.Open(ctx, ":memory:", sqlite.MaxOpenConns(1))
```

## What `SqliteDialect` provides

It embeds `dao.GenericDialect` and overrides only the SQLite-specific parts:

- `?` positional placeholders (overrides `$n`).
- 999 bind-parameter limit (drives batch chunking).
- SQLite result-code → dao sentinel translation: `SQLITE_CONSTRAINT_UNIQUE` /
  `…_PRIMARYKEY` → `ErrDuplicate`, `…_NOTNULL` → `ErrNotNull`, `…_FOREIGNKEY` →
  `ErrForeignKey`.

Inherited from `GenericDialect`: double-quoted identifiers, `RETURNING` (modern
SQLite 3.35+, which modernc bundles), and `ON CONFLICT` upserts. There is **no COPY
fast-path**, so batches always use the chunked multi-row INSERT path.

## In-process testing

Because modernc.org/sqlite runs in-process, this driver is ideal for tests — no
external server, runs in the normal `go test` suite against a temp-file database.
The package's own `sqlite_test.go` does exactly this and exercises the whole `dao`
engine end-to-end (CRUD + RETURNING, duplicate→`ErrDuplicate`, upsert, chunked batch
via `AddRow`, transactions, and SQL+args debug logging).

```bash
go test ./dao/sqlite/   # no build tag, no external dependencies
```
