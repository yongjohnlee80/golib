# tui

A minimal-dependency, retained-mode terminal UI framework: a grapheme-cluster
cell buffer with diff-based flushing, a single-goroutine runtime (event loop,
typed pub/sub bus, bounded task pool, demand-scheduled timers), a component
tree with constraints-down/sizes-up layout, and a driver seam that makes every
app fully testable in CI without a PTY.

The core imports nothing outside the standard library and golib; the one
terminal driver (`tui/term`) confines `golang.org/x/term`/`x/sys` to the leaf.

```bash
go get github.com/yongjohnlee80/golib/tui
```

```go
import (
    "github.com/yongjohnlee80/golib/tui"        // runtime, tree, events, cells
    "github.com/yongjohnlee80/golib/tui/style"  // styles, tokens, themes
    "github.com/yongjohnlee80/golib/tui/term"   // the ANSI terminal driver
    "github.com/yongjohnlee80/golib/tui/widget" // the standard widget set
)
```

## The loop-goroutine invariant (normative)

All component state — the tree, every component's fields, focus, layout
rects, the cell buffer — is owned by the loop goroutine. `Init`, `Layout`,
`Render`, `HandleEvent`, bus handlers, and queued closures execute ONLY
there. The only operations legal from other goroutines are `App.Post`,
`App.Update`, `App.Go`, `Bus.Publish`, and `Context.Post`/`Context.Go` — all
of which enqueue and return.

Convention: code already running in a handler on the loop goroutine does not
need `App.Update` — it owns the state and mutates directly; an fn enqueued
from a handler runs in a later drain, before the next frame.

(The widget layer adds exactly one sanctioned any-goroutine surface on top:
the separate `io.Writer` handle returned by `widget.BufferView.Writer` —
bounded, ordered, and closed at unmount.)

## The two-seam portability contract

Portability is two seams. `Backend` (what a terminal is) and `Surface` (what
components draw on). Test/SSH/web backends are cheap and ship or are
trivially possible; a pixel driver is possible behind the same seams but
explicitly out of scope for v1. Component and runtime code never names a
platform: a real terminal, the in-memory `TestBackend`, and any future driver
all satisfy the same two interfaces.

## Quick start

```go
package main

import (
    "context"
    "os"

    "github.com/yongjohnlee80/golib/tui"
    "github.com/yongjohnlee80/golib/tui/term"
    "github.com/yongjohnlee80/golib/tui/widget"
)

func main() {
    backend, err := term.Open() // raw mode, alt screen, capability probe
    if err != nil {
        os.Exit(1)
    }
    input := widget.NewTextInput(widget.WithPlaceholder("type here"))
    root := widget.NewOverlayHost(
        widget.NewBox(input, widget.WithTitle("Hello")))
    app := tui.NewApp(root, tui.WithBackend(backend))
    _ = app.Run(context.Background()) // the loop runs on THIS goroutine
}
```

A complete application — split layout, focus cycling, async list fill,
streaming log panel, status bar — lives in `examples/demo`, and its whole
interaction script runs against `TestBackend` in `examples/demo/demo_test.go`.

## What's where

| Package | Contents |
|---|---|
| `tui` | `App` (loop, queue, timers, tasks), `Bus`, `Component`/`Context`, `Flex`/`Dock`/`Stack`, focus, `Surface`, `Cell`, events, `TestBackend` |
| `tui/style` | immutable `Style` values, semantic color `Token`s, `Theme`, borders, frame math |
| `tui/term` | the ANSI driver: input decoding (kitty + legacy), capability probing, diff flushing, terminal lifecycle |
| `tui/widget` | the standard widget set v1: `Box`, `TextInput`, `TextArea`, `Select`, `List`, `BufferView`, `Tabs`, `Split`, `Float`, `StatusBar`, `ProgressBar`, `Text` |

## Components in sixty seconds

A component implements four methods, all invoked on the loop goroutine:

```go
type Component interface {
    Init(ctx *tui.Context)             // once per mount; keep ctx
    Layout(c tui.Constraints) tui.Size // constraints down, size up
    Render(s tui.Surface)              // paint own chrome; children paint themselves
    HandleEvent(ev tui.Event) bool     // true = consumed, bubbling stops
}
```

Capabilities are opt-in interfaces detected on the concrete type:
`Focusable` (tab stops), `Container` (child management), `CursorReporter`
(the hardware-cursor/IME rule), `FocusScope` (modal focus traps).

Background work never touches the tree directly:

```go
ctx.Go(func(c context.Context) (any, error) {
    return db.ListTables(c) // c is cancelled when the component unmounts
})
// → delivered back as an addressed tui.TaskResult to HandleEvent.
```

## Testing without a PTY

`tui.TestBackend` is a deterministic in-memory terminal: inject events,
assert the cell grid (`String()`, `Snapshot()`), the hardware cursor
(`CursorPos()`), and the write discipline (`Flushes()` — an idle app makes
zero writes; one change makes one). Every widget contract test and the demo
gate in this repository runs on it.

## Design documents

The package is specified by ADRs 0001–0007 under `docs/tui/`: overview and
architecture, terminal backend and capability model, cell buffer and render
pipeline, component tree and layout, runtime and async, styling and theming,
and the standard widget set.
