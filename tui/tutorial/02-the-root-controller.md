# 2 — The root controller

## The bug you will hit first

You build a widget tree, pass it straight to `NewApp`, run it — and see a
single line (a box heading, maybe) at the top of an otherwise empty screen.
Nothing scrolls, nothing fills, `Ctrl-C` doesn't quit.

```go
// WRONG: a bare widget as the app root.
tabs := widget.NewTabs(widget.WithTab("Items", list))
app := tui.NewApp(widget.NewBox(tabs), tui.WithBackend(backend)) // renders ~2 rows
```

Two things went wrong at once:

1. **Nothing claimed the screen.** Layout is negotiated: the parent offers
   `Constraints{MaxW, MaxH}` and the child answers with the Size it wants.
   Widgets are polite — a Box asks for roughly its content size. With no
   parent forcing "fill everything", your tree occupies its preferred couple
   of rows and the rest of the screen stays unpainted.
2. **Nobody owned the global keys.** Raw mode turns `Ctrl-C` into a
   keystroke; without a handler at the top of the bubble chain it is
   silently dropped.

## The pattern

The app root should be a small **controller** — not a widget. It mounts the
real tree, lays it out to FILL the constraints it receives, and handles the
keys that belong to the application rather than any one widget:

```go
type root struct {
    quit func()
    tree tui.Component
    ctx  *tui.Context
}

// Init runs once at mount. Keep the ctx; mount the tree.
func (r *root) Init(ctx *tui.Context) {
    r.ctx = ctx
    ctx.Mount(r.tree)
}

// Layout: lay the child out under OUR constraints and place it over the
// whole area. This is the "claim the screen" step.
func (r *root) Layout(c tui.Constraints) tui.Size {
    sz := r.ctx.LayoutChild(r.tree, c)
    r.ctx.PlaceChild(r.tree, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
    return sz
}

// Render: nothing of our own — children paint themselves.
func (r *root) Render(tui.Surface) {}

// HandleEvent: we are the END of the bubble chain — any key no focused
// widget consumed lands here. Global keys live here and nowhere else.
func (r *root) HandleEvent(ev tui.Event) bool {
    e, ok := ev.(tui.KeyEvent)
    if !ok || e.Kind != tui.KeyPress {
        return false
    }
    switch {
    case e.Code == 'q',
        e.Code == 'c' && e.Mods&tui.ModCtrl != 0:
        r.quit()
        return true
    }
    return false
}
```

Wrap your tree in `widget.NewOverlayHost(...)` at the root if you will ever
show floats (chapter 6) — it costs nothing and saves a refactor:

```go
dock := tui.NewDock()
dock.Pin(tui.DockBottom, statusBar)
dock.Add(body)
r.tree = widget.NewOverlayHost(dock)
```

## Component lifecycle in one paragraph

`Init(ctx)` runs once per mount — keep the ctx, mount children, subscribe,
start tasks. `Layout(c)` is the only place `LayoutChild`/`PlaceChild` are
legal. `Render(s)` paints only your own chrome — children render themselves
into their own placed rects. `HandleEvent` runs on the loop goroutine and
may mutate state freely; return `true` to consume the event, `false` to let
it bubble to your parent. A remount is a NEW mount: write `Init` so it is
re-entrant.

Next: [the widget set](03-widgets.md).
