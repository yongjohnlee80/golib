# parse/html — a bounded HTML tokenizer and tree, standard library only

`html` reads HTML for a program that wants a document's content: tokens with their byte spans, and a
small tree with HTML's common implied end tags. It is not a browser's tree builder. Misnested
formatting is closed at the first end tag that names an open element, not repaired as the WHATWG
algorithm repairs it, and a page that relies on that repair still yields its text, nested a little
differently. [`extract/html`](../../extract/html/README.md) reads pages through it.

```go
import phtml "github.com/yongjohnlee80/golib/parse/html"

doc, err := phtml.Parse(src)                              // DefaultLimits
doc, err = phtml.ParseLimited(ctx, src, phtml.Limits{})   // ctx checked every 4096 tokens

t := phtml.NewTokenizer(src, phtml.Limits{})              // the tokens alone, no tree
for tok, ok := t.Next(); ok; tok, ok = t.Next() { … }
err = t.Err()
```

## Tokens

| kind | is |
| --- | --- |
| `Text` | character data, entities decoded by the standard library's `html.UnescapeString`; a `script`'s or `style`'s content raw |
| `StartTag`, `SelfClosing` | a tag, its name lower-cased, its attributes (first of each name kept, values decoded) |
| `EndTag` | a closing tag |
| `Comment` | `<!-- … -->`, or a bogus comment (`<? … >`, `</ …>` that names no tag) |
| `Doctype` | `<!DOCTYPE …>` or any other `<! … >` |

`script`, `style`, `textarea` and `title` are raw text: their content runs to their own end tag
and is never read as markup. A `<` that starts no markup is text. Every token carries `Span`, its
bytes in the source.

## The tree

`Parse` returns the document: an element with no name whose children are the top level. A `Node`
is an element, text, a comment or a doctype, with its `Span` (an element's runs from its start tag
to its end) and its `Children`.

- **Void elements** (`br`, `img`, `hr`, `input`, `meta`, …) and `<x/>` never hold content.
- **Implied end tags:** a block start tag ends an open `p`; `li` ends the open `li`; `dt` and `dd`
  end each other; a row ends the open row through its open cell; a cell ends the open cell;
  `thead`, `tbody` and `tfoot` end each other; `option` and `optgroup` end the open option. None of
  these crosses a table, a cell, a button or the other scope edges.
- **An end tag** closes the nearest open element it names and everything open inside it; one that
  names no open element is ignored.

## Limits

Every limit refuses with `ErrTooLarge`. Nothing is dropped or cut to fit, because a dropped
attribute could be the `hidden` that keeps an element's text out of what a reader extracts.

| `Limits` field | default | past it |
| --- | --- | --- |
| `MaxSource` | 32 MiB | `ErrTooLarge` before any token |
| `MaxNodes` | 1 << 20 | `ErrTooLarge` |
| `MaxAttrs` | 64 per tag | `ErrTooLarge` |
| `MaxAttr` | 64 KiB per value, as written | `ErrTooLarge` |
| `MaxDepth` | 256 | flattened (below) |

**Flattening.** An element opened past `MaxDepth` is attached to the deepest element the tree
holds, with no children, and what it contains follows it as siblings. Its meaning is kept: each
node's `Flattened()` yields the flattened elements that enclose it in the source, innermost first.
A reader that decides by an ancestor (a `hidden` element, a `noscript`) asks those too. Within
`MaxDepth`, `Flattened()` yields nothing.

The tree is built with an explicit stack, so no walk over the input recurses on its depth.

## License

See [LICENSE](../../LICENSE).
