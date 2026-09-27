# parse/markdown — CommonMark, with GFM and Obsidian as extensions

`markdown` parses [CommonMark 0.31.2](https://spec.commonmark.org/0.31.2/) into a tree of nodes that
point back into the source by byte span. [GitHub Flavored Markdown](https://github.github.com/gfm/) and
the [Obsidian](https://help.obsidian.md/obsidian-flavored-markdown) syntax are options, not forks.
[`html`](html/README.md) renders a tree as HTML.

```go
import (
    "github.com/yongjohnlee80/golib/parse/markdown"
    "github.com/yongjohnlee80/golib/parse/markdown/html"
)

doc := markdown.Parse(src, markdown.GFM(), markdown.Obsidian())
err := html.Render(w, doc) // raw HTML escaped; html.Unsafe() passes it through
```

`Parse` takes any bytes and never fails — any sequence of characters is a CommonMark document — and
it does not copy `src`. Invalid UTF-8 is kept as it is; the renderer writes it as U+FFFD.

## The tree

A `Document` holds `Root` (a `KindDocument` node), `Source`, and `Refs`: the link reference
definitions, keyed by normalized label (Unicode case folding, collapsed whitespace), first definition
winning. `doc.Position(off)` turns a byte offset into a line and a rune-counted column. Lines end at
LF, CR or CRLF, as CommonMark counts them; `parse.Scanner` does not end a line at a bare CR, so the two
differ on that input only.

Every `Node` has a `Kind`, a `Span` (`[Start, End)` into `Source`), and parent, child and sibling
links. A child's span lies inside its parent's; siblings' spans are ordered and disjoint. The other
fields are used by the kinds that need them:

| kind | what it is | fields |
| --- | --- | --- |
| `KindDocument` | the root | |
| `KindBlockQuote` | `>` lines | `Callout` (Obsidian) |
| `KindList`, `KindItem` | a list and its items | `List` (type, bullet or delimiter, start, `Tight`); `Checked` on a GFM task item |
| `KindParagraph` | | |
| `KindHeading` | ATX or setext | `Level` |
| `KindThematicBreak` | | |
| `KindCodeBlock` | fenced or indented | `Fence` (0 for indented), `Info` (decoded), `Literal` (the content) |
| `KindHTMLBlock` | | `HTML` (which of the seven start conditions), `Literal` |
| `KindLinkRefDef` | a definition, where it was written | `Label` (as written), `Dest`, `Title` |
| `KindText` | | `Literal` only when it differs from the source |
| `KindSoftBreak`, `KindHardBreak` | line endings | |
| `KindCodeSpan` | | `Literal` (normalized) |
| `KindEmph`, `KindStrong` | | |
| `KindLink`, `KindImage` | inline or reference | `Dest`, `Title` (decoded) |
| `KindAutolink` | `<…>`, and GFM's extended autolinks | `Dest` (with `mailto:` or `http://` added where the syntax implies it) |
| `KindRawHTML` | inline HTML | `Literal` |

`(*Node).Text(src)` returns a text-bearing node's text: `Literal` if set, else the source it spans.
`AppendChild`, `InsertBefore` and `Unlink` edit the tree.

**Definitions are provenance, not content.** The specification removes a definition from its paragraph
into a map, and links resolve against `Refs`. The `KindLinkRefDef` node stays in the tree so an editor
or an indexer can find it; renderers skip it, and a paragraph that held only definitions is removed.

## Extensions

`GFM()` and `Obsidian()` enable the extensions; `GFMExtension()` and `ObsidianExtension()` describe
them. An extension re-reads some valid CommonMark (a pipe table was a paragraph, `#tag` was text), so
the promise is narrower than "CommonMark is unchanged": **on a source its `Recognizes` rejects, the
tree is the same with the extension as without it.** The tests take that literally: every CommonMark
case the extension does not recognize runs with it on, and a converse test fails if a case reads
differently without being recognized.

### GFM (0.29-gfm)

| kind | what it is | fields |
| --- | --- | --- |
| `KindTable` | a header row, a delimiter row, any body rows | `Align` (per column) |
| `KindTableRow` | the first row is the header | |
| `KindTableCell` | inline content; `\|` is a pipe anywhere in it | |
| `KindStrikethrough` | `~~text~~` | |

Task list items are `KindItem` with `Checked` set; extended autolinks (`www.`, `http://`, `https://`,
`ftp://`, email) are `KindAutolink`.

### Obsidian

| kind | what it is | fields |
| --- | --- | --- |
| `KindFrontmatter` | `---` at offset 0 through the next `---` line | `Literal` (the raw YAML between the fences) |
| `KindWikilink` | `[[page#heading#^block\|alias]]` | `Target` (`Page`, `Heading`, `Block`, `Alias`, as written) |
| `KindEmbed` | `![[…]]` | `Target` (for an image, `Alias` may be a size) |
| `KindTag` | `#name`, nested with `/` | `Label` (the name without `#`) |
| `KindCalloutTitle` | a callout's title line, the first child of its block quote | |

A callout is a `KindBlockQuote` with `Callout` set (`Type`, and `Fold`: `'+'`, `'-'` or 0).
Frontmatter is not parsed here: YAML is a separate parser's job.

## Readings taken

Where a specification is silent, or the reference implementations disagree with its text, the
reading is pinned by a rule and its cases. The ones a user may notice:

- **CommonMark.** Escapes and entity references decode in one pass, so an escaped `&` starts no
  reference (`\&amp;` is `&amp;` as text), in info strings and destinations too — as commonmark.js
  reads it; cmark 0.31.1 decodes references first. Unescaped parentheses in a link destination nest
  at most 32 deep, cmark's limit (the specification permits one).
- **GFM.** The specification's text is followed, which in places is stricter than GitHub's renderer:
  strikethrough takes exactly two tildes (cmark-gfm also accepts one); the domain after `www.` needs a
  period of its own (`www.com` is text); a URL's domain needs a period (`http://localhost` is text); an
  email autolink needs the same boundary as the others (a line start, whitespace, `*`, `_`, `~` or
  `(`). A table's delimiter row is tried after every CommonMark block start, as in cmark-gfm, so
  `- | -` under a paragraph is a list item.
- **Obsidian.** The help pages describe rather than specify. A wikilink is a link, so the innermost
  link wins, as for inline links: no `[` before a wikilink closes a link around it (an embed, like an
  image, may sit in link text). A tag starts a line or follows whitespace, and holds at least one
  non-digit. A callout's marker is on the block quote's first line; the rest of that line is its title,
  never part of the body.

Not read: GFM's disallowed-raw-HTML filter (the renderer escapes all raw HTML unless `Unsafe()`), and
Obsidian's block ids (`^id` at a line's end), footnotes, `%%comments%%`, `==highlights==` and math.

## How it is tested

- **A versioned rule checklist per syntax** — CommonMark 0.31.2 (89 rules), GFM 0.29-gfm (32),
  Obsidian's help pages as read on 2026-09-28 (25). Every rule has a positive case (the construct forms)
  and a negative one (a near miss that must not), or a stated reason it has no near miss; the coverage
  tests fail otherwise. Cases are written from the rules, not copied from the specifications' examples.
- **Collision cases** assert both readings of valid CommonMark an extension re-reads.
- **Robustness.** `FuzzParse` holds the span invariants and renders, with each combination of
  extensions. Adversarial families (`[` nested 500,000 deep, delimiter runs of every length mod 3, deep
  quotes and lists, unclosed link destinations and wikilinks, trailing parentheses after an autolink,
  and more) are timed at n and 10n and must scale by about 10×, not 100×. Only the families measured are
  claimed to be linear.
