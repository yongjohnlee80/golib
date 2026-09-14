# 4 — Events, focus and keys

## Routing: target, then bubble

Every key event goes to the **focused** node first, and walks UP the parent
chain until something consumes it — ending at your root controller. There is no
capture phase.

At each node on that walk the runtime does four things in order:

1. **Pointer policy.** If the event is pointer input and this node's effective
   policy is disabled, the node is skipped entirely and the walk continues.
2. **Resolve.** The node's action resolvers are tried, consumer entries first
   and then the widget's own defaults. The first one to claim the event wins.
3. **Semantic dispatch.** If a resolver produced an action, it goes to the
   node's `HandleAction`. Returning `true` consumes the event.
4. **Raw dispatch.** Otherwise — no resolver matched, or the action went
   unhandled — the node's `HandleEvent` receives the original event.

Most widgets implement only `HandleEvent` and never notice steps 1–3: a
component with no resolvers and no `HandleAction` behaves exactly as it did
before actions existed. That is why the old one-line summary was almost right.

The interpretation stage is **not a third transport lane**. Lane A and Lane B
still converge into one ordered dispatch (chapter 2); this is what happens to an
event *after* it arrives, at each node it is offered to.

Two consequences worth knowing:

- **Semantic beats raw on the same node.** A widget that reads `MouseEvent`
  directly and also resolves presses into actions gets the action, not the raw
  event — otherwise its own resolver could never fire.
- **An unconsumed primary press may become a gesture.** After the whole walk
  finds no taker, the runtime offers the press to its gesture recogniser, which
  is what makes `Button` work with no pointer code of its own. A gesture holds
  the pointer until release, and those captured events run the same four steps
  on the owner. See `Activatable`, `ActivationAvailability` and
  `WithGestureRecognizer`.

This explains most "why didn't my key work" confusion:

- The focused `TextInput` consumes printable keys — your global `'y'`
  shortcut won't fire while the user is typing. That is correct behavior;
  don't fight it.
- A focused `List` consumes `↑/↓/Enter`; an *unfocused* list never sees
  them — but your controller can forward events explicitly:
  `list.HandleEvent(ev)` from its own HandleEvent.
- In configurable widgets like `Editor`, you can explicitly unbind chords using
  `ActUnbound`. An unbound chord does not execute or get consumed by structural
  fallbacks; it returns `false` from `HandleEvent` and bubbles directly up to parent
  containers (chapter 3).
- Anything nobody consumed reaches the root — that is where `q`, `Ctrl-C`
  and app-wide shortcuts belong (chapter 2).

## Focus

Widgets opt in with `AcceptsFocus() bool`. `Tab`/`Shift-Tab` walk the ring
of visible focusable nodes. Programmatic focus:

```go
ctx.RequestFocus()            // focus MYSELF (the calling component)
ctx.FocusComponent(someChild) // focus another mounted component
```

`FocusComponent` is how controllers direct traffic: Enter on the menu →
focus the content table; a form opens → focus its first input; a viewer
closes → focus back to the list.

Two facts worth tattooing somewhere:

1. **Nothing is focused at startup** (unless a widget claims it, e.g.
   `Tabs` with `WithAutoFocus(true)`). Unfocused keys go straight to the
   root — your app may seem to "work" until a widget takes focus and starts
   consuming keys you thought were global.
2. **Focus requires a mounted, laid-out, visible node.** Requesting focus
   on something that hasn't been laid out yet silently does nothing — see
   the float gotcha in chapter 6.

## Key events

```go
e, ok := ev.(tui.KeyEvent)
if !ok || e.Kind != tui.KeyPress { return false } // ignore releases
switch e.Code {
case tui.KeyLeft, tui.KeyRight, tui.KeyUp, tui.KeyDown:
case tui.KeyEnter, tui.KeyEscape, tui.KeyTab:
case 'a', 'e', 'r':                       // plain runes
case 'c':
    if e.Mods&tui.ModCtrl != 0 { ... }    // Ctrl-chords
}
```

Remember: raw mode means `Ctrl-C` is `KeyEvent{Code: 'c', Mods: ModCtrl}`,
`Esc` is a key like any other, and the terminal's own copy-selection is
disabled while mouse reporting is on (offer OSC 52 copy instead — see
`Context.CopyToClipboard`).

## Modifier chords belong to the application

Ctrl-modified keys arrive with the **bare letter** in `Code`:
`Ctrl-l` is `Code: 'l', Mods: ModCtrl`. A widget that switches on `Code`
without checking `Mods` will read it as plain `l`, act on it, and consume
it — and your `Ctrl-hjkl` pane motion will never fire.

Widgets that do not bind a modifier chord bubble it so applications can use it
(e.g. `Ctrl-C`, `Ctrl-D`). Note that widgets with text editing capabilities
(like `Editor` and `TextInput`) intentionally consume their own supported
shortcuts (such as `Ctrl+A`, `Ctrl+Z`, `Ctrl+K`), but bubble unhandled chords.
Do the same in your custom components: bubble unhandled chords so application-level
shortcuts keep working:

```go
if k.Mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper) != 0 {
    return false // an unhandled application chord, bubble up
}
```

## Cursors, focus, and delegating wrappers

Two rules that look unrelated until they bite together:

1. **Only the FOCUSED component is asked for a cursor.** The runtime
   parks the terminal cursor by consulting the focused node's
   `CursorReporter`. A wrapper that holds focus and forwards keys to a
   child therefore *hides that child's cursor entirely*.
