# logger

A small, toggleable, level-based logging hook. Zero external dependencies
(stdlib `log`/`fmt`/`log/slog` only).

The package exists so other golib packages — notably `dao` — can accept
optional logging without pulling in a logging framework. The `Logger` interface
is intentionally shape-identical to `monstercat/golib/logger.Logger`, and
`Severity` uses the same six values as monstercat and `ddex-sftp`, so records
bridge between them losslessly.

```bash
go get github.com/yongjohnlee80/golib/logger
```

```go
import "github.com/yongjohnlee80/golib/logger"
```

## The interface

```go
type Logger interface {
    Log(severity Severity, payload any)
}
```

That single method is the whole contract — implement it and you have a logger
golib packages will accept.

`Severity` is a string with six constants, ordered least-to-most severe:
`SeverityDebug`, `SeverityInfo`, `SeverityNotice`, `SeverityWarning`,
`SeverityError`, `SeverityCritical`. `InList(list, s)` tests membership (parity
with monstercat's helper).

## Implementations

```go
logger.Nop{}                 // discards everything — the dao default; alloc-free
logger.New(                  // canonical constructor: functional options
    logger.WithContext("api"),
    logger.WithWriter(os.Stderr),               // injectable sink (testable, no globals)
    logger.WithMinLevel(logger.SeverityInfo),   // drop anything below Info
    logger.WithBlockList(logger.SeverityNotice),
)
logger.NewLogger("ctx")      // monstercat-parity: SimpleLogger with context, no filtering
logger.NewMulti(a, b)        // fan-out to several loggers (nil entries skipped)
logger.NewContextual(l, ctx) // wrap a Logger, attaching a fixed context to every record
```

Prefer `New(opts...)` — it takes an injectable `io.Writer` so tests need no
global `log.SetOutput`. `SimpleLogger`'s fields (`Context`, `MinLevel`,
`BlockList`) remain settable for monstercat-parity call sites.

```go
l := logger.New(logger.WithContext("api"), logger.WithMinLevel(logger.SeverityInfo))
l.Log(logger.SeverityInfo, "served request") // [Info] {api} served request
l.Log(logger.SeverityDebug, "noisy")         // dropped by MinLevel
```

> A severity outside the known six ranks below all filters and is **never**
> dropped by `MinLevel`.

## Level helpers

Ergonomic call-site helpers that route to the matching severity:

```go
logger.Debug(l, payload)
logger.Info(l, payload)
logger.Notice(l, payload)
logger.Warning(l, err, payload) // wraps err + payload; nil err logs payload alone
logger.Error(l, err, payload)
logger.Critical(l, err, payload)
```

## Structured fields & error entries

`Fields` is a structured payload: structure-aware sinks (the slog bridge below)
emit each key as an attribute; text sinks render the map. A `"msg"` string key
becomes the record message.

```go
logger.Info(l, logger.Fields{"msg": "request served", "status": 200, "dur_ms": 12})
```

The error-level helpers wrap `err`+payload in an `Entry`, which implements
`error` and unwraps to the original — so the chain stays inspectable with
`errors.Is`/`errors.As` instead of being flattened into a string, and a
structured sink can destructure it:

```go
logger.Error(l, err, logger.Fields{"op": "save"})
// payload is Entry{Err: err, Payload: Fields{...}}; errors.Is(entry, io.EOF) still works
```

## Bridging an external logger

`Adapt` turns a function (or a closure over another logger) into a `Logger`, so
golib never imports the external package. The severity string values are
identical across monstercat/ddex-sftp, so the cast is lossless:

```go
import (
    glog "github.com/yongjohnlee80/golib/logger"
    mclog "github.com/monstercat/golib/logger"
)

func bridge(l mclog.Logger) glog.Logger {
    return glog.Adapt(func(s glog.Severity, p any) { l.Log(mclog.Severity(s), p) })
}

// then:
dao.New(conn, dao.WithLogger(bridge(services.Logger)), ...)
```

## slog interop

Both directions are built in (stdlib-only, still zero external deps):

```go
// An app already on slog hands golib packages a Logger with one call:
l := logger.FromSlog(slog.Default()) // Fields -> attrs, Entry -> err attr

// Or run an existing *slog.Logger over a golib Logger:
sl := slog.New(logger.NewSlogHandler(l)) // attrs -> Fields, message under "msg"
```

Severity ↔ slog level mapping is threshold-based; `Notice` and `Critical` sit
between/beyond the four stdlib levels. `NewSlogHandler.Enabled` returns true for
every level — filtering is delegated to the destination `Logger`
(`MinLevel`/`BlockList`).

## Generic variant

`GenericLogger[S]` mirrors `ddex-sftp`'s parameterized logger for callers that
want a custom severity type. The `dao` package uses the non-generic `Logger` so
its signatures don't grow an extra type parameter.

## File layout

| File | Contents |
|---|---|
| `logger.go` | `Logger`, level helpers, `Entry` |
| `severity.go` | `Severity`, the six constants, `InList` |
| `simple.go` | `SimpleLogger`, `New`, `WithContext`/`WithWriter`/`WithMinLevel`/`WithBlockList` |
| `nop.go` | `Nop` |
| `multi.go` | `Multi`, `NewMulti` |
| `wrapper.go` | `Contextual`, `NewContextual` |
| `adapt.go` | `Adapt` |
| `slog.go` | `Fields`, `FromSlog`, `NewSlogHandler` |
| `generic.go` | `GenericLogger[S]` |

## License

[MIT](../LICENSE)