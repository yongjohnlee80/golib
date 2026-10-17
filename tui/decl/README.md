# tui/decl — golib/tui screens written in QML

`tui/decl` is the adapter between the [`decl`](../../decl/README.md) engine and
[`golib/tui`](../README.md): it turns a QML document into a tree of real golib
widgets, and keeps that tree in step with the document and with the program's
state. The Go program owns behaviour; the QML owns structure and looks.

```go
import tuidecl "github.com/yongjohnlee80/golib/tui/decl"
```

```qml
import tui 1.0
import editor 1.0                 // the host's App singleton
import editor.theme.retro 1.0     // a Theme singleton — swap the line to swap the theme

Window {
    Shortcut { sequence: "Ctrl+Q"; onActivated: quitDialog.open() }
    MenuBar {
        Dock.edge: Tui.Top
        Menu { title: "&File"
            MenuItem { text: "&Save"; onTriggered: App.saveFile() }
        }
    }
    Frame { Editor { id: editor; focus: true; keyset: App.keyset } }
    StatusBar { Dock.edge: Tui.Bottom; left: App.mode; right: App.clock }
    Dialog {
        id: quitDialog
        title: "Quit"
        standardButtons: Dialog.Yes | Dialog.No
        onAccepted: App.quit()
        Text { text: "Are you sure to quit?" }
    }
}
```

The whole program, in one call:

```go
p, err := tuidecl.NewProgram(
    tuidecl.Layout(files, "editor.qml"),
    tuidecl.Singleton("editor", "1.0", "App"),
    tuidecl.Sources(map[string]any{"App.keyset": "vim", "App.mode": "NORMAL"}),
    tuidecl.Commands(map[string]func() error{"App.saveFile": save, "App.quit": quit}),
    tuidecl.Themes(files, "themes", "editor.theme", "1.0"),
    tuidecl.Components(files, "dialogs", "editor.dialogs", "1.0"),
    tuidecl.AppOptions(tui.WithBackend(backend)),
)
if err != nil { return err }
return p.Run(ctx)
```

**Start with [USAGE.md](USAGE.md)** — how to wire a host, modules and themes,
component files, dialogs, reloading, mixing QML with hand-built Go widgets, and
the rules that keep both safe. This file is the reference.

The worked example is [`tui/examples/editor-qml`](../examples/editor-qml/README.md):
a complete text editor whose every screen is QML. [`tui/examples/docks`](../examples/docks/README.md)
is a dock style written once and used for two panels, each with its own keys.

---

## The vocabulary

`StdRegistry()` builds all of these; `StdProperties()` is their property
contract, handed to `New`. A **ctor** property is read when the widget is
built — changing it on a reload rebuilds that node. A **setter** property can
change at runtime, which is what makes it bindable to a source.

