// Package docx extracts Markdown from Microsoft Word documents (.docx) as a golib [extract.Extractor],
// using the Go standard library alone (archive/zip, encoding/xml).
//
// # Architectural Mission & Design Philosophy
//
// Word documents (.docx) are compressed ZIP archives containing OpenXML schemas. Full OpenXML parsers
// typically carry massive dependency trees, allocate extensively, and buffer entire documents.
//
// golib/extract/docx provides a high-throughput, low-allocation streaming converter built on four principles:
//
//  1. Zero Third-Party Dependencies: Implemented strictly with standard library archive/zip and encoding/xml.
//  2. Token-Driven XML Streaming: Document body XML (word/document.xml) is parsed sequentially token by token.
//     Only one paragraph or table row is held in memory at any given instant.
//  3. Defense-in-Depth Zip-Bomb & Resource Bounds: Decompression counters track decompressed bytes per-entry
//     ([DefaultMaxEntry]) and in aggregate ([DefaultMaxTotal]). Because ZIP header metadata can be spoofed,
//     decompression limits are enforced actively during reads. XML nesting depth ([DefaultMaxDepth])
//     and paragraph/row block sizes ([DefaultMaxBlock]) are likewise bounded.
//  4. Clean CommonMark Formatting: Generates consistent GitHub Flavored Markdown (GFM):
//     headings (#–######), bullet/decimal lists with hierarchical indentation, table grids with pipe escaping,
//     inline line breaks (<br>), and title/page metadata extraction.
//
// # Processing Pipeline
//
//	┌────────────────────────────────────────────────────────┐
//	│                 DOCX Container (ZIP)                   │
//	│               Accessed via io.ReaderAt                 │
//	└───────────────────────────┬────────────────────────────┘
//	                            │
//	                            ├─► 1. docProps/core.xml, app.xml (Title & Pages)
//	                            ├─► 2. word/styles.xml (Paragraph styles & Heading levels)
//	                            ├─► 3. word/numbering.xml (List levels & formats)
//	                            │
//	                            ▼
//	┌────────────────────────────────────────────────────────┐
//	│           word/document.xml Streaming Loop             │
//	│    - Token-by-token XML traversal (bounded depth)      │
//	│    - Buffered Paragraphs & Table Rows (bounded size)   │
//	│    - Markdown block emission to io.Writer              │
//	└────────────────────────────────────────────────────────┘
//
// # Concurrency
//
// [Extractor] holds no mutable state and is safe for concurrent use across multiple goroutines.
package docx
