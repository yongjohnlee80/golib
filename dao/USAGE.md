# dao — usage cookbook

Worked, end-to-end examples for `github.com/yongjohnlee80/golib/dao`. For the
reference overview see [README.md](README.md). The snippets use the Postgres driver
([`dao/postgres`](postgres/README.md)); swap `postgres.Open` for `sqlite.Open` to run
the same code on SQLite.

---

## 1. A complete entity, from table to queries

```go
package store

import (
    "context"

    "github.com/yongjohnlee80/golib/dao"
    "github.com/yongjohnlee80/golib/dao/postgres"
    "github.com/yongjohnlee80/golib/logger"
)

// --- model -----------------------------------------------------------------

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
    ArtistLabelGroup ArtistField = "label_group_name"
)

type ArtistSort string

const (
    SortName    ArtistSort = "name"
    SortCreated ArtistSort = "created"
)

// --- declaration (the single source of truth) ------------------------------

const (
    TableArtist     = "artist"
    TableLabelGroup = "label_group"
)

var artistFields = map[ArtistField]dao.Field[*Artist]{
    ArtistID:     {Expr: dao.T(TableArtist, ArtistID), Scan: sID, Value: vID},
    ArtistName:   {Expr: dao.T(TableArtist, ArtistName), Scan: sName, Value: vName},
    ArtistURI:    {Expr: dao.T(TableArtist, ArtistURI), Scan: sURI, Value: vURI},
    ArtistPublic: {Expr: dao.T(TableArtist, ArtistPublic), Scan: sPublic, Value: vPublic},
    ArtistLabelGroup: {
        Expr:     dao.Coalesce(dao.T(TableLabelGroup, "name"), ""),
        Scan:     sLabel,
        Join:     TableLabelGroup,
        ReadOnly: true,
    },
}

func sID(a *Artist) any     { return &a.ID }
func sName(a *Artist) any   { return &a.Name }
func sURI(a *Artist) any    { return &a.URI }
func sPublic(a *Artist) any { return &a.Public }
func sLabel(a *Artist) any  { return &a.LabelGroupName }
func vID(a *Artist) any     { return a.ID }
func vName(a *Artist) any   { return a.Name }
func vURI(a *Artist) any    { return a.URI }
func vPublic(a *Artist) any { return a.Public }

// --- schema (built once, at startup) ----------------------------------------

type ArtistSchema = dao.Schema[*Artist, ArtistField, ArtistSort, string]

func BuildArtistSchema(conn dao.DataConn, log logger.Logger) *ArtistSchema {
    type O = dao.Option[*Artist, ArtistField, ArtistSort, string]
    return dao.New[*Artist, ArtistField, ArtistSort, string](conn,
        O(dao.Table[*Artist, ArtistField, ArtistSort, string]("artist")),
        O(dao.ID[*Artist, ArtistField, ArtistSort, string](ArtistID)),
        O(dao.Fields[*Artist, ArtistField, ArtistSort, string](artistFields)),
        O(dao.Default[*Artist, ArtistField, ArtistSort, string](ArtistID, ArtistName, ArtistURI)),
        O(dao.OptionalJoin[*Artist, ArtistField, ArtistSort, string]("label_group",
            "LEFT JOIN label_group ON label_group.id = artist.label_group_id")),
        O(dao.SortMap[*Artist, ArtistField, ArtistSort, string](map[ArtistSort]string{
            SortName:    "artist.name",
            SortCreated: "artist.created_at",
        })),
        O(dao.Conflict[*Artist, ArtistField, ArtistSort, string](ArtistURI)),
        O(dao.Search[*Artist, ArtistField, ArtistSort, string](
            dao.StringOp("name", ArtistName),     // name:liquid  -> case-insensitive LIKE
            dao.BoolOp("public", "artist.public"), // public:true
        )),
        O(dao.WithLogger[*Artist, ArtistField, ArtistSort, string](log)),
    )
}

func Open(ctx context.Context, dsn string, log logger.Logger) (*ArtistSchema, dao.DataConn, error) {
    conn, err := postgres.Open(ctx, dsn)
    if err != nil {
        return nil, nil, err
    }
    return BuildArtistSchema(conn, log), conn, nil
}
```

