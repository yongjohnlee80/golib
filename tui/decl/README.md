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
| `MenuItem` | `text`, `checkable`, `group`, `shortcut` | `checked`, `enabled` | `triggered` | — |
| `MenuSeparator` | — | — | — | — |
| `Shortcut` | `sequence` | — | `activated` | — |
| `Frame` | `title`, palette | — | — | — |
| `Editor` | `text`, `wrap`, palette | `keyset`, `readOnly` | `modeChanged`, `textChanged` | — |
| `StatusBar` | palette | `left`, `center`, `right` | — | — |
| `Text` | `wrapMode`, palette | `text` | — | — |
| `Button` | — | `label`, `enabled` | `clicked` | — |
| `Split` | `orientation` | — | — | — |
| `Flex` | `direction` | — | — | — |
| `Dialog` | `title`, `helpText`, `dim`, `standardButtons`, palette | — | `accepted`, `rejected`, `closed` | `open()`, `close()` |
| `FileDialog` | `title`, `helpText`, `dim`, `fileMode`, `preview`, palette | `currentFolder`, `selectedFile` | `accepted(selectedFile)`, `rejected`, `closed` | `open()`, `close()` |

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
