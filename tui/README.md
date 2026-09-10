# tui

A minimal-dependency, retained-mode terminal UI framework: a grapheme-cluster cell buffer with diff-based flushing, a single-goroutine runtime (two-lane event loop, typed pub/sub bus, bounded task pool, demand-scheduled timers), a component tree with constraints-down/sizes-up layout, and a two-seam driver architecture that makes every application fully testable in CI without a PTY.

The core imports nothing outside the standard library and `golib`; the one terminal driver (`tui/term`) isolates `golang.org/x/term` and `golang.org/x/sys` to the terminal leaf.

```bash
go get github.com/yongjohnlee80/golib/tui
```

```go
import (
    "github.com/yongjohnlee80/golib/tui"        // runtime, tree, events, cells
    "github.com/yongjohnlee80/golib/tui/style"  // styles, tokens, themes
    "github.com/yongjohnlee80/golib/tui/term"   // ANSI terminal driver
    "github.com/yongjohnlee80/golib/tui/widget" // standard widget suite
)
```

---

## Architecture & Concurrency Model

```
┌────────────────────────────────────────────────────────────────────────┐
│                           External World                               │
│  (TTY Keyboard, Mouse, Terminal Signals)    (Background Worker Tasks)  │
└───────────────────┬────────────────────────────────────┬───────────────┘
                    │ Raw Input                          │ Goroutine Work
                    ▼                                    ▼
        ┌──────────────────────┐             ┌──────────────────────┐
        │   Backend.Events()   │             │   App.Post / Go      │
        │   (Terminal Intake)  │             │   Bus.Publish        │
        └───────────┬──────────┘             └───────────┬──────────┘
                    │                                    │
                    ▼                                    ▼
        ┌──────────────────────┐             ┌──────────────────────┐
        │   Lane A: Input      │             │   Lane B: Program    │
        │   (Unbuffered Pump,  │             │   (Unbounded Queue,  │
        │    Drop-Protected)   │             │    Never Dropped)    │
        └───────────┬──────────┘             └───────────┬──────────┘
                    │                                    │
                    └───────────────┬────────────────────┘
                                    ▼
                     ┌──────────────────────────────┐
                     │    Single Event Loop (Run)   │
                     │    - Route Key/Mouse Events  │
                     │    - Drain Program Queue     │
                     │    - Fire Timer Deadlines    │
                     │    - Coalesce Render Frames  │
                     └──────────────┬───────────────┘
                                    │
                                    ▼
                     ┌──────────────────────────────┐
                     │      Frame Pipeline Pass     │
                     │    1. Layout (if dirty)      │
                     │    2. Render to Buffer       │
                     │    3. Apply Hardware Cursor  │
                     │    4. Diff against Last Frame│
                     │    5. Backend.Flush(diff)    │
                     └──────────────────────────────┘
```

### 1. The Loop-Goroutine Invariant (Normative)
All component state — the tree, every component's fields, focus, layout rects, and the cell buffer — is owned exclusively by the loop goroutine. `Init`, `Layout`, `Render`, `HandleEvent`, bus handlers, and queued closures execute **only** there.

The only thread-safe operations legal from external background goroutines are:
- `App.Post(ev)` and `Context.Post(ev)` — enqueue an event onto the program lane.
- `App.Update(fn)` — enqueue a state mutation closure.
- `App.Go(...)` and `Context.Go(...)` — schedule background tasks on the bounded pool.
- `Bus.Publish(v)` — publish a typed domain event.

All of these enqueue asynchronously and return immediately. Components never need mutexes or atomic locks.

### 2. The Two-Seam Portability Contract
Portability is achieved through two clean interfaces:
- **`Backend`**: Abstracts the terminal device. It handles raw mode, alternate screen setup, live capability probing (Kitty keyboard, TrueColor, synchronized output), un-coalesced event streams, and atomic diff flushing.
- **`Surface`**: Abstracts the drawing canvas. Components receive a local, pre-clipped `Surface` with bounds checking, grapheme cluster writes, box filling, and style resolution.

---

## Core Container Guide

Layout in `golib/tui` is single-pass, Flutter-inspired **constraints down, sizes up**: parents provide constraints (`MinW <= W <= MaxW`, `MinH <= H <= MaxH`), children report their chosen `Size`, and parents position children.

| Container | Role | Sizing Strategy |
|-----------|------|-----------------|
| `Flex` | Linear multi-child layout (`Horizontal` or `Vertical`) | Fixed children measured first; remaining space distributed to weighted children via integer largest-remainder (zero gaps). Cross axis stretches tight. |
| `Dock` | Window chrome framing | Pinned children hug edges (`DockTop`, `DockBottom`, `DockLeft`, `DockRight`) in declaration order; `DockCenter` children fill remaining area. |
| `Stack` | Z-ordered layering & popups | Children receive loose constraints; placed via alignment (`AlignCenter`, `AlignTopRight`) or explicit offsets. Later children paint on top and win mouse hit-tests. |

---

## Component Protocol in Sixty Seconds

Every node in the UI tree implements `Component`, whose four methods execute solely on the loop goroutine:

```go
type Component interface {
    Init(ctx *tui.Context)             // called once at mount; retain ctx
    Layout(c tui.Constraints) tui.Size // constraints down, size up
    Render(s tui.Surface)              // paint own chrome; children paint themselves
    HandleEvent(ev tui.Event) bool     // true = consumed, bubbling stops
}
```

Optional capability interfaces are detected at runtime via type assertions on the outer widget:
- `Focusable`: Opts the component into Tab/Shift-Tab traversal (`AcceptsFocus() bool`).
- `Container`: Public child-management surface (`Add`, `Remove`, `Move`, `Children`).
- `FocusScope`: Traps focus navigation within a subtree (used by modals and popups).
- `CursorReporter`: Reports local insertion point for real OS IME candidate window placement.
- `CursorShaper`: Changes terminal hardware cursor shape (block, underline, bar).

---

## Deterministic Testing Without a PTY

`tui.TestBackend` provides a deterministic in-memory terminal simulator designed for headless CI environments:
- Inject key, mouse, and resize events via `tb.Inject(...)`.
- Assert cell grid text and ANSI attributes via `tb.String()` or `tb.Snapshot()`.
- Validate hardware cursor coordinates via `tb.CursorPos()`.
- Verify write discipline via `tb.Flushes()` — an idle app emits zero flushes; one state change emits exactly one.
- Catch wide-cell half-cell corruption and layout constraint violations automatically.

---

## Quick Start Example

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
    backend, err := term.Open() // raw mode, alt screen, live capability probe
    if err != nil {
        os.Exit(1)
    }
    defer backend.Stop()

    input := widget.NewTextInput(widget.WithPlaceholder("Enter query..."))
    box := widget.NewBox(input, widget.WithTitle("Search"))
    root := widget.NewOverlayHost(box)

    app := tui.NewApp(root, tui.WithBackend(backend))
    if err := app.Run(context.Background()); err != nil {
        os.Exit(1)
    }
}
```
