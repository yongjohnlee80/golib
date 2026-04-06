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
ml.Commit("item1", "item2", "item3")
items, _ := ml.Flush() // returns ["item1", "item2", "item3"]
```

## CSV Export

```go
type Record struct {
    Name  string
    Value int
}

csv := ingestor.NewCSV[Record]("export", 0) // 0 = default batch size
csv.Commit(Record{"foo", 1}, Record{"bar", 2})
csv.Flush() // writes to ./export-<timestamp> (1).csv
```

## JSON Export

```go
j := ingestor.NewJSON[Record]("export", 10_000)
j.Commit(records...)
j.Flush() // writes to ./export-<timestamp> (1).json
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
