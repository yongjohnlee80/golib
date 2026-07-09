# tui/widget

The standard widget set v1 for golib/tui (ADR-0007): everything needed to
build sqlit- and lazygit-shaped applications out of the box, with no custom
widgets. Imports: standard library + `tui` + `tui/style` only.

```go
import "github.com/yongjohnlee80/golib/tui/widget"
```

## Inventory

| Widget | Kind | Focusable | Emits (Bus) |
|--------|------|-----------|-------------|
| `TextInput` | input | yes | `SubmitEvent`, `ChangeEvent` |
| `TextArea` | input | yes | `ChangeEvent` |
| `Select[T]` | input | yes | `SelectionChangedEvent`, `OpenedEvent`/`ClosedEvent` |
| `List[T]` | input | yes | `SelectionChangedEvent`, `ActivateEvent` |
| `BufferView` | display/input | yes (scroll) | `FollowTailChangedEvent` |
| `Tabs` | chrome | yes | `TabChangedEvent` |
| `Split` | container | no (children are) | `SplitResizedEvent` |
| `Float` | container | children | `DismissEvent` |
| `StatusBar` | chrome | no | — |
| `ProgressBar` | chrome | no | — |
| `Text` | display | no | — |

Every event carries `Owner tui.NodeID` first, so subscribers filter by
source; publication is enqueue-only onto the App loop (ADR-0005).

## Box — the titled window

`Box` is the one chrome implementation: border, in-border title, in-border
status line, padding, and focus visuals, all configuration:

```go
widget.NewBox(list,
    widget.WithTitle("Tables"),
    widget.WithStatus("enter: open"))
```

Title and status render inside the border rows — zero extra height — and
truncate with an ellipsis. When the Box or any descendant holds focus, the
border switches from `style.TokenBorder` to `style.TokenBorderFocused`
(re-themable; the lazygit active-panel highlight for free). Panels are
composed by wrapping — widgets themselves stay chrome-free.

## Base — the embedding contract

Every widget embeds `widget.Base` (Context plumbing, `MarkDirty`,
`RequestLayout`, default no-op `HandleEvent`). Go embedding is not
inheritance: `Base` never calls overridable methods, capability interfaces
are asserted on the outer type, and a widget overriding `Init` must chain
`b.Base.Init(ctx)` first. Third-party widgets embed it the same way.

## Overlays

Wrap the app root once:

```go
root := widget.NewOverlayHost(body)
```

`Select` mounts its open option list there automatically (focus-trapped;
Esc restores the previous focus; clicks outside close; filter-as-you-type
with `WithFilter`). `Float` attaches explicitly and toggles:

```go
dialog := widget.NewFloat(
    widget.NewBox(input, widget.WithTitle("Commit message")),
    widget.WithModal(true), widget.WithDimBackground(true))
host.Attach(dialog)
// …later, from a handler:
dialog.Show() // trap + scrim; Esc → Hide() → DismissEvent, focus restored
```

## Async (the ADR-0005 patterns)

Widgets never spawn raw goroutines. Options and rows arrive as addressed
`TaskResult`s:

```go
ctx.App().Go(sel.NodeID(), func(c context.Context) (any, error) {
    return loadItems(c) // []widget.SelectItem[T] — Select installs it itself
})
```

`BufferView` streams subprocesses through its concurrent Writer handle — the
package's ONE sanctioned any-goroutine surface (the widget value itself stays
loop-owned and does not implement `io.Writer`):

```go
view := widget.NewBufferView()          // follow-tail on
cmd := exec.CommandContext(ctx.Ctx(), "git", "log", "--color=always")
cmd.Stdout = view.Writer()              // SGR colors → styled cells
app.Go(view.NodeID(), func(c context.Context) (any, error) { return nil, cmd.Run() })
```

The handle blocks when pending bytes exceed the budget (never unbounded
buffering), delivers in write order, and returns `widget.ErrClosed` after the
view unmounts.

## List and the `ListSource` seam

`List[T]` renders through `ListSource[T] { Len() int; Item(i int) T }` —
`Item(i)` is called only for viewport rows and `Len()` once per render pass.
The provided v1 source is the in-memory adapter:

```go
list := widget.NewList(widget.WithItems(rows, Row.String)) // SliceSource sugar
```

**Known limit (ADR-0007 N3):** `SliceSource` holds every item in memory;
100k+-row datasets must page at the data layer until a windowed source ships.
That follow-up is a new `ListSource` implementation behind this same seam —
no `List` API change.

## Notes

- `TextArea` has no submit key (Enter = newline; ADR-0007 Q5) — bind
  submission at the app level. Its `[]string` buffer targets
  commit-message/query-editor scale.
- `TextInput`/`TextArea` implement the real-cursor IME rule: the hardware
  cursor parks at the insertion point while focused.
- `ProgressBar` holds a tick registration only while animating — a
  determinate, idle bar costs zero wakeups (the idle app writes zero bytes).
- `Split` resizes by Alt+arrows (when a pane is focused) and by dragging the
  divider; `WithMinSizes` clamps.
- Widget state is loop-goroutine-owned; mutate it from handlers or via
  `App.Update`, never directly from other goroutines.
