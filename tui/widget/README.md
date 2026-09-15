# tui/widget

The standard widget suite for `golib/tui`: the nineteen production-grade TUI components inventoried below, designed to build complex, terminal-native applications (such as `lazygit`-, `sqlit`-, and `neovim`-shaped tools) out of the box with zero custom widget plumbing.

```go
import "github.com/yongjohnlee80/golib/tui/widget"
```

Dependency footprint: standard library + `golib/tui` + `golib/tui/style` only.

---

## Complete Widget Inventory

| Widget        | Category        | Focusable      | Primary Emitted Events (Bus)                           |
| ------------- | --------------- | -------------- | ------------------------------------------------------ |
| `Button`      | Control         | when enabled   | `tui.ControlActivatedEvent`                            |
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
| `Modal`       | Dialog          | trap owner     | `OverlayDismissedEvent`                                |
| `Menu`        | Menu / Command  | yes            | `MenuActivatedEvent`, `MenuSelectionChangedEvent`      |
| `MenuBar`     | Menu / Chrome   | no (menu is)   | — (delegates to `Menu`)                                |
| `MenuItem`    | Control         | when enabled   | `tui.ControlActivatedEvent`                            |
| `StatusBar`   | Chrome          | no             | —                                                      |
| `ProgressBar` | Feedback        | no             | —                                                      |
| `Text`        | Static Display  | no             | —                                                      |

Every bus event carries `Owner tui.NodeID` as its first field so subscribers can filter by source. Publication is enqueue-only onto the application loop.

---

### Button

The smallest complete interactive widget, and the demonstration that the runtime
carries the interaction burden rather than each widget. It has no pointer
arithmetic, no press/release bookkeeping, no hit-testing and no capture
handling: it implements `tui.Activatable` — `Activate` and `SetArmed` — and the
runtime supplies keyboard, pointer, drag-out-and-back and programmatic
activation identically across every backend.

- A **callback-free Button is activatable, not inert**: activating it succeeds
  and publishes `tui.ControlActivatedEvent`, it simply runs no callback.
- A **disabled Button leaves the focus ring**, and focus is repaired
  synchronously if it held focus when disabled.
- **Pointer policy** is per-widget and inherited; `WithPointerPolicy` may be
  chained onto the constructor before mount and is applied at `Init`.
- **`tui.ActivationAvailability`** is an optional runtime hint that stops the
  gesture recogniser starting a gesture on a control that cannot be activated.
  It never replaces the authorization check, which lives in `Activate` alone.

Styling is an association: a Button holds a `ButtonStyle` it does not own, so one
immutable value can safely dress many Buttons, and its style tokens let the
App's theme decide the rendered colours.

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

