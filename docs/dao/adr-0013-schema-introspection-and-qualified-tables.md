# ADR-0013 — `golib/dao`: Schema introspection & qualified table quoting

- **Status:** **Proposed** (2026-08-16; authored by ultron-prime for autodb M1 —
  implementation lands on the `dao-m1` branch; acceptance at branch review)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive; grows `Dialect` — the ADR-0008 precedent)
- **Related:** ADR-0004 (DataConn/Dialect seam), ADR-0008 (capability
  predicates & `ErrUnsupported` honesty — the pattern the new methods follow),
  ADR-0011 (mysql driver — backtick `QuoteTable`), ADR-0012 (result column
  names — the result-set half of metadata)

> **Self-containment contract.** Concrete signatures, per-driver catalog
> queries, files to create/modify, and acceptance criteria below.

---

## 1. Context

Two related gaps, both blocking a DB-IDE built on dao (autodb roadmap M1):

1. **No catalog surface.** Nothing in golib can list schemas, tables, or
   columns — `grep` for `information_schema` / `pg_catalog` / `sqlite_master`
   over the repo returns zero hits. Every consumer reinvents catalog queries
   (db-tui hand-built a Postgres-only one and hit the quoting problem below).
2. **Qualified table names do not survive quoting.** Every table-position
   quote site calls `QuoteIdent`, and `GenericDialect.QuoteIdent("app.users")`
   renders `"app.users"` — one identifier containing a dot, not
   `"app"."users"`. The engine cannot address a table outside the default
   schema/search_path. The COPY path has the same defect independently:
   `pgx.Identifier{table}` is a single-element identifier
   (`dao/postgres/postgres.go`, both `copyRows` sites).

Growing the `Dialect` interface is the established move for driver-shaped
capability (ADR-0008 added six methods the same way); every in-repo dialect
and test fake embeds `GenericDialect`, so additions with Generic defaults are
compile-safe across the repo.

## 2. Decision — Part A: `QuoteTable`

### 2.1 New `Dialect` method

```go
// QuoteTable quotes an identifier appearing in table position. Unlike
// QuoteIdent it understands qualification: "app.users" renders as
// "app"."users" (each dot-separated part quoted separately). Table
// identifiers containing a literal dot in the name itself are not supported
// in qualified form — the dot is the qualification separator.
QuoteTable(ident string) string
```

`GenericDialect.QuoteTable` splits on `.` and quotes each part with the
generic double-quote rule. For unqualified names the output is byte-identical
to `QuoteIdent`, so all existing SQL is unchanged.

### 2.2 Call-site swap

The six table-position quote sites in `dao/sql.go` switch from
`QuoteIdent(table)` to `QuoteTable(table)`: `fromAndJoins`, `insertCore`,
`buildUpdate`, `buildDelete`, `whereOrSubselect` (its `qt` — the qualified
`"app"."users"."id"` projection this produces is valid SQL), and
`buildBatchInsert`. Column-position quoting is untouched.

`dao/postgres` `copyRows` (conn + tx) parse the table with
`pgx.Identifier(strings.Split(table, "."))` so COPY targets qualified tables
correctly.

### 2.3 Per-driver overrides

- postgres / sqlite: inherit the Generic default (double quotes) — correct.
- mysql (ADR-0011): dot-split with backtick parts.
- bigquery: **must** override `QuoteTable` to its existing backtick-dot-path
  `QuoteIdent` behavior when the nested module next syncs to the root
  interface (it pins an older golib today; see §5).

## 3. Decision — Part B: introspection

### 3.1 Types + entry points (new file `dao/introspect.go`)

```go
// SchemaInfo describes one schema (namespace) in a database.
type SchemaInfo struct{ Name string }

// TableKind classifies a relation.
type TableKind string

const (
	TableKindTable TableKind = "table"
	TableKindView  TableKind = "view"
)

// TableInfo describes one table or view.
type TableInfo struct {
	Schema string
	Name   string
	Kind   TableKind
}

// ColumnInfo describes one column of a table.
type ColumnInfo struct {
	Name       string
	DataType   string // dialect-native type text (e.g. "integer", "character varying(80)")
	Nullable   bool
	Default    string // dialect-native default expression; meaningful only when HasDefault
	HasDefault bool
	Position   int // 1-based ordinal
	PrimaryKey bool
}

// ListSchemas / ListTables / ListColumns delegate to the connection's dialect.
// schema == "" means the dialect's default schema (postgres: "public",
// mysql: the connection's current database, sqlite: "main").
func ListSchemas(ctx context.Context, conn DataConn) ([]SchemaInfo, error)
func ListTables(ctx context.Context, conn DataConn, schema string) ([]TableInfo, error)
func ListColumns(ctx context.Context, conn DataConn, schema, table string) ([]ColumnInfo, error)
```

