# parse/qml — a faithful QML parser

`qml` reads a QML document into a plain data tree, `SpecTree`. It judges
syntax, not meaning: whether a type exists or a property can hold a colour is
for whoever consumes the tree — the [`decl`](../../decl/README.md) engine, in
golib.

```go
import "github.com/yongjohnlee80/golib/parse/qml"

spec, err := qml.QML{File: "editor.qml"}.Parse(src)
```

## What it reads

```
Root     := { Import } Node
Import   := 'import' DottedName [ Version ] [ 'as' Qualifier ]
Node     := TypeName '{' Body '}'                   // Text { }, T.Rectangle { }
Body     := ( Property | Handler | Group | Node )*
Property := PropName ':' Value [ ';' ]
Group    := PropName '{' ( Property )* '}'          // font { bold: true }
PropName := Ident { '.' Ident }                     // plain, grouped, or attached
Handler  := 'on' Ident ':' JavaScript               // one statement, or a block
Value    := JavaScript                              // an expression
```

- **A property value is a JavaScript expression and a handler body is
  JavaScript statements**, because in QML that is what they are. Both are read
  by [`parse/js`](../js/README.md) on this parser's own scanner and depth budget,
  so nesting is bounded across the two grammars rather than within each.
- **A capitalised name followed by `{` is a node; a lower-case one is a group.**
  It is the LAST segment that decides: `T.Rectangle { }` is a type reached through
  a qualified import, `anchors { }` a grouped property. `Dock.edge: Tui.Top` is
  a dotted property.
- **`;` may end a property**, so several fit on a line:
  `MenuItem { text: "&New"; onTriggered: App.newFile() }`.
- **There are no extensions.** Styles are a singleton reached through an import,
  as in QML — not a sigil of this parser's.

## The tree

| type | holds |
| --- | --- |
| `SpecTree` | `Imports`, and the `Root` node |
| `SpecImport` | `Module`, `Version`, `Alias` (after `as`), `Pos` |
| `SpecNode` | `Type`, `ID`, `Props` (document order), `Handlers`, `Children`, `Pos` |
| `SpecProp` | `Name` (dotted: `palette.window`), `Value` |
| `SpecHandler` | `Signal`, the JavaScript `Body`, `Pos` |
| `SpecValue` | a `Kind` and the value as written |

`SpecValue` **projects** the shapes a consumer asks about constantly —
`SpecValueString`, `Number`, `Bool`, `Ref` (`Theme.menu.window`), `Call`
(`App.save()`) — and keeps the parsed tree for everything else as
`SpecValueExpr`. That is a convenience, not a smaller grammar: `parent.width / 2`
parses, and a consumer that cannot evaluate it declines it by name and position.
`ProjectValue(expr)` applies the same projection to a subexpression — an operand
of `Dialog.Yes | Dialog.No`.

Numbers are kept as WRITTEN (`Raw`), undecoded: a consumer with a numeric
policy knows how to read `0x4000`, and this package would be guessing.

## Positions

Every node, property, value and handler carries a `parse.Position`. Give the
document a name and every position carries it — the JavaScript inside
included — so a screen made of several files reports the right one:

```
dialogs/QuitDialog.qml:14:5: want } to close Dialog opened here, got end of input
```

A document that ends in the middle of a construct fails with `Incomplete` set,
and the position of where the construct OPENED — which lets a file watcher tell
"still being written" from "wrong" and keep the last good screen meanwhile.

## Limits

`MaxDepth` bounds nesting (default `DefaultQMLMaxDepth`, 64), across QML and
the JavaScript inside it: a recursive parser handed a hostile or half-written
file must fail with an error, not a stack overflow that cannot be recovered.

## Licence

See the repository's [LICENSE](../../LICENSE).