- `Select` projects its open options list onto the **nearest enclosing** overlay host, resolved from the component tree — so two hosts in one application each serve only the widgets inside them.
- `Float` attaches via `host.Attach(float)` (and detaches via `host.Detach(float)`) and provides toggleable windows (`Show()` / `Hide()`), focus trapping, background scrimming, and Esc-dismissal. It is the **lower-level** primitive: a positioned, trapping layer around arbitrary content.
- `Modal` is the **composed dialog** on the same stack — a card with a title, body and role-carrying buttons, plus the lifecycle a dialog needs. Reach for `Modal` when you want a dialog; reach for `Float` when you want a floating layer and intend to supply the behaviour yourself. See [`Modal`](#modal) below.

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

#### `Menu`, `MenuBar` and `MenuItem`

A `Menu` owns a **model** and paints it. Rows are `MenuItemModel` *values*, not
mounted children:

```go
// The dispatch table is the application's; the Menu only needs a function.
commands := map[tui.ActionID]func() bool{
    "file.new": editor.NewFile,
    "app.quit": app.Quit,
}

menu := widget.NewMenu(
    widget.WithActionExecutor(func(inv tui.ActionInvocation) bool {
        run, ok := commands[inv.Action.ActionID()]
        if !ok {
            return false // unhandled: the menu stays open
        }
        return run()
    }))

if err := menu.SetModel([]widget.MenuItemModel{
    widget.NewSubmenu("file", "File", []widget.MenuItemModel{
        widget.NewCommand("new", "New", NewFileAction{}),
        widget.NewSeparator("s1"),
        widget.NewCheck("wrap", "Wrap lines", ToggleWrapAction{}),
    }),
    widget.NewCommand("quit", "Quit", QuitAction{}),
}); err != nil {
    return err
}

// Optional: lay the root level along an edge. The bar wraps a Menu that
// already exists.
bar := widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))

// A Menu's levels are anchored overlay layers, so whichever of the two is
// mounted must sit inside an OverlayHost.
root := widget.NewOverlayHost(bar)
```

This arrangement is compile-checked as `ExampleNewMenu` in
`tui/widget/example_menu_test.go`, so it cannot drift from the public API.

**The nearest enclosing `OverlayHost` holds the levels**, and opening one is a
*synchronous transaction* against that one host: resolve it, check the anchor,
mount, and only then record the level. `Open` returns the host's error — with no
host above it, an error matching `ErrAnchorUnusable` rather than silence. Two
sibling hosts each serve their own menus, and a host nested inside another serves
the menus inside it.

**Closing is synchronous too**, whoever causes it. A level *is* a mounted popup,
so the Menu learns it is gone from that popup's own unmount rather than from a
bus event delivered later — an application can `CloseAnchored` and reopen the row
in the same update and get a popup, where a Menu reconciling on the program lane
would still be counting the level it had just lost and would treat the reopen as
a duplicate. Three things close a level and one mechanism covers all three: a row
that stops being a visible, enabled submenu; the host's anchor-loss commit when
the row is no longer laid out; and an explicit `host.CloseAnchored`.
`OverlayDismissedEvent` is still published, for observers rather than for this.
Unmounting the Menu closes every level it owns — the levels are the host's
children, so nothing else would.

**A clipped row is not a row**, and that is one set of rows rather than three. A
row outside the rect the Menu's parent allowed declares no anchor region, gets no
hit rectangle, and is not painted — so it cannot be clicked, cannot be opened,
and never reaches a consumer's `RowRenderer`.

**The renderer seam.** `RowRenderer` is handed a `RowView` and a `RowState`
whose four flags — `Selected`, `Armed`, `Focused`, `Open` — are *independent*.
That is what lets a custom painter distinguish a selected row that owns the open
submenu from one that does not, or draw the selection faintly while the menu has
lost focus. Disabled is deliberately absent: it is derivable from `RowView`
(`Enabled`, and the kind for a separator), and a flag duplicating a value in the
same call is a second copy of one fact.

**The model.** `ItemKind` is deliberately **closed** — Command, Submenu,
Separator, Check, Radio. A new kind would need a switch edit or a polymorphic
row seam, so the package ships the seam instead: `RowRenderer` for appearance,
`Action` for behaviour. A zero `MenuItemModel` is inert (disabled, invisible), so
a half-filled row shows nothing rather than appearing as a blank clickable entry.

`ItemID`s are unique **recursively**, across every level — every operation names
a row by ID alone. `SetModel` returns an error matching both
`ErrInvalidMenuModel` and a specific sentinel, and on error *nothing* changes.
The model is a value on both sides: `SetModel` deep-copies and `Model()` returns
a copy.

**Activation** is state → action → close. A check toggles before its action runs;
a radio also clears its group at any depth. The menu closes **only when the
action was handled**, because a command that could not run must not look like one
that did. `MenuActivatedEvent` fires exactly once per activation *including* when
it was not handled, and says which. Rows need their own event because
`tui.ControlActivatedEvent` identifies a node, and every row of one menu shares
it.

**Keys follow the layout.** A vertical menu steps with Up/Down and opens to the
side; a `MenuBar` steps with Left/Right and opens away from its edge. Escape
closes every level, Left closes the deepest and returns the selection to the row
that opened it, Enter and Space activate, and a row's `Hotkey` activates it from
anywhere in the level. Everything the pointer can do, the keyboard can do.

**`MenuBar` is a placement shell**, not a parallel widget: model, selection and
open/close all stay on the `Menu` and are reached through `bar.Menu()`. It does
not position itself — put it at the bottom of the screen by putting it at the
bottom of the layout that owns it. `BarPlacement` decides orientation and drop
direction, and it applies for exactly as long as the bar is **mounted**:
constructing a bar changes nothing about the Menu, and a Menu taken out of a bar
is a vertical menu again.

**`MenuItem` is the standalone leaf** — a mountable, focusable control for
ordinary layouts. `MenuItemModel` is inert data a `Menu` owns; `MenuItem` is a
node, so the runtime arms it and it emits `tui.ControlActivatedEvent`.

#### `Modal`

The composed dialog. `Modal` fills its host, traps focus, and places a card
carrying a title, your body content and a row of buttons:

```go
host := widget.NewOverlayHost(appRoot) // once, wrapping the whole UI

ok := widget.NewButton("Save", widget.WithRole(widget.ButtonRoleDefault),
    widget.WithOnActivate(func() { save() }))
no := widget.NewButton("Cancel", widget.WithRole(widget.ButtonRoleCancel))

dlg := widget.NewModal(widget.NewText("Save your changes?"),
    widget.WithModalTitle("Unsaved work"),
    widget.WithButtons(no, ok),
    widget.WithOnDismiss(func(r widget.DismissReason) { log(r) }))

if err := dlg.Open(host); err != nil { /* … */ } // loop goroutine
```

**Lifecycle.** `Open` mounts the dialog on top and moves focus into it *before
returning*, so no input reaches the covered UI in between. Reopening an open
dialog returns `ErrModalAlreadyOpen` and changes nothing; a dialog whose tree
cannot be mounted returns `ErrModalNotMountable` and leaves the host untouched.
`Dismiss` is idempotent, publishing exactly one `OverlayDismissedEvent` per
closure with a typed `DismissReason`.

**Ordering.** The dialog leaves the tree *before* the `WithOnDismiss` callback
runs, and the event is published after it — which is what makes reopening the
same dialog from its own callback ordinary rather than a double-mount panic.

**Focus.** Focus lands on the enabled `ButtonRoleDefault` button, else the first
enabled button, else the `Modal` node itself — the last case keeps Escape
reachable when every control is disabled. The preference is honoured on *every*
focus repair, not only at open. `SelectedButton()` reports the focused button's
index, or `-1` when the dialog itself holds focus.

**Escape** resolves the **cancel role**, never a label or a position. With a
cancel-role button it activates it through the runtime — publishing the same
`tui.ControlActivatedEvent` a click would, with keyboard provenance — then
dismisses with `DismissCancel`; otherwise `DismissEscape`.

**Stacking** is LIFO and owned by the host. Only the topmost dialog dims the
background, and dismissing one that is not on top closes everything above it
first, each with reason `DismissReplaced`. `host.TopModal()` reports the current
one.

**At runtime,** `SetButtons` validates the whole list before changing anything —
no nil entries, no repeated `*Button`, nothing mounted elsewhere, at most one
Default and one Cancel — and returns an error matching both
`ErrInvalidButtonList` and the specific sentinel. Accepted, it reconciles in one
batch and *moves* retained buttons rather than remounting them, so identity and
focus survive a reorder. `WithPointerPolicy` covers the whole dialog subtree;
`WithStyle` restyles the live card and its backdrop together.

**Keyboard parity.** Tab and Shift-Tab cycle the dialog's buttons and stop at
its edges, Enter activates the focused one, Escape resolves the cancel role, and
focus returns where it came from on close. A dialog with the pointer disabled
stays fully operable; no control is reachable only by clicking.

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
