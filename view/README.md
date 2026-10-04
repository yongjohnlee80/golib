# view — documents materialized from a database, declared in a .view file

A `.view` file is a query that returns one JSON value per row, plus a template that turns each row
into a document in any text format: Markdown, JSON, XML, HTML or plain text. It keeps the shaping
of data for a consumer apart from the consumer itself. An embedding pipeline reads documents. It
does not know that a track joins a label. A data engineer changes what a document holds by editing
the `.view` file, without touching Go code.

```text
---
version: 1
name: track_view
source: postgres
args:
  - entity_ids
process: |
  SELECT jsonb_build_object(
    'entity_id',   t.id,
    'track',       t.title,
    'label_title', l.title,
    'bpm',         t.bpm,
    'lyric',       t.lyric
  )
  FROM track t
  JOIN label l ON l.id = t.label_id
  WHERE t.id = ANY($1)
export: track_{{.entity_id}}.md
---
# {{.track}}

Label: {{.label_title}} | BPM: {{.bpm}}

{{if .lyric}}{{.lyric}}{{else}}*(Instrumental)*{{end}}
```

[`parse/view`](../parse/view/README.md) reads the file; this package evaluates and runs it.

## Install

```sh
go get github.com/yongjohnlee80/golib
```

```go
import "github.com/yongjohnlee80/golib/view"
```

## Using a view

```go
v, err := view.Load("track.view")       // or view.New(src)
rows, err := v.WithArgs(trackIDs).Query(ctx, conn) // conn is a dao.DataConn
if err != nil { return err }
defer rows.Close()

for x, err := range rows.All() {        // or x, err := rows.Next() until io.EOF
    if err != nil { return err }

    var t TrackView
    err = json.Unmarshal(x, &t)         // 1. the row itself, into your own struct

    doc, err := v.Parse(x)              // 2. the document, as a string

    rdr, err := v.Read(x)               // 3. the document, as a stream
    sendToEmbeddingModel(ctx, rdr)
    rdr.Close()
}
```

A row is the query's JSON, and it is the caller's to keep. The three ways to consume it compose,
so one query can feed a typed API response and an embedding request together. `Execute(w, x)`
renders into a writer of your own.

`Read` writes the document from a goroutine into a pipe, so the document is never held whole. The
goroutine ends when the document is written or the stream is closed. A consumer that stops
reading early must `Close`.

### Exporting

```go
dst, err := local.New("/var/export")    // any vfs.FS: vfs/local for file://, another driver for a bucket
n, err := v.WithArgs(labelGroupID).Export(ctx, conn, dst)
```

`Export` writes each row's document at the name its `export` template renders. Directories in the
name are created. Each file is written atomically, so a render that fails partway leaves the old
file intact. The first error stops the run, and the files already written stay. `Destination()`
returns the declared URI. This package does not dial storage itself; the caller opens the
filesystem the URI names.

## The frontmatter

| field | | |
| --- | --- | --- |
| `version` | required | `1` |
| `name` | required | names the view in errors |
| `source` | required | the dialect name of the connection: `postgres`, `sqlite`, `mysql`. `Query` refuses any other |
| `args` | optional | names of the arguments, bound in order as the query's parameters (`$1`, `$2`… in postgres) |
| `process` | required | the query. It returns exactly one column holding one JSON value per row |
| `export` | optional | a template for each document's file name, such as `track_{{.entity_id}}.md` |
| `destination` | optional | a URI for exports: `file:///dir`, `gs://bucket/prefix` |
| `format` | optional | `text`, `markdown`, `json`, `xml` or `html`. Defaults from `export`'s extension, else `text` |

The frontmatter is YAML read under the Core schema. Write a multi-line query as a block scalar,
`process: |`. An unknown field is an error, so a misspelt optional field cannot silently go missing.

**The query returns JSON.** PostgreSQL assembles it (`jsonb_build_object`, `jsonb_agg` for nested
lists), and so does SQLite (`json_object`). The row then needs no reflection or column scanning: it
is ready to decode, render or stream.

**The query is never a template.** `{{` in `process` is refused. Every argument is a bound
parameter, so a value from a search hit or a request cannot change the SQL. A Go slice binds as an
array where the driver supports one. That lets one view serve both bulk ingestion, filtered by a
label group, and point reconstruction, `WHERE t.id = ANY($1)` with the ids of a search hit. Both
paths render through the same template, so the document is the same either way.

## The body

The body is a Go `text/template`. A row's fields are `{{.field}}`, with `if`, `range`, `with`,
`define` and every built-in function. These helpers are added:

| helper | |
| --- | --- |
| `toJson` | the value as JSON (object keys sorted), printed without escaping |
| `quote` | the value as a JSON string, quotes included, printed without escaping; also valid YAML |
| `raw` | the value printed without the format's escaping |
| `trim`, `upper`, `lower` | the obvious string operations |
| `default` | `{{.genre \| default "unknown"}}`: the fallback when the value is missing, null, or an empty string, list or object |

**Every printed value is escaped for the format.** In `json`, a value is escaped as the content of
a JSON string, so a lyric holding a quote or a newline cannot break the document. In `xml` and
`html`, a value is escaped as character data. `text` and `markdown` print values as they are. The
escaping is added to every action by the package, not left to each author. A value from `toJson`,
`quote` or `raw` is printed as it is.

**A NULL or missing value prints as nothing**, never as `<no value>`. Numbers keep the digits the
database sent. A nested list or object prints as compact JSON.

A body that calls an undefined function is refused when the view loads, with the line and column
of the call.

## Writing documents for an embedding model

- **Prefer dense prose to JSON.** Braces, quotes and repeated keys spend the model's tokens and pull
  every document's vector toward a shared structure. Render the meaningful text: a synopsis, the
  lyrics, a compact header.
- **Avoid boilerplate sentences.** A sentence that repeats across every row, such as "X is a track
  produced at N BPM in the key of K", pulls every vector toward the same point. Use a terse header,
  `Label: … | BPM: … | Key: …`, and let the distinctive text dominate.
- **Keep dates and exact filter values out of the prose.** A vector cannot compare dates or evaluate
  a range. Put them in the document's own frontmatter, rendered with `quote`, where a search can
  filter on them exactly.

[`testdata/track.view`](testdata/track.view) follows all three.

## Errors

| error | when |
| --- | --- |
| `ErrInvalid` | the file is not a valid view. The message gives the file's line and column. Parser errors also unwrap to `*parse/view.Error` |
| `ErrArgs` | the argument count differs from `args`. Checked before the query runs |
| `ErrSource` | the connection's dialect is not `source`. Checked before the query runs |
| `ErrRow` | a row is not one JSON value: more or fewer than one column, NULL, or invalid JSON. The message gives the row number |
| `ErrNoExport` | `Export` on a view without `export` |

## License

See [LICENSE](../LICENSE).
