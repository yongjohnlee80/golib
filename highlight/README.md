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

**Definitions.** A `Definition` is a highlighter by name with the file-name
patterns it claims (`*.qml`) — KSyntaxHighlighting's Definition — and a
`Repository` holds them: `Definition(name)`, and `DefinitionForFileName(file)`
for a view that shows whatever file it is given, a file dialog's preview.

Implementations: `parse/qml.Highlighter()` (QML and its JavaScript) and
`highlight/sql.Highlighter(dialect)` (PostgreSQL, SQLite, MySQL; its
`Definitions()` names them as KSyntaxHighlighting does). Painters:
`tui/widget.Editor` (`WithHighlighter`), and `SyntaxHighlighter` in
[tui/decl](../tui/decl/README.md).

## Source providers and immutable catalogs

`Definition.SourceFactory` creates a per-document `Source`: highlighter,
optional `indent.Policy`, and optional leased state store. Source-language
modules in `parse/` supply these definitions; Markdown borrows them.

`Repository.Snapshot()` produces an immutable `Catalog` after registrations and
overrides. Name, filename and case-insensitive alias lookup share deterministic
priority/name selection. Returned metadata slices are copied.

```go
catalog := highlight.NewRepository(languages.Definitions()...).Snapshot()
definition, ok := catalog.DefinitionForLanguage("golang")
source := definition.NewSource(catalog)
```

`Overlay` decorates paint spans without receiving or changing lexical state.
Editor paint invalidation keeps verified source context available to indentation.
See [languages](../parse/languages/README.md) for lifecycle/configuration examples.

## Licence

See the repository's [LICENSE](../LICENSE).
