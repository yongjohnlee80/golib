# `dao/bigquery` — BigQuery driver for golib/dao

A `dao.DataConn` for **Google BigQuery**, implementing the read-mostly /
no-transaction driver contract (golib-dao **ADR-0008**). BigQuery is an OLAP /
append-only store, so it does **not** fit the transactional OLTP contract the
`dao/postgres` and `dao/sqlite` drivers satisfy — this driver plugs into the
*same* `DataConn`/`Dialect` contract via the capability surface, with no
parallel DAO hierarchy.

> **Separate Go module.** This package has its own `go.mod` so the heavy Google
> Cloud SDK is pulled **only** by callers that use BigQuery; the `golib/dao`
> core stays dependency-light.

## What works vs. what returns `dao.ErrUnsupported`

| Works | Returns `dao.ErrUnsupported` |
|-------|------------------------------|
| `Select` / `Get` / `Count` / `Exists` / `Iterate` (reads) | `Begin` / `RunTx` / `schema.On(tx)…` (transactions) |
| `Insert` (DML; returns zero id) / `Update` / `Delete` (DML) | `Upsert` (and batch `OnConflictUpdate` / `SkipConflicts`) |
| `Batch().Add(…).Flush()` → chunked multi-row INSERT | `Batch().ForceCopy()…Flush()` |

`Insert` returns the **zero `ID` with a nil error** — BigQuery has no
server-generated id, so supply ids client-side (e.g. a UUID) for append-only
tables (ADR-0008 §2.6). Unsupported operations never panic; test with
`errors.Is(err, dao.ErrUnsupported)`.

Capability predicates all report the read-mostly profile:
`SupportsReturning`, `SupportsTransactions`, `SupportsUpsert`,
`SupportsLastInsertID`, `CopySupported`, and `TwoPhaseSupported` are all
`false`.

## Usage

```go
import (
    "github.com/yongjohnlee80/golib/dao/bigquery"
    "google.golang.org/api/option"
)

// Application Default Credentials (gcloud auth application-default login),
// scoping unqualified table names to <dataset>.
conn, err := bigquery.Open(ctx, "my-project", "my_dataset")
// or with an explicit credentials file:
// conn, err := bigquery.Open(ctx, "my-project", "my_dataset", option.WithCredentialsFile("key.json"))
defer conn.Close()

schema := dao.New[*Event, EventField, EventSort, string](conn, /* options */)
rows, err := schema.DAO().With(EventType, "play").Select()
```

## Dialect specifics

Backtick-quoted identifiers, `?` positional placeholders (mapped to
`bigquery.QueryParameter`), `MaxBindParams` 10000. Row scanning bridges
`bigquery.RowIterator` to `dao.Rows` by reading each row into a
`[]bigquery.Value` and assigning positionally into the scan destinations:

- Type conversions handled: INT64/FLOAT64 → Go numerics, DATE/DATETIME →
  `time.Time` (UTC), NUMERIC/BIGNUMERIC (`*big.Rat`) → string.
- **Numeric conversions are checked**, never a silent wraparound or truncation:
  a negative value into an unsigned target, an integer overflow, a non-integral
  float into an integer target (`7.0` ok, `7.5` errors), or NaN/±Inf all return
  an error.
- A **scan destination/column count mismatch is a hard error** — no silent
  zero-fill or dropped column.

Error translation is pass-through: BigQuery has no unique/foreign-key
constraint SQLSTATEs to map to `dao.ConstraintError` sentinels.

## Testing

```bash
# Integration tests run against a REAL dataset, gated by the build tag + env
# (they create and drop their own table; need dataset write + DDL):
BQ_TEST_PROJECT=my-proj BQ_TEST_DATASET=my_dataset \
  go test -tags=integration ./...

# Creds-free unit tests (dialect contract + scan-adapter conversions):
go test ./...
```

## Notes / future work

- **Bulk ingestion** currently uses chunked INSERT DML. BigQuery is not built
  for high-frequency small DML; a load-job fast-path (the COPY-equivalent — a
  JSON `ReaderSource` loader) is a natural future addition behind
  `CopySupported()`.
- The nested `go.mod` uses a `replace => ../..` for local development; external
  `go get` of this submodule needs the root module to carry a version tag.