| Type | ctor properties | setters | read in a handler | signals | methods |
| --- | --- | --- | --- | --- | --- |
| `Window` | — | `color` (Qt's: the background under everything, where no widget paints — a theme's `app.backdrop` around a page) | — | — | — |
| `MenuBar` | `vimNavigation`, palette | `autoHide` (golib's: no row until F10 or an Alt+letter brings it up; it goes once the visit ends) | — | — | — |
| `Menu` | `title`, `align` | — | — | — | — |
| `MenuItem` | `text`, `checkable`, `group`, `shortcut` (accelerator text, or `keyId("id")`: the command's current key, as the key map binds it) | `checked`, `enabled`, `visible` | `checked` | `triggered`, `toggled` | — |
| `MenuSeparator` | — | — | — | — | — |

A `MenuSeparator` with nothing to separate is not drawn, as Qt's QMenu collapses them: one first or last among the rows shown, or right after another. So a group whose `MenuItem`s all hide (`visible: false`) takes its rule with it.

| `Shortcut` | `sequence` (a fixed key, `"Ctrl+Q"`, or a key command, `keyId("id")`) | — | — | `activated` | — |
| `FocusScope` | Shortcuts; one child, its content | `keyScope` (golib's: the key-map scope of its `keyId` Shortcuts) | `keyScope` | — | — |
| `Frame` | palette | `title`, `maximumWidth` (golib's: at most that many columns, centred: a page) | — | — | — |
| `Editor` | `wrap`, palette | `text`, `keyset`, `readOnly`, `cursorPosition`, `wrap` (live: soft-wrap long lines, or scroll them), `lineNumbers` (golib's: each line's number in a gutter at the left), `lineNumberColor` (golib's: the numbers' colour), `cursorColor` (golib's: the text cursor's colour, sent to the terminal), `ruler` (golib's: a guide at that column, where text wraps), `contextMenu` (golib's: a right-click menu with Undo, Redo, Copy, Cut and Paste, opened at the pointer), `view` (golib's: `Editor.Raw` or `Editor.Rendered`; see [Raw and Rendered](#raw-and-rendered-view-not-mode)) | — | `modeChanged` (the **Vim** mode: Normal, Insert, Visual; not the view), `keyScopeChanged` (golib's: its key-map scope moved, by a mode class or a keyset; read it as `keyScope`), `textChanged`, `cursorPositionChanged` (Qt's TextEdit signal: the cursor moved to another line or column, by a key, a click, an edit or the program) | — |
| `Image` | — | `scrollable` (golib's: the PNG at the cells' width, scrolled by ↑↓ j k h l, Page Up/Down, `[` `]`, Home/End and the wheel; only the part shown is placed) | — | — | — |
| `HTMLView` | — | `html` (golib's: the page it shows), `stylesheet` (golib's: a stylesheet a pixel layout applies after the page's own) | — | `linkActivated(href)` (a link clicked), `scrolledTo(sourceByte)` (the source byte of the block now at the top) | `scrollToSource(byte)` |
| `SyntaxHighlighter` | `definition` | — | — | — | — |
| `StatusBar` | palette; its children are its widgets, each at its own width: permanent (Qt's `QStatusBar.addPermanentWidget`) at the right end, or, with `StatusBar.permanent: false` on the child, normal (`addWidget`) at the left end; the segments share the rest | `left`, `center`, `right` | — | — | — |
| `Text` | `wrapMode`, palette | `text` | — | — | — |
| `Terminal` (golib's) | `command` (the program; the user's `$SHELL` when empty), `dir`, `scrollback` | `command`, `dir` (both at the next `start()`), `vimKeys` (Esc leaves input mode, except on the alternate screen), `themeColors` (default true: the program's default colours are the theme's), `cellFont` (golib's: the family its cells are drawn in, at the window's cell size, where the backend draws fonts — a window, not a terminal; "" the window's), `hostKeys` (golib's: keys its program never sees, a comma-separated list of Shortcut sequences, `"F11"`, left to the Window's Shortcuts; none by default, and a program in it then never gets those keys), `modeKey` (golib's: Shortcut sequences, `"Ctrl+T"`, that toggle input and normal mode, also under a full-screen program; none by default; with `vimKeys` off, Esc is then always the program's), `cursorColor` (golib's: the cursor's colour, as the Editor's; its shape shows the mode: a block in normal mode, a thin bar while typing unless the program sets its own) | — | `exited(code)`, `titleChanged(title)`, `modeChanged(mode)` — `"terminal"` or `"normal"` | `start()`, `stop()`, `focus()` |
| `TerminalTabs` (golib's) | — (no declared children: its terminals are the host's, through `TerminalTabsOf`) | `emptyText` (what the body says with no tab), `minimumContrast`, `cursorColor`, `vimKeys`, `cellFont`, `hostKeys`, `modeKey` (each as the Terminal's, for every tab, those added later too; the strip and its menus stay in the window's font) | `count`, `currentId` | `addRequested` (the strip's +), `menuRequested(id)` (≡, or a right-click on a tab; not when the host gave the strip its own menu, `SetMenu`), `changed` | — (the host adds, starts, selects, renames and removes tabs, and sets their menu, from Go) |
| `Button` | — | `text`, `enabled` | — | `clicked` | — |
| `CheckBox` | — | `text` (`&` marks the mnemonic), `checked`, `enabled` | — | `toggled`, `clicked` | — |
| `Split` | `orientation` | `ratio` | — | — | — |
| `Flex` | `direction` | — | — | — | — |
| `Flow` | — | `spacing` (the gap between two children on a line, and between lines unless `rowSpacing` says otherwise), `rowSpacing` (golib's, from Qt's GridLayout: the gap between lines) | — | — | — |
| `Dialog` | `dim`, `width`, `standardButtons`, `defaultButton`, `keyScope` (golib's: the scope of its `keyId` Shortcuts), palette | `title`, `helpText` | — | `opened`, `accepted`, `rejected`, `closed` | `open()`, `close()` |
| `DialogButtonBox` | — (its Buttons carry `DialogButtonBox.buttonRole`) | — | — | — | — |
| `ListView` | `textRole` | `model`, `currentIndex` | `currentIndex` | `activated(index)`, `currentIndexChanged(index)` | — |
| `ComboBox` | `textRole`, `valueRole`, `placeholderText`, `maxWidth` (default 40 cells) | `model`, `currentIndex` | `currentIndex`, `currentValue` | `activated(index)` | — |
| `TableView` | golib's `cellCursor`, `sortable` (see below) | `model`, `currentIndex`; with a `cellCursor`, golib's `rowNumbers` (each row's number in a gutter), `orderBy` (the sort as ORDER BY text), `filter` (the rows shown, as WHERE text) and `contextMenu` (a right-click menu of the host's rows) (see below) | `currentIndex`; with a `cellCursor`, golib's `orderBy`, `orderByError`, `filter` and `filterError` | `activated(index)`, `currentIndexChanged(index)`, golib's `contextMenuRequested(index, x, y)`, golib's `orderByChanged(orderBy)` (see below) | — |
| `TableViewColumn` | `role`, `title`, `width`, `elideMode` | — | — | — | — |
| `TabView` | palette (the bar: `window`/`windowText` behind the titles, `button`/`buttonText` for each, `highlight`/`highlightedText` for the current one) | `currentIndex` | `currentIndex`, `count` | `currentIndexChanged(index)` | — |
| `Tab` | `title`; one child, its content | — | — | — | — |
| `TreeView` | `textRole`, `badgeRole` | `model` (a tree model) | — | `activated(index)`, `expanded(index)`, `currentIndexChanged(index)` — an `Index` | `toggleExpanded(index)`, `setCurrentIndex(index)` |
| `FileDialog` | `title`, `helpText`, `dim`, `fileMode`, `preview`, palette | `currentFolder`, `selectedFile` | — | `accepted(selectedFile)`, `rejected`, `closed` | `open()`, `close()` |
| `Drawer` | `modal` (default true), golib's `resizable` (default false), `windowResize`, `movable` and `moveButton` (see Window controls); one child, its content, and any number of Shortcuts; `keyScope` (as the Dialog's) | `edge` (`Tui.Left`, `Right`, `Top`, `Bottom`, or golib's `Center`: floating in the middle, `length` wide and `size` high), `size` (a percentage across the edge), `length` (golib's: a percentage along it, centred; default 100), golib's `minimumSize` (default 10) and `minimumLength` (default 20); a movable Drawer's `floating`, `floatX`, `floatY`, `floatWidth` and `floatHeight` (percentages of the Window) | — | `opened`, `closed`, golib's `resized(size, length)`, a movable Drawer's `placed(x, y, width, height)` | `open()`, `close()`, `toggle()`, `resizeBy`, `moveBy` |
| `FolderDialog` | `title`, `helpText`, `dim`, `preview`, `keyScope` (as the Dialog's), palette; its children are fields | `currentFolder`, `selectedFolder` | — | `accepted(selectedFolder)`, `rejected`, `closed` | `open()`, `close()` |
| `Repeater`, `Instantiator` | `model` | — | — | — | — |
| `DelegateChooser` | `role` | — | — | — | — |
| `DelegateChoice` | `roleValue` | — | — | — | — |

**A row's context menu.** A `TableView` that declares `onContextMenuRequested` raises it for a
right press on a row, the cursor moved to that row first so the menu acts on what was pressed;
for a right press below the rows, with `index` -1, the cursor left where it is; and for Shift+F10
or the Menu key while it holds the keyboard, on the cursor row. `x` and `y` are where to open a
menu, in the view's own cells (the header row is row 0). It opens no menu itself: the program
opens one there, `widget.NewEditorMenu(view, nil)` with its own rows, say. A `TableView` that does
not declare the handler leaves a right press unhandled, as before.

**UI text takes `qsTrId("id")`** — Qt's id-based translation, from the program's
catalogs (`Translations`). These properties take a string or a message: `Text.text`,
`Button.text`, `CheckBox.text`, `MenuItem.text`, `Menu.title`, `Frame.title`, `StatusBar`'s
`left`, `center` and `right`, `Dialog`, `FileDialog` and `FolderDialog`'s `title` and
`helpText`, and `TextField.placeholderText`. A message is shown in the App's language and
follows `App.SetLanguage` with nothing rebound; a mnemonic keeps English's letter. `qsTrId`
takes one non-empty string. `Editor.text`, `Tab.title`, `TableViewColumn.title` and
`ComboBox.placeholderText` take a string only. See [USAGE.md §7c](USAGE.md#7c-languages).

`Repeater`, `Instantiator`, `DelegateChooser` and `DelegateChoice` are the
engine's, expanded before anything is built, so they have no widget.
`TextField` and `Popup` are Qt Quick Controls types in the
[`controls`](controls/) package, which a program adds with
`tuidecl.Types(controls.Types()...)`: `TextField` (`placeholderText`,
`echoMode`, golib's `completions` (a model whose rows Tab completes the word
from); setter and readable `text`; `accepted(text)`, `textEdited(text)`;
`clear()`) and `Popup` (`modal`, `dim`; `open()`, `close()`). A `MenuItem`'s
`toggled` is raised for each change the user's toggle makes to it, a radio
cleared by its group included, before its `triggered`.

**`Shortcut`** — Qt's key sequence: modifiers joined by `+` (`Ctrl`, `Alt`,
`Shift`, `Super`) and a key name (`Esc`, `F10`, `Space`, `Menu`…) or one character (`+` and `,`
included: `Ctrl++`, `Ctrl+,`). A letter's case is its Shift, as in Qt: `"c"` or `"C"` is the key c,
and `"Shift+C"` is the capital, whether a terminal delivers it as `C` or as Shift and c. With Ctrl or
Alt, a Ctrl+Shift letter also matches a Shortcut written without Shift, since a terminal may not
report it (`Ctrl+Q` matches however it arrives); one written with Shift wins its own key. A key held
with Hyper or Meta matches nothing. `meta` reads as Alt (golib's; Qt's Meta is `Super` here). A
sequence of several chords, `"Ctrl+K, Ctrl+C"`, is refused by name.

**Overridable keys: `keyId`.** `sequence: keyId("id")` names a key command the host declared with
`KeyCommands`: its keys are the App's key map's, so a user's key-mapping scheme (`Keymap`) moves it,
live. Every golib widget's keys are commands too (`tui.list.down`, `tui.editor.vim.normal.undo`,
`tui.window.menu` for F10): see [`tui/keymap`](../keymap/README.md). A `keyId` Shortcut belongs to its
command's scope: a `Window`'s is `global`; a `FocusScope`'s, `Dialog`'s, `FolderDialog`'s or
`Drawer`'s is its `keyScope`. One naming no declared command, in a holder with no `keyScope`, or in a
holder of another scope is refused with its position. A label's mnemonic (`&File`) is not a command:
it is the label's, and stays on English's letter.

A Shortcut is live while focus is inside the element that declares it:
- a `Window`'s everywhere;
- a `Dialog`'s or a `FolderDialog`'s while it is open;
- a `Drawer`'s or a `FocusScope`'s while focus is within it.

The focused widget has a key first, so a field still types its letters. Then the innermost element
holding a Shortcut for it, then the ones around it, then the Window. So a panel's `d` wins over the
Window's `d` while focus is in the panel, and the Window's is live everywhere else. A `Drawer`'s
Shortcut may claim Escape or q from the Drawer's own closing. A Shortcut anywhere else is refused,
naming the fix: put it in a `FocusScope`.

**`FocusScope`** — QtQuick's name, for the element whose Shortcuts are live while focus is within it.
It holds one content item and is otherwise not there:
- its content is laid out as the FocusScope's parent lays the FocusScope;
- nothing is painted;
- it takes neither focus nor the pointer, so the Tab order and what a click lands on are the content's.

Its parent lays out the FocusScope, so `Layout` and `Dock` properties go on it, as in Qt, not on its
content: a deliberate narrowing, since a parent reads them only when it takes the item in. Wrapping
an item moves them to the scope; on the content they are refused, the error naming the scope.

    FocusScope {
        Layout.fillHeight: true
        Shortcut { sequence: "n"; onActivated: App.findNext() }
        Editor { }
    }

A reload edits its Shortcuts in place: one added, removed or replaced is live or gone at once, and
the content stays mounted.

**`CheckBox`** — Qt Quick Controls' CheckBox: `[x] text` or `[ ] text`, in the colours of what
it sits on (`palette.window` and `windowText`, as a `Text`), with no button's brackets or fill.
Space, Enter or a click toggles it, raising `toggled` then `clicked`. A host that refuses the
change sets `checked` back.

**Resizable `Drawer`** (golib's) — `resizable: true` puts a grip on the corner at the panel's inner
edge:
- the top right of a bottom drawer;
- the bottom right of a left, top or centred one;
- the bottom left of a right one.

The pointer drags it to resize the panel across its edge and along it. `size` and `length` follow
the drag, as percentages of the Window:
- `size` between `minimumSize` and 90;
- `length` between `minimumLength` and 100;
- never under three cells.

`resized(size, length)` is raised once, when the drag ends, for a host to keep them. While
dragging, the panel shows the drag. A drag cancelled with Escape leaves `size` and `length` as they
were. The minimums hold for values a host sets too.

**`TabView`** — Qt Quick Controls 1's: a bar of its `Tab`s' titles over the
current one's content. A Tab's content is made when it is first current and
kept, so what was typed in a tab is still there when a handler reads it from
another; a Tab that is not current is not laid out, painted or focused.
Ctrl+PageUp and Ctrl+PageDown change tabs from inside the view; with the bar
focused, ←/→ and `[` `]` do, and so does a click on a title.

```qml
TabView { id: settings
    Tab { title: "Edit"; Flex { direction: Tui.Vertical; TextField { id: name } } }
    Tab { title: "Advanced"; Flex { direction: Tui.Vertical; Text { text: "…" } } } }
```

**`Flex`** — Qt's `ColumnLayout` / `RowLayout`. Each child takes its own size,
in order; a child marked `Layout.fillHeight: true` (in a column) or
`Layout.fillWidth: true` (in a row) shares what the others leave, and one with
`Layout.preferredWidth: n` (in a row) or `Layout.preferredHeight: n` (in a
column) takes n cells whatever its content (what is left, when less is), so a
column of labels lines up the controls beside them:

```qml
Flex { direction: Tui.Horizontal
    Text { text: "theme"; Layout.preferredWidth: 20 }
    ComboBox { model: App.themes } }
```

A view — a `ListView`, `TableView` or `TreeView` — is as tall as its content where nothing
bounds it, and fills what it is given otherwise, so a table followed by a row of
buttons is written:

```qml
Flex { direction: Tui.Vertical
    TableView { model: App.rows; Layout.fillHeight: true }
    Flex { direction: Tui.Horizontal; Button { text: "&Add" } } }
```

A row or column is as wide across as its widest child unless its parent fixes
that extent, when it stretches its children across it.

**`Flow`** — Qt's `Flow`: its children left to right, each at its own size, and
a new line when the next one would pass the right edge. A row of buttons that
must fit a narrow panel is a `Flow`, not a horizontal `Flex`, which would clip
it. `spacing` is the gap between two children on a line and, unless
`rowSpacing` sets its own, between lines; in a terminal a row of buttons wants a
column between them and no row between its lines:

```qml
Flow { spacing: 1; rowSpacing: 0
    Button { text: "Preview (&i)" }
    Button { text: "&Add" }
    Button { text: "Remove (&d)" } }
```

A child wider than the `Flow` gets the whole width, on a line of its own. A
`Flow` is as tall as its lines; in a column it is laid out at the column's
width, so it takes as many rows as that width needs.

**Models** — Qt's model/view. A host sets a `tuidecl.ListModel` (or its own
`tuidecl.ItemModel`) as a source; `model: App.people` binds a view to it, and
the view follows the model's changes itself — insert, remove, reset — with
nothing rebound. Roles are typed (a string, bool or number). A handler reads a
view by its id when it runs: `listView.currentIndex`, `combo.currentValue`
(the chosen row's `valueRole`). A view drops its subscription when the model
is replaced and when the view is destroyed.

A `TableView` shows the model's own columns — a query's result, whatever
columns it has, redrawn on the same view when they change — unless it declares
`TableViewColumn`s.

golib's `cellCursor: true` (set at construction) shows the rows through a `widget.TableCore` and
a `widget.DataTable`: a cursor on cells, scrolled both ways, with vim's keys — counts, h/j/k/l,
`w`/`b` by field, `gg`/`G` (`[n]G` to row n), 0/$, `v` cells in reading order, `V` whole rows,
Ctrl+V a block, `y` copies the selection (or the cell) as TSV and `yy` the cursor's row, Esc ends a
selection. The mouse selects too: a drag over cells, over the row numbers (whole rows), or over the
titles (whole fields). The cursor is in the palette's `highlight`/`highlightedText` while the table
has the keys, and in `inactive.highlight`/`inactive.highlightedText` once it has not.
`sortable: true` lets `s` sort the loaded rows by the cursor's column, up, down and off; the header
marks it, and says "(loaded rows)" while the model has more.

golib's `rowNumbers: true` numbers the rows, counted from 1, in a gutter as wide as the last row's
number. golib's `orderBy` is the sort as ORDER BY text over the loaded rows, `name [ASC|DESC], …`,
where a field is its title, a `"quoted title"` or a 1-based position, and "" is no sort. Setting
it sorts; text that does not parse leaves the sort as it was, and `orderByError` (read in a
handler) says why, "" when it parsed. The view keeps it: a new result is sorted by it too. `s`
writes its sort back to `orderBy`, as `"name ASC"`, and raises `orderByChanged(orderBy)`.

golib's `filter` shows only the loaded rows a WHERE clause keeps, then sorts those: comparisons
(`= != <> < <= > >=`) of a field with a number, a `'string'` or `TRUE`/`FALSE`, `IS [NOT] NULL`,
`[NOT] LIKE 'pattern'` (`%` any run, `_` one character, case-sensitive), joined by `NOT`, `AND` and
`OR` (in that precedence) and parentheses; a field is a name, a `"quoted name"` or `#n`, and ""
shows every row. It filters what is loaded and asks for nothing: no query runs, and a `Fetcher` is
asked for more rows only as the cursor's row nears the end of the loaded rows, as without it. With the model's
kinds, values compare as SQL's do — a NULL cell is unknown to every comparison and only `IS NULL`
keeps it, numbers compare exactly, booleans as booleans, anything with a string as text, and a
pairing of kinds that do not compare (a boolean with a number) is unknown, so `NOT` of it does not
keep the row either. Without kinds every cell is text: a number compares with a cell whose text is
one, and no cell is NULL, so `IS NULL` keeps none. Setting `filter` applies it; text that does not
parse leaves the filter as it was, and `filterError` says why, "" when it parsed. The view keeps
it, and a new result is filtered by it too; when it does not parse against the new fields, every
row shows and `filterError` says why. Only the document sets it, so it has no changed
signal. The cursor stays on its row while that row is shown, else goes to the first; a selection
ends; the row numbers, selections and copies count the rows shown. A filter keeping no row shows
the empty table, with no cursor row and nothing to copy.

golib's `contextMenu: true` opens a menu at a right press, the selection left as it is; its rows are the
host's, `tuidecl.TableOf(c).Menu().SetRows(…)`, as an `Editor`'s are, and with none the press goes
on. A model that knows each cell's kind (`widget.TableKinds`) sorts by kind — NULLs last, numbers
as exact decimals — when the TableView declares no `TableViewColumn`s. A model that
is a `tuidecl.Fetcher` (Qt's `canFetchMore`/`fetchMore`, asked at the root `Index{}`) is asked
for more rows as the cursor nears the end of those loaded; the host inserts them when they come.
`index` in its signals and `currentIndex` are the model's row whatever the sort shows. A
cell-cursor `TableView` takes no `delegate` and has no `contextMenuRequested` (its menu is
`contextMenu`), and `sortable`, `rowNumbers`, `orderBy`, `filter` and `contextMenu` need it. A native backend binds its own pixel layout to the `DataTable`
(`widget.DataTable.BindLayout`; gui's `Native()` style does so for every cell-cursor `TableView`)
and draws the same core: the same keys, cursor, sort, paging and signals. A row-cursor
`TableView` is drawn in cells on either.

    TableView { model: App.result; cellCursor: true; sortable: true; rowNumbers: true
                orderBy: "name DESC"; onActivated: App.open(index) }

A `TreeView` shows a tree model (`tuidecl.TreeListModel`,
or a `TreeModel` of the host's): a row opens and, the first time, the view asks
the model to load its children (`OnFetch`), which the host sets with
`SetChildren`. Its signals carry the row's `tuidecl.Index`, which the handler
passes back to the host. Enter activates any row, a branch as well as a leaf,
as Qt's item views do; `l`/Right opens a row and `h`/Left closes it. A host
that decides an activated row is a folder opens it with the view's
`toggleExpanded(index)` — `Program.Call(id, "toggleExpanded", index)` — and
uses any other row as it means. `currentIndexChanged(index)` names the row that came under the
cursor, and `setCurrentIndex(index)` puts the cursor on a row the host chose (a
search hit, say), opening its closed ancestors first. Those ancestors' children
must already be loaded in the model: the view does not fetch to reach a row.
A row whose children the model has opens with them in the key's own handling,
so `l` then `j` lands on its first child however fast they come; and with a
model that tells its views of a change as it makes it (`TreeListModel` does),
`setCurrentIndex` has the cursor on the row before it returns, as Qt's does.

**`Repeater` and `Instantiator`** — Qt's delegate per model row. The one
child is instantiated once per row of `model:`, in the Repeater's place in its
parent — a `Repeater` in a `Flex`, an `Instantiator` in a `Menu` or `MenuBar`
(where Qt needs `onObjectAdded`, the parent here takes the items itself). In
the delegate, `model.<role>` is the row's value, typed, and `index` its row; a
nested `model: model.rows` reads the outer row. Each copy is identified by its
row's key, so a model change re-expands and patches as a reload does: what a
kept row holds survives rows inserted around it.

**`DelegateChooser`** — Qt's delegate chosen per row. As a Repeater's or an
Instantiator's one delegate, it holds `DelegateChoice { roleValue: …; <delegate> }`
entries and a `role:`. Each row is built as the FIRST choice whose `roleValue`
matches the row's value of that role, by Qt's rule
(`QQmlDelegateChoice::match`): equal as values (numbers by value), else equal
as integers, else equal as strings, each tried when the one before fails. The
conversions are QVariant's: a number becomes an integer by rounding half away
from zero, so `roleValue: 1.1` matches a row's `1`; a bool is 1 or 0; a string
is an integer when it reads as a whole one. So `roleValue: 1` matches a row's
`"1"`, and `"01"` matches `"1"`. A choice with no `roleValue` matches every row, and a
row no choice matches has no delegate. Every choice is held to the rules
whether or not a row selects it today.
A menu whose rows are items and submenus, in the model's order:

```qml
Instantiator {
    model: App.menu
    DelegateChooser {
        role: "kind"
        DelegateChoice { roleValue: "item";    MenuItem { text: model.label; onTriggered: App.run(model.id) } }
        DelegateChoice { roleValue: "submenu"; Menu { title: model.label
            Instantiator { model: model.rows; MenuItem { text: model.label } } } }
    }
}
```

**`focus: true` says where the keyboard starts** — Qt's `Item.focus`. The
Window gives it the first `focus: true` item when the screen is first laid
out, and again after a menu action. Focus moves INTO that item, so
`ListView { focus: true }` gives the keyboard to the list inside the view, as
it does for an `Editor` or a `TextField`.

**Every element that takes a place on screen has `forceActiveFocus()`** — Qt's
`Item.forceActiveFocus()`: focus moves INTO it — the item itself when it takes
focus, else its first focusable part — so a pane is focused by naming it:
`explorer.forceActiveFocus()` in a handler, or `Program.Call("explorer",
"forceActiveFocus")` from Go. It reaches any component, a custom one
included, through the program's own focus owner. A `Dialog` is focused as it
is on screen: its modal, while it is open. An item that is **not focusable by
design** is refused as the document's mistake: a declaration with nothing on
screen (a menu row, a `Shortcut`, a `TableViewColumn`), or a mounted item
nothing in which takes focus at all (a `Text`, a `StatusBar`). One that could
take focus but cannot now is left as it is, as in Qt: hidden, disabled,
behind an open dialog, or not on screen at the moment (a closed dialog's
content).

**Every element that takes a place on screen has `visible`** — Qt's
`Item.visible`, a runtime property: hidden, it takes no space, is not painted
or a tab stop, and hides what is under it; removed by a reload, it is shown
again.

**Every widget has `toolTipText`** — Qt's `ToolTip.text`, a runtime property: the text it
reveals in a card while the pointer rests on it for half a second, and at once when the keyboard
brings focus to it (a `ComboBox`'s, when its field is focused). The card never takes focus or the
pointer: a dialog's keys still reach the dialog, and the pointer over the card is still over the
control. It closes when the pointer or focus leaves, on any key or mouse press, and when the text
changes; `""` shows none, and removed by a reload, the item has none.

**Every element except `Window`, `Frame`, `Split`, `Flex`, `Dialog` and the
engine's `Repeater`, `Instantiator`, `DelegateChooser` and `DelegateChoice` is
childless or holds only its own kind** — `MenuBar` holds `Menu`s, a `Menu`
holds `MenuItem`s, `Menu`s and `MenuSeparator`s. `Frame` holds exactly one child, and
`Split` exactly two. A `Dialog` holds exactly one content child, plus its own
`Shortcut`s and at most one `DialogButtonBox` (which it may not combine with
`standardButtons`).

**`HTMLView`** shows an HTML page, read-only: no script runs. A drag selects its text, `Ctrl+C`
(or `y`) copies it, and a click on a link emits `linkActivated(href)` for the host to follow. It
scrolls by the arrows or `k`/`j`, `PgUp`/`PgDn` or `Space`, `Ctrl+U`/`Ctrl+D` (half a view), and
`Home`/`End` or `g`/`G`. In a
terminal it lays the page out in cells: headings bold, links underlined, quotes barred, lists
hanging, a table's columns lined up when they fit, an image as its alt text. A native backend
binds its own pixel layout (`widget.HTMLView.BindLayout`) and draws it, with the same selection,
scroll and signals. Elements may carry `data-src="from-to"`, the source bytes they show:
`scrolledTo(sourceByte)` reports the block at the top as it scrolls, `scrollToSource(byte)`
brings one there, and a new `html` keeps the block that was at the top in its place. A host gives
large pages by id (`tuidecl.FindAs[*widget.HTMLView](p, "preview").SetHTML(page)`), and images
through `widget.DirImages(root)`, which opens nothing outside root.

**`Image`** is Qt Quick's `Image` without a `source`: the host gives it a PNG by id
(`tuidecl.FindAs[*widget.Image](p, "preview").SetPNG(png)`), and it shows it over its cells on a
terminal that draws images (kitty's graphics protocol, `tui.Capabilities.KittyGraphics`). It
reserves its cells blank; the runtime places the image, places it again when the cells move or the
PNG changes, and deletes it when the `Image` is hidden or goes. On a terminal without images the
cells stay blank, so a host shows its fallback there instead. `widget.RasterizeSVG` and
`widget.RasterizeHTML` make a PNG with optional tools found at runtime (`rsvg-convert`, a headless
Chromium or Chrome), offline and bounded.
A terminal refuses an image wider or taller than `widget.MaxImagePixels` (10000, kitty's and
Ghostty's limit), and shows nothing. A scrollable `Image` shows a taller PNG, such as a long page from
`widget.RasterizeHTMLPage`, as strips: `widget.SplitPNG` cuts them off the UI thread, and
`SetStrips` sets them. Only the strip holding the part shown is placed. `SetPNG` cuts a PNG that
large itself, on the caller's thread. One it cannot cut (an `Image` that does not scroll, or a PNG
too wide) is scaled down to fit. Any PNG that decodes is placed within the limit; one that does
not decode is placed as given.

### Functions

| function | yields | takes |
| --- | --- | --- |
| `qsTrId(id)` | the catalog message `id`, shown in the App's language by the widget it reaches | one non-empty string; anything else is refused at its position |
| `keyId(id)` | the key command `id`, its keys the App's key map's: for `Shortcut.sequence` and `MenuItem.shortcut` | one non-empty string; anything else is refused at its position |

Every item's `keyScope` is readable in a handler (`field.keyScope`): its own key-map scope, or the
nearest enclosing one's, read live (an Editor's follows its mode).

### Attached properties

| written on | read by | property | values |
| --- | --- | --- | --- |
| any child of a `Window` | `Window` | `Dock.edge` | `Tui.Top`, `Tui.Bottom`, `Tui.Left`, `Tui.Right` |
| a `Button` in a `DialogButtonBox` | `DialogButtonBox` | `DialogButtonBox.buttonRole` | `DialogButtonBox.AcceptRole`, `DialogButtonBox.RejectRole`, `DialogButtonBox.DestructiveRole`, `DialogButtonBox.ActionRole` |
| a child of a `StatusBar` | `StatusBar` | `StatusBar.permanent` | `true` (the default: at the right end, Qt's `addPermanentWidget`), `false` (at the left end, `addWidget`) |

A child of a `Window` with no `Dock.edge` fills what the docked ones leave.

### Enums — the `Tui` singleton

`import tui 1.0` brings `Tui` into scope. Qt spells these `Qt.Horizontal`; here
they are `Tui.Horizontal`.

| property | values |
| --- | --- |
| `orientation` (Split), `direction` (Flex) | `Tui.Horizontal`, `Tui.Vertical` |
| `Dock.edge` | `Tui.Top`, `Tui.Bottom`, `Tui.Left`, `Tui.Right` |
| `keyset` (Editor) | `Tui.Vim`, `Tui.Nano`, `Tui.Standard` |
| `align` (Menu) | `Tui.Left`, `Tui.Right` |
| `wrapMode` (Text) | `Tui.NoWrap` (one line, default), `Tui.WordWrap` |
| `fileMode` (FileDialog) | `Tui.OpenFile`, `Tui.SaveFile` |
| `elideMode` (TableViewColumn) | `Tui.ElideRight` (default), `Tui.ElideLeft`, `Tui.ElideMiddle`, `Tui.ElideNone`, and golib's `Tui.ElidePath` (a path cut at a `/`: `…/dao/name.go`) |

### Flags — the `Dialog` singleton

Also from `import tui 1.0`, with Qt's names and Qt's values, combined with
`|` exactly as in Qt:

```qml
standardButtons: Dialog.Yes | Dialog.No
```

| flag | label | message | answer |
| --- | --- | --- | --- |
| `Dialog.Ok` | &OK | `tui.button.ok` | accepts |
| `Dialog.Save` | &Save | `tui.button.save` | accepts |
| `Dialog.Yes` | &Yes | `tui.button.yes` | accepts |
| `Dialog.No` | &No | `tui.button.no` | rejects |
| `Dialog.Cancel` | &Cancel | `tui.button.cancel` | rejects |
| `Dialog.Close` | Close(&q) | `tui.button.close` | rejects |

The underlined letter presses the button. Each label is its message, shown in the App's
language with English's letter; a `FileDialog`'s buttons are `tui.filedialog.open`, `save`,
`select` and `close`, and its footer is its view's hint, in the language too. `|` is the only operator the engine
evaluates, and only over integer flags.

**Enter answers the default, and only a named one.** `defaultButton` names the
one standard button Enter presses, as `QMessageBox::setDefaultButton` does; a
dialog that names none answers Enter with nothing, and a `DialogButtonBox`
never has a default. It must be one of the dialog's own `standardButtons`, and
one only — anything else is refused when the document is built. A destructive
question names the safe answer, or none:

```qml
Dialog { standardButtons: Dialog.Ok | Dialog.Cancel; defaultButton: Dialog.Ok }   // a login
Dialog { standardButtons: Dialog.Yes | Dialog.No;    defaultButton: Dialog.No }   // "delete it?"
```

Enter reaches the dialog only when the focused control leaves it: a
`TextField` submits (`accepted`) and lets Enter go on, as `QLineEdit` does, so a
form answers from its last field; a value its validator refuses holds Enter. A
`ComboBox`, a list or a table keeps Enter — it opens, chooses or activates.
Focus starts on the first control in Tab order: an input dialog's first field,
a message's first button. When the dialog closes, the keyboard goes back to
where it was when it opened, as Qt's does; only a close that left nothing
focused falls back to the Window's `focus: true` item.

### Palette roles

Colours are written the way a Qt Quick Control takes them — a grouped
`palette` whose members are QPalette's roles — and bound to a theme:

```qml
MenuBar { palette.window: Theme.menu.window; palette.accent: Theme.menu.accent }
```

| role | meaning | taken by |
| --- | --- | --- |
| `window`, `windowText` | a surface and the text on it | MenuBar, Frame, StatusBar, Text, Dialog, FileDialog |
| `base`, `text` | an editing or listing area and its text | Editor, FileDialog |
| `highlight`, `highlightedText` | the selected row; the focused button; a focused frame's border | MenuBar, Frame, Editor, Dialog, FileDialog |
| `accent` | a menu's access-key letter | MenuBar |
| `button`, `buttonText` | a button nobody is on | Dialog, FileDialog |
| `inactive.highlight`, `inactive.highlightedText` | the selected row of a pane not in use | FileDialog |
| `mid`, `light` | a pane's frame, without and with the keyboard | FileDialog |

**Roles propagate**, as Qt's do ("Items propagate explicit palette properties
from parents to children"): a node wears its own roles over its parent's, nearest
winning, so a `Text` in a coloured `Dialog` wears the card's colours without
naming any. `palette` is on **every** type — a `Flex` can colour a subtree it
paints nothing of — and each type wears the roles in its column above. A
misspelt role is refused by name. Propagation is live: a role that changes (a
binding, a reload) restyles what it reaches without rebuilding it, and a role a
reload removes is reset, so the node inherits its parent's again. A document
that sets no role keeps golib's own token-based look.

Colours are ANSI slot names (`"blue"`, `"brightyellow"`, `"gray"`), `"#rrggbb"`,
or `"default"` for the terminal's own. Slot names follow the user's terminal
palette; `#rrggbb` looks the same everywhere.

---

## Syntax highlighting

```qml
Window {
    syntax.keyword: Theme.syntax.keyword       // set once, inherited below
    syntax.comment: Theme.syntax.comment

    Editor {
        SyntaxHighlighter { definition: App.syntax }   // "QML", "SQL (PostgreSQL)", …, or "" for none
    }
}
```

KDE KSyntaxHighlighting's type: Go highlights, the document names a
definition, the `syntax.*` roles colour it. It highlights the Editor it is
declared in (anywhere else, the root included, is refused). `definition` is a
runtime property naming a registered definition — the vocabulary has `QML`
(`*.qml`), `JavaScript` (`*.js`, `*.mjs`), and SQL per dialect:
`SQL (PostgreSQL)` (`*.sql`, `*.pgsql`), `SQL (SQLite)` (`*.sqlite.sql`) and
`SQL (MySQL)` (`*.mysql.sql`); add your own with
`Highlighters(highlight.Definition{…})` (Program) or `WithHighlighters`
(adapter); an unknown one is refused naming the registered.

`syntax.<style>` colours one of KSyntaxHighlighting's 31 styles (package
[highlight](../../highlight/README.md)). They are palette roles: written on the
Window, every highlighter under it wears them — the Editor's, and a
FileDialog's preview, which highlights each file by the definition its name
calls for. Bound to a theme's `syntax` group, switching theme stays the import
line. A style left unset paints as `syntax.normal`, and that unset as the text.

## Raw and Rendered: `view`, not `mode`

```qml
Editor {
    view: Editor.Rendered                        // or Editor.Raw (the default)
    SyntaxHighlighter { definition: App.syntax }
    MarkdownRenderer { headingScale: 2.0 }       // what Rendered draws with; at most one renderer
    HTMLDocumentView { }                         // Rendered as a whole page, when the host says so
}
```

`view` chooses how an Editor shows its text. `Editor.Raw` is the source as written, and
`Editor.Rendered` is drawn by the Editor's renderer: Markdown with its headings at their sizes and
its marks hidden off the cursor's line. Ctrl+T (`ActToggleRendered`, rebindable) switches the two.
The terminal's Editor has no Rendered view: it accepts `view` and the renderer, so one document
runs everywhere, and stays Raw. The GUI's native style gives the Rendered view.

**The property is `view`, not `mode`, on purpose.** The Editor already has a `modeChanged`
signal, and it reports the **Vim** mode (Normal, Insert, Visual). QML reads a property `x` and a
signal `xChanged` as one pair, so a `mode` property would make `onModeChanged` look like the view
switching. Do not rename `view` to `mode`, and do not read `modeChanged` as the view (Johno,
2026-10-08; ADR 1791385086 §4.8).

`MarkdownRenderer` is a renderer spec: plain data the document declares, which a terminal build
parses without Gio. A consumer's own renderer is a spec type of its own, implementing
`RendererSpec` and declared through `RendererNode`; the GUI's native style turns a spec into a
renderer by its `RendererKind()`.

`HTMLDocumentView` declares a **document view**: a second kind of Rendered view. A renderer lays
the text out line by line, so Rendered Markdown stays editable. A document view draws the whole
document as one read-only widget, an HTML page through `HTMLView`, in place of the text. The host
turns it on for a document it reads (gui's `Editor.SetRenderedDocument(true)`, for an HTML file)
and off for any other. Rendered then shows the page:

- Ctrl+T (as the keymap binds it) returns to Raw, where the text is edited;
- the page scrolls by its own keys (`k`/`j`, `Ctrl+U`/`Ctrl+D`, `g`/`G`, the arrows and pages);
- the focus moves with whichever is shown.

The host reaches the view through `Editor.DocumentView()`, for its links (`SetOnLink`) and images
(`SetImageResolver`). It is a spec as a renderer is: the terminal's Editor carries it and draws
none. The GUI's native style builds the view by its `DocumentKind()` ("html"), and
`WithDocumentViewFor` replaces or adds one.

## Dialogs

A `Dialog` or `FileDialog` owns its whole lifecycle. A handler opens it by id;
its buttons, their underlined letters, and Escape all close it. Every way out
is one of Qt's two answers, raised before `closed`:

| way out | signals |
| --- | --- |
| an accepting button (Ok, Save, Yes, Open; `AcceptRole`) | `accepted`, then `closed` |
| a rejecting button (`RejectRole`), or Escape | `rejected`, then `closed` |
| a `DestructiveRole` button (Discard) | `closed` only |
| an `ActionRole` button (a manager's Add, Edit) | its own `clicked` only; the dialog stays open |
| `close()` from a handler | `closed` only |

**Enter the focused control leaves is the dialog's.** It presses the button
`defaultButton` names, or nothing when the dialog names none — no standard
button is a default by being one. A `DialogButtonBox` declares none, so no
irreversible answer is one stray Enter away. Space presses the focused button:

```qml
Dialog {
    Text { text: App.question }
    DialogButtonBox {
        Button { text: "S&tay";    DialogButtonBox.buttonRole: DialogButtonBox.RejectRole }
        Button { text: "&Discard"; DialogButtonBox.buttonRole: DialogButtonBox.DestructiveRole; onClicked: App.discard() }
        Button { text: "&Save";    DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole }
    }
    onAccepted: App.save()
}
```

Each button runs its own `onClicked`, then the dialog answers for its role.
These are the widget's rules (`widget.Modal`), the same for a dialog built in Go.

A `FileDialog`'s body is a [file view](../widget/README.md#file-widgets), laid
out as every picker is: the fields over the listing in the left column, the
preview on the right (`preview`, on by default when opening), and the buttons
beneath: the choice (Open, Save, Select), then Close, which `q` also presses
outside a field. `Tui.OpenFile` has no field; `Tui.SaveFile` has the name.
Ctrl+h/j/k/l move between the parts. `accepted` carries the chosen file:

```qml
FileDialog { fileMode: Tui.OpenFile; onAccepted: App.openFile(selectedFile) }
```

A `FolderDialog` (Qt 6's) chooses a folder. Its path field follows the
listing, and a path typed there lists that folder; Select takes the field's
folder, and refuses a path that is not one. Its children are fields placed
above the path, a vocabulary extension (Qt's takes none):

```qml
FolderDialog {
    title: "add a workspace"
    Frame { title: "title"; TextField { id: wsName; text: "untitled" } }
    onAccepted: App.addWorkspace(wsName.text, selectedFolder)
}
```

It lists the local disk unless the host hands the adapter another filesystem
with `WithFileSource` — any `fs.FS`, a remote one included. Paths reach the
host in rooted form: an ordinary absolute path locally.

---

## Source-language editing

```qml
Editor {
    autoIndent: true
    // indentUnit: "    " // optional override; otherwise the source provider chooses
    indentMarks: "cursorLine" // "off" (default), "cursorLine" or "all"
    SyntaxHighlighter { definition: "Go" }
}
```

Standard source definitions include Go, Rust, JavaScript, TypeScript, Python,
Lua, Shell and YAML. Markdown source definitions borrow them by fence label.
`Highlighters` registrations are finalized into one immutable catalog used by
syntax nodes, native renderer specs, file previews and `Program.SourceLanguages()`.
Custom languages therefore work for actual source files and Raw/Rendered fences
without adding language branches to those adapters.

Auto-indent applies to typed newline, Vim open-line and completed closing tokens;
literal paste is unchanged. `indentUnit` is spaces/tabs only. Find overlays refresh
paint independently of verified semantic source state.

`indentMarks` shows a line's indentation as dimmed marks, as Vim's `listchars` does:
each leading space as `·` and each leading tab as `→`. Spaces between words stay blank.
`"cursorLine"` marks the cursor's line while the editor has the focus, in any mode; `"all"`
marks every line. The native editor draws the marks in Raw mode only. The terminal
editor marks spaces only, because it does not draw tabs yet. See
[source providers](../../parse/languages/README.md).

## Window capability widgets

`WindowMod` is a single-content wrapper; appearance is optional and separate.

```qml
WindowMod {
    id: notesWindow
    key: "notes"
    label: "Notes"
    movable: true
    resizable: true
    maximizable: true
    dragModifier: WindowMod.Alt | WindowMod.Control
    moveButton: Tui.LeftButton
    resizeButton: Tui.RightButton
    onChanged: App.windowChanged(operation, key, width, height)
    Frame {
        Flex {
            direction: Tui.Vertical
            WindowButtons { target: "notesWindow" }
            Editor { Layout.fillHeight: true }
        }
    }
}
```

Default gestures are Alt/Option-left for move and Alt/Option-right for resize.
`Tui.Alt`, `Tui.Option`, `Tui.Control`, `Tui.Shift` and `Tui.NoModifier` select
single modifiers; `WindowMod` flags combine with `|`. Capabilities and bindings
are construction properties. `changed` supplies operation, key, x, y, width,
height, maximized and minimized after a completed operation, not during motion.

Methods are `moveBy(dx,dy)`, `resizeBy(dw,dh)`, `toggleMaximize()`, `minimize()`,
`restore()` and `close()`. Declarative input methods obey the target window's
scope. Hosts use the corresponding Go methods for application-owned operations.
Inject an existing pane's zoom policy with `WithWindowTargets`, keyed by its
declared ID, rather than reimplementing the owner's layout.

`minimizable: true` requires a caller-owned `WithWindowCollector` and a non-modal
target. `WindowTaskbar {}` is an optional view of a supplied `MinimizedWindows`
model. `WindowButtons` resolves its named target, or the nearest enclosing
capability widget, when mounted.

For a Drawer, `windowResize: true` selects no-grip Alt/Option resize while keeping
its edge, percentage bounds, and once-per-release `resized(size,length)` signal.
`resizable: true` alone keeps the legacy corner grip. A Drawer may also opt into
maximize, non-modal minimize, and close; its adapter explicitly supplies its
existing `close()` method as the owner close handler. Existing Drawer open/close
methods and modal behavior remain compatible.

Drawers expose `resizeBy(widthDelta,heightDelta)` as a pointer-independent path;
it uses the configured inner-corner handle, so right/bottom-pinned panels can grow.
Applications removing grips should bind resize keys/commands for terminals that
reserve modifier-click.

`movable: true` lets a Drawer leave its edge: Alt/Option-left drag (`moveButton`,
which must differ from `resizeButton`) and `moveBy(dx,dy)` move it. While a move or
resize is in progress the panel shows where it goes; when it ends, the Drawer floats
at that rectangle, kept as percentages of the Window (to a hundredth of a percent, so a
one-cell step on a wide Window is kept) so a resize of the Window keeps it in proportion,
and raises `placed(x, y, width, height)` with them, once. A host
keeping it binds `floating`, `floatX`, `floatY`, `floatWidth` and `floatHeight` to
what `placed` reported; `floating: false` docks it at its edge again. Escape during
a drag restores the panel as it was, docked or floating, and raises nothing. A
floating Drawer raises `placed`, not `resized`; a Drawer that is not movable keeps
the docked behaviour above exactly.

```qml
Drawer {
    modal: false; movable: true; windowResize: true; edge: Tui.Left; size: 30
    floating: App.floating
    floatX: App.x; floatY: App.y; floatWidth: App.w; floatHeight: App.h
    onPlaced: App.panelPlaced(x, y, width, height)
    Frame { title: "explorer"; TreeView { model: App.files } }
}
```

## Your own widgets

A widget type is one value, `Type` — name, builder, constructor props,
setters, methods, signal parameters and a destroy hook — the same value the
standard vocabulary is a table of. `WithTypes` (or `Types` for a Program) adds
it; `Instance(name, widget)` places a widget the host already built. Helpers
read values as the built-ins do: `StringSetter`, `BoolSetter`, `NumberSetter`,
`ColorSetter`, `EnumSetter`, `NoArgMethod`, and `ReadProps` with `StringField`,
`BoolField`, `NumberField`, `ColorField`, `EnumField`. UI text is read with
`UITextSetter` and `UITextField` into a `UIText` — plain text or a catalog message — so
`qsTrId` works on a custom widget's properties as on the built-in ones. A type can declare Qt-style
enums (`Enums`), wear palette roles (`Restyle`, reading a `Palette`), and open
over the screen (`Overlaid`). A handler bound to a signal a type does not raise
is refused. Package [`controls`](controls/) — `TextField`, `Popup` — is written
with this contract alone. See [USAGE.md §6](USAGE.md#6-your-own-go-widgets-in-qml).

## Program

| option | purpose |
| --- | --- |
| `Layout(fs, file)` / `LayoutSource(name, src)` | the document |
| `Singleton(module, version, exports…)` | a module every document imports |
| `Sources(map)` | state, as Go values |
| `Commands(map)` / `Handlers(map)` | handlers without / with arguments |
| `Providers(p…)` | values on their own clock |
| `Themes(fs, dir, prefix, version)` | each `dir/*.qml` offered as `prefix.<name>` |
| `themes.Offer()` (package `tui/decl/themes`) | the toolkit's ten themes — `dark`, `light`, `mono`, `retro`, `sepia`, `catppuccin_frappe`, `kanagawa`, `nord`, `shades_of_purple`, `tokyo_night` — as `tui.theme.<name>` |
| `Components(fs, dir, module, version)` | `dir/*.qml` offered as component types |
| `Offer(module, version, loader)` | any offered module |
| `Types(t…)` / `Files(src)` | your widgets; the file dialogs' filesystem |
| `ErrorSink(fn)` | handler errors as they happen (default: kept, returned by Run) |
| `Translations(fs, dir, prefix)` | the program's catalogs, `dir/<prefix>_<language>.xml` in Qt's TS XML, layered over golib's; the language is the App's (`AppOptions(tui.WithLanguage(tag))`, then `App().SetLanguage(tag)`) — see [USAGE.md §7c](USAGE.md#7c-languages) |
| `KeyCommands(cmd…)` | the program's key commands (`keymap.Command`: an id, a scope, default keys), beside golib's — what `keyId` names and a scheme may rebind |
| `Keymap(fs, file)` | a key-mapping scheme layer: `file`, Qt Creator's keyboard-mapping-scheme XML; layers apply in order; a missing file is an empty layer — see [USAGE.md §7d](USAGE.md#7d-key-maps) |
| `HotReload(opts…)` | follow the files while running: `ReloadInterval`, `OnReload`, `OnReloadError` — see [USAGE.md §8](USAGE.md#8-reloading) |
| `AppOptions`, `AdapterOptions`, `TreeOptions`, `WithRegistry` | pass-throughs |

**The toolkit's themes.** `import tui.theme.dark 1.0` (or `light`, `mono`,
`retro`, `sepia`, `catppuccin_frappe`, `kanagawa`, `nord`, `shades_of_purple`,
`tokyo_night`) after `themes.Offer()` gives a `Theme` singleton in five groups — `app`
(the palette the Window sets and everything inherits), `menu`, `document` (an
editor, a table, a tree), `status`, and `syntax` (the `syntax.*` roles). Every
theme defines every key, so a layout bound to one runs under any. They are
AutoDB's themes and vocabulary; a program that needs a colour of its own starts
from `themes.FS()` and offers its copies with `Themes` under its own name. `themes.Values(name)` is a theme's
colours by dotted name (`"app.inactive.highlight"` → `"brightblack"`), for a
program handing the theme to something outside its layout.

Methods: `Run`, `Quit`, `Post` (any goroutine), `Set`, `SetMany`, `Find`,
`FindAs[W]`, `Call`, `Reload`, `KeyScope(id)` and `KeyBindings(id)` (the key-map scope and the
bindings that reach a component, on the loop goroutine), and `Tree`, `Adapter`, `App`, `Root`. `Value(v)`
converts a Go value, a `tui.Message` included; `Arg(args, i)` reads a handler argument.

## Testing

`Check(opts…)` is qmllint for a program: it takes `NewProgram`'s options and
mounts the layout, the layout under each alternative module (the theme it does
not import), and each component no document uses yet — imported module or not —
and reports every problem. Package [`decltest`](decltest/) wraps it for `go test` —
`decltest.Check(t, opts…)` — and runs a program on a test backend:
`decltest.Run(t, w, h, opts…)`. `CheckLanguages(opts…)` (`decltest.CheckLanguages(t,
opts…)`) is the language walk: every language the catalogs hold, golib's and the program's,
judged for messages English lacks, markers on another letter than English's, and menu rows
of one level sharing a key. `CheckKeymap(opts…)` (`decltest.CheckKeymap(t, opts…)`) is the key-map
check: what `keymap.Build` refuses, a `keyId` in the wrong holder, a fixed sequence on a command's key
in its scope, and a global key taking a menu's Alt+letter; a key an inner scope shares with an outer
one is a warning, logged (`decltest.CheckKeymapStrict` fails on it). See
[USAGE.md §11](USAGE.md#11-testing-a-qml-screen).

## Adapter options

For building the adapter by hand — a QML screen that is part of a Go program.

| option | purpose |
| --- | --- |
| `WithErrorSink(fn)` | where a handler's error goes — **required** once a document binds a handler |
| `WithSetters(type, map)` | runtime properties of a type |
| `WithConstructorProps(type, names…)` | properties a type takes only when built |
| `WithMethods(type, map)` | methods a handler may call by id: `x.open()` |
| `WithSignalParams(type, map)` | a signal's parameter names: `accepted(selectedFile)` |
| `WithFileSource(src)` | the filesystem every `FileDialog` lists |
| `WithOverlay(host)` | where a dialog outside any `Window` opens — a Go program's own `OverlayHost` (USAGE §7) |
| `WithTypes(t…)` | your own widget types |
| `WithDestroyHook(type, fn)` | what runs when a node of a type is destroyed |


## Licence

See the repository's [LICENSE](../../LICENSE).
