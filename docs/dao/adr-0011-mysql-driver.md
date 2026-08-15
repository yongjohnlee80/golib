# ADR-0011 — `golib/dao`: MySQL driver (`dao/mysql`)

- **Status:** **Accepted** (2026-08-16 — lector r2 `approved_with_amendments`, amendment folded; authored by
  ultron-prime for autodb M1, implemented on `dao-m1`. Release target
  **v0.3.0** approved by Johno; released as v0.3.0)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive — a new driver per ADR-0004's contract)
- **Related:** ADR-0004 (DataConn, drivers & batch), ADR-0008 (read-mostly /
  capability predicates — the `SupportsReturning=false` + `SupportsLastInsertID=true`
  profile this driver is the first to exercise), ADR-0013 (qualified table
  quoting — `QuoteTable`, which this driver overrides for backticks)

> **Self-containment contract.** Like ADR-0001…0010, this document is written so
> an implementer with no prior context can build the feature: concrete
> signatures, files to create/modify, and acceptance criteria.

---

## 1. Context

The dao driver matrix today is postgres (pgx), sqlite (modernc, pure Go), and
bigquery (nested module). MySQL is required by the autodb project (autovim KB,
ADR-0052 / roadmap M1: postgres, mysql, sqlite first) and named as a target in
this package's own `Dialect` doc comments since ADR-0004 — the placeholder,
bind-cap, and LastInsertId comments all cite MySQL as the model.

Two facts make this driver cheap and load-bearing at once:

1. **`database/sql` shape.** `go-sql-driver/mysql` is a pure-Go `database/sql`
   driver, so the connection code is structurally identical to `dao/sqlite`:
   `*sql.Rows` and `sql.Result` satisfy `dao.Rows` / `dao.Result` by value —
   only the method return types adapt (`dao/sqlite/sqlite.go:50-52`).
2. **First LastInsertId dialect.** MySQL has no `INSERT ... RETURNING`, but its
   OK packet carries the generated id. The engine already implements this
   profile (`query_dao.go` Insert: `SupportsLastInsertID()` → `lastInsertID[ID]`,
   ADR-0008 §2.6); no engine change is needed, but the path finally gets a real
   driver and real tests.

The one genuine impedance mismatch is the upsert suffix. MySQL's conflict
clause is `ON DUPLICATE KEY UPDATE <assignments>` — it always requires at least
one assignment, and it fires on *any* unique-key conflict (the conflict target
cannot be named). The engine's "skip conflicts" shape today calls
`BuildUpsertSuffix(nil, nil)` (`batch.go`, `suffix()`), which MySQL cannot
render without knowing at least one inserted column name.

## 2. Decision

### 2.1 Package `dao/mysql` — main module, sqlite pattern

`github.com/go-sql-driver/mysql` becomes a **direct dependency of the root
module** (the modernc/pgx precedent — moderate dependency, leaf subpackage; the
nested-module treatment is reserved for heavy trees like the GCP SDK).

Files (mirroring `dao/sqlite`):

- `dao/mysql/mysql.go` — `Option func(*sql.DB)`; `MaxOpenConns` /
  `MaxIdleConns` / `ConnMaxLifetime` / `ConnMaxIdleTime`;
  `Open(ctx, dsn, opts...)` → `OpenNamed(ctx, "mysql", dsn, opts...)`;
  `mysqlConn` / `mysqlTx` over `*sql.DB` / `*sql.Tx` with ping-on-open.
  The DSN is a go-sql-driver DSN (`user:pass@tcp(host:port)/db?parseTime=true`);
  the driver does not rewrite it. Document `parseTime=true` as the recommended
  setting for `time.Time` scanning.
- `dao/mysql/dialect.go` — `MysqlDialect struct{ dao.GenericDialect }`.
- `dao/mysql/errors.go` — errno → sentinel translation.
- `dao/mysql/doc.go`, `dao/mysql/README.md` — package docs per golib convention.
- `dao/mysql/dialect_test.go` — unit tests, no database.
- `dao/mysql/mysql_test.go` — integration tests gated on `TEST_MYSQL_DSN`,
  skipping cleanly when unset; they create and drop their own tables
  (`dao/postgres` precedent).

### 2.2 `MysqlDialect` overrides

