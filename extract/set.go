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

// Set routes document extraction to registered format [Extractor] implementations by file extension
// and enforces decoupled size limits on the container and the resulting Markdown text.
//
// # Architectural Mission & Dual-Limit Rationale
//
// Document containers exhibit wide variance in payload composition. A 100 MiB PDF manual composed
// mostly of vector diagrams and high-resolution raster images may yield only 1 MiB of textual prose.
// Conversely, a 16 MiB plain Markdown or documentation file contains millions of words capable of
// overwhelming downstream vector embeddings, search indices, and tokenizer context windows.
//
// Set separates these concerns into two independently configurable guards:
//   - [MaxContainer]: Refuses oversized archive files before any parsing or decompression begins,
//     preventing disk or I/O exhaustion.
//   - [MaxText]: Caps the cumulative Markdown bytes written to destination [io.Writer], protecting
//     downstream consumers regardless of how much text an extractor yields.
//
// # Concurrency & Goroutine Safety
//
// A Set is configured at construction via [New] and is safe for concurrent use across any number
// of goroutines. Extractor lookup is read-only and lock-free once initialized.
type Set struct {
	byExt        map[string]Extractor
	maxContainer int64
	maxText      int64
}

// Option configures functional parameters on a [Set] during [New].
type Option func(*Set)

// Register associates the given [Extractor] with one or more file extensions.
//
// File extensions are normalized case-insensitively and may be supplied with or without a leading dot
// (".PDF", "pdf", and ".pdf" map to the identical key ".pdf"). Later registrations override earlier
// ones for the same extension, enabling callers to substitute default format handlers with custom
// implementations.
//
// # Panic Invariants
//
// The returned Option closure panics when applied (typically within [New]) if:
//   - e is nil or a nil [Func] value.
//   - exts is empty, or any normalized extension evaluates to a bare dot (".").
//
// Typed nil pointers (such as (*MyExtractor)(nil)) are accepted, accommodating types whose methods
// handle nil receivers deliberately.
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

// MaxContainer establishes an upper bound (in bytes) on the source container size.
// Files exceeding this limit are refused by [Set.Extract] before reading any content.
// A value of 0 (the default) disables container size checking.
func MaxContainer(n int64) Option { return func(s *Set) { s.maxContainer = n } }

// MaxText establishes an upper bound (in bytes) on the total Markdown text written to the destination writer.
// If the extracted text exceeds n bytes, extraction is aborted and [ErrTextTooLarge] is returned.
// Any text written up to the limit remains written in the destination.
// A value of 0 (the default) imposes no limit on extracted text size.
func MaxText(n int64) Option { return func(s *Set) { s.maxText = n } }

// New constructs and returns a [Set] configured by opts.
//
// New registers no extractors by default: callers explicitly register [Text] for plain text/Markdown
// formats and format-specific extractors (such as [docx.Extractor] or application-provided PDF extractors)
// for each container format supported.
//
// # Panic Invariants
//
// New validates configuration eagerly and panics if [MaxContainer] or [MaxText] is configured with
// a negative number, or if any [Register] option fails its validation rules. This fail-early behavior
// surfaces misconfigurations at bootstrap rather than during runtime file ingestion.
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

// Supports reports whether an extractor has been registered for the extension of name.
// Path matching is performed using case-insensitive extension normalization.
func (s *Set) Supports(name string) bool {
	_, ok := s.byExt[normalize(filepath.Ext(name))]
	return ok
}

// Extract extracts the document container identified by name, reading size bytes from r and streaming
// the resulting Markdown to w.
//
// # Execution & Validation Order
//
//  1. Context Cancellation: Returns ctx.Err() immediately if ctx is already cancelled.
//  2. Extractor Resolution: Resolves the appropriate [Extractor] by normalizing filepath.Ext(name).
//     Returns an error wrapping [ErrUnsupported] if no extractor is registered.
//  3. Size Preflight: Returns an error wrapping [errs.ErrInvalidArgument] if size is negative.
//  4. Container Limit Check: If [MaxContainer] is configured (> 0) and size exceeds it, returns an error
//     wrapping [ErrContainerTooLarge] before performing any I/O against r.
//  5. Stream Extraction & Text Limiting: Wraps w with an internal [limitWriter] enforcing [MaxText] and
//     invokes the resolved extractor's Extract method.
//  6. Title Fallback: If the extractor succeeds but returns an empty [Info.Title], [Set.Extract] populates
//     it using filepath.Base(name) stripped of its extension.
//
// # Error Semantics
//
//   - If the extracted text exceeds [MaxText], returns [ErrTextTooLarge] (wrapping any underlying writer error).
//   - If destination w fails (e.g. disk full, pipe closed), the write failure is always surfaced even if the
//     underlying extractor suppressed or ignored the write error.
//   - Any format-specific parse error returned by the extractor is wrapped with name.
func (s *Set) Extract(ctx context.Context, name string, r io.ReaderAt, size int64, w io.Writer) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	ext := filepath.Ext(name)
	e, ok := s.byExt[normalize(ext)]
	if !ok {
		return Info{}, errs.Wrap(ErrUnsupported, "extract %s", name)
	}
	if size < 0 {
		return Info{}, errs.Wrap(errs.ErrInvalidArgument, "extract %s: negative size %d", name, size)
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

// normalize lowercases a file extension and guarantees a single leading dot prefix.
func normalize(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

// limitWriter wraps an [io.Writer], passing writes through until max bytes have been written.
// A max value of 0 imposes no limit.
//
// When a write would cause cumulative bytes to exceed max, limitWriter writes the portion that fits,
// records exceeded = true, and returns [ErrTextTooLarge]. The first destination error is captured in err,
// ensuring that both quota breaches and underlying destination I/O failures are reliably surfaced
// to the caller even if an extractor implementation ignores write return errors.
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
