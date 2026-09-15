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
                     │    2. Commit (side effects)  │
                     │    3. Render to Buffer       │
                     │    4. Apply Hardware Cursor  │
                     │    5. Diff against Last Frame│
                     │    6. Backend.Flush(diff)    │
                     └──────────────────────────────┘
```

### The commit phase

Layout is **pure**: it measures and places, and it does not publish, mount,
unmount or store. That rule is what lets the runtime run a layout pass whenever
it needs one — twice in a frame, or not at all — without a component observing
the difference.

Some things genuinely depend on geometry and genuinely have to happen, though: a
split publishing the ratio it actually reached, a wrapper storing the size it
was clamped to, an overlay closing because the row it hangs off is no longer
laid out. Doing them inside `Layout` is what an earlier design did, and it made
the purity rule a comment rather than a rule. So the frame carries one ordered
phase between the two pure ones:

```
… → layout (pure) → COMMIT → render (pure) → present
```

`Context.AfterLayout(key, fn)` registers a callback from inside `Layout` — and
only from inside `Layout`, because geometry is not final anywhere else.
Registrations are keyed by `(owner, CommitKey)`, so a component that lays out
twice in one frame replaces its own pending record and keeps its queue position
rather than committing twice. Records run FIFO; one whose owner has been
unmounted by an earlier callback is discarded unrun. A callback may legitimately
dirty layout, so layout and commit alternate until they settle, bounded at eight
passes — exhaustion is a fatal, because silently rendering the eighth attempt
would hide a feedback loop forever.

### Interpretation: what happens to an event at each node

The two lanes above are **transport**. Once they converge, an event is offered
to one node at a time along the bounded bubble path, and at each node the
runtime performs four steps in order:

1. **Pointer policy** — pointer input to a node whose effective policy is
   disabled skips that node entirely, semantic and raw alike.
2. **Resolve** — the node's action resolvers are tried, consumer entries before
   the widget's own defaults; first match wins.
3. **Semantic dispatch** — a resolved action is offered in two stages:
   `HandleAction` if the node implements it, then — for an `ActivateAction`
   nobody handled — `Activatable.Activate`. Either returning `true` consumes it.
4. **Raw dispatch** — otherwise the original event goes to `HandleEvent`.

The `Activatable` stage is not a detail: **`widget.Button` implements no
`HandleAction` at all.** Its keyboard resolver produces an `ActivateAction`, no
action handler takes it, and the fallback reaches `Activate`. That is how one
control serves keyboard, pointer and programmatic activation without
implementing any of the three.

This is **not a third transport lane**. Nothing about Lane A or Lane B changes;
this is what an already-delivered event meets on arrival. A component with no
resolvers, no `HandleAction` and no `Activatable` is byte-for-byte unaffected,
which is why most widgets only ever implement `HandleEvent`.

After the bounded walk, an unconsumed primary press on an opted-in control is
offered to the **gesture recogniser**, which may take the pointer and later
produce an `ActivateAction`.

What a **held capture** does with an event depends on who owns it:

- **`CaptureRaw`**, taken by a component through `Context`, runs the same four
  steps on the owner alone — no hit-test, no parent walk — so a drag begun
  semantically continues semantically.
- **`CaptureGesture`**, taken by the runtime on a component's behalf, goes to
  the recogniser **only**. It never re-enters the node pipeline; any action the
  recogniser produces is then dispatched to the owner.

"No capture phase" refers to **event routing** — there is no DOM-style downward
phase in which ancestors preview an event before its target. That is unrelated
to **pointer capture**, which is about which node keeps receiving pointer events
once a gesture has begun.

See `tutorial/04-events-focus-keys.md` for the walked-through version.

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

| Container | Role                                                   | Sizing Strategy                                                                                                                                                     |
| --------- | ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Flex`    | Linear multi-child layout (`Horizontal` or `Vertical`) | Fixed children measured first; remaining space distributed to weighted children via integer largest-remainder (zero gaps). Cross axis stretches tight.              |
| `Dock`    | Window chrome framing                                  | Pinned children hug edges (`DockTop`, `DockBottom`, `DockLeft`, `DockRight`) in declaration order; `DockCenter` children fill remaining area.                       |
| `Stack`   | Z-ordered layering & popups                            | Children receive loose constraints; placed via alignment (`AlignCenter`, `AlignTopRight`) or explicit offsets. Later children paint on top and win mouse hit-tests. |

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
