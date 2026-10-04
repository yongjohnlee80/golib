package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

var (
	// ErrUnsupported is returned for a file whose extension no extractor is registered for.
	ErrUnsupported = errs.Sentinel(errs.ErrUnsupported, "extract: no extractor for this format")

	// ErrContainerTooLarge is returned, before anything is read, for a file larger than
	// [MaxContainer].
	ErrContainerTooLarge = errs.Sentinel(errs.ErrInvalidArgument, "extract: file is larger than the container limit")

	// ErrTextTooLarge is returned when the extracted Markdown grows past [MaxText]. The text written
	// up to the limit stays written.
	ErrTextTooLarge = errs.Sentinel(errs.ErrInvalidArgument, "extract: extracted text is larger than the text limit")
)

// Set chooses an [Extractor] by file extension and enforces two separate size limits.
//
// The two limits are separate because a container's size says little about its text. A 100 MiB PDF
// manual is mostly images and fonts and may hold 1 MiB of text, while 16 MiB of Markdown is millions
// of words. [MaxContainer] bounds what may be opened at all; [MaxText] bounds the text that comes
// out, which is what an index and an embedding model pay for.
//
// A Set is configured once by [New] and is safe for concurrent use.
type Set struct {
	byExt        map[string]Extractor
	maxContainer int64
	maxText      int64
}

// Option configures a [Set].
type Option func(*Set)

// Register makes e the extractor for each of exts. An extension is written with or without its
// leading dot and is matched without regard to case: ".PDF", "pdf" and ".pdf" are one extension. A
// later registration of an extension replaces an earlier one, so a caller can override a default.
// A nil Extractor or a nil [Func] is refused. A nil pointer of another type is accepted, since its
// methods may be written to work on a nil receiver.
func Register(e Extractor, exts ...string) Option {
	return func(s *Set) {
		if e == nil {
			panic("extract: Register: nil Extractor")
		}
		if f, ok := e.(Func); ok && f == nil {
			panic("extract: Register: nil Func")
		}
		if len(exts) == 0 {
			panic("extract: Register: no extensions")
		}
		for _, ext := range exts {
			key := normalize(ext)
			if key == "." {
				panic("extract: Register: empty extension")
			}
			s.byExt[key] = e
		}
	}
}

// MaxContainer refuses, before reading anything, a file larger than n bytes. Zero, the default,
// sets no limit.
func MaxContainer(n int64) Option { return func(s *Set) { s.maxContainer = n } }

// MaxText ends an extraction whose Markdown grows past n bytes. Zero, the default, sets no limit.
func MaxText(n int64) Option { return func(s *Set) { s.maxText = n } }

// New returns a Set configured by opts. It registers no extractor of its own: a caller registers
// [Text] for the formats that are already text, and an extractor of its own for each container
// format it handles. A misconfiguration (a nil extractor, a missing or empty extension, a negative
// limit) panics here, where the wiring is written, rather than when a file arrives.
func New(opts ...Option) *Set {
	s := &Set{byExt: map[string]Extractor{}}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	if s.maxContainer < 0 || s.maxText < 0 {
		panic("extract: New: a limit is negative")
	}
	return s
}

// Supports reports whether an extractor is registered for name's extension.
func (s *Set) Supports(name string) bool {
	_, ok := s.byExt[normalize(filepath.Ext(name))]
	return ok
}

// Extract extracts the container name, of size bytes read through r, writing its Markdown to w.
// name chooses the extractor by its extension and supplies the title when the extractor finds none;
// it is never opened.
//
// It returns [ErrUnsupported] for an extension with no extractor and [ErrContainerTooLarge] for a
// file over the container limit, both before reading anything. [ErrTextTooLarge] is returned when the
// text passes the text limit, even if the extractor ignored the failed write. A failure of w itself,
// such as a full disk, is always returned, even if the extractor ignored it, and alongside
// ErrTextTooLarge when both happened. Any other error is the extractor's, wrapped with name.
func (s *Set) Extract(ctx context.Context, name string, r io.ReaderAt, size int64, w io.Writer) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	ext := filepath.Ext(name)
	e, ok := s.byExt[normalize(ext)]
	if !ok {
		return Info{}, errs.Wrap(ErrUnsupported, "extract %s", name)
	}
	if s.maxContainer > 0 && size > s.maxContainer {
		return Info{}, errs.Wrap(ErrContainerTooLarge, "extract %s: %d bytes, limit %d", name, size, s.maxContainer)
	}

	lw := &limitWriter{w: w, max: s.maxText}
	info, err := e.Extract(ctx, r, size, lw)
	switch {
	case lw.exceeded:
		return info, errs.WrapCause(ErrTextTooLarge, lw.err, "extract %s: limit %d", name, s.maxText)
	case lw.err != nil && err == nil:
		return info, fmt.Errorf("extract %s: write: %w", name, lw.err)
	case lw.err != nil && !errors.Is(err, lw.err):
		return info, fmt.Errorf("extract %s: %w (write: %w)", name, err, lw.err)
	case err != nil:
		return info, fmt.Errorf("extract %s: %w", name, err)
	}
	if info.Title == "" {
		info.Title = strings.TrimSuffix(filepath.Base(name), ext)
	}
	return info, nil
}

// normalize lowercases an extension and gives it its leading dot.
func normalize(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

// limitWriter passes writes through until max bytes have been written; max 0 sets no limit. A write
// that would pass the limit writes the part that fits and fails. exceeded records the breach and err
// the destination's first failure, so both reach the caller even from an extractor that ignored the
// error it was given.
type limitWriter struct {
	w        io.Writer
	max      int64
	n        int64
	exceeded bool
	err      error
}

func (l *limitWriter) Write(p []byte) (int, error) {
	over := false
	if room := l.max - l.n; l.max > 0 && int64(len(p)) > room {
		l.exceeded, over = true, true
		p = p[:room]
	}
	n, err := l.w.Write(p)
	l.n += int64(n)
	if err != nil {
		if l.err == nil {
			l.err = err
		}
		return n, err
	}
	if over {
		return n, ErrTextTooLarge
	}
	return n, nil
}
