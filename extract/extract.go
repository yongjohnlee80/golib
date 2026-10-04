package extract

import (
	"context"
	"io"
)

// Extractor turns one document container into Markdown.
//
// It reads the container through r, which holds size bytes, and writes the Markdown to w as it goes,
// so neither the container nor its text has to be held in memory whole. A PDF's cross-reference
// table sits at the end of the file and a DOCX is a ZIP archive whose directory sits there too, so
// an extractor needs to read at offsets, not only forward: r is an io.ReaderAt for that reason.
//
// An extractor stops when ctx is cancelled and returns ctx's error. A write error from w ends the
// extraction and is returned. Whatever was written before an error stays written; a caller that must
// not keep half a document writes to a destination it can discard, as an atomic file write does.
//
// A [Set] calls one Extractor from many goroutines at once, so an implementation used through a Set
// must be safe for concurrent use. [Text] keeps no state, and neither should most extractors: the
// per-document state belongs to the call.
type Extractor interface {
	Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error)
}

// Func adapts a function to an [Extractor].
type Func func(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error)

// Extract calls f.
func (f Func) Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error) {
	return f(ctx, r, size, w)
}

// Info describes the document an extraction read. Every field is optional: an extractor fills what
// its format knows.
type Info struct {
	// Title is the document's own title, such as a PDF's Title entry or a DOCX's core property.
	// [Set.Extract] uses the file's name when it is empty.
	Title string
	// Pages is the number of pages, for a format that has them; 0 otherwise.
	Pages int
	// Scanned reports a container that holds pages but no text, such as a scanned PDF: its text
	// can only come from OCR, which an extractor does not do.
	Scanned bool
}
