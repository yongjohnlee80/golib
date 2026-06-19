# dao

A generic, driver-agnostic data-access layer (DAL) for Go. You declare each entity
**once** (its fields, columns, scan targets, joins, sort, and search config) and
that single declaration drives column selection, scanning, query building, batch
writes, and transactions.

It is deliberately **not an ORM**: no struct-tag magic, no migrations, no lazy
relationship graphs. Explicit columns, explicit joins, column-aware reads.

- **Core (`dao`) and `logger` have zero external dependencies** — the engine builds
  SQL with a small internal builder and executes through stdlib `database/sql`-shaped
  interfaces.
- **Drivers live in sub-packages** and are the only code with a DB dependency:
  [`dao/postgres`](postgres/README.md) (pgx) and [`dao/sqlite`](sqlite/README.md)
  (pure-Go modernc).

```bash
go get github.com/yongjohnlee80/golib
```

See **[USAGE.md](USAGE.md)** for a longer, worked cookbook.

---

## The idea: one declaration per entity

The prior art this distills restated each column up to five times (enum, scanner,
SQL translator, setters, interface). Here a column's SQL expression, its scan
target, its write value, and its join trigger are declared **together, once**, as
data:

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
    ArtistID:   {Column: "artist.id", Scan: func(a *Artist) any { return &a.ID }},
    ArtistName: {Column: "artist.name", Scan: func(a *Artist) any { return &a.Name }, Value: func(a *Artist) any { return a.Name }},
    ArtistURI:  {Column: "artist.uri", Scan: func(a *Artist) any { return &a.URI }, Value: func(a *Artist) any { return a.URI }},
    ArtistPublic: {Column: "artist.public", Scan: func(a *Artist) any { return &a.Public }, Value: func(a *Artist) any { return a.Public }},
    ArtistLabelGroup: {
        Column:   "COALESCE(label_group.name,'')",
        Scan:     func(a *Artist) any { return &a.LabelGroupName },
        Join:     "label_group", // only joined when this field is selected/sorted/forced
        ReadOnly: true,          // never written
    },
}
```

`Field` carries:

| Field | Purpose |
|-------|---------|
| `Column` | the SQL expression projected for reads (fully qualified) |
| `Scan func(R) any` | pointer into the row struct for scanning |
| `Value func(R) any` | model→value extractor for `Batch().AddRow` (omit for DB-generated/read-only) |
| `Join JoinKey` | optional join this field triggers on demand |
| `ReadOnly bool` | computed/joined field that must never appear in a write |
| `WriteColumn string` | override the bare write column when `Column` is qualified/expression |

## Build a Schema (once, at startup)

The four type parameters (`R, C, K, ID`) are verbose at the `dao.New` call, so write
a per-entity **type alias** and a small `Build` function once; everything downstream
is then clean. Options are functional (`func(*config) *config`, applied in order):

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

`New` validates required options (`Table`, `Fields`, and a registered join for every
field that declares one) and **panics at construction** — not per query — on
misconfiguration. The `Schema` is immutable and safe to hold for the process
lifetime; acquiring a `DAO` from it is cheap (only small per-query state is allocated).

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
| `Errors(ErrorMap)` | constraint name → domain error |
| `WithLogger(l)` | logger to emit to (default no-op) |
| `Debug(on)` | toggle verbose per-statement SQL/arg logging |

> Note: the option is `SortMap` (not `Sort`), because `Sort` is the ORDER BY term type.

## Reads

`Get`/`Select`/`Iterate` project and scan **exactly** the requested columns; pass none
to use the schema default set. A column whose `Join` trigger isn't selected is neither
joined nor scanned.

```go
artists := BuildArtistSchema(conn, log)

// Narrow read, no join:
a, err := artists.DAO().With(ArtistURI, "monstercat").Get()

// Joins label_group only because ArtistLabelGroup is selected:
list, err := artists.DAO().
    Search("name:liquid public:true").
    OrderBy(dao.Asc(ArtistSortName)).
    Limit(50).
    Select(ArtistID, ArtistName, ArtistLabelGroup)

// Streaming (does not buffer the whole result set):
it, err := artists.DAO().Iterate(ArtistID, ArtistName)
defer it.Close()
for it.Next() { use(it.Value()) }
if err := it.Err(); err != nil { /* ... */ }

n, err := artists.DAO().With(ArtistPublic, true).Count()
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

`Set` on a `ReadOnly` field stages an error that the next terminal verb returns (no
SQL runs). `Update`/`Delete` with no predicate return `ErrNoConditions` (guards
full-table mutations). `Clear(field)` writes SQL `NULL` (distinct from "not set").

## Column targeting & predicates

`With`/`Excluding` are sugar over `Eq`/`In`/`NotIn` keyed by the field enum.
Everything else is a `Predicate` via `WithPredicate`:

```go
dao.Eq(col, v)   dao.In(col, vs)   dao.NotIn(col, vs)
dao.IsNull(col)  dao.IsNotNull(col)
dao.Gt/Gte/Lt/Lte(col, v)   dao.Between(col, lo, hi)   dao.Like(col, pattern)
dao.And(p…)  dao.Or(p…)  dao.Raw("expr = ? AND x > ?", a, b) // ? renumbered per dialect
```