> The four type parameters only appear inside `BuildArtistSchema`. Everywhere else
> you use the `*ArtistSchema` alias and the clean `DAO` surface.

### 1.1 `Expr` — declarations built from your constants (ADR-0016)

> Reference table and the two rules (write identity, quoting is not
> semantically neutral) live in
> [README → Declaring columns](README.md#declaring-columns-expr-or-column).
> This section is the why, worked.

`Field.Expr` is the dialect-resolved alternative to `Field.Column`: `dao.New`
renders it **once** against the connection's dialect, so nothing downstream
changes and there is no query-time cost.

| helper | renders (Postgres / MySQL) |
| --- | --- |
| `dao.T(TableArtist, ArtistName)` | `"artist"."name"` / `` `artist`.`name` `` |
| `dao.C(MetaKVAction)` | `"action"` / `` `action` `` |
| `dao.Coalesce(dao.T(t, c), "")` | `COALESCE("t"."c", '')` |
| `dao.Str("n/a")` · `dao.Int(0)` | `'n/a'` · `0` |
| `dao.SQL("NOW()")` | `NOW()` (verbatim, unquoted) |
| `dao.LeftJoin(TableLG, dao.T(…), dao.T(…))` | `LEFT JOIN "label_group" ON … = …` |

Why bother, when a string literal is shorter to type:

- **The constants already exist.** Your field enum names every column and
  `dao.Table` names the table; a literal restates both where the compiler cannot
  check them. Rename a table and `dao.T` call sites follow; string literals do not.
- **Quoting is dialect-specific and a declaration cannot know its dialect.** A
  package-level field map is built long before any `DataConn` exists, so a
  reserved word like `user` has to be hand-quoted as `` `"user".first_name` `` —
  ANSI quotes that are correct on Postgres/SQLite and wrong on MySQL. `dao.T`
  resolves per dialect instead.
- **`Coalesce` and friends only accept what they can render identically
  everywhere.** `dao.Str` refuses a string containing a quote, a backslash or a
  control character (MySQL's escaping depends on `NO_BACKSLASH_ESCAPES` and the
  charset), and `dao.Coalesce`'s fallback accepts only an `Expr`, a string or an
  integer — a float or a bool is a **compile** error.

Both forms coexist; migrate a field when you touch it. Setting `Column` **and**
`Expr` on one field panics at `dao.New`.

An expression has no column to write to, so `Coalesce`/`SQL` carry no write
identity: a **writable** field declared with one must say `ReadOnly: true` or set
`WriteColumn`, or `dao.New` panics rather than emitting broken DML. `dao.T`/`dao.C`
carry the raw column name, so writable fields declared with them just work.

```go
// The join clause, from the same constants:
O(dao.OptionalJoinExpr[*Artist, ArtistField, ArtistSort, string](TableLabelGroup,
    dao.LeftJoin(TableLabelGroup,
        dao.T(TableLabelGroup, "id"),
        dao.T(TableArtist, "label_group_id")))),
```

## 2. Reads

```go
// Single row by a unique field (no join — only narrow columns selected):
a, err := artists.DAO().With(ArtistURI, "monstercat").Get()
switch {
case errors.Is(err, dao.ErrNoRows):
    // not found
case err != nil:
    // real error
}

// List with search + sort + paging. The label_group join fires because
// ArtistLabelGroup is in the projection:
page, err := artists.DAO().
    Search("name:liquid public:true").
    OrderBy(dao.Desc(SortCreated), dao.Asc(SortName)).
    Limit(50).Offset(100).
    Select(ArtistID, ArtistName, ArtistLabelGroup)

// Default column set (Default option) when none is passed:
all, err := artists.DAO().Select()

// Streaming a large result set:
it, err := artists.DAO().With(ArtistPublic, true).Iterate(ArtistID, ArtistName)
if err != nil { return err }
defer it.Close()
for it.Next() {
    process(it.Value())
}
return it.Err()
```

## 3. Filtering beyond equality

```go
artists.DAO().
    With(ArtistPublic, true).                              // artist.public = $1
    WithPredicate(dao.In("artist.id", []any{1, 2, 3})).    // IN ($2,$3,$4)
    WithPredicate(dao.Or(                                  // (... OR ...)
        dao.Like("artist.name", "%liquid%"),
        dao.IsNotNull("artist.featured_at"),
    )).
    Excluding(ArtistURI, "blocked-uri").                   // uri NOT IN ($5)
    Select()
```

### Predicate constructors take a raw column string

`With`/`Excluding` are keyed by the **field enum**, so they resolve through the
schema and quote the column for you. The `dao.Eq`/`In`/`Like`/`Between`/… family
takes a **column string, emitted verbatim** — convenient for an expression, but
it means anything needing quotes must be written quoted:

```go
// Fine: unquoted snake_case resolves on every dialect.
dao.Between("artist.release_year", 2000, 2010)

// A reserved word (or a mixed-case identifier) does NOT:
dao.Between("user.order", lo, hi)      // -> ERROR: syntax error at or near "order"
dao.Between(`"user"."order"`, lo, hi)  // correct — quote it yourself
```

Prefer `With`/`Excluding` when the predicate is a plain equality/IN on a
declared field: they cannot get this wrong. The expression helpers
(`dao.T`/`dao.C`) are deliberately **not** wired into predicate position
(ADR-0016 §2.7), so a hand-written quoted string is the tool here. This is a
narrow edge — every identifier in the surveyed consumers is lower-case
`snake_case`, where nothing needs quoting.

### Filtering on a joined column

A join fires when its column is **selected**, or when a sort key declares it —
**not** when you merely filter on it. If a filter is your only reference to the
joined table, force the join with `DAO.Join`, or the emitted SQL will name a
table it never joined:

```go
// WRONG: no join is emitted, so the server rejects the statement
// (Postgres: missing FROM-clause entry for table "label_group").
artists.DAO().With(ArtistLabelGroup, "alpha").Count()

// RIGHT: force it.
artists.DAO().
    Join(JoinLabelGroup).                   // <- the trigger
    With(ArtistLabelGroup, "alpha").
    Count()

// Selecting the joined column is also a trigger, so this needs no Join():
artists.DAO().With(ArtistLabelGroup, "alpha").Select(ArtistID, ArtistLabelGroup)

// Writes work the same way. The forced join switches UPDATE/DELETE to the
// portable "WHERE id IN (SELECT … JOIN …)" form, because neither can JOIN.
artists.DAO().
    Join(JoinLabelGroup).
    With(ArtistLabelGroup, "alpha").
    Set(ArtistPublic, false).
    Update()
```

## 4. Writes & upserts

```go
id, err := artists.DAO().
    Set(ArtistName, "Pegboard Nerds").
    SetMap(map[ArtistField]any{ArtistURI: "pegboard-nerds", ArtistPublic: true}).
    Insert() // INSERT ... RETURNING "id"

err = artists.DAO().With(ArtistID, id).Set(ArtistPublic, false).Update()

// Upsert on the configured Conflict target (uri):
err = artists.DAO().Set(ArtistName, "X").Set(ArtistURI, "x").Upsert()
// -> INSERT ... ON CONFLICT ("uri") DO UPDATE SET "name" = EXCLUDED."name"

err = artists.DAO().With(ArtistID, id).Delete()
```

## 5. Bulk import (auto-chunked; COPY on Postgres)

```go
func Import(artists *ArtistSchema, rows []*Artist) error {
    b := artists.DAO().Batch()
    for _, a := range rows {
        b.AddRow(a) // pulls each Field.Value
    }
    // For 100k rows on a 12-column table this emits ceil(100000/5461)=19 statements,
    // none exceeding Postgres' 65535 bind-param limit — or a single COPY on Postgres.
    if err := b.SkipConflicts().Flush(); err != nil {
        var be *dao.BatchError
        if errors.As(err, &be) {
            // be.Errors identifies the failed chunk(s); other chunks still ran
        }
        return err
    }
    return nil
}
```

## 6. Transactions

```go
// Single DB — fully atomic. A CommitError means nothing was written.
// No connection argument: each schema supplies its own (ADR-0015).
err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
    id, err := artists.On(tx).Set(ArtistName, "X").Set(ArtistURI, "x").Insert()
    if err != nil {
        return err // -> rollback
    }
    _, err = albums.On(tx).Set(AlbumArtistID, id).Set(AlbumTitle, "Y").Insert()
    return err
})

// Two DBs — ordered commit, partial-failure reported. Spanning is REQUIRED to
// touch a second database: undeclared, the second one fails with
// ErrUnknownConnection instead of quietly becoming a non-atomic commit.
err = dao.RunTx(ctx, func(tx *dao.Transaction) error {
    if err := labels.On(tx).With(LabelID, id).Set(LabelName, n).Update(); err != nil {
        return err
    }
    return goldLabels.On(tx).With(GoldLabelID, id).Set(GoldLabelName, n).Update()
}, dao.Spanning(lmConn, goldConn))
var ce *dao.CommitError
if errors.As(err, &ce) && len(ce.AlreadyDurable) > 0 {
    log.Critical(logger, ce, "cross-DB inconsistency — reconcile") // impossible to miss now
}

// Compensating non-DB resource (delete an upload if the DB work rolls back):
err = dao.RunTx(ctx, func(tx *dao.Transaction) error {
    if err := blobs.Put(ctx, key, data); err != nil {
        return err
    }
    tx.Register("blob:"+key, dao.ResourceFunc(
        func() error { return nil },
        func() error { return blobs.Delete(ctx, key) },
    ))
    _, err := artists.On(tx).Set(ArtistName, "X").Set(ArtistURI, "x").Insert()
    return err
})
```

`schema.OnCtx(ctx)` reads a transaction stashed with `dao.WithTx(ctx, tx)` for code
that threads a `context.Context` — the explicit `*Transaction` remains source of truth.

## 7. Mapping a constraint to a domain error

```go
var ErrURITaken = errors.New("artist URI already taken")

dao.Errors[*Artist, ArtistField, ArtistSort, string](dao.ErrorMap{
    "artist_uri_key": ErrURITaken, // the unique-index name
})

// then:
if _, err := artists.DAO().Set(ArtistURI, "dup").Insert(); errors.Is(err, ErrURITaken) {
    // 409 Conflict
}
```

## 8. Debug logging (SQL + args)

```go
artists := BuildArtistSchema(conn, logger.NewLogger("artist-dao")) // SimpleLogger
// enable verbose logging for a deep-debug session:
debugSchema := BuildArtistSchemaWithDebug(conn, log) // ... dao.Debug(true)

debugSchema.DAO().With(ArtistURI, "x").Get()
// logs at SeverityDebug: {dao: "artist", sql: `SELECT artist.id, ... WHERE artist.uri = $1 LIMIT $2`, args: ["x", 1]}
```

Bridge an existing (e.g. monstercat) logger without a dependency:

```go
import (
    glog "github.com/yongjohnlee80/golib/logger"
    mclog "github.com/monstercat/golib/logger"
)
func bridge(l mclog.Logger) glog.Logger {
    return glog.Adapt(func(s glog.Severity, p any) { l.Log(mclog.Severity(s), p) })
}
```

## 9. Query-time hooks (tenant scoping, soft delete, metrics)

Hooks run cross-cutting logic on *every* statement — declared once via
`dao.Hooks(...)` at schema build, or attached per call via
`schema.DAO(dao.WithHooks(...))`. Embed `dao.NopHook` and override only the
phase you need.

**Tenant scoping** — a `WHERE org_id = ?` on reads and `UPDATE`/`DELETE`, and a
forced `org_id` column on `INSERT`/`UPSERT` (a hook `Where` on insert/upsert
fails loudly, so branch on the op):

```go
type tenantHook struct{ dao.NopHook }

func (tenantHook) HookName() string { return "tenant" }
func (tenantHook) BeforeBuild(ctx context.Context, q *dao.QueryInfo, s dao.Stager) error {
    org, ok := auth.OrgID(ctx) // your ctx extraction
    if !ok {
        return fmt.Errorf("tenant: no org in context for %s on %s", q.Op, q.Table)
    }
    switch q.Op {
    case dao.OpInsert, dao.OpUpsert:
        s.SetColumn("org_id", org)
    default:
        s.Where(dao.Eq("org_id", org)) // SELECT + UPDATE + DELETE all scoped
    }
    return nil
}
```

**Soft delete** — filter reads by default, opt out per call:

```go
type softDeleteHook struct{ dao.NopHook }

func (softDeleteHook) HookName() string { return "softdelete" }
func (softDeleteHook) BeforeBuild(_ context.Context, q *dao.QueryInfo, s dao.Stager) error {
    if !q.Op.IsWrite() {
        s.Where(dao.IsNull("deleted_at"))
    }
    return nil
}
```

**Metrics** — observe duration and outcome (`AfterExec`'s return replaces the
statement error, so return `out.Err` to pass it through):

```go
type metricsHook struct{ dao.NopHook }

func (metricsHook) AfterExec(_ context.Context, q *dao.QueryInfo, out dao.Outcome) error {
    queryDuration.Observe(q.Table, string(q.Op), out.Duration)
    return out.Err
}
```

Wire them up, then every call is scoped, filtered, and measured:

```go
artists := dao.New[*Artist, ArtistField, ArtistSort, string](conn,
    /* ...fields... */
    dao.Hooks[*Artist, ArtistField, ArtistSort, string](tenantHook{}, softDeleteHook{}, metricsHook{}),
)

list, err := artists.OnCtx(reqCtx).Select()                      // scoped + filtered + measured
all, err := artists.OnCtx(reqCtx, dao.SkipHooks("softdelete")).Select() // admin: include deleted
n, err := artists.DAO(dao.WithQueryContext(reqCtx)).Count()      // explicit ctx on a pool DAO
```

## 10. Partial (PATCH) updates

`SetRules` applies a per-field disposition — write / skip / clear — with rules
taking precedence over `Set`/`SetMap` and `DefaultValues`. It is wire-facing:
unknown or read-only keys are silently skipped.

```go
err := artists.DAO().With(ArtistID, id).SetRules(map[ArtistField]dao.Rule{
    ArtistName: dao.Write("New Name"),
    ArtistURI:  dao.Clear(),   // → ClearValue (SQL NULL, or the field's sentinel)
    ArtistID:   dao.Skip(),    // authoritative: don't touch even if staged elsewhere
}).Update()
```

Declare per-column clearability on the `Field` and, optionally, `StrictClears()`
on the schema to make a `Clear` on a non-clearable field an error rather than a
skip:

```go
artistFields := map[ArtistField]dao.Field[*Artist]{
    ArtistURI: {Expr: dao.T(TableArtist, ArtistURI), Scan: sURI, Value: vURI, Clearable: true},
    ArtistDate: {Expr: dao.T(TableArtist, ArtistDate), Scan: sDate, Value: vDate,
        Clearable: true, ClearValue: lib.DateSentinel}, // NOT NULL → sentinel, not NULL
}
```

To drive this straight from an HTTP PATCH body with zero per-entity code, bind a
[`partial.Patch[T]`](../partial/README.md) and project it onto the DAO:

```go
p, err := partial.BindReader[Artist](r.Body) // three-state: value / absent / null
if err != nil { /* 400 on *partial.ValidationError */ }
p.Remove("id") // strip server-owned fields
d, err := partial.ApplyRules(artists.OnCtx(r.Context()).With(ArtistID, id), p)
if err != nil { /* ... */ }
err = d.Update() // writes only what the client sent; nulls clear per the Field's ClearValue
```

## 11. Two-phase commit across databases

For strict cross-DB atomicity on a capable dialect (Postgres, with
`max_prepared_transactions > 0`), opt in with `TwoPhase()`:

```go
// TwoPhase is a construction option, not a mid-flight switch: the commit
// protocol is fixed before any statement runs. Declared together with the span,
// an incapable dialect fails from RunTx BEFORE fn (ADR-0015 §2.5).
err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
    if _, err := lm.On(tx).Set(LMName, n).Insert(); err != nil {
        return err
    }
    return gold.On(tx).With(GoldID, id).Set(GoldName, n).Update()
}, dao.Spanning(lmConn, goldConn), dao.TwoPhase())

var ce *dao.CommitError
if errors.As(err, &ce) && len(ce.PreparedPending) > 0 {
    // a COMMIT PREPARED failed after a successful prepare; ce.PreparedPending
    // maps context → gid for operator resolution (pg_prepared_xacts)
}
```

On a dialect that doesn't support 2PC (`GenericDialect`, sqlite, bigquery),
`TwoPhase().Commit()` fails fast with `ErrTwoPhaseUnsupported` rather than
silently degrading to an ordered commit.
