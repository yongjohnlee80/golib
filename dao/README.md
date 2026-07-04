# dao

A generic, driver-agnostic data-access layer (DAL) for Go. You declare each
entity **once** (its fields, columns, scan targets, joins, sort, and search
config) and that single declaration drives column-aware reads, scanning, query
building, batch writes, transactions, and query-time hooks.

It is deliberately **not an ORM**: no struct-tag magic, no migrations, no lazy
relationship graphs. Explicit columns, explicit joins, column-aware reads.

- **Core (`dao`) and `logger` have zero external dependencies** — the engine
  builds SQL with a small internal builder and executes through stdlib
  `database/sql`-shaped interfaces.
- **Drivers live in sub-packages** and are the only code with a DB dependency:
  [`dao/postgres`](postgres/README.md) (pgx), [`dao/sqlite`](sqlite/README.md)
  (pure-Go modernc), [`dao/bigquery`](bigquery/README.md) (GCP SDK, separate
  module — a read-mostly/no-transaction store).

```bash
go get github.com/yongjohnlee80/golib
```

See **[USAGE.md](USAGE.md)** for a longer, worked cookbook (hooks, partial
updates, and transactions with runnable snippets).

---

## The idea: one declaration per entity

A column's SQL expression, its scan target, its write value, and its join
trigger are declared **together, once**, as data:

```go
type Artist struct {
    ID, Name, URI, LabelGroupName string
    Public                        bool
}

type ArtistField string
const (
    ArtistID         ArtistField = "id"
    ArtistName       ArtistField = "name"
    ArtistURI        ArtistField = "uri"
    ArtistPublic     ArtistField = "public"
    ArtistLabelGroup ArtistField = "label_group_name" // joined, read-only
)
type ArtistSort string
const ArtistSortName ArtistSort = "name"

var artistFields = map[ArtistField]dao.Field[*Artist]{
    ArtistID:     {Column: "artist.id", Scan: func(a *Artist) any { return &a.ID }},
    ArtistName:   {Column: "artist.name", Scan: func(a *Artist) any { return &a.Name }, Value: func(a *Artist) any { return a.Name }},
    ArtistURI:    {Column: "artist.uri", Scan: func(a *Artist) any { return &a.URI }, Value: func(a *Artist) any { return a.URI }},
    ArtistPublic: {Column: "artist.public", Scan: func(a *Artist) any { return &a.Public }, Value: func(a *Artist) any { return a.Public }},
    ArtistLabelGroup: {
        Column:   "COALESCE(label_group.name,'')",
        Scan:     func(a *Artist) any { return &a.LabelGroupName },
        Join:     "label_group", // only joined when this field is selected/sorted/forced
        ReadOnly: true,          // never written
    },
}
```

`Field[R]` carries:

| Field | Purpose |
|-------|---------|
| `Column` | SQL expression projected for reads (fully qualified) |
| `Scan func(R) any` | pointer into the row struct for scanning |
| `Value func(R) any` | model→value extractor for `Batch().AddRow` (omit for DB-generated/read-only) |
| `Join JoinKey` | optional join this field triggers on demand |
| `ReadOnly bool` | computed/joined field that must never appear in a write |
| `WriteColumn string` | override the bare write column when `Column` is qualified/an expression |
| `Clearable bool` | a rules-driven `Clear` (partial updates) may target this column |
| `ClearValue any` | value written when cleared: `nil` → SQL `NULL`; non-nil → a NOT-NULL sentinel (requires `Clearable`) |

## Build a Schema (once, at startup)

The four type parameters (`R, C, K, ID`) are verbose at the `dao.New` call, so
write a per-entity **type alias** and a small `Build` function once; everything
downstream is then clean. Options are functional, applied in order:

