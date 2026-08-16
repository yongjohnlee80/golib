# 4 — Events, focus and keys

## Routing: target, then bubble

Every key event goes to the **focused** node first. If its `HandleEvent`
returns `false`, the event walks UP the parent chain until something returns
`true` — ending at your root controller. There is no capture phase.

This single rule explains most "why didn't my key work" confusion:

- The focused `TextInput` consumes printable keys — your global `'y'`
  shortcut won't fire while the user is typing. That is correct behavior;
  don't fight it.
- A focused `List` consumes `↑/↓/Enter`; an *unfocused* list never sees
  them — but your controller can forward events explicitly:
  `list.HandleEvent(ev)` from its own HandleEvent.
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

The shipped widgets bubble anything carrying Ctrl/Alt/Super/Hyper/Meta.
Do the same in your own:

```go
if k.Mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper) != 0 {
    return false // an application chord, not widget input
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

## The pub/sub bus

Widgets announce semantic events on a bus rather than requiring wrapping:
`widget.ActivateEvent` (Enter on a list row), `SelectionChangedEvent`,
`SubmitEvent` (text input Enter), `TabChangedEvent`, `DismissEvent` (float
closed). Subscribe in `Init` with automatic cleanup at unmount:

```go
tui.SubscribeScoped(ctx, func(ev widget.ActivateEvent) {
    if ev.Owner != myTable.List().NodeID() { // events carry their OWNER —
        return                               // always filter, or you will
    }                                        // react to other lists too
    openDetail(rows[ev.Index])
})
```

The `Owner != …NodeID()` check matters the moment your app has two lists.

Next: [async work](05-async-tasks.md).
