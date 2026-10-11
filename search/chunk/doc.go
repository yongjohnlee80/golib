// Package chunk divides structured and unstructured documents into discrete sections
// suitable for search indexing and vector embedding.
//
// # Chunking Strategies
//
// The package provides format-aware splitting and budget-packing policies:
//   - Markdown ([Markdown]): Groups blocks under heading hierarchies, packing blocks up to about
//     350/512 of the configured token budget (with the budget as a hard cap). Individual list items
//     are treated as atomic units (splitting oversized lists by item), while oversized tables, code
//     blocks, or paragraphs are split across lines.
//   - Plain Text ([Text]): Packs paragraphs up to about 350/512 of the token budget (capped at the
//     configured limit), splitting oversized paragraphs along line breaks.
//   - YAML ([YAML]): Flattens top-level mapping or sequence entries into "key > path: value" lines,
//     packing lines within the token budget.
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
