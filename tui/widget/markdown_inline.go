package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/parse/markdown"
	mdhtml "github.com/yongjohnlee80/golib/parse/markdown/html"
)

// MARKDOWN INLINE — how one line's Markdown is drawn, byte by byte: which bytes are marks a
// Rendered view hides, and how the rest is styled. Both editors read a line by it (gui's Rendered
// view into its spans, the cell Editor's into its cells), so they hide and style the same bytes.

// MarkdownAttr is how one byte of a line is drawn: hidden (a mark: emphasis's stars, a code span's
// backticks, a heading's #s, a link's brackets and target, an escape's backslash, a bare <br>), and
// the styles of the constructs it is in.
type MarkdownAttr struct {
	Hidden                           bool
	Bold, Italic, Code, Strike, Link bool
	Wiki, Tag                        bool // an Obsidian [[wikilink]] or ![[embed]]; a #tag
	Heading                          int  // the heading level, 0 for none
	Br                               int  // a bare <br>'s bytes, hidden, a line break after them: its start plus one (0: none), so two in a row stay two
}

// MarkdownLineAttrs reads one line's Markdown: each byte's attributes, and the line's heading
// level (0 for none). A list item's marker is the caller's (MarkdownListItem, MarkdownBullet): read
// the text after it.
func MarkdownLineAttrs(line string) (attrs []MarkdownAttr, level int) {
	src := []byte(line)
	doc := markdown.Parse(src, markdown.WithGFM(), markdown.WithObsidian())
	attrs = make([]MarkdownAttr, len(src))
	var walk func(n *markdown.Node)
	walk = func(n *markdown.Node) {
		switch n.Kind {
		case markdown.KindHeading:
			level = n.Level()
			for i := n.Span.Start; i < n.Span.End; i++ {
				attrs[i].Heading = n.Level()
			}
			if n.FirstChild == nil {
				mdHide(attrs, n.Span.Start, n.Span.End)
			} else {
				mdHide(attrs, n.Span.Start, n.FirstChild.Span.Start)
				if tail := line[n.LastChild.Span.End:n.Span.End]; strings.Trim(tail, "# ") == "" {
					mdHide(attrs, n.LastChild.Span.End, n.Span.End)
				}
			}
		case markdown.KindEmph:
			mdMarkRange(attrs, n.Span.Start, n.Span.End, n, func(a *MarkdownAttr) { a.Italic = true })
		case markdown.KindStrong:
			mdMarkRange(attrs, n.Span.Start, n.Span.End, n, func(a *MarkdownAttr) { a.Bold = true })
		case markdown.KindStrikethrough:
			mdMarkRange(attrs, n.Span.Start, n.Span.End, n, func(a *MarkdownAttr) { a.Strike = true })
		case markdown.KindLink:
			mdMarkRange(attrs, n.Span.Start, n.Span.End, n, func(a *MarkdownAttr) { a.Link = true })
		case markdown.KindWikilink, markdown.KindEmbed:
			// its one child is what it shows (the alias, else the target): the brackets and a
			// target behind an alias are hidden
			mdMarkRange(attrs, n.Span.Start, n.Span.End, n, func(a *MarkdownAttr) { a.Wiki = true })
		case markdown.KindTag:
			for i := n.Span.Start; i < n.Span.End; i++ {
				attrs[i].Tag = true
			}
		case markdown.KindText:
			// a backslash escape draws the character it escapes, as CommonMark renders it: its
			// backslash is a mark, hidden off the cursor. Where the parser takes no escapes, the
			// backslashes are text: a code span's, and an autolink's, whose Literal is only its
			// decoded entities
			if n.Literal != nil && (n.Parent == nil || n.Parent.Kind != markdown.KindAutolink) {
				for i := n.Span.Start; i+1 < n.Span.End; i++ {
					if src[i] == '\\' && mdIsASCIIPunct(src[i+1]) {
						mdHide(attrs, i, i+1)
						i++
					}
				}
			}
		case markdown.KindRawHTML:
			// a bare <br> is the line break it is, as the HTML renderer writes it (html.BareBr);
			// any other raw HTML is drawn as written, as before
			if mdhtml.BareBr(n.Literal) {
				mdHide(attrs, n.Span.Start, n.Span.End)
				for i := max(n.Span.Start, 0); i < min(n.Span.End, len(attrs)); i++ {
					attrs[i].Br = n.Span.Start + 1
				}
			}
		case markdown.KindCodeSpan:
			s, e := n.Span.Start, n.Span.End
			open := 0
			for s+open < e && src[s+open] == '`' {
				open++
			}
			mdHide(attrs, s, s+open)
			mdHide(attrs, max(e-open, s+open), e)
			for i := s + open; i < e-open; i++ {
				attrs[i].Code = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.Next {
			walk(c)
		}
	}
	walk(doc.Root)
	return attrs, level
}

// markdownBullets are the bullet glyphs by nesting level, the deepest repeating.
var markdownBullets = [...]string{"•", "◦", "▪"}

// MarkdownBullet is the glyph a "-", "*" or "+" item is drawn with, by its indentation: two columns
// a level, a tab four.
func MarkdownBullet(indent string) string {
	level := 0
	for _, c := range indent {
		level++
		if c == '\t' {
			level += 3
		}
	}
	return markdownBullets[(level/2)%len(markdownBullets)]
}

// mdMarkRange styles a node's content bytes with set, and hides its own marks: the bytes of its
// span that none of its children cover.
func mdMarkRange(attrs []MarkdownAttr, start, end int, n *markdown.Node, set func(*MarkdownAttr)) {
	covered := make([]bool, end-start)
	for c := n.FirstChild; c != nil; c = c.Next {
		for i := max(c.Span.Start, start); i < min(c.Span.End, end); i++ {
			covered[i-start] = true
		}
	}
	for i := start; i < end; i++ {
		set(&attrs[i])
		if !covered[i-start] {
			attrs[i].Hidden = true
		}
	}
}

// mdIsASCIIPunct reports whether c is ASCII punctuation: what a backslash escapes.
func mdIsASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// mdHide marks a range of byte attributes as hidden in the Rendered view.
func mdHide(attrs []MarkdownAttr, start, end int) {
	for i := max(start, 0); i < min(end, len(attrs)); i++ {
		attrs[i].Hidden = true
	}
}
