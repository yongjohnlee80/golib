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
