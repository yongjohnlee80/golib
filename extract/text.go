package extract

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/yongjohnlee80/golib/errs"
)

// textChunk is how much of the container Text copies at a time, and so the memory it holds; it is
// also how often it checks for cancellation.
const textChunk = 64 << 10

// Text is the extractor for containers that are already text: Markdown, plain text, logs. Plain text
// is valid Markdown, so Text copies the container to w as it is, a chunk at a time, holding no more
// than one chunk however large the file. It neither checks nor converts the encoding.
//
// A negative size is an error wrapping errs.ErrInvalidArgument, as it is through a [Set]. A source
// that ends before size bytes is an error wrapping io.ErrUnexpectedEOF, so a file that
// shrank after its size was read is not taken as complete. A destination that accepts fewer bytes
// than it was given is io.ErrShortWrite.
type Text struct{}

// Extract copies the container to w.
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
