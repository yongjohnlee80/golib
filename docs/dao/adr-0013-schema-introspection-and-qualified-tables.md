# ADR-0013 — `golib/dao`: Schema introspection & qualified table quoting

- **Status:** **Proposed — under lector review, rev 1** (2026-08-16; authored by
  ultron-prime for autodb M1, implemented on `dao-m1`. Release target
  **v0.3.0** approved by Johno; merge + tag on lector acceptance)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive; `Dialect` itself is **unchanged** — see rev 1)
- **Related:** ADR-0004 (DataConn/Dialect seam), ADR-0008 (capability honesty
  & `ErrUnsupported`), ADR-0011 (mysql driver — backtick `QuoteTable`),
  ADR-0012 (result column names — the same optional-interface shape)

> **Self-containment contract.** Concrete signatures, per-driver catalog
> queries, files to create/modify, and acceptance criteria below.

> **Revision history. Rev 1 (2026-08-16)** — folded from lector's r1 review
> (`change_requested`). Must-fix #1: rev 0 grew the `Dialect` interface with
> `GenericDialect` defaults and §5 claimed lagging embedders (dao/bigquery)
> would fail to compile in mixed builds. That analysis was WRONG: Go method
> promotion means an embedder *satisfies* the grown interface and silently
> inherits the generic behavior — BigQuery tables would render
> `"dataset"."table"` (double quotes are string literals in BigQuery SQL)
> instead of its backtick dot-path. Rev 1 replaces interface growth with the
> **optional capability interfaces** `TableQuoter` and `Introspector`
> (lector's preferred remedy), which leaves every non-implementing dialect
> byte-identical. Must-fix #2: `ColumnInfo.Nullable` is now a normalized
> contract — primary-key columns report non-nullable; the sqlite driver
> reconciles `pragma_table_info`'s `notnull=0` rowid-alias quirk, pinned by
> a test that failed before the fix. The generalized rule is ratified as the
> KB convention `interface-evolution-capability-interfaces`.

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

**Why NOT interface growth (rev 1).** The ADR-0008 precedent (growing
`Dialect` with `GenericDialect` defaults) is safe only for *behavior-inert
predicates* (`SupportsX() bool { return false }`). A default `QuoteTable`
is not inert: its correct output depends on `QuoteIdent`, which embedders
override — and Go promotion is static, so the promoted default uses the
*generic* `QuoteIdent`, silently overriding the embedder's table quoting in
any mixed-version build. Capability = optional interface; predicate =
embeddable default. (KB convention
`interface-evolution-capability-interfaces` generalizes this rule.)

## 2. Decision — Part A: `TableQuoter`

### 2.1 Optional capability interface (in `dao/dialect.go`)

```go
// TableQuoter is an optional Dialect capability: a dialect that understands
// schema-qualified table names implements it. Deliberately NOT part of
// Dialect and NOT implemented by GenericDialect — a promoted default would
// silently override the table quoting of embedding dialects with their own
// QuoteIdent conventions in mixed-version builds.
type TableQuoter interface {
	// QuoteTable quotes an identifier appearing in table position:
	// "app.users" renders as "app"."users". Identifiers containing a literal
	// dot are not supported in qualified form — the dot is the separator.
	QuoteTable(ident string) string
}
```

### 2.2 Engine probe + fallback (in `dao/sql.go`)

```go
func quoteTable(d Dialect, table string) string {
	if tq, ok := d.(TableQuoter); ok {
		return tq.QuoteTable(table)
	}
	return d.QuoteIdent(table) // historical behavior, byte-identical
}
```

All six table-position quote sites switch to `quoteTable`: `fromAndJoins`,
`insertCore`, `buildUpdate`, `buildDelete`, `whereOrSubselect` (its qualified
`"app"."users"."id"` projection is valid SQL), and `buildBatchInsert`.
Column-position quoting is untouched. `dao/postgres` `copyRows` (conn + tx)
parse the table with `pgx.Identifier(strings.Split(table, "."))` so COPY
targets qualified tables correctly.

### 2.3 Per-driver opt-ins

- **postgres / sqlite**: explicit `QuoteTable` (dot-split, each part through
  their double-quote `QuoteIdent`) + `var _ dao.TableQuoter = ...`
  compile-time assertions.
- **mysql** (ADR-0011): dot-split with backtick parts.
- **bigquery**: **no change required, ever** — it does not implement
  `TableQuoter`, so the fallback uses its own backtick dot-path `QuoteIdent`,
  byte-identical to today, in old and mixed builds alike. (Rev 0 required a
  coordinated nested-module release here; rev 1 eliminates that.)

## 3. Decision — Part B: introspection

### 3.1 Types + entry points (`dao/introspect.go`)

```go
type SchemaInfo struct{ Name string }

type TableKind string // TableKindTable | TableKindView

type TableInfo struct{ Schema, Name string; Kind TableKind }

type ColumnInfo struct {
	Name       string
	DataType   string // dialect-native type text
	Nullable   bool   // PK columns ALWAYS report non-nullable (normalized;
	                  // sqlite's pragma notnull=0 rowid-alias quirk and its
	                  // legacy nullable non-INTEGER PKs are not surfaced)
	Default    string // meaningful only when HasDefault
	HasDefault bool
	Position   int // 1-based ordinal
	PrimaryKey bool
}

// Package-level entry points probe the dialect for Introspector and return
// wrapped ErrUnsupported when absent. schema == "" means the dialect default
// (postgres: "public", mysql: current database, sqlite: "main").
func ListSchemas(ctx context.Context, conn DataConn) ([]SchemaInfo, error)
func ListTables(ctx context.Context, conn DataConn, schema string) ([]TableInfo, error)
func ListColumns(ctx context.Context, conn DataConn, schema, table string) ([]ColumnInfo, error)
```

### 3.2 Optional capability interface

```go
// Introspector is an optional Dialect capability; NOT implemented by
// GenericDialect (same promotion hazard as TableQuoter).
type Introspector interface {
	ListSchemas(ctx context.Context, q Querier) ([]SchemaInfo, error)
	ListTables(ctx context.Context, q Querier, schema string) ([]TableInfo, error)
	ListColumns(ctx context.Context, q Querier, schema, table string) ([]ColumnInfo, error)
}

// SupportsIntrospection reports whether d implements Introspector.
func SupportsIntrospection(d Dialect) bool
```

Introspection statements run through `Querier` directly (the raw path, like
`Copy`); they are **not** hook-observed in v1 — no new `Op` constant, no
`IsWrite`/`whereCapable` churn. Revisit only if a consumer needs audit hooks
on catalog reads.

### 3.3 Per-driver catalog queries

- **postgres** (`dao/postgres/introspect.go`) — `pg_catalog`, not
  `information_schema` (dao quoting + speed; the db-tui finding):
  - schemas: `pg_namespace`, excluding `pg_*` and `information_schema`.
  - tables: `pg_class` join `pg_namespace`, `relkind IN ('r','p') → table`,
    `('v','m') → view`, filtered by schema, ordered by name.
  - columns: `pg_attribute` + `pg_get_expr(pg_attrdef)` (default) +
    `format_type` (type text) + `pg_index.indisprimary` (PK), keyed by
    `format('%I.%I', $1, $2)::regclass` so binds stay binds.
- **sqlite** (`dao/sqlite/introspect.go`):
  - schemas: `pragma_database_list` (typically `main` + attached).
  - tables: `sqlite_master` (or `<schema>.sqlite_master`), types
    `table`/`view`, excluding `sqlite_%` internals.
  - columns: `pragma_table_info(?, ?)` table-valued pragma (bindable in
    modernc); `pk > 0` marks primary-key membership and forces
    `Nullable = false` per the §3.1 contract.
- **mysql** (ADR-0011's `dao/mysql`, `introspect.go`) —
  `information_schema.SCHEMATA` / `TABLES` / `COLUMNS`, excluding the four
  system schemas; `schema == ""` resolves via `DATABASE()`; string flags
  (`IS_NULLABLE`, `COLUMN_KEY`) compared in Go, not SQL.

## 4. Consequences

**Easier:** a DB-IDE (autodb M4/M6) lists schemas/tables/columns uniformly
across the three OLTP drivers with zero raw SQL app-side; qualified tables
work end-to-end (engine SQL + COPY); **`Dialect` is unchanged**, so no
implementor anywhere — in-repo, nested-module, or third-party — is affected
unless it opts in.

**Harder:** two probe type-assertions per statement build (negligible);
catalog queries are per-driver maintenance surface; the dot-separator
contract forbids literal dots inside table identifiers used in qualified
form (documented; unchanged for unqualified names).

## 5. Nested module (bigquery) — rev 1 correction

`dao/bigquery` needs **no synchronized change**. It does not implement the
capability interfaces, so `quoteTable` falls back to its own `QuoteIdent`
(backtick dot-path — correct) and introspection reports `ErrUnsupported`
(honest). Mixed root/nested-module builds are a non-event. Optional future
work, on the module's own cadence: implement `Introspector` over BigQuery's
`INFORMATION_SCHEMA` and `RowsColumns` on `bqRows` (ADR-0012).

## 6. Acceptance criteria

- `GenericDialect` implements NEITHER `TableQuoter` NOR `Introspector`
  (asserted in tests — the promotion-hazard pin).
- A BigQuery-shaped embedder (custom `QuoteIdent`, no `TableQuoter`) keeps
  its own quoting in table position (regression test for lector r1
  must-fix #1).
- Fallback golden: non-implementing dialects render table SQL byte-identical
  to pre-ADR output; opt-in dialects render qualified tables correctly in
  SELECT/INSERT/UPDATE/DELETE/batch.
- Generic introspection entry points return `errors.Is(err, ErrUnsupported)`.
- sqlite in-memory integration: create table (PK, NOT NULL, DEFAULT) + view →
  ListSchemas/ListTables/ListColumns round-trip including `id.Nullable ==
  false` (the must-fix #2 pin — this assertion failed before the fix).
- postgres (`TEST_PGURL`) and mysql (`TEST_MYSQL_DSN`) integration tests with
  the same shape, each creating and dropping its own objects, skipping
  cleanly when the env is unset.
