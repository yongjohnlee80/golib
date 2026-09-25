# editor-qml — a text editor whose screen is QML

A complete terminal text editor built on golib/tui, with every screen written in
QML and the Go limited to behaviour. It mirrors
[yongjohnlee80/editor](https://github.com/yongjohnlee80/editor), which builds the
same screen by hand in Go — so the two can be read side by side.

```bash
go build -o bin/editor-qml .
bin/editor-qml notes.md
bin/editor-qml -dev . notes.md    # QML read from this directory, and followed
```

## What it does

- **File** — New, Open (a folder listing beside a preview of the file under the
  cursor), Save (asks for a name only when the buffer has none), Save As
  (starts from the file being edited), Exit (asks first; warns about unsaved
  changes).
- **Option > Keymaps** — Vim (modal) or Nano (modeless), a radio pair bound to
  the editor.
- **Help > About.**
- **A command prompt** — `Ctrl+P` or File > Command: `w`, `q`, `wq`,
  `e <file>`, as vim's `:` line.
- **Syntax highlighting** for `.qml` and `.js` files, in the theme's colours.
- A status line: the mode, the file or the last message, and a clock.
- Keys: `Ctrl+S` save, `Ctrl+Shift+S` save as (where the terminal can report
  it), `Ctrl+P` the prompt, `Ctrl+Q` quit, `F10` or `Alt`+underlined letter
  for the menu.

## The files

| file | what it is |
| --- | --- |
| `editor.qml` | the screen: menus, the editor, the status line, the prompt, the dialogs — no colours |
| `themes/retro.qml`, `themes/mono.qml` | the themes; the layout's import line picks one |
| `dialogs/*.qml` | one component file per dialog: Quit, About, Open, Save |
| `app.go` | the Host: `options` (everything the program is), and `New` — `NewProgram(options…)` then `attach` |
| `modules.go` | the QML the program embeds, and the modules it offers |
| `state.go` | what the document reads: `App.mode`, `App.status`, `App.path`… |
| `commands.go` | what the document invokes: `App.saveFile()`, `App.openFile(path)`… |
| `prompt.go` | what a prompt command does: the same commands, one more way in |
| `files.go` | reading and writing the buffer's file |
| `clock.go` | a provider: `App.clock`, ticking on its own goroutine |
| `main.go` | opens the terminal and runs the Program |

## Best practice, as this example does it

- **The layout names no colour, and a palette is set once, where it starts.**
  The Window carries the application palette (`Theme.app`); the menu bar, the
  document and the status line — distinct parts of the design — override only
  their own roles; every dialog, its panes and buttons, and the command prompt
  inherit and name no colour at all. Roles propagate parent to child, as Qt's
  do, and a theme change reaches everything live.
- **Switching theme is one line.** Change `import editor.theme.retro 1.0` to
  `…mono 1.0`. Only the imported theme is read; a new theme is a file under
  `themes/` and nothing else.
- **One file per dialog, each a type.** `dialogs/QuitDialog.qml` is
  `QuitDialog { id: quitDialog }` in the layout. Dialogs close themselves:
  `quitDialog.open()` opens one; its buttons, letters and Escape close it; the
  file says only what an answer does.
- **Widgets come from a vocabulary, including golib's own additions.** The
  prompt is a `Dialog` — like About: the field, a rule, the help line — holding
  Qt Quick Controls' `TextField`, from `tui/decl/controls`, added with `tuidecl.Types(controls.Types()...)` — the
  way a program adds widgets of its own.
- **The host decides what a document cannot.** Save opens the Save dialog only
  when the buffer has no name — `h.p.Call("saveDialog", "open")` — because QML
  has no way to say "if". The prompt's commands are the menu's commands, not
  a second implementation.
- **The Go never builds a widget.** It publishes state and commands, and
  reaches into exactly one widget — the editor, by its id.
- **One options function builds everything.** `Host.options` is the whole
  program; `main` builds from it (`New`), `decltest.Check` lints it, and every
  test runs it (`decltest.RunWith`). A program assembled twice is two programs.
- **Highlighting is Go's; choosing and colouring it is QML's.** The Editor holds
  a `SyntaxHighlighter { definition: App.syntax … }`; the host sets `App.syntax`
  from the file's extension; each theme has a `syntax` group.
- **Develop with `-dev`.** The QML is read from disk and followed: save
  `editor.qml`, a theme or a dialog and the running editor shows it, keeping
  what is typed. A refused edit shows in the status line and the screen stays.
  Go code is compiled; changing it means rebuilding.

## Tests

- `check_test.go` — `decltest.Check`: every QML file the program can load,
  the theme it does not import included, is mounted and judged.
- The rest run the whole program through `decltest.RunWith` on a test backend
  and read the screen: layout, menus, every dialog, the prompt, both themes'
  colours (including what the dialogs inherit), the files written to disk, and
  hot reload in `-dev` mode.

See [tui/decl/USAGE.md](../../decl/USAGE.md) for the guide this example is the
worked version of.
