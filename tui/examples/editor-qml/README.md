# editor-qml — a text editor whose screen is QML

A complete terminal text editor built on golib/tui, with every screen written in
QML and the Go limited to behaviour. It mirrors
[yongjohnlee80/editor](https://github.com/yongjohnlee80/editor), which builds the
same screen by hand in Go — so the two can be read side by side.

```bash
go build -o bin/editor-qml .
bin/editor-qml notes.md
```

## What it does

- **File** — New, Open (a folder listing beside a preview of the file under the
  cursor), Save (asks for a name only when the buffer has none), Save As
  (starts from the file being edited), Exit (asks first; warns about unsaved
  changes).
- **Option > Keymaps** — Vim (modal) or Nano (modeless), a radio pair bound to
  the editor.
- **Help > About.**
- A status line: the mode, the file or the last message, and a clock.
- Keys: `Ctrl+S` save, `Ctrl+Shift+S` save as (where the terminal can report
  it), `Ctrl+Q` quit, `F10` or `Alt`+underlined letter for the menu.

## The files

| file | what it is |
| --- | --- |
| `editor.qml` | the screen: menus, the editor, the status line, the dialogs — no colours |
| `themes/retro.qml`, `themes/mono.qml` | the themes; the layout's import line picks one |
| `dialogs/*.qml` | one component file per dialog: Quit, About, Open, Save |
| `app.go` | the Host, and `New` — one `tuidecl.NewProgram` call |
| `modules.go` | the QML the program embeds, and the modules it offers |
| `state.go` | what the document reads: `App.mode`, `App.status`, `App.path`… |
| `commands.go` | what the document invokes: `App.saveFile()`, `App.openFile(path)`… |
| `files.go` | reading and writing the buffer's file |
| `clock.go` | a provider: `App.clock`, ticking on its own goroutine |
| `main.go` | opens the terminal and runs the Program |

## What to look at

- **Switching theme is one line.** Change `import editor.theme.retro 1.0` in
  `editor.qml` to `…mono 1.0`. Only the imported theme is read. A new theme is
  a file under `themes/`, and nothing else.
- **Dialogs close themselves.** `quitDialog.open()` opens one; its buttons,
  their letters and Escape close it. The files say only what an answer does.
- **The host decides what a document cannot.** Save opens the Save dialog only
  when the buffer has no name — `h.p.Call("saveDialog", "open")` — because QML
  has no way to say "if".
- **The Go never builds a widget.** It publishes state and commands, and reaches
  into exactly one widget — the editor, by its id.

The tests (`*_test.go`) run the whole program on a test backend and read the
screen: layout, menus, every dialog, both themes' colours, and the files written
to disk.

See [tui/decl/USAGE.md](../../decl/USAGE.md) for the guide this example is the
worked version of.
