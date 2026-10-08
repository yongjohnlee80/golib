# docx

Streaming Markdown extraction from Word documents, as a golib `extract.Extractor`.

## Install

```go
import "github.com/yongjohnlee80/golib/extract/docx"
```

## Features

- Headings (a style's outline level, or a "heading N" / "Title" name) as `#`–`######`.
- List items as `- ` or `1. ` by the numbering format, indented by level.
- Tables as GFM tables, pipes escaped and line breaks inside a cell as `<br>`.
- Hyperlinks keep their text; media, deleted text, headers, footers and comments are skipped.
- `Info.Title` from `docProps/core.xml`, `Info.Pages` from `docProps/app.xml`.
- Bounded memory: one paragraph or table row at a time; every entry read through a counting limit,
  per entry (`MaxEntry`) and across entries (`MaxTotal`), so a zip bomb stops; XML depth
  (`MaxDepth`) and block size (`MaxBlock`) capped. A 6 MiB document grows the heap by about 1.5 MiB.
- `ID()` and `Version()` for a derived cache's key.

## Example

```go
s := extract.New(extract.Register(docx.Extractor{}, ".docx"), extract.MaxText(16<<20))
info, err := s.Extract(ctx, "manual.docx", f, size, w)
```

## License

See [LICENSE](../../LICENSE).
