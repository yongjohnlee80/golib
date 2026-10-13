# The editor core functions

`EditorCore` is the behaviour behind every golib editor: the text, the cursor, the modes and keys,
undo, the register, the selection and the highlighting. The terminal `Editor` (cells) and the GUI
`Editor` (`gui/widget`, pixels) each hold one and draw it. What it does is therefore the same in
both views, and the same for whoever drives it: the user's keys, the host program, a script, or
an AI agent working on the document beside the user.

This page covers the functions a program builds on. Each one is a method on `*EditorCore`; both
editors hand theirs out with `Core()`.

```go
core := editor.Core() // *widget.EditorCore, the terminal's or the GUI's
```

Every function runs on the UI loop goroutine, as all widget state does. From another goroutine,
post the work to the loop: `Program.Post`, or a `tui.TaskResult`.

## Positions

Positions are in the buffer, never in cells or pixels:

```go
type TextPos struct{ Line, Col int }  // Line from 0; Col in grapheme clusters from 0
type TextRange struct{ From, To TextPos } // From included, To not; line breaks inside count
```

A column counts **grapheme clusters**, the unit the cursor moves in. `é` written as `e` plus a
combining accent is one column. Every function clamps what it is given: a column past a line's
end is its end, and a line past the last is the last. A range given backwards is put in order.

| Function | Does |
|---|---|
| `Cursor() TextPos` / `SetCursor(p)` | where the cursor is; move it (and reveal it) |
| `Line() (row, col)` / `SetLine(row, col)` | the same, as two ints |
| `LineCount() int`, `LineAt(i) string`, `Lines() []string` | read lines (`Lines` is a copy) |
| `Value() string` | the whole text, lines joined by `\n` |
| `TextIn(r TextRange) string` | the text of a range |
| `Select(r)` / `Unselect()` / `Selection() (TextRange, bool)` | show a range as the Visual selection; leave it; read it |

## Edits

```go
end, err := core.Insert(widget.TextPos{Line: 3, Col: 0}, "## Notes\n")
err = core.Delete(widget.TextRange{From: a, To: b})
end, err = core.Replace(r, "new text")
```

- `Insert` puts text at a position. `\n`, `\r\n` and `\r` all split lines. It returns the position
  just after the text, which is also where the cursor now is.
- `Delete` removes a range. The cursor goes to where the range began.
- `Replace` is a delete and an insert in one undo step.
- An edit leaves any selection first, since its anchor would not survive the change.
- On a read-only core (`SetReadOnly(true)`) every edit returns `ErrReadOnly` and changes nothing.

Each edit is **one undo step and one `ChangeEvent`**, and the `CoreOnChange` listener runs once.
`Batch` makes several edits one of each:

```go
err := core.Batch(func() error {
	for _, r := range rows {
		if _, err := core.Insert(core.Cursor(), r+"\n"); err != nil {
			return err
		}
	}
	return nil
})
```

- One `u` undoes the whole batch.
- Batches nest; the outermost one sends the change.
- An error from the function is returned, and the edits made before it stay (an undo reverts them).
- A batch that makes no edit leaves no undo step and sends no change.

Every command and every script edit should run inside a `Batch`. The command line already runs
each command in one.

## Reading and writing: `io.Reader`, `io.Writer`

```go
f, _ := os.Open(path)
_, err := core.ReadFrom(f) // io.ReaderFrom: the document becomes the file's text
...
var buf bytes.Buffer
_, err = core.WriteTo(&buf) // io.WriterTo: the text, lines joined by "\n"
```

- `ReadFrom` reads to EOF and replaces the document, as `SetValue` does: undo history is cleared,
  the cursor goes to the start, and the document is unmodified. A read error leaves the document
  as it was.
- `WriteTo` writes the text with no newline added at the end. It does not mark the document saved,
  because a writer need not be the document's file: the host calls `MarkSaved` once the file is
  written.

## Modified state

```go
core.Modified() // the text differs from the last saved one
core.MarkSaved() // record the text as saved: call it after a successful write
```

Every edit gives the text a new revision, and `Modified` compares it with the revision `MarkSaved`
recorded. An undo restores the revision the text had, so undoing back to the saved text reads as
unmodified. A new edit after that never reuses an old revision. `SetValue` and `ReadFrom` start a
new document, which is unmodified.

## Find and replace

```go
r, ok := core.Find("term", core.Cursor(), widget.FindOptions{IgnoreCase: true, Wrap: true})
all := core.FindAll("term", widget.FindOptions{})
n, err := core.ReplaceAll("old", "new", widget.FindOptions{}) // one undo step
```

