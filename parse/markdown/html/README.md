# parse/markdown/html — a document as HTML

`html.Render(w, doc, opts...)` writes a [`markdown`](../README.md) document as HTML in the form the
CommonMark specification's examples use. It is a consumer of the tree: the parser decides what the
document is, and this package decides how it is written.

```go
err := html.Render(w, doc)                // raw HTML escaped: safe for an untrusted author
err = html.Render(w, doc, html.Unsafe())  // raw HTML passed through, as CommonMark specifies
```

**The default escapes raw HTML** (HTML blocks and inline HTML), so a note from someone else renders as
text rather than as markup. `Unsafe()` passes it through; it is the form the conformance cases compare.

## The form

The specification allows harmless variation in HTML output. This renderer fixes one form so cases
compare exactly:

- Blocks end with a newline; a tight list's paragraphs are written without `<p>`.
- Text escapes `&`, `<`, `>` and `"`; an invalid UTF-8 sequence becomes U+FFFD, so the output is
  valid UTF-8.
- A destination is percent-encoded where it is not URL-safe (an existing `%XX` is kept), then escaped;
  `'` is written `&#x27;`, as cmark writes it.
- An image's `alt` is the plain text of its content.
- A fenced code block's info string gives `class="language-<first word>"`.
- Definitions and frontmatter render as nothing.

## Extension kinds

| kind | written as |
| --- | --- |
| GFM table | `<table>`, `<thead>` for the first row, `<tbody>` only when there are body rows; `align="left\|center\|right"` on cells |
| GFM task item | `<li><input disabled="" type="checkbox"> …` (`checked=""` when checked) |
| GFM strikethrough | `<del>` |
| Obsidian wikilink | `<a class="wikilink" href="page#heading">` (`#^block` for a block) |
| Obsidian embed | `<span class="embed" data-href="…">` — an embed is content drawn from elsewhere, not a link |
| Obsidian tag | `<a class="tag" href="#name">#name</a>` |
| Obsidian callout | `<div class="callout" data-callout="type">` (lowercased; `data-callout-fold` when foldable), a `callout-title` div (the type, capitalized, when there is no title), then a `callout-content` div |

A wikilink's page is written as it was: which document it names is the consumer's to resolve, and so
is what an embed shows.
