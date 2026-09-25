# highlight — syntax highlighting, the contract

What colours source text and what paints it agree on this package, and on
nothing else: a parser implements a `Highlighter`, a widget paints one, and
neither imports the other.

```go
type Highlighter interface {
    HighlightBlock(line string, previous State) (spans []Span, next State)
}
```

It is Qt's `QSyntaxHighlighter`: one line at a time, with a `State` carried
from each line to the next for what spans lines — a block comment, a template
string. A highlighter must never refuse: text being typed is invalid most of
the time.

| type | is |
| --- | --- |
| `Style` | KSyntaxHighlighting's `TextStyle` — all 31, in KDE's order and names (`keyword`, `controlFlow`, `dataType`, `string`, `comment`, …). A theme colours every language from one table |
| `State` | the block state; 0 is "nothing open" |
| `Span` | a styled run in the line's **byte** offsets, `[Start, End)`, with an optional finer `Name` |
| `HighlighterFunc` | a Highlighter written as a function |
| `StyleForCapture(name)` | tree-sitter capture → Style, by longest known prefix (`keyword.control.import` → `ControlFlow`) |

**From a syntax tree.** A program that already parses — with tree-sitter, say —
implements `Highlighter` from its tree: each node's byte range is a `Span`,
`StyleForCapture` gives its style, and the capture name is kept in `Span.Name`.

Implementations: `parse/qml.Highlighter()` (QML and its JavaScript). Painters:
`tui/widget.Editor` (`WithHighlighter`), and `SyntaxHighlighter` in
[tui/decl](../tui/decl/README.md).

## Licence

See the repository's [LICENSE](../LICENSE).
