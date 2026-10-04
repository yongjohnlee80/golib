package extract

import (
	"context"
	"errors"
	"io"
)

// textChunk is how much of the container Text copies at a time, and so the memory it holds; it is
// also how often it checks for cancellation.
const textChunk = 64 << 10

// Text is the extractor for containers that are already text: Markdown, plain text, logs. Plain text
// is valid Markdown, so Text copies the container to w as it is, a chunk at a time, holding no more
// than one chunk however large the file. It neither checks nor converts the encoding.
type Text struct{}

// Extract copies the container to w.
func (Text) Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error) {
	src := io.NewSectionReader(r, 0, size)
	buf := make([]byte, min(int64(textChunk), max(size, 1)))
	for {
		if err := ctx.Err(); err != nil {
			return Info{}, err
		}
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return Info{}, werr
			}
		}
		if errors.Is(err, io.EOF) {
			return Info{}, nil
		}
		if err != nil {
			return Info{}, err
		}
	}
}
