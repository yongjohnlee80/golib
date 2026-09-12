# tui/widget

The standard widget suite for `golib/tui`: a complete set of fourteen production-grade TUI components designed to build complex, terminal-native applications (such as `lazygit`-, `sqlit`-, and `neovim`-shaped tools) out of the box with zero custom widget plumbing.

```go
import "github.com/yongjohnlee80/golib/tui/widget"
```

Dependency footprint: standard library + `golib/tui` + `golib/tui/style` only.

---

## Complete Widget Inventory

| Widget        | Category        | Focusable      | Primary Emitted Events (Bus)                           |
| ------------- | --------------- | -------------- | ------------------------------------------------------ |
| `TextInput`   | Form Input      | yes            | `SubmitEvent`, `ChangeEvent`                           |
| `TextArea`    | Multi-line Text | yes            | `ChangeEvent`                                          |
| `Select[T]`   | Form Input      | yes            | `SelectionChangedEvent`, `OpenedEvent`, `ClosedEvent`  |
| `List[T]`     | Collection      | yes            | `SelectionChangedEvent`, `ActivateEvent`               |
| `Table[T]`    | Collection      | yes (via List) | `SelectionChangedEvent`, `ActivateEvent`               |
| `Tree`        | Navigation      | yes            | `ExpandRequestEvent`, `CollapseEvent`, `ActivateEvent` |
| `Editor`      | Modal Text      | yes            | `ModeChangedEvent`, `YankEvent`                        |
| `BufferView`  | Stream / Pager  | yes (scroll)   | `FollowTailChangedEvent`                               |
| `Tabs`        | Navigation      | yes (bar)      | `TabChangedEvent`                                      |
| `Split`       | Container       | no (panes are) | `SplitResizedEvent`, `SplitZoomEvent`                  |
| `Float`       | Overlay / Modal | children       | `DismissEvent`                                         |
| `StatusBar`   | Chrome          | no             | —                                                      |
| `ProgressBar` | Feedback        | no             | —                                                      |
| `Text`        | Static Display  | no             | —                                                      |

Every bus event carries `Owner tui.NodeID` as its first field so subscribers can filter by source. Publication is enqueue-only onto the application loop.

---

## Architectural Principles

### 1. Loop-Goroutine Ownership & Concurrency Boundaries

All widgets are retained, mutable `tui.Component` implementations living strictly on the loop goroutine. Every widget method is loop-goroutine-only.

**The One Concurrent Exception:** `BufferView.Writer()` returns an `io.Writer` handle that is safe to call from any background goroutine (e.g. streaming `exec.Cmd.Stdout`). It utilizes a bounded semaphore queue to prevent unbounded memory allocation when the loop lags, guarantees ordered delivery, and returns `widget.ErrClosed` after unmount. The `BufferView` itself remains loop-owned and deliberately does not implement `io.Writer`.

**Async Tasks:** Background workloads schedule work via `App.Go` addressing the widget's `NodeID`, and results arrive safely on the loop as a typed `tui.TaskResult`.

### 2. Composable Panel Chrome (`Box`)

Widgets remain chrome-free. Any visual panel is wrapped in a `Box`:

```
┌─ [Title] ────────────────────────────────────────────────────────┐
│                                                                  │
│                     Wrapped Child Widget                         │
│                    (List, Table, Editor)                         │
│                                                                  │
└─────────────────────────────────────────────── [Status / Hints] ─┘
```

`Box` embeds the title in the top border and status hints in the bottom border—consuming **zero extra vertical lines**. When the Box or any child holds focus, the border automatically transitions to `style.TokenBorderFocused` (the `lazygit` active-panel highlight for free).

```go
panel := widget.NewBox(list,
    widget.WithTitle("Tables"),
    widget.WithStatus("enter: open | /: filter"))
```

### 3. Base Embedding Contract

Every widget embeds `widget.Base` by value, gaining method promotion (`Context`, `NodeID`, `MarkDirty`, `RequestLayout`) without indirection. Go embedding is not virtual dispatch: `Base` never calls template methods, and capability interfaces (`tui.Focusable`, `tui.Container`, `tui.CursorReporter`) are asserted on the outer type.