### 3.2 New `Dialect` methods

```go
// SupportsIntrospection reports whether the dialect implements the catalog
// listing trio. Default false (ADR-0008 capability honesty).
SupportsIntrospection() bool
// ListSchemas/ListTables/ListColumns execute the dialect's catalog queries on
// q. Implemented only when SupportsIntrospection reports true; the Generic
// default returns ErrUnsupported (wrapped). schema semantics per §3.1.
ListSchemas(ctx context.Context, q Querier) ([]SchemaInfo, error)
ListTables(ctx context.Context, q Querier, schema string) ([]TableInfo, error)
ListColumns(ctx context.Context, q Querier, schema, table string) ([]ColumnInfo, error)
```

Introspection statements run through `Querier` directly (the raw path, like
`Copy`); they are **not** hook-observed in v1 — no new `Op` constant, no
`IsWrite`/`whereCapable` churn. Revisit only if a consumer needs audit hooks on
catalog reads.

### 3.3 Per-driver catalog queries

- **postgres** (new file `dao/postgres/introspect.go`) — `pg_catalog`, not
  `information_schema` (dao quoting + speed; the db-tui finding):
  - schemas: `pg_namespace`, excluding `pg_*` and `information_schema`.
  - tables: `pg_class` join `pg_namespace`, `relkind IN ('r','p') → table`,
    `('v','m') → view`, filtered by schema, ordered by name.
  - columns: `pg_attribute` + `pg_get_expr(pg_attrdef)` (default) +
    `format_type` (type text) + `pg_index.indisprimary` (PK), keyed by
    `format('%I.%I', $1, $2)::regclass` so binds stay binds.
- **sqlite** (new file `dao/sqlite/introspect.go`):
  - schemas: `pragma_database_list` (typically `main` + attached).
  - tables: `sqlite_master` (or `<schema>.sqlite_master`), types
    `table`/`view`, excluding `sqlite_%` internals.
  - columns: `pragma_table_info(?)` table-valued pragma (bindable in modernc);
    `pk > 0` marks primary-key membership.
- **mysql** (part of ADR-0011's `dao/mysql`, file `introspect.go`) —
  `information_schema.SCHEMATA` / `TABLES` / `COLUMNS`, excluding the four
  system schemas; `schema == ""` resolves via `DATABASE()`; string comparisons
  (`IS_NULLABLE`, `COLUMN_KEY`) are made in Go, not SQL, to avoid driver
  bool-conversion trouble.

## 4. Consequences

**Easier:** a DB-IDE (autodb M4/M6) lists schemas/tables/columns uniformly
across the three OLTP drivers with zero raw SQL app-side; qualified tables
work end-to-end (engine SQL + COPY); dialects state introspection capability
honestly.

**Harder:** `Dialect` grows five methods (the ADR-0008 trade, accepted);
catalog queries are per-driver maintenance surface; the dot-separator contract
forbids literal dots inside table identifiers used in qualified form
(documented; unchanged for unqualified names).

## 5. Follow-ups (nested module)

`dao/bigquery` pins an older golib and syncs on its own release cadence. At
its next coordinated bump it must add: `QuoteTable` (backtick dot-path — via
its existing `QuoteIdent`), `SupportsIntrospection` (INFORMATION_SCHEMA is
available; false is acceptable for the first sync), and `RowsColumns` on
`bqRows` (ADR-0012). Until that bump, mixed builds resolving the new root
golib alongside the old `dao/bigquery` will fail to compile the BigQuery
dialect — the same window every past `Dialect` growth had.

## 6. Acceptance criteria

- `QuoteTable` unit tests: unqualified byte-parity with `QuoteIdent`
  (generic + mysql), `app.users` → `"app"."users"` / `` `app`.`users` ``.
- Golden SQL tests proving unqualified-table statements are unchanged and a
  qualified table renders correctly in SELECT/INSERT/UPDATE/DELETE/batch.
- Generic introspection defaults return `errors.Is(err, ErrUnsupported)`.
- sqlite in-memory integration: create table (PK, NOT NULL, DEFAULT) + view →
  ListSchemas/ListTables/ListColumns round-trip, including `pk`, nullability,
  default detection.
- postgres (`TEST_PGURL`) and mysql (`TEST_MYSQL_DSN`) integration tests with
  the same shape, each creating and dropping its own objects, skipping cleanly
  when the env is unset.
