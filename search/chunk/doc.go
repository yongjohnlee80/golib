// Package chunk cuts documents into the sections a search index holds: Markdown by its headings,
// plain text by its paragraphs, YAML by its top-level entries. Every chunk keeps its source span
// and a breadcrumb of the title and headings above it, and stays within a budget of estimated
// tokens, splitting a table, a code block or a list only when one alone is over it.
//
//	doc := markdown.Parse(src, markdown.GFM(), markdown.Obsidian())
//	meta := chunk.ReadMeta(doc, "notes/storage.md")
//	chunks := chunk.Markdown(doc, meta.Title, 512)
//
// MarkdownChunker, TextChunker and YAMLChunker are the same as search.Chunker values, for a
// search.Chunkers registry:
//
//	var r search.Chunkers
//	_ = r.Register(".md", chunk.MarkdownChunker{})
//	_ = r.Register(".txt", chunk.TextChunker{})
//	_ = r.Register(".yaml", chunk.YAMLChunker{})
package chunk