### 4. Stack Overlays & Floating Windows

Wrap your root layout in an `OverlayHost` once:

```go
root := widget.NewOverlayHost(mainLayout)
```

```
┌──────────────────────────────────────────────────────────────────┐
│ [OverlayHost] Layer Stack                                        │
│                                                                  │
│  Layer 3: Ephemeral Popups (Select dropdown options)             │
│           ▲                                                      │
│  Layer 2: Floating Dialogs (Float modal windows)                 │
│           ▲                                                      │
│  Layer 1: Scrim / Dimmer ("░" backdrop dimming)                  │
│           ▲                                                      │
│  Layer 0: Base Application Tree (Flex, Split, Panels)            │
└──────────────────────────────────────────────────────────────────┘
```

- `Select` automatically projects its open options list onto the overlay host via an internal bus handshake.
- `Float` attaches via `host.Attach(float)` and provides toggleable modals (`Show()` / `Hide()`), focus trapping, background scrimming, and Esc-dismissal.

---

## Component Guide & Usage Patterns

### Form Controls

#### `TextInput`

Single-line grapheme-addressed editor with selection, horizontal scrolling, placeholder, password masking, validation hooks, and real hardware cursor positioning for IME composition.

```go
input := widget.NewTextInput(
    widget.WithPlaceholder("Enter database URL..."),
    widget.WithValidate(func(val string) error {
        if !strings.HasPrefix(val, "postgres://") {
            return errors.New("must be a postgres URL")
        }
        return nil
    }),
    // Synchronous hook advances focus immediately without bus queue lag:
    widget.WithOnSubmit(func(val string) {
        nextField.RequestFocus()
    }),
)
```

#### `TextArea`

Multi-line text editor with soft wrapping (`WrapSoft`) or horizontal scrolling (`WrapNone`), grapheme addressing, selection, and clipboard paste safety.

```go
area := widget.NewTextArea(
    widget.WithWrap(widget.WrapSoft),
    widget.WithTextAreaStyles(widget.TextInputStyles{
        Text: style.New().Foreground(style.TokenForeground),
    }),
)
```

#### `Select[T]`

Dropdown selector with closed-state rendering, filter-as-you-type in the open popup, and asynchronous option loading:

```go
sel := widget.NewSelect[string](
    widget.WithOptions([]widget.SelectItem[string]{
        {Label: "PostgreSQL", Value: "postgres"},
        {Label: "SQLite", Value: "sqlite"},
        {Label: "MySQL", Value: "mysql"},
    }),
    widget.WithFilter(true), // filter-as-you-type
)
```

### Collections & Explorers

#### `List[T]`

High-performance virtualized list rendering through the `ListSource[T]` seam. Only visible rows are rendered:

```go
list := widget.NewList(
    widget.WithItems(users, func(u User) string {
        return fmt.Sprintf("%-20s %s", u.Name, u.Email)
    }),
    widget.WithEmptyText("No users found"),
)
```

#### `Table[T]`

Column-structured list with fixed and flex column widths, automatic remainder distribution, and header rendering:

```go
cols := []widget.TableColumn[Process]{
    {Title: "PID", Width: 8, Cell: func(p Process) string { return strconv.Itoa(p.PID) }},
    {Title: "USER", Width: 12, Cell: func(p Process) string { return p.User }},
    {Title: "COMMAND", Width: 0, Cell: func(p Process) string { return p.Cmd }}, // 0 = flex column
}

table := widget.NewTable(cols)
table.SetItems(processList)
```

#### `Tree`

Hierarchical tree explorer supporting lazy-loaded asynchronous subtrees via generation tokens (`ExpandRequestEvent`), trailing badges, and structural cursor reconciliation:

```go
tree := widget.NewTree(
    widget.NewTreeNode("src", "src/",
        widget.WithBadge("dir"),
    ),
)

// Handle async child expansion:
tui.Subscribe(bus, func(ev widget.ExpandRequestEvent) {
    app.Go(tree.NodeID(), func(ctx context.Context) (any, error) {
        children := fetchSubdir(ev.Node.ID())
        return children, nil
    })
})
```

### Editors & Streaming Viewers

#### `Editor`

