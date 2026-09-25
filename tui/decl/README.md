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
a complete text editor whose every screen is QML.

---

## The vocabulary

`StdRegistry()` builds all of these; `StdProperties()` is their property
contract, handed to `New`. A **ctor** property is read when the widget is
built — changing it on a reload rebuilds that node. A **setter** property can
change at runtime, which is what makes it bindable to a source.

| Type | ctor properties | setters | read in a handler | signals | methods |
| --- | --- | --- | --- | --- | --- |
| `Window` | — | — | — | — | — |
| `MenuBar` | `vimNavigation`, palette | — | — | — | — |
| `Menu` | `title`, `align` | — | — | — | — |
| `MenuItem` | `text`, `checkable`, `group`, `shortcut` | `checked`, `enabled`, `visible` | `checked` | `triggered`, `toggled` | — |
| `MenuSeparator` | — | — | — | — | — |
| `Shortcut` | `sequence` | — | — | `activated` | — |
| `Frame` | palette | `title` | — | — | — |
| `Editor` | `wrap`, palette | `text`, `keyset`, `readOnly` | — | `modeChanged`, `textChanged` | — |
| `SyntaxHighlighter` | `definition` | — | — | — | — |
| `StatusBar` | palette | `left`, `center`, `right` | — | — | — |
| `Text` | `wrapMode`, palette | `text` | — | — | — |
| `Button` | — | `text`, `enabled` | — | `clicked` | — |
| `Split` | `orientation` | `ratio` | — | — | — |
| `Flex` | `direction` | — | — | — | — |
| `Dialog` | `dim`, `width`, `standardButtons`, `defaultButton`, palette | `title`, `helpText` | — | `opened`, `accepted`, `rejected`, `closed` | `open()`, `close()` |
| `DialogButtonBox` | — (its Buttons carry `DialogButtonBox.buttonRole`) | — | — | — | — |
| `ListView` | `textRole` | `model`, `currentIndex` | `currentIndex` | `activated(index)`, `currentIndexChanged(index)` | — |
| `ComboBox` | `textRole`, `valueRole`, `placeholderText` | `model`, `currentIndex` | `currentIndex`, `currentValue` | `activated(index)` | — |
| `TableView` | — | `model`, `currentIndex` | `currentIndex` | `activated(index)`, `currentIndexChanged(index)` | — |
| `TableViewColumn` | `role`, `title`, `width` | — | — | — | — |
| `TreeView` | `textRole`, `badgeRole` | `model` (a tree model) | — | `activated(index)`, `expanded(index)` — an `Index` | `toggleExpanded(index)` |
| `FileDialog` | `title`, `helpText`, `dim`, `fileMode`, `preview`, palette | `currentFolder`, `selectedFile` | — | `accepted(selectedFile)`, `rejected`, `closed` | `open()`, `close()` |
| `Repeater`, `Instantiator` | `model` | — | — | — | — |
| `DelegateChooser` | `role` | — | — | — | — |
| `DelegateChoice` | `roleValue` | — | — | — | — |

`Repeater`, `Instantiator`, `DelegateChooser` and `DelegateChoice` are the
engine's, expanded before anything is built, so they have no widget.
`TextField` and `Popup` are Qt Quick Controls types in the
[`controls`](controls/) package, which a program adds with
`tuidecl.Types(controls.Types()...)`: `TextField` (`placeholderText`,
`echoMode`; setter and readable `text`; `accepted(text)`, `textEdited(text)`;
`clear()`) and `Popup` (`modal`, `dim`; `open()`, `close()`). A `MenuItem`'s
`toggled` is raised for each change the user's toggle makes to it, a radio
cleared by its group included, before its `triggered`.

**`Shortcut`** — Qt's key sequence: modifiers joined by `+` (`Ctrl`, `Alt`,
`Shift`) and a key name (`Esc`, `F10`, `Space`…) or one character. A letter's
case is its Shift, as in Qt: `"c"` or `"C"` is the key c, and `"Shift+C"` is the
capital, whether a terminal delivers it as `C` or as Shift and c. With Ctrl or
Alt, case is not relied on (`Ctrl+Q` matches however it arrives).

