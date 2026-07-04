# ingestor

Generic, thread-safe data-ingestion pipelines: buffer items in memory and flush
them in batches to CSV or JSON files (or any `io.Writer` backend you supply).
Background writes are bounded and drain-aware; write errors are aggregated and
returned on flush. Its only dependency is golib's own `threadsafe`.

```bash
go get github.com/yongjohnlee80/golib/ingestor
```

```go
import "github.com/yongjohnlee80/golib/ingestor"
```

## The `Ingestor` interface

Every ingestor implements one contract:

```go
type Ingestor[T any] interface {
    Commit(ctx context.Context, items ...T) error // buffer items
    Flush(ctx context.Context) ([]T, error)        // drain + write + wait; returns the drained rows
    Total() uint64                                  // cumulative committed count
    Close() error                                   // terminal: flush + wait, discarding rows
}
```

All methods are safe for concurrent use. A cancelled `ctx` aborts before any
state change.

## Basic usage

```go
csv := ingestor.NewCSV[Record]("export") // writes ./export-<unix>-NNN.csv

for _, r := range records {
    if err := csv.Commit(ctx, r); err != nil {
        return err
    }
}
rows, err := csv.Flush(ctx) // writes any remainder, waits for background writes
// rows is the batch just flushed; err is *BatchErrors if any background write failed
```

When you don't need the flushed rows back, use `Close` as a terminal `defer`:

```go
j := ingestor.NewJSON[Record]("export")
defer j.Close() // flush + wait, discard rows
for _, r := range records {
    _ = j.Commit(ctx, r)
}
```

`MemoryLoader[T]` is a pure in-memory buffer (no files) — use it directly when
you only need buffering, or embed it to build a custom backend (below).

## Batching and background writes

`CSV`/`JSON` accumulate items and, whenever the buffer reaches the batch size,
spawn a background goroutine to write that batch to its own file. Defaults are
`DefaultCSVBatchSize` (1,000,000 rows) and `DefaultJSONBatchSize` (100,000). The
remainder is written by `Flush`/`Close`, which also wait for all background
writes to finish and return any accumulated errors as `*BatchErrors`.

```go
var be *ingestor.BatchErrors
if _, err := csv.Flush(ctx); errors.As(err, &be) {
    for _, e := range be.Errors { // errors.Is/As also walk these via Unwrap() []error
        log.Println("batch write failed:", e)
    }
}
```

Files are named `<sanitized-description>-<unix-timestamp>-<NNN>.<ext>` (the
counter is zero-padded; `/`, `\`, and spaces in the description become `-`).

## Options

`CSV`/`JSON` take functional options:

| Option | Default | Effect |
|---|---|---|
| `WithBatchSize(n)` | per-format default | rows/items that trigger a background batch (`0` keeps the default) |
| `WithDir(dir)` | `"."` | output directory (must already exist) |
| `WithOpener(fn)` | create files in `WithDir` | replace file creation entirely — write each batch to the `io.WriteCloser` you return; makes the writers unit-testable and lets you target buffers, sockets, or object storage. Overrides `WithDir`. |
| `WithMaxWriters(n)` | `4` | cap on concurrent background writes. `Commit` blocks once the cap is reached — backpressure, not unbounded goroutine growth. |

```go
csv := ingestor.NewCSV[Record]("orders",
    ingestor.WithBatchSize(500_000),
    ingestor.WithDir("/data/exports"),
    ingestor.WithMaxWriters(2),
)
```

### In-memory / custom sink via `WithOpener`

```go
var buf bytes.Buffer
csv := ingestor.NewCSV[Record]("test", ingestor.WithOpener(
    func(name string) (io.WriteCloser, error) {
        return nopCloser{&buf}, nil // capture output instead of touching disk
    }))
```

## CSV column mapping

`CSV` builds its header and rows by reflection over the struct fields. A `csv`
struct tag controls the column name; `csv:"-"` omits the field; unexported
fields are always skipped. Values are formatted with `%v`.

```go
type Record struct {
    Name   string `csv:"full_name"` // header: "full_name"
    Secret string `csv:"-"`         // omitted
    Count  int                      // header: "Count"
}
```

`CSVHeaderRow[T](sample)` exposes the same header derivation if you need it
standalone.

## Extending: a custom backend

Embed `*MemoryLoader[T]` and override `Commit`/`Flush` to drive any backend —
this is exactly how `CSV` and `JSON` are built. `MemoryLoader` gives you the
buffer primitives: `Commit`, `Flush`, `Shift(n)` (remove and return the first
`n` items), `Len`, `Total`, `Description`/`SetDescription`.

```go
type Kafka[T any] struct {
    *ingestor.MemoryLoader[T]
    topic string
}

func NewKafka[T any](topic string) *Kafka[T] {
    return &Kafka[T]{MemoryLoader: ingestor.NewMemoryLoader[T](topic), topic: topic}
}

func (k *Kafka[T]) Commit(ctx context.Context, items ...T) error {
    if err := k.MemoryLoader.Commit(ctx, items...); err != nil {
        return err
    }
    for k.Len() >= 1000 {
        batch := k.Shift(1000)
        if err := k.publish(ctx, batch); err != nil {
            return err
        }
    }
    return nil
}
```

## Gotchas

- `Commit`/`Flush` take a `context.Context` — a cancelled ctx aborts before the
  buffer changes. If ctx is cancelled after a full batch has already been
  drained during `Commit`, that batch is written synchronously (never lost) and
  `Commit` returns `ctx.Err()`.
- `WithMaxWriters` makes `Commit` **block** at the cap. That is intentional
  backpressure; size it for your throughput.
- `WithDir`'s directory must already exist; `WithOpener` ignores `WithDir`.
- `MemoryLoader.Close` has nowhere to write — it just discards. Prefer `Flush`
  when the buffered items matter.

## File layout

| File | Contents |
|---|---|
| `ingestor.go` | `Ingestor[T]` interface |
| `memory.go` | `MemoryLoader[T]` base buffer |
| `csv.go` | `CSV[T]`, `CSVHeaderRow`, `DefaultCSVBatchSize` |
| `json.go` | `JSON[T]`, `DefaultJSONBatchSize` |
| `options.go` | `Option`, `WithBatchSize`/`WithDir`/`WithOpener`/`WithMaxWriters` |
| `errors.go` | `BatchErrors` aggregate |

## License

See [LICENSE](../LICENSE).