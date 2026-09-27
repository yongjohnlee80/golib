package markdown

import (
	"sort"

	"github.com/yongjohnlee80/golib/parse"
)

// Kind names what a node is. The CommonMark kinds are constants here; extensions add kinds from
// KindExtension up.
type Kind uint16

const (
	KindDocument Kind = iota
	KindBlockQuote
	KindList
	KindItem
	KindParagraph
	KindHeading
	KindThematicBreak
	KindCodeBlock
	KindHTMLBlock
	KindLinkRefDef
	KindText
	KindSoftBreak
	KindHardBreak
	KindCodeSpan
	KindEmph
	KindStrong
	KindLink
	KindImage
	KindAutolink
	KindRawHTML

	// KindExtension is the first kind an extension may define.
	KindExtension Kind = 1 << 8
)

var kindNames = map[Kind]string{
	KindDocument: "document", KindBlockQuote: "block_quote", KindList: "list", KindItem: "item",
	KindParagraph: "paragraph", KindHeading: "heading", KindThematicBreak: "thematic_break",
	KindCodeBlock: "code_block", KindHTMLBlock: "html_block", KindLinkRefDef: "link_ref_def",
	KindText: "text", KindSoftBreak: "softbreak", KindHardBreak: "linebreak", KindCodeSpan: "code",
	KindEmph: "emph", KindStrong: "strong", KindLink: "link", KindImage: "image",
	KindAutolink: "autolink", KindRawHTML: "html_inline",
	KindTable: "table", KindTableRow: "table_row", KindTableCell: "table_cell", KindStrikethrough: "strikethrough",
	KindFrontmatter: "frontmatter", KindWikilink: "wikilink", KindEmbed: "embed", KindTag: "tag",
	KindCalloutTitle: "callout_title",
}

func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return "kind(" + itoa(int(k)) + ")"
}

// Span is a half-open byte range [Start, End) of Document.Source.
type Span struct{ Start, End int }

// ListType tells a bullet list from an ordered one.
type ListType uint8

const (
	ListBullet ListType = iota + 1
	ListOrdered
)

// ListData describes a list and, on its items, the marker that opened each.
type ListData struct {
	Type      ListType
	Tight     bool // set when the list closes: no blank line separates its items or their children
	Bullet    byte // '-', '+' or '*'
	Delimiter byte // '.' or ')' for an ordered list
	Start     int  // the first number of an ordered list

	markerOffset int // columns of indent before the marker
	padding      int // columns from the marker's start to the item's content
}

// Node is one element of the document tree. The fields after Span are used only by the kinds that
// need them and are zero otherwise.
type Node struct {
	Kind                                      Kind
	Span                                      Span
	Parent, FirstChild, LastChild, Prev, Next *Node

	// Literal holds a text-bearing node's text when it differs from Source[Span]: after backslash
	// escapes and entity references are decoded, a code span is normalized, or a NUL is replaced.
	// Code blocks and HTML blocks always carry their content here, since it is assembled from lines.
	Literal []byte

	Level int      // heading level, 1–6
	Info  []byte   // a fenced code block's info string, decoded
	Fence byte     // '`' or '~' for a fenced code block; 0 for an indented one
	List  ListData // KindList and KindItem
	Dest  []byte   // link or image destination, decoded
	Title []byte   // link or image title, decoded
	Label []byte   // KindLinkRefDef: the label as written; KindTag: the tag without its '#'
	HTML  int      // KindHTMLBlock: which of the seven start conditions opened it

	Checked *bool    // a GFM task list item: whether it is checked; nil on any other item
	Align   []Align  // KindTable: each column's alignment
	Target  *Target  // KindWikilink and KindEmbed: where it points
	Callout *Callout // a KindBlockQuote that is an Obsidian callout; nil on any other

	blk *blockState // parse-time state of an open block; nil once the parse is done
}

// Text returns a text-bearing node's text: Literal when set, else the source it spans.
func (n *Node) Text(src []byte) []byte {
	if n.Literal != nil {
		return n.Literal
	}
	return src[n.Span.Start:n.Span.End]
}

// AppendChild adds c as n's last child.
func (n *Node) AppendChild(c *Node) {
	c.Parent = n
	c.Prev = n.LastChild
	c.Next = nil
	if n.LastChild != nil {
		n.LastChild.Next = c
	} else {
		n.FirstChild = c
	}
	n.LastChild = c
}

// InsertBefore puts c immediately before n, under n's parent.
func (n *Node) InsertBefore(c *Node) {
	c.Parent = n.Parent
	c.Next = n
	c.Prev = n.Prev
	if n.Prev != nil {
		n.Prev.Next = c
	} else if n.Parent != nil {
		n.Parent.FirstChild = c
	}
	n.Prev = c
}

// Unlink removes n from its parent, keeping its own children.
func (n *Node) Unlink() {
	if n.Prev != nil {
		n.Prev.Next = n.Next
	} else if n.Parent != nil {
		n.Parent.FirstChild = n.Next
	}
	if n.Next != nil {
		n.Next.Prev = n.Prev
	} else if n.Parent != nil {
		n.Parent.LastChild = n.Prev
	}
	n.Parent, n.Prev, n.Next = nil, nil, nil
}

// LinkRef is one link reference definition, as links resolve against it.
type LinkRef struct {
	Dest, Title []byte
}

// Document is a parsed Markdown source.
type Document struct {
	Root   *Node
	Source []byte
	// Refs maps normalized labels to their definitions. The first definition of a label wins.
	Refs map[string]LinkRef

	lines []int // offsets at which lines begin, after LF, CR or CRLF
}

// Position resolves a byte offset to a line and a rune-counted column. Lines end at LF, CR or CRLF,
// as CommonMark defines them; an invalid UTF-8 byte counts as one column.
func (d *Document) Position(off int) parse.Position {
	if off < 0 {
		off = 0
	}
	if off > len(d.Source) {
		off = len(d.Source)
	}
	i := sort.Search(len(d.lines), func(i int) bool { return d.lines[i] > off })
	start := 0
	if i > 0 {
		start = d.lines[i-1]
	}
	return parse.Position{Offset: off, Line: i + 1, Column: 1 + runeCount(d.Source[start:off])}
}

// Parse reads src as CommonMark, plus the syntax of any extensions given. It never fails: any
// sequence of characters is a Markdown document. src is not copied.
//
// https://spec.commonmark.org/0.31.2/
func Parse(src []byte, opts ...Option) *Document {
	cfg := config{}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	cfg.init()
	d := &Document{Source: src, Refs: map[string]LinkRef{}, lines: lineStarts(src)}
	p := newBlockParser(d, &cfg)
	p.run()
	parseInlines(d, &cfg)
	clearState(d.Root) // block state was kept for the inline phase
	return d
}

func lineStarts(src []byte) []int {
	var out []int
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			out = append(out, i+1)
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			out = append(out, i+1)
		}
	}
	return out
}

func runeCount(b []byte) int {
	n := 0
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			i++
		default:
			size := utf8Len(b[i:])
			i += size
		}
		n++
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
