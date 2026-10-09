# parse/languages

Install: `go get github.com/yongjohnlee80/golib/parse/languages`.

`Definitions()` composes the shipped Go, Rust, JavaScript, TypeScript, Python,
Lua, POSIX/Bash and YAML source providers. Each language owns its behavior;
this package owns only composition and imports no Markdown/UI package.

```go
repo := highlight.NewRepository(languages.Definitions()...)
repo.Add(customDefinition)
catalog := repo.Snapshot()
definition, ok := catalog.DefinitionForFileName("main.go")
source := definition.NewSource(catalog)
```

Register once, snapshot after overrides, and give the same catalog to source
editing and Markdown. `SourceFactory` creates per-document highlighter, optional
indent policy and optional leased state store. A caller that drives a stateful
source directly retains its active states before collecting unused tuples; an
`EditorCore` owns that lifecycle automatically through `CoreSourceFactory`.

Declarative programs use `tuidecl.Highlighters` for extensions. Auto-indent is
opt-in via `Editor.autoIndent`; paste remains literal. Legacy `Highlighter`
definitions remain supported. See [highlight](../../highlight/README.md),
[indent](../../indent/README.md) and [LICENSE](../../LICENSE).
