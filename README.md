# golib

A collection of reusable Go packages with zero external dependencies.

```bash
go get github.com/yongjohnlee80/golib
```

## Packages

### threadsafe

Generic, thread-safe value containers.

- **`SynchronizedValue[T]`** — exclusive-access mutex wrapper (simple default)
- **`MultiReadSyncValue[T]`** — read-write mutex wrapper (optimized for read-heavy workloads)
- Both implement the `Value[T]` interface for interchangeable use

```go
import "github.com/yongjohnlee80/golib/threadsafe"

counter := threadsafe.NewSynchronizedValue(0)
counter.Set(10)
counter.Update(func(v int) int { return v + 1 })

// Read-heavy workload
cache := threadsafe.NewMultiReadSyncValue(map[string]string{})
m := cache.Lock()
val := m["key"]
cache.Unlock()
```

| | `SynchronizedValue` | `MultiReadSyncValue` |
|---|---|---|
| Read concurrency | Exclusive | Concurrent |
| Write concurrency | Exclusive | Exclusive |
| Best for | General use, write-heavy | Read-heavy, many goroutines reading |

### collections

Generic collection types and functional slice operations.

- **`Set[T]`** — unordered unique collection with union, intersect, diff, subset operations
- **`Map`**, **`Filter`**, **`Reduce`** — functional slice operations inspired by [cyc-ttn/go-collections](https://github.com/cyc-ttn/go-collections)

See [collections/README.md](collections/README.md) for full documentation.

### request

HTTP client with generic error handling, functional options, and multipart form support.

- **`Request()`** — core HTTP function; separates transport errors from status codes
- **`DecodeResponse[T]()`** — generic response decoder parameterized by error type
- **`FormWriter`** — multipart/form-data builder implementing `CustomPayload`
- **`RequestOption`** — functional options pattern for modifying requests
- **`Histories`** — bounded ring buffer of recent request/response pairs for debugging

```go
import "github.com/yongjohnlee80/golib/request"

p := &request.Params{
    Method: "POST",
    Url:    "https://api.example.com/tracks",
    Headers: map[string]string{"Authorization": "Bearer tok"},
}
request.Request(p, payload, request.ContentType(request.JSON))
err := request.DecodeResponse[request.Error](p, &response)
```

See [request/README.md](request/README.md) for full documentation.

### ingestor

Generic, thread-safe data ingestion pipelines. Buffer items in memory and flush to CSV or JSON files.

- **`MemoryLoader[T]`** — in-memory buffer (base for other ingestors)
- **`CSV[T]`** — batched CSV file export with background writes
- **`JSON[T]`** — batched JSON file export with background writes
- **`Ingestor[T]`** — interface for custom backend implementations

Background write errors are collected and returned by `Flush()` as `*BatchErrors`.

See [ingestor/README.md](ingestor/README.md) for full documentation.

## License

See [LICENSE](LICENSE) file.