Embedded modal Vim-like editor featuring Normal, Insert, and Visual modes, a data-driven keymap, single unnamed register, bounded undo ring (64 snapshots), and escape chord support ("jk"):

```go
editor := widget.NewEditor(
    widget.WithInitialText("package main\n\nfunc main() {\n}\n"),
    widget.WithEscapeChord("jk"), // jk in Insert mode returns to Normal mode
)

// Unbound Normal-mode keys (like Space) bubble up, enabling app-level leader menus:
```

#### `BufferView`

High-throughput append-oriented log pager with ring-bounded scrollback, ANSI SGR color interpretation, follow-tail auto-scrolling, and thread-safe streaming writer:

```go
logs := widget.NewBufferView(
    widget.WithMaxLines(10000),
    widget.WithFollowTail(true),
)

// Pipe command output concurrently from any goroutine:
cmd := exec.CommandContext(ctx, "git", "log", "--color=always")
cmd.Stdout = logs.Writer()
go cmd.Run()
```

### Layout & Containers

#### `Split`

Interactive two-pane divider (horizontal or vertical) with Alt+arrow keyboard resize, mouse drag, deterministic integer division, and zoom toggle (`SplitZoomEvent`):

```go
split := widget.NewSplit(widget.Horizontal, leftPane, rightPane,
    widget.WithRatio(0.3),
    widget.WithMinSizes(15, 30),
)

// Zoom a pane to full screen:
split.Zoom(widget.PaneA)
split.Unzoom()
```

#### `Float`

Floating window for modal dialogs and alert popups, supporting focus trapping, background scrimming (`░`), and percentage-based sizing:

```go
modal := widget.NewFloat(
    widget.NewBox(confirmForm, widget.WithTitle("Confirm Delete")),
    widget.WithModal(true),
    widget.WithDimBackground(true),
    widget.WithSizeFraction(50, 40), // 50% width, 40% height of terminal
)
host.Attach(modal)

// Display modal:
modal.Show() // Esc dismisses and restores previous focus
```

### Application Chrome & Indicators

#### `StatusBar`

One-line docked footer bar with left, center, and right segments. Truncation priority is center-first, then left, preserving critical right-hand keybinding hints:

```go
bar := widget.NewStatusBar()
bar.SetLeft("NORMAL")
bar.SetCenter("main.go")
bar.SetRight("utf-8 | 12:45 | ?: help")
```

#### `ProgressBar`

Determinate progress bar (with sub-cell 1/8th block precision), sweeping indeterminate block, or single-cell spinner. Employs zero-wakeup idle timers:

```go
progress := widget.NewProgressBar()
progress.SetProgress(0.75) // determinate 75%

// Or switch to indeterminate spinner:
progress.SetIndeterminate()
```

---

## Real-World Composition Example

Here is how these widgets combine to build a complete master-detail development workspace:

```go
func NewDevWorkspace(app *tui.App) tui.Component {
    // 1. File tree navigation
    tree := widget.NewTree(widget.NewTreeNode("root", "my-project"))
    leftPanel := widget.NewBox(tree, widget.WithTitle("Files"))

    // 2. Code editor and build output split
    editor := widget.NewEditor(widget.WithEscapeChord("jk"))
    editorBox := widget.NewBox(editor, widget.WithTitle("Editor"))

    logs := widget.NewBufferView(widget.WithFollowTail(true))
    logsBox := widget.NewBox(logs, widget.WithTitle("Build Output"))

    rightSplit := widget.NewSplit(widget.Vertical, editorBox, logsBox,
        widget.WithRatio(0.7))

    // 3. Main master-detail split
    mainSplit := widget.NewSplit(widget.Horizontal, leftPanel, rightSplit,
        widget.WithRatio(0.25))

    // 4. Status bar footer
    status := widget.NewStatusBar()
    status.SetLeft("Git: main")
    status.SetRight("Ctrl+Q: Quit | Space: Leader")

    body := tui.NewFlex(tui.FlexVertical).
        Add(mainSplit, tui.FlexItem{Flex: 1}).
        Add(status, tui.FlexItem{Fixed: 1})

    // 5. Wrap root in OverlayHost to host modals and popups
    return widget.NewOverlayHost(body)
}
```
