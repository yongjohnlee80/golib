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

The divider is draggable and keyboard-operable out of the box: `Alt`+arrows
along the split's own axis move it by one cell, and `WithSplitResizeStep` sets a
different amount or unit. Ask for what you want and read back what you got —
they differ whenever a minimum or a narrow terminal has had a say:

```go
body.RequestedRatio() // the last unclamped request — persist THIS
body.Ratio()          // the effective division actually on screen
body.Cells()          // the same answer in integers, plus whether it exists yet
```

Persisting the effective value is the bug this separation prevents: a split
clamped by a minimum on a narrow terminal would save the clamp, and every
restore at that width would walk the divider a little further.

## Resizable — making anything resizable

Resizing is a **wrapper**, not something a widget opts into. Any component
becomes resizable by being wrapped, without being modified and without knowing
that resizing exists:

```go
panel := widget.NewResizable(widget.NewBox(tree, widget.WithTitle("Files")),
    widget.WithMinSize(tui.Size{W: 12, H: 4}),
    widget.WithMaxSize(tui.Size{W: 60, H: 30}),
    widget.WithHandles(widget.HandleBottomRight, widget.HandleRight))
```

`Size()` is what it reached and `RequestedSize()` what was asked for, the same
split as above. Two things are worth knowing before you reach for it:

- **Wrapping changes Tab order in no way.** Neither the grips nor the wrapper is
  a tab stop. Keyboard resizing reaches the wrapper by *bubbling* from a focused
  descendant, so a wrapper around content with nothing focusable in it — or
  around an `Editor` or `TextArea`, which consume `Shift`+arrows for selection —
  needs you to drive it from the grips, from `Context.DoAction`, or from a focus
  owner of your own.
- **One action vocabulary serves both widgets.** `ResizeBeginAction`,
  `ResizeUpdateAction`, `ResizeStepAction`, `ResizeSetAction`, `ResizeEndAction`
  and `ResizeCancelAction` are interpreted by `Resizable` and by `Split` alike,
  so a key you bind to a resize action works on whichever of the two is in
  front of the user:

```go
// Grow whatever is focused, wrapper or split, by one cell.
ctx.DoAction(widget.ResizeStepAction{DX: 1, Unit: widget.StepCells})
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

### Column widths: fixed, or share the rest

`Width: 0` marks a **flex** column. Every flex column shares the width
left after the fixed ones, evenly (the odd cell or two goes to the
leftmost). A table of all-flex columns therefore renders an even grid —
useful when the content width is unknowable, e.g. a result set where one
column holds uuids and another holds `true`.

```go
cols := []widget.TableColumn[Row]{
    {Title: "ID",   Width: 5,  Cell: ...},   // fixed
    {Title: "NAME",            Cell: ...},   // flex ┐ share the remainder
    {Title: "NOTE",            Cell: ...},   // flex ┘ evenly
}
```

## List — when you don't need headers

Same interaction model as Table's row area. `WithItems(items, render)`,
`WithEmptyText`, `SetItems`, `Selected`, `SelectionChangedEvent`,
`ActivateEvent`. The cursor row is painted even while the list is NOT
focused — a controller can forward `↑/↓` to an unfocused list and the user
still sees the cursor move.

### Driving a list or tree from the host

Widgets own their cursor, but the *host* often knows something they
cannot: which pane is focused, what the user searched for, which row to
restore. These are the seams for that:

```go
list.SetCursor(i)          // programmatic sibling of j/k — search, reveal
list.Len()                 // row count
list.SetStyles(widget.ListStyles{CursorRow: focusedStyle})  // restyle live

tree.SetCursor(i)
tree.Cursor()
tree.VisibleRows()         // flattened display order; node.Label() reads one
tree.SetStyles(...)
tree.Reload("notes:7")     // refresh ONE subtree after its data changed
```

`Tree.Reload` is the one to remember: when the data behind a loaded
subtree changes (a file written, a row deleted), it drops the cached
children and re-requests them under a NEW generation — stale in-flight
loads stay inert and **the cursor does not move**. `SetChildren` needs a
generation you do not have, and `ExpandPath` moves the cursor, so
neither is a substitute.

`SetStyles` exists because a widget cannot see focus that rests on a
delegating wrapper (chapter 4): the host holds the focus knowledge, so
the host supplies the focused and blurred styles.

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
before the view mounts return `(0, widget.ErrClosed)`**, rejecting your startup
logs unless deferred or relayed.

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

## Editor — multi-line text editing, keymaps, and viewers

`widget.Editor` is a configurable multi-line text editor supporting both
modal (Vim) and modeless (Nano, Standard/GUI) interaction styles, granular
feature capabilities, dynamic keymap reflection, and read-only viewing.

### 1. Keymap profiles and modal editing

By default, the editor boots with modal Vim editing (`KeysetVim`). You can
configure modeless editing or switch to alternative keymap profiles:

```go
// 1. Default Vim modal editor (Normal, Insert, Visual, VisualLine)
edVim := widget.NewEditor(
    widget.WithVimKeymap(),
    widget.WithEscapeChord("jk"), // map 'jk' to Esc in insert mode
)

