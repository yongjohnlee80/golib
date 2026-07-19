# 3 — Widgets

The composition vocabulary, with the patterns that work in practice. All of
these come from `golib/tui/widget`.

## Box — the panel

Border + title around any component. It highlights its border while it *or
any descendant* holds focus (the lazygit active-panel look) — you get panel
highlighting for free by focusing the content inside.

```go
widget.NewBox(content, widget.WithTitle("Logs"))
```

## Split and Dock — the page skeleton

`Split` divides an area between two children by ratio with minimums;
`Dock` pins components to edges and gives a center child the rest.

```go
// menu on top, status bar on the bottom, content+logs split in between
body := widget.NewSplit(widget.Vertical,
    contentPanel, logPanel,
    widget.WithRatio(0.62), widget.WithMinSizes(3, 3))
dock := tui.NewDock()
dock.Pin(tui.DockTop, menuPanel)
dock.Pin(tui.DockBottom, statusBar)
dock.Add(body)
```

## Tabs — switching content

```go
tabs := widget.NewTabs(
    widget.WithTab("Deliveries", deliveriesView),
    widget.WithTab("Releases", releasesView),
    widget.WithKeepMounted(true), // keep inactive tabs' state alive
    widget.WithAutoFocus(true),   // arrow-navigable immediately
)
```

Keys: `←/→` and `[`/`]` cycle when the bar is focused; `Ctrl+PgUp/PgDn`
cycle from anywhere inside. `WithoutBar()` hides the bar entirely — use it
when a separate widget (a custom menu panel) drives `tabs.Select(i)` and
Tabs is purely the content switcher. Note `WithKeepMounted` keeps *state*;
tabs still mount lazily on first selection.

## Table — rows with headers

A header row over a cursor-driven List. Columns have fixed widths; give ONE
column `Width: 0` and it absorbs whatever space remains, so the table works
at any terminal size.

```go
table := widget.NewTable(
    []widget.TableColumn[Delivery]{
        {Title: "User", Width: 10, Cell: func(d Delivery) string { return d.User }},
        {Title: "Message ID", Width: 0, Cell: func(d Delivery) string { return d.MessageID }},
        {Title: "State", Width: 18, Cell: func(d Delivery) string { return d.State }},
    },
    widget.WithEmptyText[Delivery]("No deliveries received yet."),
)
table.SetItems(rows)                  // replace rows (loop goroutine)
idx, ok := table.Selected()           // cursor position
ctx.FocusComponent(table.List())      // focus the inner list
```

`↑/↓`, paging, Home/End work when the inner list is focused; `Enter`
publishes `widget.ActivateEvent{Owner, Index}` — subscribe to open a detail
view (chapter 6). Size the fixed columns generously: cells truncate with an
ellipsis, and a too-narrow status column will truncate exactly the value
you assert on in tests.

## List — when you don't need headers

Same interaction model as Table's row area. `WithItems(items, render)`,
`WithEmptyText`, `SetItems`, `Selected`, `SelectionChangedEvent`,
`ActivateEvent`. The cursor row is painted even while the list is NOT
focused — a controller can forward `↑/↓` to an unfocused list and the user
still sees the cursor move.

## BufferView — logs and pagers

Append-oriented ring buffer with scrollback and follow-tail. This is THE
place for logs — never stderr (chapter 1).

```go
logView := widget.NewBufferView(
    widget.WithFollowTail(true),
    widget.WithMaxLines(5000),
    widget.WithANSIPassthrough(true), // SGR colors in log lines render
)
w := logView.Writer() // io.Writer, safe from ANY goroutine…
```

…but read the writer contract in chapter 5 before you use it: **writes made
before the view mounts are dropped**, which eats your startup logs.

`y` (focused) copies the whole buffer to the system clipboard via OSC 52;
`PlainText()` gives you the unstyled contents.

## StatusBar — key hints

```go
status := widget.NewStatusBar()
status.SetLeft("myapp")
status.SetRight("↵ open · ←/→ menu · q quit")
```

Update `SetRight` per mode (which tab is active, whether a modal is open) —
it is the difference between a discoverable UI and a guessing game.

## Text inputs — forms

`TextInput` (single line: `WithPlaceholder`, `WithMask('*')` for passwords,
`WithInitialValue`, `WithValidate`) publishes `SubmitEvent` on Enter.
Compose a form as a container component that mounts several inputs, moves
focus between them on Tab/Enter (`ctx.FocusComponent(input)`), and handles
Esc/Ctrl-S itself — unconsumed keys bubble from the focused input straight
to your form container.

Next: [events, focus and keys](04-events-focus-keys.md).
