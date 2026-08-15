# ADR-0012 — `golib/dao`: Result-set column names (`RowsColumns`)

- **Status:** **Proposed** (2026-08-16; authored by ultron-prime for autodb M1 —
  implementation lands on the `dao-m1` branch; acceptance at branch review)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive)
- **Related:** ADR-0004 (DataConn & the `Rows` seam), ADR-0002 (interfaces —
  why the engine's own scan path never needs column names), ADR-0013 (schema
  introspection — the *catalog* half of metadata; this ADR is the *result-set*
  half)

> **Self-containment contract.** Concrete signatures, files to modify, and
> acceptance criteria below; no prior context needed beyond ADR-0004.

---

## 1. Context

`dao.Rows` (`dao/dataconn.go:18`) is deliberately minimal — `Next / Scan /
Close / Err` — because the engine's scan plan is produced by the same
`Schema.resolve` pass as the projection, so the engine itself never needs to
ask a result set what its columns are.

Consumers running **raw SQL** are different. `DataConn` embeds `Querier`, so an
application can (and autodb's execution core does) run
`conn.QueryContext(ctx, userSQL, args...)` directly and receive a `dao.Rows` —
and then it has no way to learn the result's column names. The db-tui project
hit exactly this wall and had to wrap every statement in
`SELECT row_to_json(t) FROM (<stmt>) t` to smuggle column names through the
seam — a Postgres-only hack that breaks streaming types and column order
guarantees for the general case. A DB-IDE result grid needs the names
first-class.

Widening the `Rows` interface itself would break every existing implementor
(three drivers in-repo plus any third-party `DataConn`). The codebase already
has the answer to this shape of problem: optional capability interfaces probed
with a type assertion (`pgxCopier` in `dao/postgres/dialect.go:43`, `prepareTx`
in `PostgresDialect.Prepare`).

## 2. Decision

### 2.1 Optional interface + probe helper (new file `dao/columns.go`)

```go
// RowsColumns is an optional extension of [Rows]: a driver whose row stream
// can report the result set's column names implements it. The stdlib
// *sql.Rows satisfies it natively, so database/sql-backed drivers (sqlite,
// mysql) get it for free; other drivers adapt their native metadata.
type RowsColumns interface {
	Rows
	// Columns returns the result set's column names, in projection order.
	Columns() ([]string, error)
}

// Columns reports the column names of rows when its driver exposes them. It
// returns ErrUnsupported (wrapped) when rows does not implement [RowsColumns]
// — never a silent empty slice.
func Columns(rows Rows) ([]string, error) {
	if rc, ok := rows.(RowsColumns); ok {
		return rc.Columns()
	}
	return nil, fmt.Errorf("%w: result column names", ErrUnsupported)
}
```

`dao.Rows` itself is untouched; nothing breaks.

### 2.2 Driver coverage

- **sqlite / mysql** — nothing to do: both return `*sql.Rows` values as
  `dao.Rows`, and `*sql.Rows` already has `Columns() ([]string, error)`.
  A compile-time `var _ dao.RowsColumns = (*sql.Rows)(nil)` assertion is added
  to each driver's tests so a future wrapper cannot silently drop the
  capability.
- **postgres** — `pgxRows` (`dao/postgres/postgres.go:133`) gains:

  ```go
  func (r *pgxRows) Columns() ([]string, error) {
      fds := r.rows.FieldDescriptions()
      out := make([]string, len(fds))
      for i, fd := range fds {
          out[i] = string(fd.Name)
      }
      return out, nil
  }
  ```

- **bigquery** — the nested module lags the root by design (pinned to an older
  golib); `bqRows` adapts its iterator schema in the module's own next
  coordinated release. Until then `dao.Columns` correctly reports
  `ErrUnsupported` for it. Tracked in §4 of ADR-0013's follow-ups.

### 2.3 Scope: names only, engine untouched

Column *type* metadata (OIDs / `ColumnTypes`) is deliberately out of scope —
the raw-path consumer scans into `[]any` and inspects driver-native Go values,
which is sufficient for grid/JSON rendering; a typed-metadata surface can land
additively later if a concrete consumer needs it. The engine's `Iterator[R]`
also stays unchanged (adding a method would break external implementors); the
schema path knows its columns by construction.

## 3. Consequences

**Easier:** raw-SQL consumers (autodb's execution core, ad-hoc tooling) get
column names portably across postgres/sqlite/mysql; the `row_to_json`
workaround class dies.

**Harder:** nothing measurable — one probe helper, one 8-line pgx method.
Third-party `Rows` implementations simply keep reporting `ErrUnsupported`
until they adopt the interface.

## 4. Acceptance criteria

- `dao.Columns` unit-tested against a fake implementing `RowsColumns` and one
  that does not (asserting `errors.Is(err, ErrUnsupported)`).
- sqlite in-memory integration test: raw `QueryContext` → `dao.Columns` returns
  the projection names in order.
- postgres integration test (env-gated `TEST_PGURL`): same assertion through
  `pgxRows`.
- Compile-time `RowsColumns` assertions in sqlite/mysql driver tests.
