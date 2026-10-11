package extract

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/yongjohnlee80/golib/errs"
)

// textChunk defines the buffer size (64 KiB) allocated for streaming text extraction.
// It establishes the constant bounded heap footprint of [Text.Extract] and dictates how
// frequently the streaming loop samples context cancellation during large file transfers.
const textChunk = 64 << 10

// Text provides an [Extractor] implementation for document containers that are already text-native
// (such as Markdown (.md, .markdown), plain text (.txt), and log files).
//
// # Architectural Invariants & Memory Bounds
//
//  1. Chunked Pass-Through Streaming: Copies the container payload directly to w in 64 KiB chunks
//     ([textChunk]). The maximum allocated heap memory remains capped at one chunk regardless of
//     whether the underlying source container is a few kilobytes or hundreds of gigabytes.
//  2. Encoding Neutrality: Plain text is valid CommonMark/Markdown. Text performs no transcoding or
//     character set conversion, passing bytes through as written.
//  3. Truncation & Shrinkage Detection: If the underlying reader encounters [io.EOF] before yielding
//     size bytes, Extract fails with an error wrapping [io.ErrUnexpectedEOF]. This guarantees that
//     concurrent file truncation or partial writes are not indexed as complete documents.
//  4. Stateless Concurrency: Text stores no mutable state and is safe for concurrent use across
//     any number of goroutines.
type Text struct{}

// Extract copies size bytes from r to w in bounded 64 KiB chunks, checking for context cancellation
// at each chunk boundary.
//
// It returns an error wrapping [errs.ErrInvalidArgument] if size is negative, [io.ErrUnexpectedEOF]
// if the reader yields fewer than size bytes, or [io.ErrShortWrite] if destination w accepts fewer
// bytes than requested without error.
func (Text) Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error) {
	if size < 0 {
		return Info{}, errs.Wrap(errs.ErrInvalidArgument, "extract: negative size %d", size)
	}
	src := io.NewSectionReader(r, 0, size)
	buf := make([]byte, min(int64(textChunk), max(size, 1)))
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return Info{}, err
		}
		n, err := src.Read(buf)
		if n > 0 {
			written, werr := w.Write(buf[:n])
			if werr != nil {
				return Info{}, werr
			}
			if written < n {
				return Info{}, io.ErrShortWrite
			}
			copied += int64(n)
		}
		if errors.Is(err, io.EOF) {
			if copied < size {
				return Info{}, fmt.Errorf("%w: read %d of %d bytes", io.ErrUnexpectedEOF, copied, size)
			}
			return Info{}, nil
		}
		if err != nil {
			return Info{}, err
		}
	}
}