2. **Focus changes repaint; they do NOT re-layout.** Anything derived
   from focus must react to `FocusEvent` (which bubbles to the root), not
   be computed in `Layout` — or it will lag until something else happens
   to trigger a layout, which reads as "it only updates when I open a
   menu".

So: if your panel wraps a child that draws a cursor (an editor, a list),
**delegate focus to the child**:

```go
func (p *panel) AcceptsFocus() bool { return false }   // container, not a stop
func (p *panel) FocusTarget() tui.Component { return p.current }
// host: ctx.FocusComponent(panel.FocusTarget())
```

Unconsumed keys still bubble from the child up through the panel, so
panel-level bindings keep working. Only intercept *before* the child when
you must override its own binding (Enter on a tree node, say) — that is
the one reason to hold focus in the wrapper, and then the child's cursor
must be one that paints unfocused (`List` and `Tree` both do).

## The Two-Lane Event Funnel (Lane A vs Lane B)

Input events and program events travel down two independent lanes to reach the
central event loop:

```
┌────────────────────────────────────────────────────────────────────────┐
│                              External World                            │
│     (Keyboard, Mouse, Terminal TTY)       (Background Workers, Tasks)  │
└───────────────────┬────────────────────────────────────┬───────────────┘
                    │ Raw Input                          │ Program Closures / Events
                    ▼                                    ▼
        ┌──────────────────────┐             ┌──────────────────────┐
        │   Lane A: Input      │             │   Lane B: Program    │
        │   - Backend.Events() │             │   - ctx.Go TaskResult│
        │   - Unbuffered pump  │             │   - ctx.Post events  │
        │   - Drop-oldest rate │             │   - App.Update fn    │
        │   - Latest-wins size │             │   - Bus.Publish ev   │
        └───────────┬──────────┘             └───────────┬──────────┘
                    │                                    │
                    └───────────────┬────────────────────┘
                                    ▼
                     ┌──────────────────────────────┐
                     │    Single Event Loop (Run)   │
                     │    - Process Lane A Input    │
                     │    - Drain Lane B Program Q  │
                     │    - Render Dirty Frame      │
                     └──────────────────────────────┘
```

1. **Lane A (Input)**:
   - Receives hardware terminal events (keystrokes, mouse moves, terminal resize).
   - An internal intake pump reads from `Backend.Events()` into an unbuffered channel backed by a bounded queue (`inputQueueSize`).
   - High-volume input bursts (e.g. rapid mouse dragging) employ bounded drop-oldest protection, preventing slow frames from blocking terminal reads.
2. **Lane B (Program)**:
   - Carries application-driven signals: completed `ctx.Go` tasks, `Bus.Publish` events, and user-posted closures via `App.Update`.
   - By default, Lane B has an unlimited queue (`eventQueueLimit == 0`) and never drops events. If an optional ceiling is configured via `WithEventQueueLimit(n)`, exceeding the limit triggers an immediate fail-loud panic rather than silently dropping events.
3. **Queue Capacity Isolation vs Serialized Dispatch**:
   - Independent queue capacities prevent Lane B from exhausting Lane A's queue buffer (and vice versa).
   - However, the event loop itself runs on a **single goroutine**: when Lane B has events, `drainProgramLane` processes the captured batch. Draining a very large or slow batch can temporarily delay the next Lane A selection. Keep event handlers and closures fast!

## The pub/sub bus

Components and widgets communicate decoupled state changes through the application `Bus` without tight parent-child coupling:

- Shipped widget events:
  - `widget.ActivateEvent`: Enter pressed on a table or list row (`Owner`, `Index`).
  - `widget.SelectionChangedEvent`: Selection moved in a list or table (`Owner`, `Index`).
  - `widget.SubmitEvent`: Enter pressed in a `TextInput` (`Owner`, `Value`).
  - `widget.TabChangedEvent`: Active tab switched in `Tabs` (`Owner`, `Index`).
  - `widget.DismissEvent`: Modal float dismissed via Esc (`Owner`). (Backdrop mouse clicks are trapped and consumed by the modal layer, but do not dismiss it).

### 1. Scoped Subscriptions (`tui.SubscribeScoped`)

Always prefer `tui.SubscribeScoped(ctx, handler)` inside a component's `Init(ctx)`:

```go
func (c *myController) Init(ctx *tui.Context) {
    c.ctx = ctx
    ctx.Mount(c.table)

    // Automatically unsubscribed when c unmounts:
    tui.SubscribeScoped(ctx, func(ev widget.ActivateEvent) {
        if ev.Owner != c.table.List().NodeID() {
            return // Filter by owner!
        }
        c.openDetail(ev.Index)
    })
}
```

Bare subscriptions (`cancel := tui.Subscribe(bus, handler)`) return a cancellation function that must be called explicitly when tearing down to avoid leaks. `tui.SubscribeScoped` handles this automatically upon component unmount.

### 2. Thread-Safe Publishing

Any goroutine can safely publish to the bus:

```go
// From anywhere (loop or worker goroutine):
bus.Publish(MyCustomEvent{ID: 42, Status: "synced"})
```

Events are queued onto Lane B and delivered synchronously to subscribers on the event loop goroutine.

### 3. Defining Custom Application Events

Custom events are standard Go structs. Always include an `Owner` or source identifier when multiple instances could publish the same event:

```go
type OrderSelectedEvent struct {
    Owner   tui.NodeID
    OrderID string
}
```

Next: [async work and tasks](05-async-tasks.md).
