# 6 — Floats and modals

## Setup: OverlayHost at the root

Floats live on overlay layers above your UI. Wrap the root tree once:

```go
host := widget.NewOverlayHost(dock) // dock = your normal UI
r.tree = host                       // keep the host reference!
```

## Opening a modal detail panel

```go
func (m *Model) openFloat(title string, content tui.Component) *widget.Float {
    f := widget.NewFloat(
        widget.NewBox(content, widget.WithTitle(title)),
        widget.WithModal(true),         // focus trap + Esc dismisses
        widget.WithDimBackground(true),
    )
    m.host.Attach(f)
    f.Show()
    // Detach on dismissal, or every open stacks another hidden layer.
    tui.SubscribeScoped(m.ctx, func(ev widget.DismissEvent) {
        if ev.Owner == f.NodeID() {
            m.host.Stack.Remove(f)
        }
    })
    return f
}
```

`WithModal(true)` gives you the two behaviors you want without writing
them: focus is trapped inside the float (Tab cycles within), and `Esc`
hides it, restoring focus to whatever had it before.

A scrollable JSON/detail viewer is just a `BufferView` written after Show:

```go
view := widget.NewBufferView(widget.WithFollowTail(false))
m.openFloat("Release detail", view) // Show mounts it → writer is live
fmt.Fprintln(view.Writer(), prettyJSON)
```

## Gotcha: the focus seed races your data

Modal `Show` seeds focus into the first focusable widget of the float's
content — but that walk only finds nodes that are **laid out and visible**,
and at Show-time your float hasn't had its first layout pass. If the
content is a table whose rows also arrive async, the seed finds nothing,
the *layer* keeps focus, and Enter/arrows mysteriously do nothing.

Fix: claim focus when the content is actually ready — e.g. in the
`TaskResult` handler that delivers the rows:

```go
case filesLoaded:
    b.table.SetItems(v.files)
    b.ctx.FocusComponent(b.table.List()) // seed ran too early; do it now
    return true
```

## Two-level floats (list → viewer → back)

For a browser-in-a-float (file list, Enter opens contents, Esc goes back),
make the float content a small container that swaps children and handles
`Esc` itself while the inner view is open:

```go
case tui.KeyEvent:
    if e.Code == tui.KeyEscape && b.viewer != nil {
        b.closeViewer() // unmount viewer, focus back to the list
        return true     // CONSUMED — the modal layer never sees this Esc
    }
```

Bubbling does the work: Esc from the viewer hits your container first
(step back); Esc from the list bubbles past you to the modal layer (float
closes). One key, two meanings, zero special cases.

## Esc must reach the float

A modal `Float` dismisses on Esc — *if the key reaches it*. Focus is on
your content, so a child that consumes Esc leaves the float
undismissable. Consume Esc only when it CANCELS something (an insert
session, a selection, a pending prefix); otherwise return false and let
it bubble. The shipped `Editor` follows that rule; a hand-rolled widget
that returns `true` for every key it recognises will not.

## Title your choosers

`Float` titles are the only label a modal carries. A which-key style
chooser reused for confirmations ("delete this note?"), conflicts ("x
changed on disk") and menus must say which one it is — titling them all
"SPC — commands" makes two very different prompts indistinguishable to
the user, and indistinguishable to your tests, which then match the wrong
one and race ahead. (See chapter 8: this exact collision produced four
bad patches before a trace showed the cause.)

## Global keys while a modal is open

Unconsumed keys still bubble past the float to your root. If the root's
`q` means "quit the app", pressing `q` inside a detail view kills the whole
program — a genuinely nasty misfire. Track open floats and re-route:

```go
if e.Code == 'q' && len(m.floats) > 0 {
    m.floats[len(m.floats)-1].Hide() // close the top float instead
    return true
}
```

…and say so in the status bar (`Esc/q close`) while a float is open.

Next: [the pitfalls list](07-pitfalls.md).
