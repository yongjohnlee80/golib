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

var artistFields = map[ArtistField]dao.Field[*Artist]{
    ArtistID:     {Column: "artist.id", Scan: sID, Value: vID},
    ArtistName:   {Column: "artist.name", Scan: sName, Value: vName},
    ArtistURI:    {Column: "artist.uri", Scan: sURI, Value: vURI},
    ArtistPublic: {Column: "artist.public", Scan: sPublic, Value: vPublic},
    ArtistLabelGroup: {
        Column:   "COALESCE(label_group.name,'')",
        Scan:     sLabel,
        Join:     "label_group",
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
err := dao.RunTx(ctx, []dao.DataConn{conn}, func(tx *dao.Transaction) error {
    id, err := artists.On(tx).Set(ArtistName, "X").Set(ArtistURI, "x").Insert()
    if err != nil {
        return err // -> rollback
    }
    return albums.On(tx).Set(AlbumArtistID, id).Set(AlbumTitle, "Y").Insert2()
})

// Two DBs — ordered commit, partial-failure reported.
err = dao.RunTx(ctx, []dao.DataConn{lmConn, goldConn}, func(tx *dao.Transaction) error {
    if err := labels.On(tx).With(LabelID, id).Set(LabelName, n).Update(); err != nil {
        return err
    }
    return goldLabels.On(tx).With(GoldLabelID, id).Set(GoldLabelName, n).Update()
})
var ce *dao.CommitError
if errors.As(err, &ce) && len(ce.AlreadyDurable) > 0 {
    log.Critical(logger, ce, "cross-DB inconsistency — reconcile") // impossible to miss now
}

// Compensating non-DB resource (delete an upload if the DB work rolls back):
err = dao.RunTx(ctx, []dao.DataConn{conn}, func(tx *dao.Transaction) error {
    if err := blobs.Put(ctx, key, data); err != nil {
        return err
    }
    tx.Register("blob:"+key, dao.ResourceFunc(
        func() error { return nil },
        func() error { return blobs.Delete(ctx, key) },
    ))
    return artists.On(tx).Set(ArtistName, "X").Set(ArtistURI, "x").Insert2()
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
