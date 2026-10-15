# Language packages — how a language lives in golib

A language golib understands — for highlighting, indentation, splitting or parsing — is one
package under `parse/`. This page is the rule for writing one, and for moving one. The provider
API it builds on is in [highlight](../highlight/README.md) (`Definition`, `Source`, `Catalog`);
[parse/languages](languages/README.md) composes the packages.

## 1. One package per language

```
parse/<lang>/
  README.md        what it reads, what it does not, and why
  doc.go           the package doc
  source.go        Definition() — or definition.go, Definitions() for a language with dialects
  …                forms, a scanner, a parser, as the language needs
```

- The package is named for the language (`golang`, `yaml`, `sql`), never for a use (`sqlhighlight`).
- A language with dialects exports `Definitions()`, one `Definition` per dialect, each named as
  KSyntaxHighlighting names it (`SQL (PostgreSQL)`).

## 2. What bytes ARE apart from what they MEAN

- Forms, a lexer, a splitter or a parser say what a run of bytes is: a string, a comment, a word, a
  statement's end. They know no keywords.
- A `Definition` says what the bytes mean to a reader: keywords, control flow, types, an indent
  policy. That is a judgement, and it lives in its own file.
- Both may live in the package, as `parse/yaml` holds a parser and a `Definition`, and `parse/sql`
  holds forms, a statement splitter and a highlighter. Neither imports the other's opinions: the
  splitter never colours, the highlighter never decides where a statement ends.

## 3. A Definition makes per-document Sources

```go
func Definition() highlight.Definition {
	return highlight.Definition{Name: "Go", Extensions: []string{"*.go"}, Aliases: []string{"go", "golang"},
		SourceFactory: func(*highlight.Catalog) highlight.Source { … }}
}
```

- Use `SourceFactory`, never the `Highlighter` field: a factory gives each document a highlighter of
  its own, with its own states, an optional `indent.Policy` and an optional state store.
- File-name patterns go in `Extensions`; Markdown fence labels go in `Aliases`; a narrower pattern
  that must win over a wider one (`*.mysql.sql` over `*.sql`) takes a higher `Priority`.

## 4. Registration and imports

- A language is registered in one place: `parse/languages.Definitions()`.
- A language package imports `highlight`, `indent`, `parse` and its own helpers. It imports no
  Markdown, UI, QML or cgo package, and no other language package (`internal/audit` holds the
  `parse/*` layering).

## 5. Tests

- The highlighter's vocabulary and states: each construct's style, and each construct left open at
  the end of a line carried to the next.
- Forms and parsers through `parse/parsetest`, which drives every split of the input.
- A Markdown fence by each alias, and a file name by each pattern, reach the definition through a
  `Repository`.
- A rule that decides a boundary (a comment, a quote, a statement's end) is tested against the
  package without the rule, so the test shows what the rule changes.

## 6. Moving a language

1. The new home first: the package above, registered in `parse/languages`, tested.
2. The old import path stays, as forwarders with a `// Deprecated:` line naming the new home,
   returning what it always returned.
3. A golden of the old behaviour, captured from the code before it moves, holds both paths to it.
4. The old path is removed only in a later release that says so.

`highlight/sql` → `parse/sql` is the worked example.
