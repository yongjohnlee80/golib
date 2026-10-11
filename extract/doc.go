// Package extract turns document containers (PDF, DOCX, HTML, plain text) into Markdown,
// streaming, so that a search index or an embedding model reads structured text rather than raw files.
//
// # Architectural Mission & Design Philosophy
//
// Document ingestion for search and LLM retrieval pipelines often struggles with memory blowup,
// unconstrained third-party dependency graphs, and format fragmentation. Most off-the-shelf extractors
// read complete container files into heap memory or shell out to heavyweight external processes.
//
// golib/extract is built on four core architectural pillars:
//
//  1. Zero-Dependency Core Seam: The base package imports only the standard library and golib/errs.
//     Formats that require standard library parsers (plain text, DOCX via archive/zip and encoding/xml,
//     HTML via parse/html) live in pure Go subpackages. Heavy format parsers requiring third-party
//     modules (such as PDF) remain leaf packages in downstream products and bridge seamlessly
//     via the [Extractor] interface.
//  2. Streaming Seams & Bounded Heap Footprint: An [Extractor] accepts [io.ReaderAt] and writes
//     to an [io.Writer]. Formats with terminal metadata tables (such as PDF cross-reference tables or
//     DOCX ZIP directories) read at precise byte offsets without whole-file buffering. Extractors stream
//     output or bound intermediate structures according to format requirements: [Text] streams chunks,
//     [docx.Extractor] streams paragraphs token-by-token, while DOM-based parsers like [html.Extractor]
//     perform bounded buffered parsing before writing blocks.
//  3. Decoupled Two-Limit Protection Model: Enforces separate bounds for input containers
//     ([MaxContainer]) and produced Markdown text ([MaxText]). A 100 MiB PDF manual may contain only
//     1 MiB of textual prose (mostly images and fonts), whereas 16 MiB of extracted Markdown represents
//     millions of words that downstream tokenizers and vector embeddings must budget for. Note that
//     [MaxText] bounds the cumulative text emitted to the destination [io.Writer], not an extractor's
//     internal parse-tree or rendering allocations (which are bounded by extractor-specific limits).
//  4. Concurrency Safety: Extractor implementations are stateless or keep per-document state
//     strictly bound to call frames, allowing a single [Set] instance to serve high-throughput
//     concurrent pipeline workers safely across goroutines.
//
// # The Streaming Extraction Pipeline
//
//	┌────────────────────────────────────────────────────────────────────────┐
//	│                           Source Container                             │
//	│                (File, SectionReader, Object Storage)                   │
//	│                     Accessed via io.ReaderAt                           │
//	└───────────────────────────────────┬────────────────────────────────────┘
//	                                    │
//	                                    ▼
//	                     ┌──────────────────────────────┐
//	                     │          extract.Set         │
//	                     │   1. Validate Context & Size │
//	                     │   2. Enforce MaxContainer    │
//	                     │   3. Select Extractor by Ext │
//	                     └──────────────┬───────────────┘
//	                                    │
//	                                    ▼
//	                     ┌──────────────────────────────┐
//	                     │      Target Extractor        │
//	                     │   (Text, docx, html, custom) │
//	                     │   Streams tokens / chunks    │
//	                     └──────────────┬───────────────┘
//	                                    │ Writes via limitWriter
//	                                    ▼
//	                     ┌──────────────────────────────┐
//	                     │     Markdown Destination     │
//	                     │   (Enforces MaxText Limit)   │
//	                     │     Emits to io.Writer       │
//	                     └──────────────────────────────┘
//
// # Concurrency & Goroutine Safety
//
// A configured [Set] is immutable and safe for concurrent use across any number of goroutines.
// Implementations of [Extractor] must be safe for concurrent use; per-document state belongs
// inside the [Extractor.Extract] call frame, never on the receiver.
//
// # Usage Examples
//
// Setting up an extraction set with multiple formats, custom extractors, and limits:
//
//	set := extract.New(
//		extract.Register(extract.Text{}, ".md", ".markdown", ".txt"),
//		extract.Register(docx.Extractor{}, ".docx"),
//		extract.Register(pdfExtractor, ".pdf"), // external or application-specific extractor
//		extract.MaxContainer(250<<20),          // refuse files larger than 250 MiB before reading
//		extract.MaxText(16<<20),                // bound output Markdown to 16 MiB
//	)
//
//	f, err := os.Open(path)
//	if err != nil {
//		return err
//	}
//	defer f.Close()
//
//	st, err := f.Stat()
//	if err != nil {
//		return err
//	}
//
//	var out bytes.Buffer
//	info, err := set.Extract(ctx, path, f, st.Size(), &out)
//	if err != nil {
//		return err
//	}
//	fmt.Printf("Extracted %q (%d pages): %d bytes of Markdown\n", info.Title, info.Pages, out.Len())
package extract