```go
artists.DAO().
    WithPredicate(dao.Between("artist.created", lo, hi)).
    WithPredicate(dao.Or(dao.Eq("artist.public", true), dao.IsNull("artist.deleted"))).
    Select()
```

An unknown field key fails fast with a clear `ErrUnknownField` (not a silent no-op).

## On-demand joins

Every join is optional and demand-driven. It's emitted at most once per query, and
only when triggered by (1) a **selected** column with a `Join`, (2) a **sort** key with
a `JoinForSort`, or (3) a **forced** `DAO.Join(keys…)` — the last for filtering on a
joined table without selecting its columns.

## Batch writes (auto-chunked, optional COPY)

`Batch()` accumulates rows and flushes them as the **minimum number of statements that
respect the dialect's bind-parameter limit** — the Postgres 65535 limit cannot be
exceeded by construction. COPY-capable dialects (Postgres) use a bulk-load fast-path.

```go
b := artists.DAO().Batch()
for _, a := range many {
    b.AddRow(a) // uses each Field.Value; or b.Add(map[ArtistField]any{...})
}
err := b.SkipConflicts().Flush()         // chunked multi-row INSERT, or COPY for large batches
// b.OnConflictUpdate(ArtistURI) — upsert on flush
// b.ForceCopy() / b.ForceInsert()        — override the COPY-vs-INSERT heuristic
```

A failed chunk yields a `*BatchError` whose `Unwrap() []error` identifies the chunk.

## Transactions

`RunTx` is the primary entry point: commit on success, rollback on error, and
rollback-then-**re-panic** on panic. A DAO from `schema.On(tx)` runs every statement on
the transaction — there is no per-statement `.Use(tx)` to forget. `schema.DAO()` runs on
the pool (autocommit) — now an explicit choice.

```go
err := dao.RunTx(ctx, []dao.DataConn{conn}, func(tx *dao.Transaction) error {
    id, err := artists.On(tx).Set(ArtistName, "X").Insert()
    if err != nil { return err }
    return albums.On(tx).Set(AlbumArtistID, id).Insert2()
})
```

Across multiple `DataConn`s, commit happens in deterministic touch order; a partial
failure returns a `*CommitError` naming the failed context and listing those already
durably committed (the inconsistency the prior art hid is now loud):

```go
var ce *dao.CommitError
if errors.As(err, &ce) && len(ce.AlreadyDurable) > 0 {
    // genuine cross-DB inconsistency — reconcile / alert
}
```

Non-DB resources can participate (e.g. delete an uploaded file on rollback):

```go
tx.Register("file:"+id, dao.ResourceFunc(
    func() error { return nil },              // commit: keep it
    func() error { return store.Delete(id) }, // rollback: remove the orphan
))
```

Single-goroutine per transaction (driver txs aren't concurrency-safe); background work
uses an unbound DAO.

## Optional logging — SQL + args

Logging is opt-in, toggleable, and logs the **builder's exact SQL and bind args**:

```go
artists := dao.New[...](conn, dao.WithLogger(log), dao.Debug(true), ...)
```

- Omit `WithLogger` → the no-op logger (never nil, allocation-free).
- `Debug(false)` (default) → no per-statement logging, no allocation on the statement path.
- `Debug(true)` → each statement (and each batch chunk) logs `{dao, sql, args}` at
  `logger.SeverityDebug` — exactly what is sent to the driver.

The logger is golib's [`logger`](../logger/README.md) interface, shape-identical to
`monstercat/golib/logger`; bridge any external logger with `logger.Adapt`.

## Errors

Driver errors are translated at the boundary, so callers never see `*pgconn.PgError`.

| Sentinel | Meaning |
|----------|---------|
| `ErrNoRows` | a single-row read found nothing |
| `ErrDuplicate` / `ErrNotNull` / `ErrForeignKey` | constraint violations |
| `ErrNoConditions` | `Update`/`Delete` with no predicate |
| `ErrNothingToInsert` | `Insert`/`Upsert` with no staged values |
| `ErrReadOnlyField` / `ErrUnknownField` | staged intent errors |
| `ErrTransactionClosed` / `ErrUnknownConnection` / `ErrTwoPhaseUnsupported` | transaction errors |

Constraint violations carry a `*ConstraintError{Constraint, Kind, Err}` — match the
sentinel via `errors.Is`, or the structured value via `errors.As`. Map a specific
constraint name to a domain error with `dao.Errors(dao.ErrorMap{"artist_uri_key": ErrTaken})`.

## Writing a new driver

Implement `DataConn` + `Dialect` in a sub-package; the engine, interfaces, and entity
declarations need **no change**. `dao.GenericDialect` is an embeddable base with
Postgres/SQLite-shaped defaults (`$n`, 65535, `"`-quoting, RETURNING, ON CONFLICT) —
override only what differs (see `dao/sqlite`, which overrides just `?` placeholders,
the 999 limit, and error translation).

## Status & limitations

- **Two-phase commit** is opt-in (`tx.TwoPhase()`) but the prepared-transaction
  execution is not yet wired; drivers report `TwoPhaseSupported() == false`, so
  `TwoPhase()` **fails fast** rather than silently degrading. Single-DB and multi-DB
  *ordered* commit with `CommitError` detection are fully implemented.
- **No `Registry` helper** — use a plain struct of `*Schema` (the recommended shape).
- **No whole-row custom `Scanner` option** — the per-field `Scan` path is the route.
