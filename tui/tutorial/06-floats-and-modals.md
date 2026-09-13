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

## Sizing: fixed, or a fraction of the screen

A detail popover has a natural size — let the content decide. A WORKING
surface does not: a history browser, a log viewer, a picker over many
rows wants "most of the screen", and a fixed column count overflows a
narrow terminal, wastes a wide one, and ignores a resize.

```go
widget.NewFloat(body,
    widget.WithModal(true),
    widget.WithSizeFraction(90, 90),   // 90% of the available area
)
```

Per axis, 1..100; 0 leaves that axis natural (`WithSizeFraction(0, 60)`
is a full-width strip 60% tall). The fraction covers the whole box,
border included, so stacking 90% under 80% reads as a detail ON a list.

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

---

## The Overlay Architecture & Render Pipeline

To use modals reliably, understand how `OverlayHost`, `Float`, and the render pipeline cooperate:

### 1. The Overlay Layer Stack

```
┌────────────────────────────────────────────────────────────┐
│ Terminal Screen (Back Buffer)                              │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Base UI (Dock, Split, Tabs, BufferView)              │  │
│  └──────────────────────────────────────────────────────┘  │
│                             ▲                              │
│                             │ Composited Beneath           │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Scrim Canvas (Dimmed background cells: ░░░░░░░░░░)   │  │
│  │ ┌──────────────────────────────────────────────────┐ │  │
│  │ │ Modal Float (Anchored Center / AtRect)           │ │  │
│  │ │ - FocusScope Trap (Tab cycles inside)            │ │  │
│  │ │ - Child Box & Content (Input fields, Buttons)    │ │  │
│  │ └──────────────────────────────────────────────────┘ │  │
│  └──────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────┘
```

1. **`OverlayHost`**: Mounts your base application tree as its primary child and maintains a `LayerStack`.
2. **`floatLayer`**: When you attach a `Float` and call `Show()`, `OverlayHost` mounts an internal `floatLayer` component.
   - **Focus Scope**: If `WithModal(true)` is enabled, `floatLayer.TrapsFocus()` returns `true`. The focus ring is sealed—`Tab` and `Shift-Tab` will never jump outside the modal.
   - **Scrim Rendering**: When `WithDimBackground(true)` is set, `floatLayer.Render()` iterates over background cells beneath the float rect, applying dim attributes or stippling patterns.
   - **Anchoring**: `Float` positions its child using anchors:
     - `widget.Center`: centered horizontally and vertically.
     - `widget.TopRight`, `widget.BottomLeft`, etc.: pinned to viewport edges.
     - `widget.AtRect(r)`: pinned relative to another widget's coordinates (for dropdowns, autocomplete menus, and context tooltips).

### 2. Double-Buffering & The One-Write Render Pass

`golib/tui` uses a retained-mode, grapheme-cluster double-buffering pipeline:

1. **Local Surface Drawing**: When a component's `Render(s tui.Surface)` runs, `s` is a sub-surface clipped and translated to the component's placed rectangle. Writing outside bounds is safely clipped.
2. **Grapheme Clusters**: The cell buffer stores full Unicode grapheme clusters (including multi-byte emoji and zero-width joiners) and caches display column widths (1 or 2 cells).
3. **Dirty Coalescing**: Calling `ctx.MarkDirty()` marks the layout branch as dirty. The runtime throttles renders to `WithMinFrameInterval` (~60fps), coalescing rapid state mutations into a single draw pass.
4. **Cell Diff & One-Write Flush**: Before outputting bytes to the terminal, the engine diffs the newly rendered frame against the previous frame. **Only modified cells are sent to the terminal driver**, and `Backend.Flush` writes the entire ANSI update sequence in a **single atomic I/O write**. This completely eliminates terminal flicker.

### 3. Overlay Best Practices Checklist

- [ ] **Always wrap the root in `OverlayHost`** during application bootstrap, even if no floats are shown initially.
- [ ] **Always subscribe to `DismissEvent`** to remove ephemeral floats from `host.Stack`, preventing unneeded hidden layers from accumulating in memory.
- [ ] **Use `WithSizeFraction`** for document and log viewers so they scale gracefully with terminal resizing.
- [ ] **Re-seed focus in your data callback** if the modal's contents load asynchronously.
- [ ] **Do not consume `Esc`** unless actively canceling a local action (e.g. exiting insert mode); let unhandled `Esc` bubble to the float layer for clean dismissal.

Next: [the pitfalls list](07-pitfalls.md).
