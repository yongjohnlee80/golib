# golib/tui — Tutorial

`golib/tui` is a cell-buffer terminal UI framework: a component tree with
constraint-based layout, an event loop with focus routing, async tasks that
post back onto the loop, and a widget set (Box, Tabs, Table, List, BufferView,
StatusBar, Split, Dock, Float, text inputs).

These tutorials are written from building real applications with the package
(`db-tui`, `ddex-server`, `autodb`). Every chapter leads with the mistake that
cost an afternoon, then the pattern that fixes it. If you read nothing else,
read [chapter 2](02-the-root-controller.md) and the
[pitfalls](07-pitfalls.md); when something is on screen but your key does
nothing, go straight to [chapter 8](08-debugging.md).

| # | Chapter | You will learn |
|---|---------|----------------|
| 1 | [Getting started](01-getting-started.md) | Backend, App, run loop, headless fallback, TestBackend |
| 2 | [The root controller](02-the-root-controller.md) | Why a bare widget as app root renders one line — mount, fill-layout, global keys |
| 3 | [Widgets](03-widgets.md) | Box, Tabs, Table, List, BufferView, StatusBar, Split, Dock — composition patterns |
| 4 | [Events, focus and keys](04-events-focus-keys.md) | Target-then-bubble routing, Focusable, FocusComponent, quit keys in raw mode |
| 5 | [Async: tasks, ticks and writers](05-async-tasks.md) | ctx.Go + TaskResult, ctx.Every auto-refresh, the BufferView writer contract |
| 6 | [Floats and modals](06-floats-and-modals.md) | OverlayHost, modal Float, Esc dismissal, the focus-seed gotcha |
| 7 | [Pitfalls](07-pitfalls.md) | The complete list of ways this package has actually bitten people |
| 8 | [Debugging](08-debugging.md) | `WithTrace`: reading focus, mount and key-routing decisions when "the key does nothing" |

## The 60-second version

```go
package main

import (
    "context"

    "github.com/yongjohnlee80/golib/tui"
    "github.com/yongjohnlee80/golib/tui/term"
    "github.com/yongjohnlee80/golib/tui/widget"
)

// root is a CONTROLLER component: it mounts a tree, lays it out to fill
// the screen, and owns the quit keys. See chapter 2 for why you need it.
type root struct {
    quit func()
    tree tui.Component
    ctx  *tui.Context
}

func (r *root) Init(ctx *tui.Context) { r.ctx = ctx; ctx.Mount(r.tree) }

func (r *root) Layout(c tui.Constraints) tui.Size {
    sz := r.ctx.LayoutChild(r.tree, c)
    r.ctx.PlaceChild(r.tree, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
    return sz
}

func (r *root) Render(tui.Surface) {}

func (r *root) HandleEvent(ev tui.Event) bool {
    if e, ok := ev.(tui.KeyEvent); ok && e.Kind == tui.KeyPress &&
        (e.Code == 'q' || (e.Code == 'c' && e.Mods&tui.ModCtrl != 0)) {
        r.quit() // raw mode: Ctrl-C is a KEYSTROKE, not a signal
        return true
    }
    return false
}

func main() {
    backend, err := term.Open()
    if err != nil {
        panic(err) // no terminal — run your headless path instead
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    tree := widget.NewBox(widget.NewText("hello, tui"), widget.WithTitle("demo"))
    app := tui.NewApp(&root{quit: cancel, tree: tree}, tui.WithBackend(backend))
    if err := app.Run(ctx); err != nil {
        panic(err)
    }
}
```

Run it, see a full-screen bordered box, quit with `q` or `Ctrl-C`. Everything
else in these tutorials builds on this skeleton.
