# threadsafe

Generic, thread-safe value containers.

## Install

```bash
go get github.com/yongjohnlee80/golib/threadsafe
```

## Types

| | `SynchronizedValue` | `MultiReadSyncValue` | `AtomicValue` |
|---|---|---|---|
| Read concurrency | Exclusive | Concurrent (read lock) | Lock-free |
| Write concurrency | Exclusive | Exclusive | CAS retry loop |
| Best for | General use, write-heavy | Read-heavy | Read-dominated hot paths |

All three implement the `Value[T]` interface: `Get`, `Set`, `Update`, `Do`, `RDo`.

## Usage

```go
import "github.com/yongjohnlee80/golib/threadsafe"

counter := threadsafe.NewSynchronizedValue(0)
counter.Set(10)
counter.Update(func(v int) int { return v + 1 }) // atomic read-modify-write

// Compound access to reference types happens under the lock via closures:
cache := threadsafe.NewMultiReadSyncValue(map[string]string{})
var v string
cache.RDo(func(m map[string]string) { v = m["key"] })       // shared read lock
cache.Do(func(m *map[string]string) { (*m)["key"] = "new" }) // exclusive lock

// Lock-free reads for hot paths (Update/Do run a CAS retry loop and must be
// pure — under contention the function can execute more than once):
cfg := threadsafe.NewAtomicValue(Config{Debug: false})
current := cfg.Get()
```

`Get()+Set()` is a lost-update race — use `Update`. Indexing a map/slice
returned by `Get()` races concurrent writers — use `RDo`/`Do`.

## License

See [LICENSE](../LICENSE).