**`Flex`** — Qt's `ColumnLayout` / `RowLayout`. Each child takes its own size,
in order; a child marked `Layout.fillHeight: true` (in a column) or
`Layout.fillWidth: true` (in a row) shares what the others leave. A view — a
`ListView`, `TableView` or `TreeView` — is as tall as its content where nothing
bounds it, and fills what it is given otherwise, so a table followed by a row of
buttons is written:

```qml
Flex { direction: Tui.Vertical
    TableView { model: App.rows; Layout.fillHeight: true }
    Flex { direction: Tui.Horizontal; Button { text: "&Add" } } }
```

A row or column is as wide across as its widest child unless its parent fixes
that extent, when it stretches its children across it.

**Models** — Qt's model/view. A host sets a `tuidecl.ListModel` (or its own
`tuidecl.ItemModel`) as a source; `model: App.people` binds a view to it, and
the view follows the model's changes itself — insert, remove, reset — with
nothing rebound. Roles are typed (a string, bool or number). A handler reads a
view by its id when it runs: `listView.currentIndex`, `combo.currentValue`
(the chosen row's `valueRole`). A view drops its subscription when the model
is replaced and when the view is destroyed.

A `TableView` shows the model's own columns — a query's result, whatever
columns it has, redrawn on the same view when they change — unless it declares
`TableViewColumn`s. A `TreeView` shows a tree model (`tuidecl.TreeListModel`,
or a `TreeModel` of the host's): a row opens and, the first time, the view asks
the model to load its children (`OnFetch`), which the host sets with
`SetChildren`. Its signals carry the row's `tuidecl.Index`, which the handler
passes back to the host. Enter activates any row, a branch as well as a leaf,
as Qt's item views do; `l`/Right opens a row and `h`/Left closes it. A host
that decides an activated row is a folder opens it with the view's
`toggleExpanded(index)` — `Program.Call(id, "toggleExpanded", index)` — and
uses any other row as it means.

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

**Every element except `Window`, `Frame`, `Split`, `Flex`, `Dialog` and the
engine's `Repeater`, `Instantiator`, `DelegateChooser` and `DelegateChoice` is
childless or holds only its own kind** — `MenuBar` holds `Menu`s, a `Menu`
holds `MenuItem`s, `Menu`s and `MenuSeparator`s. `Frame` holds exactly one child, and
`Split` exactly two. A `Dialog` holds exactly one content child, plus its own
`Shortcut`s and at most one `DialogButtonBox` (which it may not combine with
`standardButtons`).

### Attached properties

| written on | read by | property | values |
| --- | --- | --- | --- |
| any child of a `Window` | `Window` | `Dock.edge` | `Tui.Top`, `Tui.Bottom`, `Tui.Left`, `Tui.Right` |
| a `Button` in a `DialogButtonBox` | `DialogButtonBox` | `DialogButtonBox.buttonRole` | `DialogButtonBox.AcceptRole`, `DialogButtonBox.RejectRole`, `DialogButtonBox.DestructiveRole` |

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

### Flags — the `Dialog` singleton

Also from `import tui 1.0`, with Qt's names and Qt's values, combined with
`|` exactly as in Qt:

```qml
standardButtons: Dialog.Yes | Dialog.No
```

| flag | label | answer |
| --- | --- | --- |
| `Dialog.Ok` | &OK | accepts |
| `Dialog.Save` | &Save | accepts |
| `Dialog.Yes` | &Yes | accepts |
| `Dialog.No` | &No | rejects |
| `Dialog.Cancel` | &Cancel | rejects |
| `Dialog.Close` | C&lose | rejects |

The underlined letter presses the button. `|` is the only operator the engine
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
        SyntaxHighlighter { definition: App.syntax }   // "QML", "JavaScript", or "" for none
    }
}
```

KDE KSyntaxHighlighting's type: Go highlights, the document names a
definition, the `syntax.*` roles colour it. It highlights the Editor it is
declared in (anywhere else, the root included, is refused). `definition` is a
runtime property naming a registered definition — the vocabulary has `QML`
(`*.qml`) and `JavaScript` (`*.js`, `*.mjs`); add your own with
`Highlighters(highlight.Definition{…})` (Program) or `WithHighlighters`
(adapter); an unknown one is refused naming the registered.

`syntax.<style>` colours one of KSyntaxHighlighting's 31 styles (package
[highlight](../../highlight/README.md)). They are palette roles: written on the
Window, every highlighter under it wears them — the Editor's, and a
FileDialog's preview, which highlights each file by the definition its name
calls for. Bound to a theme's `syntax` group, switching theme stays the import
line. A style left unset paints as `syntax.normal`, and that unset as the text.

## Dialogs

A `Dialog` or `FileDialog` owns its whole lifecycle. A handler opens it by id;
its buttons, their underlined letters, and Escape all close it. Every way out
is one of Qt's two answers, raised before `closed`:

| way out | signals |
| --- | --- |
| an accepting button (Ok, Save, Yes, Open; `AcceptRole`) | `accepted`, then `closed` |
| a rejecting button (`RejectRole`), or Escape | `rejected`, then `closed` |
| a `DestructiveRole` button (Discard) | `closed` only |
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

A `FileDialog`'s body is a [file view](../widget/README.md#file-widgets): a
folder listing with an optional preview (`fileMode: Tui.OpenFile`) or a name
field over one (`Tui.SaveFile`). `accepted` carries the chosen file:

```qml
FileDialog { fileMode: Tui.OpenFile; onAccepted: App.openFile(selectedFile) }
```

It lists the local disk unless the host hands the adapter another filesystem
with `WithFileSource` — any `fs.FS`, a remote one included. Paths reach the
host in rooted form: an ordinary absolute path locally.

---

## Your own widgets

A widget type is one value, `Type` — name, builder, constructor props,
setters, methods, signal parameters and a destroy hook — the same value the
standard vocabulary is a table of. `WithTypes` (or `Types` for a Program) adds
it; `Instance(name, widget)` places a widget the host already built. Helpers
read values as the built-ins do: `StringSetter`, `BoolSetter`, `NumberSetter`,
`ColorSetter`, `EnumSetter`, `NoArgMethod`, and `ReadProps` with `StringField`,
`BoolField`, `NumberField`, `ColorField`, `EnumField`. A type can declare Qt-style
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
| `Components(fs, dir, module, version)` | `dir/*.qml` offered as component types |
| `Offer(module, version, loader)` | any offered module |
| `Types(t…)` / `Files(src)` | your widgets; the file dialogs' filesystem |
| `ErrorSink(fn)` | handler errors as they happen (default: kept, returned by Run) |
| `HotReload(opts…)` | follow the files while running: `ReloadInterval`, `OnReload`, `OnReloadError` — see [USAGE.md §8](USAGE.md#8-reloading) |
| `AppOptions`, `AdapterOptions`, `TreeOptions`, `WithRegistry` | pass-throughs |

Methods: `Run`, `Quit`, `Post` (any goroutine), `Set`, `SetMany`, `Find`,
`FindAs[W]`, `Call`, `Reload`, and `Tree`, `Adapter`, `App`, `Root`. `Value(v)`
converts a Go value; `Arg(args, i)` reads a handler argument.

## Testing

`Check(opts…)` is qmllint for a program: it takes `NewProgram`'s options and
mounts the layout, the layout under each alternative module (the theme it does
not import), and each component no document uses yet — imported module or not —
and reports every problem. Package [`decltest`](decltest/) wraps it for `go test` —
`decltest.Check(t, opts…)` — and runs a program on a test backend:
`decltest.Run(t, w, h, opts…)`. See [USAGE.md §11](USAGE.md#11-testing-a-qml-screen).

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
