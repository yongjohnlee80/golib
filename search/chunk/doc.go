// Package chunk divides structured and unstructured documents into discrete sections
// suitable for search indexing and vector embedding.
//
// # Chunking Strategies
//
// The package provides format-aware splitting policies that preserve semantic continuity:
//   - Markdown ([Markdown]): Split along heading boundaries, preserving document titles
//     and heading breadcrumbs. Atomic blocks (tables, code blocks, lists) are kept contiguous
//     unless an individual block exceeds the configured token budget.
//   - Plain Text ([Text]): Split by paragraph boundaries and empty lines.
//   - YAML ([YAML]): Split into top-level key-value trees.
//
// Each produced chunk preserves byte offset coordinates referencing the original source document,
// enabling exact-span highlighting in search results.
//
// # Registry Integration
//
// [MarkdownChunker], [TextChunker], and [YAMLChunker] implement the [search.Chunker] interface,
// allowing applications to assemble extension-routed chunking registries:
//
//	var r search.Chunkers
//	_ = r.Register(".md", chunk.MarkdownChunker{})
//	_ = r.Register(".txt", chunk.TextChunker{})
//	_ = r.Register(".yaml", chunk.YAMLChunker{})
package chunk