```go
type ArtistSchema = dao.Schema[*Artist, ArtistField, ArtistSort, string]

func BuildArtistSchema(conn dao.DataConn, log logger.Logger) *ArtistSchema {
    return dao.New[*Artist, ArtistField, ArtistSort, string](conn,
        dao.Table[*Artist, ArtistField, ArtistSort, string]("artist"),
        dao.ID[*Artist, ArtistField, ArtistSort, string](ArtistID),
        dao.Fields[*Artist, ArtistField, ArtistSort, string](artistFields),
        dao.Default[*Artist, ArtistField, ArtistSort, string](ArtistID, ArtistName, ArtistURI),
        dao.OptionalJoin[*Artist, ArtistField, ArtistSort, string]("label_group",
            "LEFT JOIN label_group ON label_group.id = artist.label_group_id"),
        dao.SortMap[*Artist, ArtistField, ArtistSort, string](map[ArtistSort]string{ArtistSortName: "artist.name"}),
        dao.Conflict[*Artist, ArtistField, ArtistSort, string](ArtistURI),
        dao.Search[*Artist, ArtistField, ArtistSort, string](
            dao.StringOp("name", ArtistName),
            dao.BoolOp("public", "artist.public"),
        ),
        dao.WithLogger[*Artist, ArtistField, ArtistSort, string](log), // default: no-op
        dao.Debug[*Artist, ArtistField, ArtistSort, string](false),    // toggle SQL logging
    )
}
```

`New` validates required options and **panics at construction** — not per query
— on misconfiguration (missing `Table`/`Fields`, an `ID`/`Conflict` field not in
`Fields`, a `Field.Join` referencing an unregistered join, a `ClearValue`
without `Clearable`, a nil or duplicate-named hook). The `Schema` is immutable
and safe to hold for the process lifetime; acquiring a `DAO` from it is cheap.

### Option reference

