# search/chunk — documents cut into searchable sections

Markdown is cut by its headings, plain text by its paragraphs, and YAML by its top-level entries.
Every chunk keeps its byte span in the source, and a breadcrumb of the title and headings above it.
Chunks stay within a budget of estimated tokens (`Tokens`: the larger of 1.3 per word and one per
four bytes). A table, a code block or a list is split only when one alone is over the budget. A
split table repeats its header in every part, and a split code block repeats its fence.

## Install

```go
import "github.com/yongjohnlee80/golib/search/chunk"
```

## Example

```go
doc := markdown.Parse(src, markdown.GFM(), markdown.Obsidian())
meta := chunk.ReadMeta(doc, "guides/storage.md") // title, tags, aliases, frontmatter as JSON
for _, c := range chunk.Markdown(doc, meta.Title, 512) {
	fmt.Println(c.Ord, c.Breadcrumb, c.ByteStart, c.ByteEnd)
}
_, yamlChunks := chunk.YAML(src, "config/service.yaml", 0) // 0: DefaultTokens
textChunks := chunk.Text(src, "notes", 0)
```

The functions take a document already parsed, so a caller that also reads its links parses once.
`MarkdownChunker`, `TextChunker` and `YAMLChunker` are the same as `search.Chunker` values, for a
`search.Chunkers` registry. `Version` changes whenever the same document would be cut differently.

`Snippet` is the start of a chunk's text, cut at a rune boundary: a semantic hit's snippet.

## License

See [LICENSE](../../LICENSE).
