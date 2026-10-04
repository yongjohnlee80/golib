# extract — document containers to Markdown, streaming

`extract` turns document containers (PDF, DOCX, plain text) into Markdown, so a search index or an
embedding model reads text rather than files. It defines the interface every format implements and
a `Set` that chooses an implementation by file extension. golib ships only what the standard
library can do. A format whose parser needs a third-party module, such as PDF, is implemented in the
product that accepts that dependency and registered there.

## Install

```sh
go get github.com/yongjohnlee80/golib
```

```go
import "github.com/yongjohnlee80/golib/extract"
```

## The interface

```go
type Extractor interface {
    Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (Info, error)
}

type Info struct {
    Title   string // the document's own title; the Set falls back to the file name
    Pages   int    // for a format that has pages
    Scanned bool   // pages but no text: OCR territory
}
```

- **The container is read through `io.ReaderAt`**, never loaded whole. A PDF's cross-reference
  table and a DOCX's ZIP directory both sit at the end of the file, so an extractor reads at
  offsets, not only forward.
- **The Markdown is written to `w` as it is produced**, so the text is never held whole either.
- **An extractor stops when `ctx` is cancelled.** Whatever it wrote before an error stays written,
  so a caller that must not keep half a document writes to a destination it can discard, such as an
  atomic file write.
- **A `Set` calls one extractor from many goroutines**, so an implementation must be safe for
  concurrent use. Keep per-document state in the call, not in the value.

`extract.Func` adapts a plain function to the interface.

## A Set

```go
set := extract.New(
    extract.Register(extract.Text{}, ".md", ".markdown", ".txt"),
    extract.Register(pdfExtractor, ".pdf"), // an Extractor of the caller's own
    extract.MaxContainer(250<<20),
    extract.MaxText(16<<20),
)

f, err := os.Open(path)
st, err := f.Stat()
info, err := set.Extract(ctx, path, f, st.Size(), w)
```

| | |
| --- | --- |
| `Register(e, exts...)` | `e` handles each extension. Case and the leading dot don't matter (`.PDF` = `pdf`). A later registration replaces an earlier one |
| `MaxContainer(n)` | refuse a file over `n` bytes **before reading it**. `0` means no limit |
| `MaxText(n)` | stop when the Markdown passes `n` bytes. `0` means no limit |
| `Supports(name)` | whether an extractor is registered for `name`'s extension |

**Why two limits.** A container's size says little about its text. A 100 MiB PDF manual is mostly
images and fonts and may hold 1 MiB of text, while 16 MiB of Markdown is millions of words.
`MaxContainer` bounds what may be opened at all; `MaxText` bounds the text that comes out, which is
what an index and an embedding model pay for. The text limit holds even for an extractor that
ignores the failed write.

`New` registers nothing of its own, and a misconfiguration (a nil extractor or nil `Func`, a missing
or empty extension, a negative limit) panics at `New`, where the wiring is written. A nil pointer of
another type is accepted, since its methods may be written to work on a nil receiver.

## Text

`extract.Text{}` handles containers that are already text: Markdown, plain text, logs. Plain text is
valid Markdown, so it copies the file to `w` as it is, 64 KiB at a time, holding one chunk however
large the file. It neither checks nor converts the encoding. A source that ends before `size` bytes
is an error wrapping `io.ErrUnexpectedEOF`, so a file that shrank after `Stat` is not indexed as if
complete. A destination that accepts fewer bytes than it was given is `io.ErrShortWrite`.

## Errors

| error | when |
| --- | --- |
| `ErrUnsupported` | no extractor for the extension. Before anything is read; also `errs.ErrUnsupported` |
| `ErrContainerTooLarge` | the file is over `MaxContainer`. Before anything is read; also `errs.ErrInvalidArgument` |
| `ErrTextTooLarge` | the Markdown passed `MaxText`. The text up to the limit stays written; also `errs.ErrInvalidArgument` |
| the destination's own error | `w` failed, such as a full disk. Always returned, even if the extractor ignored it, and alongside `ErrTextTooLarge` when the limit was crossed in the same write |
| anything else | the extractor's error, wrapped with the file name |

## License

See [LICENSE](../LICENSE).
