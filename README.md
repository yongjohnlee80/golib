# golib

A collection of reusable Go packages.

## Packages

### threadsafe

Generic, thread-safe value containers with zero dependencies.

- **`SynchronizedValue[T]`** — exclusive-access mutex wrapper (simple default)
- **`MultiReadSyncValue[T]`** — read-write mutex wrapper (optimized for read-heavy workloads)
- Both implement the `Value[T]` interface for interchangeable use

#### Install

```bash
go get github.com/yongjohnlee80/golib
```

#### Quick Start

```go
import "github.com/yongjohnlee80/golib/threadsafe"

// Basic usage
counter := threadsafe.NewSynchronizedValue(0)
counter.Set(10)
fmt.Println(counter.Get()) // 10

// Atomic update (safe read-modify-write)
counter.Update(func(v int) int { return v + 1 })

// Read-heavy workload — use MultiReadSyncValue
cache := threadsafe.NewMultiReadSyncValue(map[string]string{})

// Manual lock for indexing into maps/slices
m := cache.Lock()
val := m["key"]
cache.Unlock()
```

#### Choosing an Implementation

| | `SynchronizedValue` | `MultiReadSyncValue` |
|---|---|---|
| Read concurrency | Exclusive | Concurrent |
| Write concurrency | Exclusive | Exclusive |
| Best for | General use, write-heavy | Read-heavy, many goroutines reading |

### ingestor

Generic, thread-safe data ingestion pipelines. Buffer items in memory and flush to CSV or JSON files. Zero external dependencies.

- **`MemoryLoader[T]`** — in-memory buffer (base for other ingestors)
- **`CSV[T]`** — batched CSV file export
- **`JSON[T]`** — batched JSON file export
- **`Ingestor[T]`** — interface for custom backend implementations

See [ingestor/README.md](ingestor/README.md) for full documentation.

## License

See [LICENSE](LICENSE) file.