- A term matches whole grapheme clusters within one line.
- `FindOptions` has three flags: `IgnoreCase`, `Backward` and `Wrap`. The zero value is a literal,
  case-sensitive, forward search that stops at the end.
- Searching forward, `Find` returns the first match that starts *after* `from`, so finding again
  from a match moves on. Searching backward, it returns the last match that starts before `from`.

The **active find** is what the editor shows and what `n` and `N` step through:

```go
core.SetFind("term", widget.FindOptions{IgnoreCase: true}) // mark it; n / N go through it
core.FindNext(false)                                       // as n; FindNext(true) is N
core.SetFind("", widget.FindOptions{})                     // clear it
```

- Each match on screen takes the `Alert` highlight style, laid over the host's own overlay
  (`SetHighlightOverlay`).
- An editor whose syntax styles leave `Alert` unset shows no marks. The cursor still goes to each
  match.
- With no active find, `n` and `N` are not consumed. An application can therefore keep an `n` of
  its own.

**Replacing one match at a time** is a `ReplaceSession`. It walks the whole document from the
top, visiting each match once, so a replacement that contains the term is never visited again.
All the replacements of one session undo as a single step:

```go
s, ok := core.StartReplace("cat", "dog", widget.FindOptions{}) // the first match, selected
for ok {
	r, _ := s.Current()             // the match to ask about, selected in the editor
	more, err := s.Answer(ask(r))   // replace (true) or skip (false), then the next match
	if err != nil {
		return err
	}
	ok = more
}
// s.Stop() ends a session early; what was replaced stays
```

## The command line

The editor's `:` prompt runs **command lines**. So does `RunCommand`, for a host, a script or an
agent:

```go
err := core.RunCommand("42")          // go to line 42
err = core.RunCommand("replace a b")  // a verb and its arguments
```

