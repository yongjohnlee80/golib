# logger

A small, toggleable, level-based logging hook. Zero external dependencies (stdlib `log`/`fmt` only).

The package exists so other golib packages — notably `dao` — can accept optional logging without pulling in a logging framework. The `Logger` interface is intentionally shape-identical to `monstercat/golib/logger.Logger`, and `Severity` uses the same six values as monstercat and `ddex-sftp`, so records bridge between them losslessly.

## Install

```bash
go get github.com/yongjohnlee80/golib
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

`Severity` is a string with six constants, ordered least-to-most severe:
`SeverityDebug`, `SeverityInfo`, `SeverityNotice`, `SeverityWarning`, `SeverityError`, `SeverityCritical`.

## Implementations

```go
logger.Nop{}                 // discards everything — the dao default; alloc-free
logger.NewLogger("ctx")      // SimpleLogger over the standard logger
logger.New(                  // canonical constructor: functional options
    logger.WithContext("api"),
    logger.WithWriter(os.Stderr),        // injectable sink (testable, no globals)
    logger.WithMinLevel(logger.SeverityInfo),
)
logger.NewMulti(a, b)        // fan-out to several loggers
logger.NewContextual(l, ctx) // attach a fixed context to every record
```

`SimpleLogger` can filter:

```go
l := &logger.SimpleLogger{
    Context:   "api",
    MinLevel:  logger.SeverityInfo,                 // drop Debug (e.g. in prod)
    BlockList: []logger.Severity{logger.SeverityNotice}, // suppress specific levels
}
l.Log(logger.SeverityInfo, "served request") // [Info] {api} served request
```

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

## Bridging an external logger

## Structured fields & slog interop

`Fields` carries structure that survives to structure-aware sinks; the
error-level helpers wrap `err` + payload in an `Entry` that keeps the chain
inspectable (`errors.Is`/`As`) instead of flattening to a string:

```go
logger.Info(l, logger.Fields{"msg": "request served", "status": 200, "dur_ms": 12})
logger.Error(l, err, logger.Fields{"op": "save"}) // payload is Entry{Err: err, ...}
```

Both slog directions are built in (stdlib-only, still zero external deps):

```go
// An app already on slog backs golib logging with one call:
l := logger.FromSlog(slog.Default()) // Fields -> attrs, Entry -> err attr

// Or run an existing *slog.Logger over a golib Logger:
sl := slog.New(logger.NewSlogHandler(l))
```

golib never imports monstercat. `Adapt` turns a function into a `Logger`, so a consumer that imports both packages bridges its logger in three lines — the cast is lossless because the severity string values are identical:

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

## Generic variant

`GenericLogger[S]` mirrors `ddex-sftp`'s parameterized logger for callers that want a custom severity type. The `dao` package uses the non-generic `Logger` so its signatures don't grow an extra type parameter.

## License

[MIT](../LICENSE)
