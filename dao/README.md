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
  (pure-Go modernc), [`dao/mysql`](mysql/README.md) (pure-Go go-sql-driver;
  the LastInsertId profile), [`dao/bigquery`](bigquery/README.md) (GCP SDK,
  separate module — a read-mostly/no-transaction store).
- **Declarations built from your constants** (ADR-0016): `Field.Expr` with
  `dao.T`/`dao.C`/`dao.Coalesce`/`dao.LeftJoin` renders identifiers through the
  connection's dialect at construction time, so a reserved word or a
  schema-qualified name quotes correctly on every driver instead of being
  hand-quoted for one.
- **Catalog + result metadata** (ADR-0012/0013): `dao.ListSchemas` /
  `ListTables` / `ListColumns` introspect a connection's catalog uniformly
  across postgres/sqlite/mysql; `dao.Columns(rows)` reports a raw query's
  column names via the optional `RowsColumns` extension. Schema-qualified
  table names (`"app.users"`) quote correctly in table position.

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

const (
    TableArtist     = "artist"
    TableLabelGroup = "label_group"
)

var artistFields = map[ArtistField]dao.Field[*Artist]{
    ArtistID:     {Expr: dao.T(TableArtist, ArtistID), Scan: func(a *Artist) any { return &a.ID }},
    ArtistName:   {Expr: dao.T(TableArtist, ArtistName), Scan: func(a *Artist) any { return &a.Name }, Value: func(a *Artist) any { return a.Name }},
    ArtistURI:    {Expr: dao.T(TableArtist, ArtistURI), Scan: func(a *Artist) any { return &a.URI }, Value: func(a *Artist) any { return a.URI }},
    ArtistPublic: {Expr: dao.T(TableArtist, ArtistPublic), Scan: func(a *Artist) any { return &a.Public }, Value: func(a *Artist) any { return a.Public }},
    ArtistLabelGroup: {
        Expr:     dao.Coalesce(dao.T(TableLabelGroup, "name"), ""),
        Scan:     func(a *Artist) any { return &a.LabelGroupName },
        Join:     TableLabelGroup, // only joined when this field is selected/sorted/forced
        ReadOnly: true,            // never written
    },
}
```

`Expr` builds the column from the constants you already declared and resolves
it once, per dialect, at `dao.New` — see
[Declaring columns](#declaring-columns-expr-or-column). `Column: "artist.id"`
remains valid everywhere `Expr` appears; the two forms coexist field by field.

`Field[R]` carries:

| Field | Purpose |
|-------|---------|
| `Column` | SQL expression projected for reads (fully qualified), as raw SQL |
| `Expr` | the same expression built from constants and resolved per dialect at `dao.New` — mutually exclusive with `Column` |
| `Scan func(R) any` | pointer into the row struct for scanning |
| `Value func(R) any` | model→value extractor for `Batch().AddRow` (omit for DB-generated/read-only) |
| `Join JoinKey` | optional join this field triggers on demand |
| `ReadOnly bool` | computed/joined field that must never appear in a write |
| `WriteColumn string` | override the bare write column when `Column` is qualified/an expression |
| `Clearable bool` | a rules-driven `Clear` (partial updates) may target this column |
| `ClearValue any` | value written when cleared: `nil` → SQL `NULL`; non-nil → a NOT-NULL sentinel (requires `Clearable`) |

## Declaring columns: `Expr` or `Column`

`Column` is raw SQL, emitted verbatim. `Expr` is the same expression built from
your constants, resolved **once** at `dao.New` against the connection's dialect
and then stored as `Column` — so nothing downstream differs and there is no
query-time cost.

| helper | renders (Postgres / MySQL) |
|--------|----------------------------|
| `dao.T(TableArtist, ArtistName)` | `"artist"."name"` / `` `artist`.`name` `` |
| `dao.C(MetaKVAction)` | `"action"` / `` `action` `` |
| `dao.Coalesce(dao.T(t, c), "")` | `COALESCE("t"."c", '')` |
| `dao.Str("n/a")` · `dao.Int(0)` | `'n/a'` · `0` |
| `dao.SQL("NOW()")` | `NOW()` — verbatim, unquoted |
| `dao.LeftJoin(t, dao.T(…), dao.T(…))` | `LEFT JOIN "t" ON … = …` (also `InnerJoin`) |

Both are generic over `~string`, so a typed field enum and an untyped table
constant both pass without conversion. `dao.T` renders the table part in table
position, so a schema-qualified constant (`"app.users"`) splits per part on a
dialect implementing `TableQuoter`.

Why not just write the string:

- **A declaration cannot know its dialect.** A package-level field map is built
  long before any `DataConn` exists, so a reserved word has to be hand-quoted —
  `` `"user".first_name` `` is correct on Postgres/SQLite and, on MySQL, a
  *string literal* rather than an identifier. `dao.New` does know the dialect.
- **The constants already exist.** Your field enum names every column and
  `Table(…)` names the table; a literal restates both where the compiler cannot
  check them.
- **Literals stay portable by refusing to guess.** `dao.Str` rejects a string
  containing a quote, a backslash or a control character (MySQL escaping depends
  on `NO_BACKSLASH_ESCAPES` and the charset), and `dao.Coalesce`'s fallback
  accepts only an `Expr`, a string or an integer — a float or a bool is a
  **compile** error.

Two rules worth knowing:

- **Write identity.** `dao.T`/`dao.C` carry the raw column name, so
  `INSERT`/`UPDATE` quote one identifier exactly once. An expression
  (`Coalesce`, `SQL`) has no column to write to, so a **writable** field
  declared with one must set `ReadOnly: true` or an explicit `WriteColumn` —
  otherwise `dao.New` panics rather than emitting broken DML.
- **Quoting is not semantically neutral.** Postgres folds unquoted identifiers,
  so `dao.C("MyCol")` renders `"MyCol"` and will not match a column created
  unquoted as `MyCol` (stored as `mycol`). For a mixed-case identifier keep
  `Column`, or use `dao.SQL`.

Setting `Column` **and** `Expr` on one field is a declaration error and panics
at `dao.New`. See [USAGE §1.1](USAGE.md) for the worked version.

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
| `OptionalJoinExpr(key, expr)` | the same, from `dao.LeftJoin`/`dao.InnerJoin`, resolved per dialect (later option wins across both forms) |
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

> **Predicate constructors take a raw column string**, emitted verbatim —
> `With`/`Excluding` are the ones that resolve through the schema's declared
> column, so they quote correctly for free. If you reach for `dao.Eq`/`Between`/…
> on a column needing quotes (a reserved word, mixed case), write the quoted
> form yourself: ``dao.Between(`"user"."order"`, lo, hi)``. Expression helpers are
> deliberately not wired into predicate position (ADR-0016 §2.7).

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
key with a `JoinForSort`, or (3) a **forced** `DAO.Join(keys…)`.

> **A predicate is not a trigger.** `Count`, `Exists`, `Update` and `Delete`
> take their joins from `DAO.Join` alone, and a `Select` joins only what it
> projects — so a query whose *only* reference to the joined table is a filter
> emits SQL naming a table it never joined, and the database rejects it
> (Postgres: `missing FROM-clause entry for table …`). Force the join whenever
> you filter on a joined column without selecting it:
>
> ```go
> artists.DAO().Join(JoinLabelGroup).With(ArtistLabelGroup, "alpha").Count()
> ```
>
> For `Update`/`Delete` the forced join also switches the statement to the
> portable `WHERE id IN (SELECT …)` form, since neither can `JOIN` directly.

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

> **Conflict handling never degrades silently.** `OnConflictUpdate(cols…)` names
> its target; `OnConflictUpdate()` with no columns means *this entity's declared
> `Conflict(…)` target* — the same thing `DAO.Upsert` uses — and when the schema
> declares none, `Flush` returns `ErrNoConflictTarget` rather than emitting a
> plain `INSERT` that would fail on the very duplicates you asked to update.
> `SkipConflicts()` is `DO NOTHING`: it does not update.

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

**You do not pass connections.** Each schema already holds its `DataConn`, so a
tx-bound DAO enlists it on its first statement (ADR-0015):

```go
err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
    id, err := artists.On(tx).Set(ArtistName, "X").Set(ArtistURI, "x").Insert()
    if err != nil { return err }
    return albums.On(tx).Set(AlbumArtistID, id).Set(AlbumTitle, "Y").Insert()
})
```

Writing to a **second** database is the one case you declare, with
`dao.Spanning` — undeclared, the first database to join locks the transaction
and a second one fails with `ErrUnknownConnection` rather than silently
becoming a non-atomic cross-database commit:

```go
err = dao.RunTx(ctx, fn, dao.Spanning(lmConn, goldConn))
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

`dao.TwoPhase()` opts into **true two-phase commit** where the dialect supports
it (see below): prepare all, then commit all, with `CommitError.PreparedPending`
reporting any prepared-but-uncommitted transactions for operator recovery. It is
a construction option, so the commit protocol cannot be switched mid-flight.
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
  false`, so `dao.TwoPhase()` fails fast there rather than silently degrading —
  from `RunTx` before the body runs when the span is declared, at `Commit`
  otherwise.
  Single-DB and multi-DB *ordered* commit with `CommitError` detection work on
  every transactional driver.
- **Declarative column expressions are implemented** (ADR-0016): `Field.Expr`
  plus `dao.T`/`C`/`Str`/`Int`/`SQL`/`Coalesce`/`LeftJoin`/`InnerJoin` and
  `OptionalJoinExpr`, resolved once per schema at `dao.New`. Purely additive —
  `Column` is unchanged and migration is per field. Predicate position is
  deliberately not covered.
- **No `Registry` helper** — use a plain struct of `*Schema`.
- **No whole-row custom `Scanner` option** — the per-field `Scan` path is the
  route.