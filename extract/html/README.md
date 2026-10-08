# extract/html — a web page's content, as Markdown

`html` is an [`extract.Extractor`](../README.md) for HTML. It writes a page's content as Markdown,
without tags, scripts, styles or the page's chrome, so a search index embeds what the page says.
It parses with [`parse/html`](../../parse/html/README.md) and the standard library alone.

```go
import xhtml "github.com/yongjohnlee80/golib/extract/html"

set := extract.New(extract.Register(xhtml.Extractor{}, ".html", ".htm"), extract.MaxText(16<<20))
info, err := set.Extract(ctx, "page.html", r, size, w) // info.Title: <title>, else the first h1
```

## What is read

- **A page with `main`** is read as that element alone; else its first `article`; else its body
  without `nav`, `header`, `footer` and `aside`.
- **Dropped with what they hold:** `script`, `style`, `noscript`, `template`, `svg`, `canvas`,
  `iframe`, `object`, `form`, `head`, comments, and anything `hidden` or `aria-hidden="true"`. An
  element flattened past the parse's depth limit still drops what it held.

| HTML | Markdown |
| --- | --- |
| `h1`–`h6` | `#`–`######` |
| `p`, `div`, `section`, … | paragraphs; HTML's whitespace collapsed |
| `br` | a line break |
| `ul`, `ol` (with `start`), `li` | `-` and `1.` lists, nested lists indented under their item |
| `table` | a GFM table, its first row the header, short rows padded, `\|` escaped |
| `pre` (`language-x` on it or its `code`) | a fenced block, its fence longer than any backticks inside |
| `code`, `kbd`, `samp` | a code span |
| `strong`/`b`, `em`/`i` | `**…**`, `*…*` |
| `a` | its text; the target is dropped |
| `img` | its `alt` |
| `blockquote` | `>` |

## Identity and limits

- `ID()` is `golib/html`, `Version()` is `1`: a derived cache keys the text by both.
- A page over any of `parse/html`'s `Limits` (source size, node count, a tag's attributes, one
  value's length) is `ErrTooLarge`, refused before it is read when its size is over the source
  limit. Nothing is written. Through an `extract.Set`, `MaxText` cuts the output, as it does any
  extractor's.

## License

See [LICENSE](../../LICENSE).
