# ingestor

Generic, thread-safe data ingestion pipelines for Go. Buffer items in memory and flush them to CSV or JSON files.

## Features

- **Generic**: Works with any type `T`
- **Thread-safe**: Concurrent commits from multiple goroutines
- **Batched writes**: Automatically flushes to disk when batch thresholds are reached
- **Error collection**: Background write errors are collected and returned by `Flush()`
- **Zero external dependencies**

## Install

```bash
go get github.com/yongjohnlee80/golib
```

## Quick Start

```go
import "github.com/yongjohnlee80/golib/ingestor"

// In-memory buffer
ml := ingestor.NewMemoryLoader[string]("my-data")
ml.Commit(ctx, "item1", "item2", "item3")
items, _ := ml.Flush(ctx) // returns ["item1", "item2", "item3"]
```

## CSV Export

```go
type Record struct {
    Name  string
    Value int
}

csv := ingestor.NewCSV[Record]("export") // functional options configure everything:
//   ingestor.WithBatchSize(500_000)  rows per file (default DefaultCSVBatchSize)
//   ingestor.WithDir("/data/exports") destination directory (default ".")
//   ingestor.WithOpener(fn)           fully custom write target (tests, streams)
//   ingestor.WithMaxWriters(2)        cap on concurrent background writes (default 4)
// Field names become CSV headers; `csv:"name"` overrides, `csv:"-"` omits.
csv.Commit(ctx, Record{"foo", 1}, Record{"bar", 2})
csv.Flush(ctx) // writes to ./export-<timestamp>-001.csv
defer csv.Close() // terminal: drains + writes any remainder, discarding rows
```

## JSON Export

```go
j := ingestor.NewJSON[Record]("export", ingestor.WithBatchSize(10_000))
j.Commit(ctx, records...)
j.Flush(ctx) // writes to ./export-<timestamp>-001.json
```

## Custom Ingestor

Implement the `Ingestor[T]` interface, or embed `MemoryLoader[T]` and override `Commit`/`Flush`:

```go
type MyIngestor[T any] struct {
    *ingestor.MemoryLoader[T]
}

func (m *MyIngestor[T]) Commit(items ...T) error {
    _ = m.MemoryLoader.Commit(items...)
    // custom batching logic...
    return nil
}
```

## License

See [LICENSE](../LICENSE) file.