| Line | Does |
|---|---|
| `<n>` | goes to line n, counted from 1, at its first non-blank character. `0` is line 1; past the last line it is refused, naming the line count. |
| `w`, `write` | saves (the host's `CoreOnWrite(false)`) |
| `wq` | saves, then quits once the write succeeded (`CoreOnWrite(true)`) |
| `x`, `exit` | saves if modified, then quits; quits at once if not |
| `q`, `quit` | quits (the host's `CoreOnQuit(false)`); the host decides what happens over unsaved work |
| `q!` | quits, discarding unsaved changes (`CoreOnQuit(true)`) |
| `find <term>` | makes the term the active find and goes to it. A term in lower case ignores case; one with a capital letter matches it exactly. |
| `replace <term> <with>` | the first word is the term and the rest of the line the replacement. With a widget to ask, it asks at each match; without one, it replaces every match. |
| `nohl` | clears the find's marks |

**Saving and quitting belong to the host.** The core asks for them through two hooks:

```go
widget.CoreOnWrite(func(quit bool) error { ... }) // w: false, wq / x: true
widget.CoreOnQuit(func(force bool) error { ... }) // q: false, q!: true
```

- `quit` asks the host to quit once *its* write has succeeded. A write may finish later, after a
  Save As dialog or a daemon round trip, so only the host knows when.
- Without a hook, `w` answers `ErrNoWriter` and `q` answers `ErrNoQuit`.
- The terminal `Editor` also takes them as `WithOnWrite` and `WithOnQuit`.

**Commands are a registry the host composes into.** Every core starts with `DefaultCommands()`:

```go
err := core.Commands().Add(widget.Command{
	Name:    "table",
	Aliases: []string{"tbl"},
	Args:    "<cols> <rows>",            // shown by Tab completion
	Help:    "insert a Markdown table",  // shown by Tab completion
	Run: func(x widget.CommandContext) error {
		// x.Core: the editor; x.Args: the words; x.Line: the rest of the line; x.Bang: "table!"
		return x.Core.Batch(func() error { ... })
	},
})
```

- `Add` refuses a name or alias that is already registered (`ErrCommandExists`). To override one
  on purpose, use `Set`, which replaces it.
- `Remove`, `Lookup`, `All` and `Complete(prefix)` read and edit the registry.
- A name is letters, digits, `-` and `_`. It cannot be all digits, which would read as a line
  number.
- `CoreCommands(s)` gives a core a registry of its own. `CoreCommands(nil)` gives it none, and then
  `:` is not consumed.
- A command's error is shown in the prompt, which reopens with the line so it can be corrected.

## The prompt, `CommandLine`

`:` (in the Vim keyset, in Normal and Visual mode) opens a small dialog titled **VIM Commands**.
It opens over the editor, under the nearest `OverlayHost`:

- **Enter** runs the line. The prompt closes *first*, so a command can open a dialog of its own.
- **Esc** closes the prompt.
- **Tab** completes the verb:
  - with one candidate, Tab completes it, adds a space, and shows its arguments and help;
  - with several, it fills in their common prefix and lists every candidate, each as `name args —
    help`; further presses of Tab and Shift+Tab cycle through them;
  - once the line has a space, Tab shows the typed verb's arguments.
- `replace` asks at each match on a small card in the corner, with the match selected behind it.
  **y** replaces, **n** skips, and **q** or **Esc** stops.

The title, the help line and the replace card's text are translated with the rest of the
toolkit's strings.

Both editors hold a prompt, on by default:

| Terminal `Editor` / GUI `Editor` | |
|---|---|
| `WithCommandLine(false)` | no prompt: `:` reaches the application; `RunCommand` still works |
| `SetCommandLineEnabled(on)`, `CommandLineEnabled()` | turn it on or off at run time |
| `OpenCommandLine(initial)` | open it from a key or a menu row, with `initial` in the field |
| `CommandLineOpen()` | whether it is showing |
| `Commands()`, `RunCommand(line)` | the core's |

Another editor widget can hold the same module: `NewCommandLine(owner, core)` and then
`SetEnabled(true)`. A widget with a prompt of its own answers `:` through `SetCommandLine(open)`,
and calls `CommandLineClosed()` once its line has run or been cancelled: Insert's Ctrl-o waits for
that before Insert resumes.

### In QML

```qml
Editor {
    id: editor
    commandLine: true                         // the default
    onWriteRequested: App.save(quit)          // w, wq, x
    onQuitRequested: App.quit(force)          // q, q!
}
Shortcut { sequence: "Ctrl+P"; onActivated: editor.openCommandLine() }
```

The two signals are wired only when the document handles them. Without a handler, `w` answers in
the prompt that the editor cannot save.

## Registers

The Vim keyset keeps Vim's registers: the unnamed one, `a`–`z` (`A`–`Z` append), `0`–`9`, `-`,
and `+` and `*` (the system clipboard). Each holds characters, whole lines, or a block (a Visual
block's yank: its pieces, one per line, and its width in display cells, a wide character two and a
tab four).

| Function | Does |
|---|---|
| `Register()` / `SetRegister(text, linewise)` | the unnamed register |
| `NamedRegister(name)` / `SetNamedRegister(name, text, linewise)` | any register, as text (a block's pieces joined by `\n`) |
| `RegisterKind(name) (RegisterKind, bool)` | `RegisterChars`, `RegisterLines` or `RegisterBlock` |
| `NamedBlock(name) (pieces, width, ok)` / `SetNamedBlock(name, pieces, width)` | a block; width 0 is the widest piece's, and a smaller width is raised to it |

A block put (`p`, `P`) goes in on successive lines at the cursor's display column, padding short
lines with spaces, as Vim's does. Text read from the system clipboard is never a block.

## Modes

`Mode()` is `ModeNormal`, `ModeInsert`, `ModeVisual`, `ModeVisualLine` or `ModeVisualBlock`
(Ctrl-v). The three Visual modes share one set of bindings. `InsertOneShot()` reports Insert's
Ctrl-o running its one Normal-mode command: the mode is Normal until the command is complete, so
a status line can show `-- (insert) --`.

## Writing aids

- **List editing** (`CoreListEditing`) continues, ends, nests and un-nests Markdown list items in
  Insert mode.
- **Table editing** (`CoreTableEditing`): in Insert mode, Tab on a table row goes to the next cell
  and selects its text, so typing replaces it.
  - Past a row's end it goes to the next row (passing over the `|---|` row).
  - Past the table's last cell it adds an empty row.
  - Shift+Tab goes back a cell.
  - With a cell selected, Backspace and Delete empty it.
- Both apply only on the lines `SetListEditingWhere` allows, so a Markdown host leaves its code
  blocks out.

## A worked example: a command a script or an agent could write

```go
// :upper — upper-case the selection, or the current line.
core.Commands().Add(widget.Command{
	Name: "upper", Help: "upper-case the selection or the line",
	Run: func(x widget.CommandContext) error {
		c := x.Core
		r, ok := c.Selection()
		if !ok {
			ln := c.Cursor().Line
			r = widget.TextRange{From: widget.TextPos{Line: ln}, To: widget.TextPos{Line: ln, Col: 1 << 30}}
		}
		_, err := c.Replace(r, strings.ToUpper(c.TextIn(r)))
		return err
	},
})
```

The function reads with `Selection` and `TextIn`, edits with `Replace`, and is one undo step.
The same seven functions (`Cursor`, `TextIn`, `Insert`, `Replace`, `Find`, `Batch` and
`RunCommand`) are what a scripting binding or an agent's tool needs to expose.
