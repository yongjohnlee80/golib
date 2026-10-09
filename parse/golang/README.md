# parse/golang

Install: `go get github.com/yongjohnlee80/golib/parse/golang`.

`Definition()` supplies Go vocabulary, comments, interpreted/raw literals and
structural indentation for editable source. It is a tolerant lexical provider,
not a Go compiler or formatter.

```go
definition := golang.Definition()
source := definition.NewSource(nil)
```

Use the complete source on `EditorCore` through `CoreSourceFactory`; it owns
highlighting, indentation and state leases together. The default indent unit is
a tab, overridable by the editor. Markdown borrows the same provider by `go` or
`golang`. See [languages](../languages/README.md) and [LICENSE](../../LICENSE).
