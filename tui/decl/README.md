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

| Type | ctor properties | setters | signals | methods |
| --- | --- | --- | --- | --- |
| `Window` | — | — | — | — |
| `MenuBar` | `vimNavigation`, palette | — | — | — |
| `Menu` | `title`, `align` | — | — | — |
| `MenuItem` | `text`, `checkable`, `group`, `shortcut` | `checked`, `enabled`, `visible` | `triggered` | — |
| `MenuSeparator` | — | — | — | — |
| `Shortcut` | `sequence` | — | `activated` | — |
| `Frame` | palette | `title` | — | — |
| `Editor` | `wrap`, palette | `text`, `keyset`, `readOnly` | `modeChanged`, `textChanged` | — |
| `StatusBar` | palette | `left`, `center`, `right` | — | — |
| `Text` | `wrapMode`, palette | `text` | — | — |
| `Button` | — | `text`, `enabled` | `clicked` | — |
| `Split` | `orientation` | `ratio` | — | — |
| `Flex` | `direction` | — | — | — |
| `Dialog` | `dim`, `width`, `standardButtons`, palette | `title`, `helpText` | `opened`, `accepted`, `rejected`, `closed` | `open()`, `close()` |
| `ListView` | `textRole` | `model`, `currentIndex` | `activated(index)`, `currentIndexChanged(index)` | — |
| `ComboBox` | `textRole`, `valueRole`, `placeholderText` | `model` | `activated(index)` | — |
| `TableView` | — | `model`, `currentIndex` | `activated(index)`, `currentIndexChanged(index)` | — |
| `TableViewColumn` | `role`, `title`, `width` | — | — | — |
| `TreeView` | `textRole`, `badgeRole` | `model` (a tree model) | `activated(index)`, `expanded(index)` — an `Index` | — |
| `FileDialog` | `title`, `helpText`, `dim`, `fileMode`, `preview`, palette | `currentFolder`, `selectedFile` | `accepted(selectedFile)`, `rejected`, `closed` | `open()`, `close()` |

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
passes back to the host.

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
equals the row's value of that role (numbers compare by value). A choice with
no `roleValue` matches every row, and a row no choice matches has no delegate.
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

**Every element that takes a place on screen has `visible`** — Qt's
`Item.visible`, a runtime property: hidden, it takes no space, is not painted
or a tab stop, and hides what is under it; removed by a reload, it is shown
again.

**Every element except `Window`, `Frame`, `Split`, `Flex` and `Dialog` is
childless or holds only its own kind** — `MenuBar` holds `Menu`s, a `Menu`
holds `MenuItem`s, `Menu`s and `MenuSeparator`s. `Frame` and `Dialog` hold
exactly one child; `Split` exactly two.

### Attached properties

| written on | read by | property | values |
| --- | --- | --- | --- |
| any child of a `Window` | `Window` | `Dock.edge` | `Tui.Top`, `Tui.Bottom`, `Tui.Left`, `Tui.Right` |

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
| an accepting button (Ok, Save, Yes, Open) | `accepted`, then `closed` |
| a rejecting button, or Escape | `rejected`, then `closed` |
| `close()` from a handler | `closed` only |

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
