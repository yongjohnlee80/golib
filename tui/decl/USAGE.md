# tui/decl — usage guide

How to build a golib/tui program whose screens are QML, how to put your own Go
widgets into those screens, how to put a QML screen inside a Go-built one, and
the rules that keep a program doing both correct.

The reference — every type, property, enum and palette role — is
[README.md](README.md). The complete worked program is
[`tui/examples/editor-qml`](../examples/editor-qml/README.md); every snippet
below is a trimmed piece of it.

1. [The shape of a host](#1-the-shape-of-a-host)
2. [State and commands — the App singleton](#2-state-and-commands--the-app-singleton)
3. [Modules, themes and component files](#3-modules-themes-and-component-files)
4. [Dialogs](#4-dialogs)
5. [File dialogs, local or remote](#5-file-dialogs-local-or-remote)
6. [Your own Go widgets in QML](#6-your-own-go-widgets-in-qml)
7. [A QML screen inside a Go program](#7-a-qml-screen-inside-a-go-program)
   - [7b. Syntax highlighting](#7b-syntax-highlighting)
8. [Reloading](#8-reloading)
9. [Using both safely — the rules](#9-using-both-safely--the-rules)
10. [Best practice](#10-best-practice)
11. [Testing a QML screen](#11-testing-a-qml-screen)

---

## 1. The shape of a host

`NewProgram` builds everything a QML app needs, in the order it has to be
built, and `Run` runs it:

```go
//go:embed editor.qml themes dialogs
var files embed.FS

func run(ctx context.Context) error {
    backend, err := term.Open()
    if err != nil {
        return err
    }
    var p *tuidecl.Program
    p, err = tuidecl.NewProgram(
        tuidecl.Layout(files, "editor.qml"),               // diagnostics say editor.qml:12:5
        tuidecl.Singleton("editor", "1.0", "App"),         // `import editor 1.0` → App
        tuidecl.Sources(map[string]any{"App.status": "ready", "App.count": 0}),
        tuidecl.Commands(map[string]func() error{
            "App.save": save,
            "App.quit": func() error { p.Quit(); return nil },
        }),
        tuidecl.Handlers(map[string]decl.HandlerFunc{     // commands that take arguments
            "App.openFile": func(args []qml.SpecValue) error {
                path, err := tuidecl.Arg(args, 0)
                if err != nil {
                    return err
                }
                return open(path)
            },
        }),
        tuidecl.Providers(clock),                          // values on their own clock
        tuidecl.Themes(files, "themes", "editor.theme", "1.0"),
        tuidecl.Components(files, "dialogs", "editor.dialogs", "1.0"),
        tuidecl.Types(gauge),                              // your own widgets, §6
        tuidecl.AppOptions(tui.WithBackend(backend)),
    )
    if err != nil {
        return err
    }
    return p.Run(ctx)
}
```

What it wires: an adapter over the standard vocabulary plus your types; a tree
whose scheduler posts to the App's loop (buffering the moment before the App
exists); every module declared or offered; sources and handlers injected; the
layout parsed under its file name and mounted; the App built around the root;
and the tree torn down when `Run` returns.

**Handler errors are never dropped.** With no `ErrorSink`, the Program keeps
them and `Run` returns them joined with its own error. Give `ErrorSink` to show
them as they happen.

What a Program gives back:

| call | does | where |
| --- | --- | --- |
| `Run(ctx)` / `Quit()` | runs until ctx ends or Quit; tears down | Quit: anywhere |
| `Post(fn)` | runs fn on the UI loop | **any goroutine** |
| `Set(name, v)` / `SetMany(m)` | moves sources; bindings follow | UI loop |
| `Find(id)` / `FindAs[W](p, id)` | the widget a document declared | UI loop |
| `Call(id, method, args…)` | a method by id, as a handler's `x.open()` | UI loop |
| `Reload(src)` | reconciles against a new document | UI loop |
| `Tree()`, `Adapter()`, `App()`, `Root()` | what it built, for anything else | — |

`NewProgram` is sugar over `tuidecl.New`, `decl.New` and `tui.NewApp`, not a
second way in. Building those by hand is still supported — the pieces are
public, and [the adapter options](README.md#adapter-options) are the same.

Keep a larger host's construction a list of steps, each a method in the file
that owns that concern — the example's `New` is one `NewProgram` over
`h.modules()`, `h.state()`, `h.commands()` and a clock.

## 2. State and commands — the App singleton

Publish your program as **one singleton** a module exports. Everything the
document can reach of your program is then under one name, and nothing else of
it is reachable at all.

**State is a source.** A binding that reads one is re-evaluated when it moves,
and only those bindings are:

```go
tuidecl.Sources(map[string]any{"App.mode": "NORMAL"})     // at construction
// later, on the UI goroutine:
p.Set("App.mode", "INSERT")
p.SetMany(map[string]any{"App.keyset": "nano", "App.status": "switched"})
// from any other goroutine:
p.Post(func() { _ = p.Set("App.status", "build finished") })
```

```qml
StatusBar { left: App.mode }
Editor    { keyset: App.keyset }     // a bound setter: the editor follows the source
```

**Commands are handlers.** Keep them in one table, so the table IS the list of
what a document can do:

```go
func (h *Host) commands() map[string]decl.HandlerFunc {
    return map[string]decl.HandlerFunc{
        "App.saveFile": none(h.saveFile),
        "App.openFile": onePath("App.openFile", h.openFile),   // App.openFile(selectedFile)
        "App.quit":     none(func() error { h.p.Quit(); return nil }),
    }
}
```

`none` and `onePath` are the example's own two-line helpers (a Program's
`Commands` option does what `none` does): they refuse arguments a command does
not take, so a document calling `App.quit("now")` is told so rather than
silently obeyed.

**A value that changes on its own clock is a provider** — a clock, a file
watcher, a socket. It delivers from its own goroutine; the engine routes it
through the scheduler onto the UI goroutine, and drops a delivery older than
one it already applied:

```go
tuidecl.Providers(clock)   // clock implements decl.Provider: Subscribe(fn func(decl.Update)) …
```

**Signal parameters** reach a command as arguments, by the name the signal
declares:

```qml
FileDialog { onAccepted: App.openFile(selectedFile) }
```

## 3. Modules, themes and component files

**Declare** a module the document always needs (`App`). **Offer** one it may or
may not import — the engine loads an offered module the first time a document
imports it, and never otherwise:

```go
tuidecl.Singleton("editor", "1.0", "App")                        // declared
tuidecl.Themes(files, "themes", "editor.theme", "1.0")           // themes/retro.qml → editor.theme.retro
tuidecl.Components(files, "dialogs", "editor.dialogs", "1.0")    // dialogs/QuitDialog.qml → QuitDialog
tuidecl.Offer("editor.extras", "1.0", myLoader)                  // any decl.ModuleLoader
```

**A theme is a QML file of constants**, loaded with `decl.ValueModule`:

```qml
// themes/retro.qml
Theme {
    menu   { window: "#aaaaaa"; windowText: "#000000"; accent: "#aa0000" }
    editor { base: "#0000aa"; text: "#ffff55" }
}
```

The layout binds palette roles to it and names no colour. Switching theme is
the import line alone — `import editor.theme.retro 1.0` → `…mono 1.0` — and
only the imported theme is ever read. Importing two themes that both export
`Theme` is refused: that is an ambiguity, not a choice.

**A component file is a type named for the file**:

```qml
// dialogs/QuitDialog.qml
Dialog {
    title: "Quit"
    standardButtons: Dialog.Yes | Dialog.No
    onAccepted: App.quit()
    Text { text: App.quitQuestion; wrapMode: Tui.WordWrap }
}
```

```qml
// editor.qml
import editor.dialogs 1.0
Window { QuitDialog { id: quitDialog } }
```

A use is expanded with Qt's rules: the use site's properties replace the
component's, handlers from both run, the use site's children follow, and the use
site gives the id. **A component file imports nothing** — its names resolve in
the document that uses it, as an inline component's do — which is what lets one
theme line in the layout dress every file. **It declares no ids**, since one
would be declared once per use.

## 4. Dialogs

A dialog opens by id, from a handler, and closes itself:

```qml
MenuItem { text: "E&xit"; onTriggered: quitDialog.open() }
```

Its buttons, their underlined letters and Escape all close it, and each is one
of two answers — `accepted` or `rejected` — before `closed`. **Write what an
answer does, never how the dialog goes away.** A dialog opened by a bound
`visible` would close itself on Escape and leave the binding claiming it was
open; every host would have to write the reset, and the one that forgot would
have a dialog that never opens again.

`dim: false` leaves the screen behind undimmed — right for a question about what
is on screen ("Are you sure to quit?"), wrong for a dialog that should have the
user's whole attention.

**When the host decides whether a dialog opens**, the host opens it — a
document cannot say "if":

```go
// Save asks for a name only when the buffer has none.
func (h *Host) saveFile() error {
    if h.path == "" {
        return h.p.Call("saveDialog", "open")
    }
    return h.write(h.path)
}
```

## 5. File dialogs, local or remote

```qml
FileDialog {
    title: "Open"
    fileMode: Tui.OpenFile          // Tui.SaveFile for a name field over the listing
    currentFolder: App.folder       // where it opens
    selectedFile: App.path          // where Save As starts
    preview: false                  // on by default for OpenFile
    onAccepted: App.openFile(selectedFile)
}
```

Opening a folder is not a choice — Enter or Open on one goes into it. The footer
follows the keyboard; Tab moves between the listing, the preview and the
buttons.

It lists through a `widget.FileSource`: any `io/fs.FS` and the root its paths
are written under. The local disk is the default. **To browse something else,
give the adapter a source** — nothing in the QML changes:

```go
tuidecl.Files(widget.FileSource{FS: sftpFS, Root: "sftp://host/"})   // a NewProgram option
// or, building the adapter by hand: tuidecl.WithFileSource(…)
```

`selectedFile` then arrives as `sftp://host/path/to/file`. The dialog only
chooses; reading and writing the file is the host's, through whatever it has
for that store.

## 6. Your own Go widgets in QML

A widget type is ONE value, `tuidecl.Type` — the same value the built-in
vocabulary is a table of — so yours binds, reloads, is called by id and raises
signals exactly as `Editor` does.

```go
gauge := tuidecl.Type{
    Name: "Gauge",
    Build: func(b tuidecl.Build) (tui.Component, []string, error) {
        g := mywidgets.NewGauge()
        var max float64 = 100
        consumed, err := tuidecl.ReadProps(b.Props, map[string]tuidecl.Field{
            "maximum": tuidecl.NumberField(&max),   // a constructor-only property
        })
        if err != nil {
            return nil, nil, err
        }
        g.SetMaximum(max)
        b.EmitterWith("overflowed")                 // see Signals below
        return g, consumed, nil
    },
    Ctor: []string{"maximum"},
    Setters: map[string]tuidecl.Setter{
        "value": tuidecl.NumberSetter((*mywidgets.Gauge).SetValue),
        "label": tuidecl.StringSetter((*mywidgets.Gauge).SetLabel),
        "color": tuidecl.ColorSetter((*mywidgets.Gauge).SetColor),
    },
    Methods:   map[string]tuidecl.Method{"reset": tuidecl.NoArgMethod((*mywidgets.Gauge).Reset)},
    Signals:   map[string][]string{"overflowed": {"value"}},
    Destroyed: func(c tui.Component) { c.(*mywidgets.Gauge).StopSampling() },
}

tuidecl.NewProgram(…, tuidecl.Types(gauge))
// or, building the adapter by hand:
tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(), tuidecl.WithTypes(gauge))...)
```

```qml
Gauge {
    id: cpu
    maximum: 100
    label: "CPU"
    value: App.cpu                       // bound: follows the source
    onOverflowed: App.warn(value)        // the signal's parameter
}
MenuItem { text: "&Reset"; onTriggered: cpu.reset() }
```

What each field is for:

| field | put here | why |
| --- | --- | --- |
| `Build` | construction; read constructor props with `ReadProps` | returns what it consumed, so the engine does not apply it twice |
| `Ctor` | properties only the builder takes | a reload that changes one rebuilds the node instead of failing |
| `Setters` | properties that can change | only these can be bound to a source |
| `Methods` | what a handler may call by id | checked when the document mounts, not when the button is pressed |
| `Signals` | parameter names, in raise order | a handler names them; `b.EmitterWith(sig)(values…)` raises them |
| `Destroyed` | releasing what the widget holds | runs when a reload drops the node, or the tree is torn down |
| `Enums` | the enumerations its properties take | published as Qt spells them — `Gauge.Dial` — and read with `EnumField` / `EnumSetter` |
| `Restyle` | how it wears a palette | called with the effective `Palette` whenever it changes — its own roles or a parent's |

A signal with no parameters needs no `Signals` entry: `b.Emitter("clicked")`
returns a `func()` to hand the widget as its callback, and a no-op when the
document bound nothing to it. **A builder asks for every signal its widget
raises, at construction** — a handler the document binds to a signal the
builder never asked for is refused, as Qt refuses `onFoo` on a type with no
`foo`, rather than left never to fire.

**Enumerations.** Qt writes an enum as the type that defines it, a dot, the
value. Declare one and read it:

```go
var mode = tuidecl.Enum{Scope: "Gauge", Values: []string{"Bar", "Dial"}}
tuidecl.Type{Name: "Gauge", Enums: []tuidecl.Enum{mode},
    Setters: map[string]tuidecl.Setter{"style": tuidecl.EnumSetter(mode, (*Gauge).SetStyle)}, …}
```

```qml
Gauge { style: Gauge.Dial }
```

A scope that is already a singleton — `Tui`, `Dialog`, another type's — panics
at `New`.

**Palettes.** A type's subtree inherits palette roles through it whatever it
does; to WEAR them, give it `Restyle`. It receives the node's effective
palette — `p.Look(tuidecl.RoleBase, tuidecl.RoleText)`, `p.Color(role)` — and an
empty one must leave the widget as golib draws it.

**Widgets that open over the screen** — a popup, a prompt — implement
`Overlaid` (`SetOverlay(host, afterClose)`). A Window does not lay them out; it
hands each its overlay when it arranges its children. Outside a Window,
`Build.Overlay` is the adapter's `WithOverlay` host. A child that should be
focused when a modal surface opens is found through `tui.Container`, so a
wrapper around content is one (build on `tui.MultiChild`, never by embedding a
concrete container: its methods call their own receiver, and Go has no virtual
dispatch).

Package [`controls`](controls/) is written this way, with nothing but this
contract: `TextField` and `Popup`, from Qt Quick Controls.

**A widget the host already built** — wired to a process, a socket, a running
model — is placed with `Instance`:

```go
tuidecl.Types(tuidecl.Instance("Terminal", myTerminal))
```

```qml
Split { Editor { }  Terminal { } }
```

A widget can be in the tree once: a second `Terminal { }` is refused by name and
position. Place an Instance where its node is stable — a reload that would give
it a new node (moving it to another parent) builds the replacement before
releasing the old one, and is refused rather than putting one widget in two
places.

A type named like a built-in panics at `New`: a vocabulary is code, and two
types of one name is a programming mistake, not a document's.

## 7. A QML screen inside a Go program

What the adapter builds is ordinary `tui.Component`s. A QML screen that is only
PART of a Go program is built without NewProgram — NewProgram's App would own
the whole screen — so the host builds the adapter and tree, and mounts the root
wherever a component goes:

```go
adapter := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
    tuidecl.WithErrorSink(sink))...)
tree := decl.New(adapter, decl.WithScheduler(func(fn func()) { app.Update(fn) }))
// … declare, inject, parse, tree.Mount(spec) …
qmlRoot, _ := adapter.Component(tree.Root())
screen := widget.NewSplit(widget.Horizontal, mySidebar, qmlRoot)   // Go around QML
app = tui.NewApp(screen, tui.WithBackend(backend))
```

Reach into a QML-declared widget by its id:

```go
editor, ok := tuidecl.FindAs[*widget.Editor](p, "editor")   // with a Program
// by hand:
id, _ := tree.NodeByID("editor")
c, _ := adapter.Component(id)
```

The document owns that widget's structure; the host may call its methods. If the
document is reloaded, the node can be rebuilt — look it up again rather than
keeping the pointer across a reload.

**A QML dialog over a Go screen.** A `Dialog` opens on its Window's overlay —
and a Go program has no QML Window. Give the adapter the program's own
`OverlayHost`, the one its Go modals already open on:

```go
host := widget.NewOverlayHost(screen)                  // the Go program's layer
adapter := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
    tuidecl.WithErrorSink(sink), tuidecl.WithOverlay(host))...)
// … mount quit.qml, whose root is a Dialog …
app = tui.NewApp(host, tui.WithBackend(backend))

id, _ := tree.NodeByID("quitDialog")
adapter.Invoke(id, "open", nil)                        // from a Go key binding
```

The dialog answers `accepted`/`rejected` into the handlers the host injected,
closes on its buttons, letters and Escape, and hands the keyboard back to the Go
widget that had it — the runtime restores the focus a modal took. A document may
be nothing but dialogs; they are opened, never laid out. Under a QML `Window`,
dialogs open on the Window's own host whatever the adapter was given.

## 7b. Syntax highlighting

Put a `SyntaxHighlighter` in the Editor and name a definition; bind the
styles you colour, once, on the Window:

```qml
Window {
    syntax.keyword: Theme.syntax.keyword
    syntax.string: Theme.syntax.string
    syntax.comment: Theme.syntax.comment

    Editor {
        SyntaxHighlighter { definition: App.syntax }
    }
}
```

- **The colours are inherited**, as palette roles are: every highlighter under
  the Window wears them, a FileDialog's preview as well as the Editor's.

- **The host decides the definition.** `App.syntax` from the file's extension
  — QML for `.qml`, `""` otherwise; the document cannot say "if".
- **A new language is a registration, nothing else**: implement
  `highlight.Highlighter` and pass
  `Highlighters(highlight.Definition{Name: "SQL", Extensions: []string{"*.sql"}, Highlighter: sql})`
  — the extensions are what a FileDialog's preview picks it by.
  A highlighter colours one line at a time and must never refuse (package
  `highlight`); from a tree-sitter tree, `highlight.StyleForCapture` maps its
  captures.
- **Only what is seen is highlighted**: the Editor highlights down to the last
  visible line, and again only a line whose text or starting state changed. A
  jump deep into a long file catches up over a few frames, a bounded number of
  lines each, rather than freezing one.
- A style left unset paints as `syntax.normal`, and that unset as the Editor's
  text; set the ones your theme distinguishes.

## 8. Reloading

**While developing, let the program follow its files.** Read the QML from disk
and add `HotReload`: every file read through `Layout`, `Themes` and `Components`
is polled, and a saved change is on screen a moment later.

```go
files := os.DirFS("ui")                       // not the embed.FS
p, err := tuidecl.NewProgram(
    tuidecl.Layout(files, "editor.qml"),
    tuidecl.Themes(files, "themes", "editor.theme", "1.0"),
    tuidecl.Components(files, "dialogs", "editor.dialogs", "1.0"),
    tuidecl.HotReload(tuidecl.OnReloadError(showInStatusLine)),
    …)
```

- A change is acted on once the files **hold still** for one interval
  (`ReloadInterval`, 250ms): an editor saving in two writes is one reload.
- The engine's component cache is cleared first (Qt's
  `clearComponentCache`), so an edited **theme or dialog** file is read again —
  and a line written the same under an edited theme is re-applied.
- A save caught **half-written** is waited out in silence. Any other refusal
  goes to `OnReloadError` and **the screen stays as it was**. A reload that
  failed part-way is recovered by the next good save, which builds the screen
  afresh and keeps the host's current source values.
- **What survives:** a node that keeps its identity — its `id`, else its
  position — keeps its focus, scroll and typed text, and so do its unchanged
  siblings, the Window's included. A node that must be rebuilt starts fresh;
  `OnReload`'s `Result.Rebuilt` says which and why.
- **Go is the boundary.** Handlers, functions and the host's code are
  compiled; changing them is a rebuild and restart.
- Without `HotReload` nothing is polled: ship the embedded files as before. The
  example's `-dev dir` flag is this, switched on.

By hand, `Program.Reload(src)` is the same reconcile:

```go
res, err := p.Reload(src)          // or tree.Reload(src)
switch {
case errors.Is(err, decl.ErrIncomplete):  // a save in progress: keep the screen, wait
case err != nil:                           // a real mistake: show it, keep the last good screen
default:
    for _, rb := range res.Rebuilt { log.Println(rb) }  // what lost its state, and why
}
```

A module a reload is the first to import is loaded then; a reload refused
before it touched the tree unloads it again. Call `Tree().ClearComponentCache()`
first to have imported modules read again. A reload that replaces the root is
put on screen with `App.SetRoot`. Only the QML-built parts reload; Go widgets
around them are untouched.

## 9. Using both safely — the rules

**One goroutine owns the UI.** Everything that touches the tree — `Set`,
`SetMany`, `Reload`, `Call`, `Find` — runs on the App's loop, exactly as a Go
widget's methods must. A handler already runs there. Work from another
goroutine reaches it through `p.Post(fn)` (or `app.Update(fn)`), or through a
provider, which the scheduler routes there.

**Keep state in one place.** State the document shows is a source; the host
changes the source, and bindings follow. Do not also set the widget directly —
two writers to one widget disagree the moment a binding re-evaluates.

**Let the dialog own its lifecycle.** Open by id; never mirror "is it open" in
host state (§4).

**One focus tree.** Go widgets and QML widgets share it. A QML `Window` returns
the keyboard to its `focus: true` target after a menu action or a dialog — but
only within itself; a Go container around it decides for its own children.

**A QML Window has its own overlay layer.** A QML dialog opens over that Window,
not over the whole Go screen around it. Right for a QML pane; if a small QML
piece must open a dialog over everything, open it from Go on the outer host.

**Colours come from two places.** Go widgets follow golib's theme tokens
(`App.SetTheme`); QML widgets follow the imported Theme's palette roles. Both
can share a screen; making them match is the program's job.

**Look widgets up again after a reload.** A rebuilt node is a new widget.

## 10. Best practice

- **Structure in QML, behaviour in Go.** A document says what is on screen and
  what each control triggers; the host decides what triggering does.
- **One singleton for the program**, one table of commands, one place per piece
  of state.
- **Name ids for what they are** (`quitDialog`, `editor`), and never the same as
  an injected name — an id that spells one is refused as ambiguous.
- **Bind, don't push.** `left: App.mode` beats the host calling
  `statusBar.SetLeft` — the binding survives a reload, the call does not.
- **A decision that needs "if" belongs to the host** (§4).
- **Set a palette once, where it starts.** Roles propagate to children, so a
  `Dialog`'s roles dress everything in its card; repeat a role on a child only
  to make it differ. (golib has no transparent cell, so a `Text` paints its own
  background — which is why it must inherit the card's, and now does.)
- **`#rrggbb` for a theme that must look the same everywhere**; ANSI slot names
  for one that should follow the user's terminal palette.
- **Offer, don't declare, what a document may not import** — forty dialogs cost
  nothing until one is imported.

## 11. Testing a QML screen

A QML mistake is a runtime error unless a test finds it first. Package
[`decltest`](decltest/) holds the two tests every program should have, and both
take the options `NewProgram` takes — so give your program one function that
returns them, and build from it both in `main` and in the tests:

```go
func TestTheQMLIsSound(t *testing.T) {
    decltest.Check(t, programOptions()...)
}

func TestSaveAsksForAName(t *testing.T) {
    s := decltest.Run(t, 80, 24, programOptions()...)
    s.Keys(t, decltest.Ctrl('s'))
    s.WaitForText(t, "Save As")
}
```

**`decltest.Check` is qmllint for the program.** `NewProgram` reads only what
the layout imports, so a test that just builds the program never sees the theme
you are not using, or a dialog no screen uses yet — those break the day someone
switches to them. Each offered module has a **context** — the imports a
document using it would have: the layout's own if the layout imports it;
otherwise the layout's with every import it clashes with replaced by it, or with
it added. (Two modules that bring one name into scope can never be imported
together, so one sharing a name with an imported module can only ever be used
*instead* of it.) `Check` mounts:

| what | how |
| --- | --- |
| the layout | as written |
| each alternative | the layout under the alternative's context — `editor.theme.mono` in place of `editor.theme.retro` |
| each unused component | under its module's context, alone and inside a Window — sound if either placement accepts it, since nothing says yet where it will be used |

and loads every value module. Each problem is its own test failure,
labelled with what was mounted and placed at its file and line:

```
editor.qml with import editor.theme.mono in place of editor.theme.retro:
  … resolve Theme.menu.accent at editor.qml:42:25: "Theme.menu" has no member "accent"
```

It runs nothing: providers subscribe and are released, and no App is built.
What it cannot know is a component's use-site properties — an unused component
is mounted as `Name {}`, the way the first use without overrides would be.

**`decltest.Run` runs the program** on a `tui.NewTestBackend(w, h)` and stops it
when the test ends. A handler error fails the test unless the options give an
`ErrorSink` of their own. `Screen.WaitFor`, `WaitForText`, `Keys`, and the key
constructors `Rune`, `Type`, `Ctrl` and `Alt` cover the rest.

If a program wraps `NewProgram` in a host of its own, as the example's `New`
does, the same rules hold by hand: read what reached the screen — "the tree was
built" is not the claim a user cares about.

- **Wait for the screen, don't settle once.** A change travels through the event
  bus and lands a frame or two later; poll the grid for what you expect.
- **Read the attribute mask.** Reverse is an attribute, not a colour swap: a
  reversed cell reports the colours it was given and shows the opposite pair.
- **Match whole phrases.** A temporary directory carries the test's name, and a
  status bar showing that path will contain any word of it.
- **Give a file dialog room.** It wants an ordinary terminal's rows — test it at
  24, not 14.
- **Break the code to check the test.** Remove the fix and the test must go red;
  a test that stays green is reading the wrong cell.

The example's `check_test.go` runs `decltest.Check`; its `app_test.go`,
`dialog_test.go`, `theme_test.go` and `filedialog_test.go` do the rest by hand.
