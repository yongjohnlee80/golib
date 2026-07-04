# threadsafe

Generic, thread-safe value containers. Zero dependencies. Three
implementations share one `Value[T]` interface, so you can swap the locking
strategy without changing call sites.

```bash
go get github.com/yongjohnlee80/golib/threadsafe
```

```go
import "github.com/yongjohnlee80/golib/threadsafe"
```

## Choosing an implementation

| | `SynchronizedValue` | `MultiReadSyncValue` | `AtomicValue` |
|---|---|---|---|
| Read concurrency | Exclusive | Concurrent (read lock) | Lock-free |
| Write concurrency | Exclusive | Exclusive | CAS retry |
| `Update`/`Do` runs `fn` | once, under lock | once, under lock | possibly **more than once** (must be pure) |
| Best for | general use, write-heavy | read-heavy | read-dominated hot paths |

```go
counter := threadsafe.NewSynchronizedValue(0)     // mutex
cache   := threadsafe.NewMultiReadSyncValue(m)     // RWMutex
config  := threadsafe.NewAtomicValue(Config{})     // atomic.Pointer snapshot
```

## The `Value[T]` interface

```go
type Value[T any] interface {
    Get() T                // read a snapshot
    Set(T)                 // replace wholesale
    Update(func(T) T)      // atomic read-modify-write
    Do(fn func(*T))        // mutate under the (write) lock
    RDo(fn func(T))        // read under the (read) lock
}
```

## Correct usage — the closure discipline

`Get()` then `Set()` is a **lost-update race**: another goroutine may change the
value between the two calls. Use `Update` instead:

```go
counter.Update(func(v int) int { return v + 1 }) // atomic increment
```

Indexing a map or slice returned by `Get()` races concurrent writers. Read or
mutate compound values **under the lock**, via closures:

```go
cache := threadsafe.NewMultiReadSyncValue(map[string]int{})

var n int
cache.RDo(func(m map[string]int) { n = m["key"] })       // shared read lock
cache.Do(func(m *map[string]int) { (*m)["key"] = n + 1 }) // exclusive write lock
```

The closure passed to `Do`/`RDo`/`Update` **must not** call back into the same
`Value` (self-deadlock) and **must not** retain the pointer or value after it
returns.

## AtomicValue: the pure-function contract

`AtomicValue` reads are lock-free. `Update` and `Do` run their function inside a
compare-and-swap loop, so **under contention the function may run more than
once** — it must have no side effects. `Do` mutates a *copy* and installs it,
so values previously read via `Get`/`RDo` (shared immutable snapshots) are never
affected:

```go
cfg := threadsafe.NewAtomicValue(Config{MaxConns: 10})
current := cfg.Get()                                   // lock-free
cfg.Do(func(c *Config) { c.MaxConns = 20 })            // CAS install of a mutated copy
```

## Gotchas

- `SynchronizedValue.RDo` takes the **exclusive** lock (there is no shared read
  mode); readers serialize. Use `MultiReadSyncValue` for true concurrent reads.
- `MultiReadSyncValue` is one-writer / many-readers.
- Treat values from `AtomicValue.Get`/`RDo` as read-only shared snapshots.
- These types embed a mutex / atomic pointer — don't copy them after first use;
  always construct via the `New*` functions (they return pointers).

## File layout

| File | Contents |
|---|---|
| `value.go` | `Value[T]` interface |
| `value_synchronized.go` | `SynchronizedValue[T]` (mutex) |
| `value_multi_read.go` | `MultiReadSyncValue[T]` (RWMutex) |
| `value_atomic.go` | `AtomicValue[T]` (lock-free) |

## License

See [LICENSE](../LICENSE).