package extract

import (
	"context"
	"io"
)

// Extractor turns one document container into Markdown.
//
// # Architectural Contract & Design Invariants
//
//  1. Offset-Based Streaming Input: Reads the container via r ([io.ReaderAt]), which holds size bytes.
//     Because complex document formats (such as PDF cross-reference tables or DOCX ZIP directories)
//     locate central directories and indexes at the end of the file, r allows random-access offset
//     reads without requiring the full container to be buffered in memory.
//  2. Streamed Output: Emits Markdown chunks incrementally to w ([io.Writer]). Neither the input container
//     nor the rendered output text is held in memory in its entirety.
//  3. Cancellation & Lifecycle: Halts immediately when ctx is cancelled and returns ctx.Err().
//     Write errors from w terminate the extraction immediately and are returned to the caller.
//  4. Failure Semantics & Atomic Guarantees: Any content written to w prior to encountering an error
//     persists in the destination. Callers requiring all-or-nothing atomicity should direct w to a
//     temporary or discardable buffer (such as an atomic file write pattern).
//  5. Concurrency Invariant: An [Extractor] used inside a [Set] can be invoked concurrently across
//     multiple goroutines. Implementations must be safe for concurrent use; state should not be stored
//     on the extractor receiver across extraction calls.
type Extractor interface {
	Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error)
}

// Func adapts a function with signature matching [Extractor.Extract] into an [Extractor].
type Func func(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error)

// Extract calls f(ctx, r, size, w).
func (f Func) Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error) {
	return f(ctx, r, size, w)
}

// Info describes structural metadata discovered while extracting a document container.
// All fields are optional: an extractor populates the metadata its specific container format provides.
type Info struct {
	// Title is the document's embedded title, extracted from format-specific metadata
	// (such as a PDF Title dictionary entry or a DOCX core property dc:title).
	// When empty, [Set.Extract] falls back to the container file's basename without extension.
	Title string

	// Pages is the total count of rendered or physical pages for paginated formats (such as PDF or DOCX);
	// 0 if the format is unpaginated (such as plain text or HTML) or if page count is unavailable.
	Pages int

	// Scanned indicates whether a paginated document contains pages with no textual payload
	// (for example, a scanned PDF containing raster images exclusively). Optical character
	// recognition (OCR) is outside the scope of this package.
	Scanned bool
}
