# tui/keymap — overridable key maps

Every key golib acts on is a **command**: an id, a scope and its default keys, declared in code. A
**scheme** — a user's file — changes a command's keys by id. A **Map** is the commands with the
schemes layered on them, validated once and immutable; the App holds one, and every widget asks its
Context which of its commands a key is (`Context.KeyCommand`).

Design: ADR 1792255248 (KB `adrs/1792255248-golib-tui-overridable-key-maps.md`).

## A scheme

Qt Creator's keyboard-mapping scheme, in a `.xml` file:

```xml
<!DOCTYPE KeyboardMappingScheme>
<mapping>
  <shortcut id="tui.list.down"><key value="Down"/><key value="Ctrl+N"/></shortcut>
  <shortcut id="tui.list.top"><key value=""/></shortcut>   <!-- unbound -->
</mapping>
```

- **A `<shortcut>` replaces its command's keys**, all of them; several `<key>`s give several keys.
  `<key value=""/>` unbinds the command. A command the file does not name keeps its defaults.
- **Keys are Qt's portable text** ([`tui/key`](../key/)): `Ctrl+S`, `Shift+Down`, `F10`, `?`,
  `Ctrl++`, `Ctrl+,`. A letter is its key (`"G"` is g) and `Shift+G` the capital. `meta` reads as
  Alt (golib's deviation; `Super` is the Windows or Command key). One chord only: `Ctrl+K, Ctrl+C`
  is refused by name.
- **Every refusal names the file and line**: XML that is not well formed; an element other than
  `<mapping>`, `<shortcut>`, `<key>`, or an attribute other than `id` and `value` (a misspelling
  never silently unbinds); a `<shortcut>` with no id, or one written twice; a `<key>` with no
  `value`; a `<shortcut>` that both unbinds and binds; a key `tui/key` cannot read.

## Layers and the Map

```go
schemes := []*keymap.Scheme{product, user}           // keymap.ParseScheme each
m, err := keymap.Build(append(widget.KeyCommands(), mine...), schemes...)
app := tui.NewApp(root, tui.WithKeymap(m))           // or tuidecl.KeyCommands + tuidecl.Keymap
```

- **Later layers win per command.** A product's scheme, then the user's.
- **`Build` refuses with every problem at once** (`*keymap.Error`, wrapping `errs.ErrInvalidArgument`):
  a command declared twice, a scheme id nobody declared, two commands of one scope sharing a key.
- **Warnings** (`Map.Warnings`): a key an inner scope shares with `global` or `tui.app`. It is legal —
  routing gives it to the inner stop while focus is there — and reported so a user who moves a global
  key onto one a widget uses learns the widget will take it.
- **The Map is immutable**, and every getter returns a copy. A nil Map, or one that does not declare a
  command, gives the command's defaults: a host with no map keeps every key it had.

## Matching

One model, used by every lookup (`Context.KeyCommand`, a Shortcut, `App.KeyBindings`):

1. `key.Of(code, mods, text)` is the event's chord: lock bits dropped; a one-rune printable text is
   the key when no Ctrl, Alt or Super is held (kitty's `('/', Shift, "?")` is `?`); a letter's case
   is its Shift. An event holding Hyper or Meta matches nothing.
2. `key.Candidates(chord)` is tried in order: the exact chord, then, for a Ctrl or Alt letter with
   Shift, the chord without Shift (a legacy terminal sends Ctrl+Shift+S as Ctrl+S). The first
   candidate any binding holds wins, so Ctrl+S and Ctrl+Shift+S in one scope each win their own
   event and are not a conflict.
3. `key.Overlaps(a, b)` (some event's candidates hold both) is for warnings only; what an event
   really reaches is decided by 2, which `App.KeyBindings` runs over `key.Accepts`.

## Scopes

A scope is where keys must be unique. Routing asks the focused component's scope first, then each
enclosing holder's up to a modal trap's ceiling, then `global` (the Window's), then `tui.app`
(traversal, which runs inside a trap too). Two widgets' scopes never conflict: only one has focus.

## golib's commands

`widget.KeyCommands()` returns them all. A label's mnemonic (`&File`, a button's letter) is not a
command and cannot be rebound.

| scope | commands (defaults) |
| --- | --- |
| `tui.app` | `tui.app.focusNext` (Tab), `tui.app.focusPrev` (Shift+Tab) |
| `tui.capture` | `tui.capture.cancel` (Esc: cancels a held drag or resize) |
| `tui.tooSmall` | `tui.tooSmall.quit` (q, Ctrl+C: quits from the too-small screen, which prints the bound keys) |
| `global` | `tui.window.menu` (F10, Super+F10: a declarative Window's menu bar) |
| `tui.button` | `tui.button.activate` (Enter, Space: a Button, a standalone MenuItem) |
| `tui.list` | `tui.list.up` (Up, Shift+Up, K), `tui.list.down` (Down, Shift+Down, J), `tui.list.top` (G), `tui.list.bottom` (Shift+G), `tui.list.pageUp`, `tui.list.pageDown`, `tui.list.home`, `tui.list.end`, `tui.list.activate` (Enter), `tui.list.toggle` (Space, multi-select), `tui.list.contextMenu` (Shift+F10, Menu) |
| `tui.tree` | `tui.tree.up`, `tui.tree.down`, `tui.tree.pageUp`, `tui.tree.pageDown`, `tui.tree.first` (Home, G), `tui.tree.last` (End, Shift+G), `tui.tree.expand` (Right, L), `tui.tree.collapse` (Left, H), `tui.tree.activate` (Enter), `tui.tree.contextMenu` (Shift+F10, Menu) |
| `tui.table` | `tui.table.up`, `tui.table.down`, `tui.table.left`, `tui.table.right` (arrows, hjkl), `tui.table.fieldNext` (W), `tui.table.fieldPrev` (B), `tui.table.pageUp`, `tui.table.pageDown`, `tui.table.goPrefix` (G, then the literal G: the first row), `tui.table.last` (Shift+G), `tui.table.fieldFirst` (Home, 0), `tui.table.fieldLast` (End, $), `tui.table.selectCells` (V), `tui.table.selectRows` (Shift+V), `tui.table.selectBlock` (Ctrl+V), `tui.table.escape` (Esc), `tui.table.yank` (Y; on a row cursor, the row), `tui.table.rowCursor` (R: the cursor between its cell and its whole row), `tui.table.sort` (S), `tui.table.activate` (Enter; on an editable source it edits the cell), `tui.table.edit` (I, F2), `tui.table.openRow` (O), `tui.table.openRowAbove` (Shift+O), `tui.table.deleteRow` (D twice; on a selection, D cuts it at once, a row selection's rows deleted), `tui.table.clearCell` (X, Delete; on a selection, it cuts it, its cells emptied), `tui.table.pasteAfter` (P: the register's rows after the cursor's row, or its cells from the cursor's), `tui.table.pasteBefore` (Shift+P). A count's digits are its grammar, not commands; on an editable source a printable key that is no command opens the cell's field with it typed, Space and `:` excepted (the application's) |
| `tui.tableField` | a table cell's open field, before its TextArea's keys: `tui.tableField.keep` (Enter: keep, move down), `tui.tableField.newline` (Alt+Enter, Shift+Enter), `tui.tableField.next` (Tab: keep, move right), `tui.tableField.previous` (Shift+Tab), `tui.tableField.drop` (Esc), `tui.tableField.literal` (Ctrl+V, then Tab: a tab in the value) |
| `tui.input` | `tui.input.left`, `tui.input.selectLeft`, `tui.input.wordLeft`, `tui.input.selectWordLeft`, `tui.input.right`, `tui.input.selectRight`, `tui.input.wordRight`, `tui.input.selectWordRight`, `tui.input.home`, `tui.input.selectHome`, `tui.input.end`, `tui.input.selectEnd`, `tui.input.backspace`, `tui.input.wordBackspace`, `tui.input.delete`, `tui.input.submit` (Enter), `tui.input.complete` (Tab), `tui.input.completePrev` (Shift+Tab), `tui.input.lineStart` (Ctrl+A), `tui.input.lineEnd` (Ctrl+E), `tui.input.killLine` (Ctrl+U), `tui.input.killWord` (Ctrl+W). Shift extends a selection, Ctrl or Alt goes by word; typed text is not a command |
| `tui.menu` | `tui.menu.left`, `tui.menu.right`, `tui.menu.up`, `tui.menu.down`, `tui.menu.vimLeft` (H), `tui.menu.vimDown` (J), `tui.menu.vimUp` (K), `tui.menu.vimRight` (L: with vim navigation, after a row's mnemonic), `tui.menu.activate` (Enter, Space), `tui.menu.close` (Esc) |
| `tui.modal` | `tui.modal.dismiss` (Esc), `tui.modal.accept` (Enter), `tui.modal.prev`, `tui.modal.next` (arrows), `tui.modal.vimPrev` (H, K), `tui.modal.vimNext` (L, J) |
| `tui.tabs` | `tui.tabs.prev` (Ctrl+PgUp), `tui.tabs.next` (Ctrl+PgDn): with any of Shift, Alt and Super too, from anywhere in the container; `tui.tabs.left` (Left, [), `tui.tabs.right` (Right, ]): the bar focused |
| `tui.tabStrip` | `tui.tabStrip.prev` (Ctrl+PgUp), `tui.tabStrip.next` (Ctrl+PgDn), `tui.tabStrip.left` (Left, [), `tui.tabStrip.right` (Right, ]), `tui.tabStrip.add` (+: with something to add), `tui.tabStrip.menu` (Enter: the active tab's menu). A TabStrip's and a TerminalTabs' strip; all but Ctrl+PgUp and Ctrl+PgDn with the strip focused |
| `tui.split` | `tui.split.left` (Alt+Left), `tui.split.right` (Alt+Right): a horizontal split's divider; `tui.split.up` (Alt+Up), `tui.split.down` (Alt+Down): a vertical split's; `tui.split.cancel` (Esc: a drag). None while a pane is zoomed |
| `tui.resizable` | `tui.resizable.left`, `tui.resizable.right`, `tui.resizable.up`, `tui.resizable.down` (Shift and the arrow: the bare arrows stay the content's), `tui.resizable.cancel` (Esc: a drag) |
| `tui.select` | `tui.select.open` (Enter, Space, Down: a closed Select) |
| `tui.select.popup` | `tui.select.popup.close` (Esc), `tui.select.popup.commit` (Enter), `tui.select.popup.up`, `tui.select.popup.down` (arrows), `tui.select.popup.vimUp` (K), `tui.select.popup.vimDown` (J: where the select does not filter, which spends every letter on its query), `tui.select.popup.pageUp`, `tui.select.popup.pageDown`, `tui.select.popup.first` (Home), `tui.select.popup.last` (End), `tui.select.popup.backspace` (the filter's last character). Each with any modifier too; typed filter text is not a command |
| `tui.textarea` | `tui.textarea.newline` (Enter), `tui.textarea.backspace`, `tui.textarea.delete`, `tui.textarea.left`, `tui.textarea.selectLeft`, `tui.textarea.wordLeft`, `tui.textarea.selectWordLeft`, `tui.textarea.right`, `tui.textarea.selectRight`, `tui.textarea.wordRight`, `tui.textarea.selectWordRight`, `tui.textarea.up`, `tui.textarea.selectUp`, `tui.textarea.down`, `tui.textarea.selectDown`, `tui.textarea.pageUp`, `tui.textarea.selectPageUp`, `tui.textarea.pageDown`, `tui.textarea.selectPageDown`, `tui.textarea.home`, `tui.textarea.selectHome`, `tui.textarea.start` (Ctrl+Home), `tui.textarea.selectStart`, `tui.textarea.end`, `tui.textarea.selectEnd`, `tui.textarea.finish` (Ctrl+End), `tui.textarea.selectFinish`, `tui.textarea.lineStart` (Ctrl+A), `tui.textarea.lineEnd` (Ctrl+E), `tui.textarea.killLine` (Ctrl+U), `tui.textarea.killWord` (Ctrl+W). Shift extends a selection, Ctrl or Alt goes by word; typed text is not a command |
| `tui.htmlview` | `tui.htmlview.up` (Up, K), `tui.htmlview.down` (Down, J), `tui.htmlview.pageUp` (PgUp), `tui.htmlview.pageDown` (PgDn, Space), `tui.htmlview.halfUp` (Ctrl+U), `tui.htmlview.halfDown` (Ctrl+D), `tui.htmlview.top` (Home, G), `tui.htmlview.bottom` (End, Shift+G), `tui.htmlview.copy` (Ctrl+C, Y), `tui.htmlview.clearSelection` (Esc: with a selection). The arrows, PgUp, PgDn, Home, End and Esc with any modifier too |
| `tui.bufferview` | `tui.bufferview.up`, `tui.bufferview.down`, `tui.bufferview.pageUp`, `tui.bufferview.pageDown`, `tui.bufferview.top` (Home), `tui.bufferview.bottom` (End: and follows the tail), each with any modifier too; `tui.bufferview.copy` (Y: the whole buffer) |
| `tui.fileview` | `tui.fileview.home` (~: the path field, ~/ written), `tui.fileview.filter` (/: the filter field), `tui.fileview.filterDone` (Enter: back to the listing, the filter kept), `tui.fileview.cancel` (Esc: the path field's folder put back, else the filter cleared), `tui.fileview.partDown` (Ctrl+J), `tui.fileview.partUp` (Ctrl+K), `tui.fileview.preview` (Ctrl+L), `tui.fileview.back` (Ctrl+H: from the preview). A file chooser's own keys, heard as they bubble up from its parts; each acts only where it belongs |
| `tui.cmdline` | `tui.cmdline.complete` (Tab), `tui.cmdline.completePrev` (Shift+Tab), `tui.cmdline.next` (Down), `tui.cmdline.prev` (Up: with the candidates' dropdown open). The command-line prompt's; each with any other modifier too |
| `tui.cmdline.replace` | `tui.cmdline.replace.yes` (Y), `tui.cmdline.replace.no` (N), `tui.cmdline.replace.quit` (Q): a command's replace question; either case, any modifier |
| `tui.terminal` | `tui.terminal.prefix` (Ctrl+\\: then Ctrl+N for normal mode, or Ctrl+\\ to send Ctrl+\\: the key after it is its grammar, the literal key, wherever a scheme moves the prefix), `tui.terminal.normalMode` (Esc: with vim keys, outside a full-screen program), `tui.terminal.restart` (Enter, I: once the program has exited). Input mode; every other key is the program's |
| `tui.terminal.normal` | `tui.terminal.normal.halfUp` (Ctrl+U), `tui.terminal.normal.halfDown` (Ctrl+D), `tui.terminal.normal.pageUp` (Ctrl+B, PgUp), `tui.terminal.normal.pageDown` (Ctrl+F, PgDn), `tui.terminal.normal.cancel` (Esc: a selection or g), `tui.terminal.normal.input` (I, A, Enter), `tui.terminal.normal.left` (H, Left), `tui.terminal.normal.right` (L, Right), `tui.terminal.normal.down` (J, Down), `tui.terminal.normal.up` (K, Up), `tui.terminal.normal.lineStart` (0, Home), `tui.terminal.normal.lineEnd` ($, End), `tui.terminal.normal.wordForward` (W), `tui.terminal.normal.wordBackward` (B), `tui.terminal.normal.wordEnd` (E), `tui.terminal.normal.goPrefix` (G, then the literal G: the first line), `tui.terminal.normal.bottom` (Shift+G), `tui.terminal.normal.visual` (V), `tui.terminal.normal.visualLine` (Shift+V), `tui.terminal.normal.yank` (Y: the selection), `tui.terminal.normal.search` (/), `tui.terminal.normal.searchBackward` (?), `tui.terminal.normal.next` (N), `tui.terminal.normal.prev` (Shift+N) |
| `tui.terminal.search` | `tui.terminal.search.close` (Esc), `tui.terminal.search.run` (Enter), `tui.terminal.search.backspace`: the normal-mode search line, which takes every key while open; typed text is not a command |
| `tui.scroll` | a ScrollView's keys, only when the focused child declines them: `tui.scroll.pageUp` (PgUp), `tui.scroll.pageDown` (PgDown), `tui.scroll.home` (Home), `tui.scroll.end` (End). The wheel scrolls it too |
| `tui.drawer` | `tui.drawer.close` (Esc), `tui.drawer.closeOnQ` (Q: where the Drawer says closeOnQ): a declarative Drawer's own keys, after its Shortcuts |
| `tui.float` | `tui.float.close` (Esc: a modal Float) |
| `tui.popup` | `tui.popup.close` (Esc: an anchored popup, such as a menu's or a select's) |
| `tui.image` | `tui.image.down` (Down, J), `tui.image.up` (Up, K), `tui.image.right` (Right, L), `tui.image.left` (Left, H), `tui.image.pageDown` (PgDn, ], Space), `tui.image.pageUp` (PgUp, [), `tui.image.bottom` (End, Shift+G), `tui.image.top` (Home, G): a scrollable Image |
| `tui.editor` | `tui.editor.contextMenu` (Shift+F10, Menu) |
| `tui.editor.<keyset>.<class>` | `tui.editor.<keyset>.<class>.<action>`: one per chord of the Vim, Standard and Nano tables, keyset `vim`, `standard` or `nano`, class `normal`, `visual` or `insert` (Standard and Nano, modeless, bind `insert` and `visual`, their selection), action the Editor's `Action` name with its first letter lowered (`undo`, `wordForward`, `deletePrefix`). Vim and TextEdit never conflict: each is its own scope. The grammar after a prefix (`d`'s motion, `f`'s character, a count) is not a command; its first key is |

## How this differs from Qt Creator

- The file's extension is `.xml`, as golib's catalogs' is; the shape is Qt Creator's `.kms`.
- Ids are golib's and the program's (`tui.list.down`), not Qt Creator's.
- A scope is checked for duplicates when the scheme loads, not reported per key press.

## Files

| file | holds |
| --- | --- |
| `keymap.go` | `Command`, `Scope` (`Global`, `App`), `Binding`, `Source`, `Problem`, `Error` |
| `scheme.go` | `Scheme`, `ParseScheme` |
| `map.go` | `Map`, `Build`, `KeysOf`, `Has`, `Bindings`, `Scopes`, `Warnings` |