// 2. Modeless Nano-style editor (Ctrl+K cut, Ctrl+U paste, Ctrl+A/E home/end)
edNano := widget.NewEditor(
    widget.WithNanoKeymap(),
)

// 3. Modeless Standard GUI-style editor (Ctrl+Z/Y undo/redo, Ctrl+C/V/X clipboard, Ctrl+A select all)
edStd := widget.NewEditor(
    widget.WithStandardKeymap(),
)

// 4. Custom modeless editing with custom bindings
edCustom := widget.NewEditor(
    widget.WithModalEditing(false),
    widget.WithKeymap(widget.Keymap{
        widget.KeyChord{Mode: widget.ModeInsert, Code: 'a', Ctrl: true}: widget.ActSelectAll,
        widget.KeyChord{Mode: widget.ModeInsert, Code: 'z', Ctrl: true}: widget.ActUndo,
    }),
)
```

### 2. Granular capability toggles

Disable features when building constrained input surfaces or read-only viewers:

```go
ed := widget.NewEditor(
    widget.WithSelection(false), // disable visual selection / ranges (v, V)
    widget.WithYank(false),      // disable explicit copy/yank actions (y, yy, ActCopy)
    widget.WithUndo(false),      // disable undo / redo history stack
    widget.WithEditorWrap(widget.WrapSoft), // soft line-wrapping
)
```

Note: `WithYank(false)` disables explicit yank/copy actions (`ActCopy`, `ActVisualYank`). It does not disable pasting; internal register paste remains supported, and destructive edits (such as line delete) can still populate the internal register.

### 3. Read-only viewer mode

```go
ed.SetValue(doc); ed.Lines()      // document in / snapshot out
ed.SetLine(row, col)              // jump — search hits, error locations
ed.SetReadOnly(true)              // VIEWER: motions, visual select, yank; no edits
```

`SetReadOnly` turns the editor into a navigable document: motions (`hjkl`,
arrows, word/paragraph jumps), visual selection (`v`/`V`), and yank (`y`)
work normally, while text insertion, deletions, paste, and bracketed-paste
are refused. This is ideal for structured logs, query result panels, or JSON
inspectors where `BufferView` is insufficient because you need cursor navigation
and text selection.

### 4. Runtime keymap reflection & bubbling

The editor exposes its runtime keymap structure, allowing parent controllers
and status bars to dynamically display active bindings:

```go
// Query editor mode for status bars (NORMAL, INSERT, VISUAL, V-LINE)
mode := ed.Mode().String()

// Inspect configured bindings and escape chord
snap := ed.SnapshotKeymap()
escape := snap.EscapeChord // "jk"
bindings := snap.Bindings  // []KeyBinding: chord, action name, description

// Query bindings for a specific mode or reverse-lookup chords
normalBindings := ed.BindingsForMode(widget.ModeNormal)
copyChords := ed.ChordsForAction(widget.ActCopy)
action, bound := ed.ActionForChord(widget.KeyChord{Mode: widget.ModeInsert, Code: 's', Ctrl: true})

// Unbinding: allow specific keys to bubble up to parent containers
edBubbling := widget.NewEditor(
    widget.WithKeymap(widget.Keymap{
        // Unbinding KeyHome lets it bubble to parent instead of moving editor cursor
        widget.KeyChord{Mode: widget.ModeInsert, Code: tui.KeyHome}: widget.ActUnbound,
    }),
)
```

## Text inputs — forms

`TextInput` (single line: `WithPlaceholder`, `WithMask('*')` for passwords,
`WithInitialValue`, `WithValidate`) publishes `SubmitEvent` on Enter.
Compose a form as a container component that mounts several inputs, moves
focus between them on Tab/Enter (`ctx.FocusComponent(input)`), and handles
Esc/Ctrl-S itself — unconsumed keys bubble from the focused input straight
to your form container.

Next: [events, focus and keys](04-events-focus-keys.md).