| Option | Effect |
|--------|--------|
| `Table(name)` | table/relation name (**required**) |
| `Fields(map[C]Field[R])` | the one-source-of-truth field map (**required**) |
| `ID(field)` | primary-key field — drives `RETURNING` and the join-subselect key |
| `Default(fields…)` | column set for `Get`/`Select`/`Iterate` when none is passed (defaults to all) |
| `NewRow(func() R)` | row allocator (defaults to `new(T)` for a pointer row `*T`) |
| `OptionalJoin(key, sql)` | register a demand-driven join |
| `JoinForSort(sortKey, join)` | a sort key that triggers a join |
| `SortMap(map[K]string)` | sort key → ORDER BY expression |
| `Search(ops…)` | declared search operators (see below) |
| `Conflict(cols…)` | ON CONFLICT target for `Upsert` |
| `DefaultValues(map[C]any)` | values applied to every write before per-call `Set` |
| `StrictClears()` | make a rules-driven `Clear` on a non-`Clearable` field fail with `ErrNotClearable` instead of skipping |
| `Hooks(hs…)` | schema-wide query-time hooks (see [Hooks](#query-time-options--hooks)) |
| `Errors(ErrorMap)` | constraint name → domain error |
| `WithLogger(l)` | logger to emit to (default no-op) |
| `Debug(on)` | toggle verbose per-statement SQL/arg logging |

> The option is `SortMap` (not `Sort`) — `Sort` is the ORDER BY term type.

## Reads

`Get`/`Select`/`Iterate` project and scan **exactly** the requested columns;
pass none to use the schema default set. A column whose `Join` trigger isn't
selected is neither joined nor scanned.

```go
a, err := artists.DAO().With(ArtistURI, "monstercat").Get() // narrow, no join

list, err := artists.DAO().
    Search("name:liquid public:true").
    OrderBy(dao.Asc(ArtistSortName)).
    Limit(50).Offset(100).
    Select(ArtistID, ArtistName, ArtistLabelGroup) // joins label_group

it, err := artists.DAO().Iterate(ArtistID, ArtistName) // streaming
defer it.Close()
for it.Next() { use(it.Value()) }
if err := it.Err(); err != nil { /* ... */ }

n, err := artists.DAO().With(ArtistPublic, true).Count() // ignores Limit/Offset
ok, err := artists.DAO().With(ArtistURI, "x").Exists()
```

## Writes

```go
id, err := artists.DAO().
    Set(ArtistName, "Pegboard Nerds").
    SetMap(map[ArtistField]any{ArtistURI: "pegboard-nerds", ArtistPublic: true}).
    Insert() // RETURNING id where the dialect supports it

err = artists.DAO().With(ArtistID, id).Set(ArtistName, "X").Update() // ErrNoConditions if no predicate
err = artists.DAO().Set(ArtistName, "Y").Set(ArtistURI, "y").Upsert() // ON CONFLICT (uri) DO UPDATE
err = artists.DAO().With(ArtistID, id).Delete()                       // ErrNoConditions if no predicate
```

`Set`/`SetMap` on a `ReadOnly` or unknown field fail loudly (`ErrReadOnlyField`
/ `ErrUnknownField`); no SQL runs. `Update`/`Delete` with no predicate return
`ErrNoConditions` (guards full-table mutations). `Clear(field)` writes the
field's `ClearValue` — SQL `NULL` by default, or a declared NOT-NULL sentinel.
`Insert`/`Upsert` with nothing staged return `ErrNothingToInsert`; an empty
`Update` is a silent no-op.

## Column targeting & predicates

`With`/`Excluding` are sugar over `Eq`/`In`/`NotIn` keyed by the field enum;
everything else is a `Predicate` via `WithPredicate`:

```go
dao.Eq(col, v)   dao.In(col, vs)   dao.NotIn(col, vs)
dao.IsNull(col)  dao.IsNotNull(col)
dao.Gt/Gte/Lt/Lte(col, v)   dao.Between(col, lo, hi)
dao.Like(col, pattern)      dao.EscapeLike(userInput) // escape %,_,\ for a literal match
dao.And(p…)  dao.Or(p…)  dao.Raw("expr = ? AND x > ?", a, b) // ? renumbered per dialect
```

An unknown field key fails fast with `ErrUnknownField`. `Like` takes a raw
pattern (wildcards live); wrap user input with `EscapeLike` for a literal
substring match.

### Search operators

`Search(ops…)` declares a `token:value token2:value2` query surface. Operators:

| Constructor | Matches | Keyed by |
|---|---|---|
| `StringOp(token, field)` | `LOWER(col) LIKE LOWER('%value%')` (case-insensitive, escaped) | field enum |
| `ExactOp(token, field)` | `col = value` | field enum |
| `BoolOp(token, col)` | `col = true/false` (`"true"`/`"1"` → true) | raw column |
| `ArrayOp(token, col)` | `value = ANY(col)` | raw column |
| `RawOp(token, fn)` | a caller-supplied `func(value string) Predicate` | — |

`ParseSorts("-created", "name")` turns HTTP sort specs into `[]Sort` (`-` =
desc); pass to `OrderBy`.

## On-demand joins

Every join is optional and demand-driven — emitted at most once per query, and
only when triggered by (1) a **selected** column with a `Join`, (2) a **sort**
key with a `JoinForSort`, or (3) a **forced** `DAO.Join(keys…)` (for filtering
on a joined table without selecting its columns).

## Query-time options & hooks

Beyond schema-wide configuration, you can attach behavior per acquisition and
per statement. `Schema.DAO`/`On`/`OnCtx` take variadic `QueryOption`s:

```go
schema.DAO(dao.WithQueryContext(ctx))        // per-call context (top of precedence)
schema.DAO(dao.WithHooks(metricsHook{}))     // add hooks for this DAO only
schema.DAO(dao.SkipHooks("softdelete"))      // opt out of a schema hook (incl. "dao.log")
```

A **hook** observes and augments every statement through three phases — embed
`dao.NopHook` and override only what you need:

```go
type Hook interface {
    BeforeBuild(ctx context.Context, q *QueryInfo, s Stager) error // mutate staged intent
    BeforeExec(ctx context.Context, q *QueryInfo) error            // observe / rewrite SQL+args
    AfterExec(ctx context.Context, q *QueryInfo, out Outcome) error // observe outcome; may replace the error
}
```

`Stager` lets a schema-agnostic hook shape the statement — `Where`/`OrderBy`/
`Limit` (reads + `Update`/`Delete`) and `SetColumn` (writes). `Where` on an
`OpInsert`/`OpUpsert` fails loudly (`ErrHookWhereUnsupported`) — branch on
`QueryInfo.Op` and use `SetColumn` there. Register schema-wide with `Hooks(…)`.
This is the seam for **tenant scoping, soft-delete filters, metrics, and
tracing** — declared once, enforced on every verb. See
[USAGE §Query-time hooks](USAGE.md) for worked hooks. With no hooks registered
the pipeline is a zero-allocation fast path. The `Debug(true)` SQL logger is
itself the built-in `"dao.log"` hook.

## Partial updates (PATCH)

`SetRules(map[C]Rule)` applies a wire-facing update disposition — `dao.Write(v)`,
`dao.Skip()`, or `dao.Clear()` per field — with the precedence
`DefaultValues → Set/SetMap → SetRules` (rules win). Unlike `Set`/`SetMap`,
`SetRules` **silently skips** unknown or read-only keys (a PATCH may carry more
than the entity writes). `Clear` resolves against the field's `Clearable`/
`ClearValue`: a non-clearable field is skipped, or errors under `StrictClears()`.

The [`golib/partial`](../partial/README.md) package turns a three-state JSON
PATCH body into exactly this rules map with zero per-entity code — bind a
`partial.Patch[T]` and hand it to `partial.ApplyRules(dao, patch)`.

## Batch writes (auto-chunked, optional COPY)

`Batch()` flushes rows as the **minimum number of statements that respect the
dialect's bind-parameter limit** — the Postgres 65535 limit cannot be exceeded
by construction. COPY-capable dialects (Postgres) use a bulk-load fast-path.

```go
b := artists.DAO().Batch()
for _, a := range many {
    b.AddRow(a) // uses each Field.Value; or b.Add(map[ArtistField]any{...})
}
err := b.SkipConflicts().Flush()  // chunked multi-row INSERT, or COPY for large batches
// b.OnConflictUpdate(ArtistURI)  — upsert on flush
// b.ForceCopy() / b.ForceInsert() — override the COPY-vs-INSERT heuristic
```

A failed chunk yields a `*BatchError` whose `Unwrap() []error` identifies the
chunk. Conflict handling on a non-upsert dialect, or `ForceCopy` on a non-COPY
dialect, returns `ErrUnsupported`.

## Transactions

`RunTx` is the primary entry point: commit on success, rollback on error, and
rollback-then-**re-panic** on panic. A DAO from `schema.On(tx)` runs every
statement on the transaction — no per-statement `.Use(tx)` to forget.
`schema.DAO()` runs on the pool (autocommit) — an explicit choice.

```go
err := dao.RunTx(ctx, []dao.DataConn{conn}, func(tx *dao.Transaction) error {
    id, err := artists.On(tx).Set(ArtistName, "X").Set(ArtistURI, "x").Insert()
    if err != nil { return err }
    return albums.On(tx).Set(AlbumArtistID, id).Set(AlbumTitle, "Y").Insert()
})
```

Across multiple `DataConn`s, commit happens in deterministic touch order; a
partial failure returns a `*CommitError` naming the failed context and listing
those already durably committed — the inconsistency the prior art hid is now
loud:

```go
var ce *dao.CommitError
if errors.As(err, &ce) && len(ce.AlreadyDurable) > 0 {
    // genuine cross-DB inconsistency — reconcile / alert
}
```

`tx.TwoPhase()` opts into **true two-phase commit** where the dialect supports
it (see below): prepare all, then commit all, with `CommitError.PreparedPending`
reporting any prepared-but-uncommitted transactions for operator recovery.
Non-DB resources can participate via `tx.Register(name, dao.ResourceFunc(commit,
rollback))` (e.g. delete an uploaded file on rollback). One transaction is
single-goroutine; background work uses an unbound DAO.

## Optional logging — SQL + args

Logging is opt-in and toggleable, and logs the **statement's final SQL and bind
args** (after any hook rewrite):

```go
artists := dao.New[...](conn, dao.WithLogger(log), dao.Debug(true), ...)
```

- Omit `WithLogger` → the no-op logger (never nil, allocation-free).
- `Debug(false)` (default) → no per-statement logging.
- `Debug(true)` → each statement (and each batch chunk) logs `{dao, sql, args}`
  at `logger.SeverityDebug`. Silence one call with `SkipHooks("dao.log")`.

Uses golib's [`logger`](../logger/README.md) interface (shape-identical to
`monstercat/golib/logger`); bridge any external logger with `logger.Adapt`.

## Errors

Driver errors are translated at the boundary, so callers never see
`*pgconn.PgError`.

| Sentinel | Meaning |
|----------|---------|
| `ErrNoRows` | a single-row read found nothing |
| `ErrDuplicate` / `ErrNotNull` / `ErrForeignKey` | constraint violations |
| `ErrNoConditions` | `Update`/`Delete` with no predicate |
| `ErrNothingToInsert` | `Insert`/`Upsert` with no staged values |
| `ErrReadOnlyField` / `ErrUnknownField` | staged-intent errors |
| `ErrNotClearable` | rules-driven `Clear` on a non-clearable field under `StrictClears()` |
| `ErrUnsupported` | a capability the dialect lacks (transactions, upsert, COPY) |
| `ErrHookWhereUnsupported` | a hook called `Stager.Where` on insert/upsert |
| `ErrTransactionClosed` / `ErrUnknownConnection` / `ErrTwoPhaseUnsupported` | transaction errors |

Constraint violations carry a `*ConstraintError{Constraint, Kind, Err}` — match
the sentinel via `errors.Is`, or the structured value via `errors.As`. Map a
constraint name to a domain error with
`dao.Errors(dao.ErrorMap{"artist_uri_key": ErrTaken})`.

## Writing a new driver

Implement `DataConn` + `Dialect` in a sub-package; the engine, interfaces, and
entity declarations need **no change**. `dao.GenericDialect` is an embeddable
base with Postgres/SQLite-shaped defaults (`$n`, 65535, `"`-quoting, RETURNING,
ON CONFLICT, transactions + upsert on) — override only what differs.
[`dao/sqlite`](sqlite/README.md#writing-a-driver-the-sqlite-pattern) is the
minimal template (four overrides). For OLAP / append-only stores, flip the
`SupportsTransactions`/`SupportsUpsert`/`SupportsReturning` predicates off and
let capability-gated operations return `ErrUnsupported` — see
[`dao/bigquery`](bigquery/README.md).

Set the capability predicates honestly: `SupportsReturning`, `CopySupported`,
`TwoPhaseSupported`, `SupportsTransactions`, `SupportsUpsert`,
`SupportsLastInsertID`. Two-phase-capable drivers also implement
`Prepare`/`CommitPrepared`/`RollbackPrepared`.

## Status

- **Two-phase commit is implemented** and gated on the dialect:
  [`dao/postgres`](postgres/README.md) supports it (`PREPARE TRANSACTION` /
  `COMMIT PREPARED`, server needs `max_prepared_transactions > 0`);
  `GenericDialect` (and thus sqlite/bigquery) reports `TwoPhaseSupported() ==
  false`, so `tx.TwoPhase()` fails fast there rather than silently degrading.
  Single-DB and multi-DB *ordered* commit with `CommitError` detection work on
  every transactional driver.
- **No `Registry` helper** — use a plain struct of `*Schema`.
- **No whole-row custom `Scanner` option** — the per-field `Scan` path is the
  route.