| Method | Value | Why |
|---|---|---|
| `Name()` | `"mysql"` | |
| `Placeholder(n)` | `"?"` | positional |
| `MaxBindParams()` | `65535` | the binary-protocol hard cap (2-byte param count); chunking stays packet-sane because `perChunkRows` divides by column count |
| `QuoteIdent(s)` | backticks, `` ` `` → ```` `` ```` | MySQL identifier quoting |
| `QuoteTable(s)` | dot-split, each part backtick-quoted | implements the optional `dao.TableQuoter` capability (ADR-0013 rev 1) |
| `SupportsReturning()` | `false` | no `INSERT ... RETURNING` |
| `SupportsLastInsertID()` | `true` | OK-packet id (ADR-0008 §2.6) |
| `BuildUpsertSuffix` | see §2.3 | |
| `TranslateError` | see §2.4 | |

Everything else stays at the embedded `GenericDialect` default (no COPY, no
two-phase, transactions and upsert supported).

### 2.3 Upsert suffix + the skip-conflicts column hint (engine tweak)

`BuildUpsertSuffix(conflictCols, updateCols)` for MySQL:

1. `conflictCols` non-empty, `updateCols` non-empty →
   `ON DUPLICATE KEY UPDATE `c` = VALUES(`c`), ...` (one assignment per update
   column). `conflictCols` are **not expressible** in MySQL — the clause fires
   on any unique-key violation. This is documented capability semantics, not a
   silent degrade: the caller's conflict target is a subset of "any key".
   (`VALUES()` is deprecated-but-supported in MySQL 8.x and universal in
   MariaDB; the row-alias form can replace it in a later revision.)
2. `conflictCols` non-empty, `updateCols` empty → the MySQL do-nothing idiom,
   a self-assignment of the first conflict column:
   `ON DUPLICATE KEY UPDATE `c1` = `c1``.
3. `conflictCols` empty, `updateCols` non-empty (the skip-conflicts shape, see
   below) → self-assignment of the first update column.
4. Both empty → unreachable through the engine after this ADR; renders the
   bare (invalid) `ON DUPLICATE KEY UPDATE` so a direct misuse fails loudly at
   the server rather than silently dropping conflict handling.

**Engine tweak:** `batchWriter.suffix()` changes its skip-conflicts call from
`BuildUpsertSuffix(nil, nil)` to `BuildUpsertSuffix(nil, cols)` — passing the
insert column list as a hint. `GenericDialect` (and therefore postgres/sqlite)
ignores `updateCols` when `conflictCols` is empty and still renders
`ON CONFLICT DO NOTHING`, so existing dialects emit byte-identical SQL. The
`Dialect.BuildUpsertSuffix` doc comment is updated to state the hint contract.

### 2.4 Error translation

`errors.As(err, *mysql.MySQLError)`, then by `Number`:

| errno | sentinel | kind |
|---|---|---|
| 1062, 1586 | `dao.ErrDuplicate` | `dao.Unique` |
| 1048 | `dao.ErrNotNull` | `dao.NotNull` |
| 1216, 1217, 1451, 1452 | `dao.ErrForeignKey` | `dao.ForeignKey` |
| 3819 | `*ConstraintError{Kind: Check}` (wraps a check sentinel-less error: keep `Err` as the driver error) | `dao.Check` |

`ConstraintError.Constraint` stays empty — MySQL reports the key name only
inside the human-readable message; parsing it is fragile (the sqlite precedent,
`dao/sqlite/errors.go`). Unrecognized errors pass through unchanged.

## 3. Consequences

**Easier:** autodb's three target engines are all dao-native; the ADR-0008
LastInsertId profile gets its first real driver + integration coverage; the
root module gains one moderate pure-Go dependency.

**Harder / caveats:** MySQL upsert semantics are approximate by nature (any-key
conflict target, affected-rows of 2 for updated rows); documented, not hidden.
Integration tests need a reachable MySQL (`TEST_MYSQL_DSN`), so CI coverage is
environment-dependent, like postgres.

## 4. Acceptance criteria

- `go test ./dao/...` green with no database (unit tests; integration skips).
- With `TEST_MYSQL_DSN` set: Insert (LastInsertId id round-trip), Get/Select,
  Update, Delete, Upsert (both update and do-nothing shapes), Batch (chunked +
  SkipConflicts + OnConflictUpdate), transaction commit/rollback via `RunTx`,
  and error translation (duplicate, not-null, FK) all pass against a real
  MySQL 8.x.
- `BuildUpsertSuffix` unit-tested for all four shapes of §2.3.
- Suffix behavior of postgres/sqlite provably unchanged (golden assertions on
  the generic suffix with the new hint argument).
