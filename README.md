# golib

A collection of reusable Go packages. The core packages have **zero external
dependencies**; the optional `dao` database drivers are the only packages that pull
one in (`dao/postgres` requires pgx, `dao/sqlite` requires the pure-Go modernc
SQLite driver).

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

### logger

A small, toggleable, level-based logging hook. Zero external dependencies (stdlib
`log`/`fmt` only). The `Logger` interface is shape-identical to
`monstercat/golib/logger`, and `Adapt` bridges any external logger without a
dependency.

- **`Logger`** — `Log(severity Severity, payload any)`; six `Severity` levels
- **`Nop`** / **`SimpleLogger`** / **`Multi`** / **`Contextual`** — implementations
- **`Adapt(fn)`** — wrap a function (or bridge an external logger) as a `Logger`

See [logger/README.md](logger/README.md) for full documentation.

### dao

A generic, driver-agnostic data-access layer (DAL). Declare each entity **once**
(fields, columns, scan targets, joins, sort, search) and that drives column-aware
reads, query building, scanning, auto-chunked batch writes, and multi-database
transactions. Not an ORM — explicit columns, explicit joins, no struct-tag magic.
Optional, toggleable SQL+args logging.

- **`dao`** — the core: zero external dependencies; `DAO[R,C,ID]` surface,
  `Schema`/builder, predicates, on-demand joins, batch, transactions, error translation
- **`dao/postgres`** — reference driver over pgx (native COPY, SQLSTATE translation)
- **`dao/sqlite`** — pure-Go SQLite driver (in-process; great for tests)

See [dao/README.md](dao/README.md) for the reference overview and
[dao/USAGE.md](dao/USAGE.md) for a worked cookbook.

## License

See [LICENSE](LICENSE) file.
