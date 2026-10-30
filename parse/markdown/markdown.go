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

// Node is one element of the document tree. Kind, Span, the tree links and Literal are fields; what
// only some kinds carry (a heading's level, a link's destination, a list's data, …) is read through
// methods, which answer the zero value when it is not set, and kept off the Node itself, so text,
// emphasis and paragraphs, most of a document, stay small. The values hang off the Node by a pointer:
// a copy of a Node that carries values shares them, so a setter on the copy changes what the original
// reads too.
type Node struct {
	Kind                                      Kind
	Span                                      Span
	Parent, FirstChild, LastChild, Prev, Next *Node

	// Literal holds a text-bearing node's text when it differs from Source[Span]: after backslash
	// escapes and entity references are decoded, a code span is normalized, or a NUL is replaced.
	// Code blocks and HTML blocks always carry their content here, since it is assembled from lines.
	Literal []byte

	a   *nodeAttrs  // what only some kinds carry; nil on a node that carries none
	blk *blockState // parse-time state of an open block; nil once the parse is done
	// height is how many inline levels an emphasis, strikethrough, link or image node holds,
	// itself included: what MaxNesting bounds (0 for every other node)
	height uint16
}

// nodeAttrs are the values only some kinds of node carry. A node of another kind has none.
type nodeAttrs struct {
	level              int      // heading level, 1–6
	info               []byte   // a fenced code block's info string, decoded
	fence              byte     // '`' or '~' for a fenced code block; 0 for an indented one
	list               ListData // KindList and KindItem
	dest, title, label []byte   // a link's or image's destination and title; a definition's or tag's label
	html               int      // KindHTMLBlock: which of the seven start conditions opened it
	checked            *bool    // a GFM task list item: whether it is checked
	align              []Align  // KindTable: each column's alignment
	target             *Target  // KindWikilink and KindEmbed: where it points
	callout            *Callout // a KindBlockQuote that is an Obsidian callout
}

// attrs is n's values, made when the first one is set: from the slab for the kinds that always carry
// them, and otherwise here, on the heap, on the first set (a setext heading, a callout). A nil node
// has none to set: its values go to a scratch block nobody reads, so a setter on a nil node does
// nothing rather than panic.
func (n *Node) attrs() *nodeAttrs {
	if n == nil {
		return new(nodeAttrs)
	}
	if n.a == nil {
		n.a = new(nodeAttrs)
	}
	return n.a
}

// Level is a heading's level, 1–6; 0 when none is set, as on every other kind the parser makes.
func (n *Node) Level() int { return n.attrsOrZero().level }

// Info is a fenced code block's info string, decoded; nil when none is set.
func (n *Node) Info() []byte { return n.attrsOrZero().info }

// Fence is '`' or '~' for a fenced code block; 0 for an indented one, and when none is set.
func (n *Node) Fence() byte { return n.attrsOrZero().fence }

// List is a KindList's or KindItem's list data; the zero ListData when none is set.
func (n *Node) List() ListData { return n.attrsOrZero().list }

// Dest is a link's or image's destination, decoded; nil when none is set.
func (n *Node) Dest() []byte { return n.attrsOrZero().dest }

// Title is a link's or image's title, decoded; nil when none is set.
func (n *Node) Title() []byte { return n.attrsOrZero().title }

// Label is a KindLinkRefDef's label as written, or a KindTag's tag without its '#'; nil when none is
// set.
func (n *Node) Label() []byte { return n.attrsOrZero().label }

// HTML is which of the seven start conditions opened a KindHTMLBlock; 0 when none is set.
func (n *Node) HTML() int { return n.attrsOrZero().html }

// Checked is whether a GFM task list item is checked; nil when none is set, as on an item that is
// not a task.
func (n *Node) Checked() *bool { return n.attrsOrZero().checked }

// Align is a KindTable's column alignments; nil when none is set.
func (n *Node) Align() []Align { return n.attrsOrZero().align }

// Target is where a KindWikilink or KindEmbed points; nil when none is set.
func (n *Node) Target() *Target { return n.attrsOrZero().target }

// Callout is a KindBlockQuote's Obsidian callout; nil when none is set, as on a block quote that is
// not a callout.
func (n *Node) Callout() *Callout { return n.attrsOrZero().callout }

// SetLevel sets a heading's level.
func (n *Node) SetLevel(v int) { n.attrs().level = v }

// SetInfo sets a fenced code block's info string.
func (n *Node) SetInfo(v []byte) { n.attrs().info = v }

// SetFence sets a fenced code block's fence character.
func (n *Node) SetFence(v byte) { n.attrs().fence = v }

// SetList sets a list's or item's list data.
func (n *Node) SetList(v ListData) { n.attrs().list = v }

// SetDest sets a link's or image's destination.
func (n *Node) SetDest(v []byte) { n.attrs().dest = v }

// SetTitle sets a link's or image's title.
func (n *Node) SetTitle(v []byte) { n.attrs().title = v }

// SetLabel sets a definition's or tag's label.
func (n *Node) SetLabel(v []byte) { n.attrs().label = v }

// SetHTML sets which start condition opened an HTML block.
func (n *Node) SetHTML(v int) { n.attrs().html = v }

// SetChecked sets whether a task list item is checked.
func (n *Node) SetChecked(v *bool) { n.attrs().checked = v }

// SetAlign sets a table's column alignments.
func (n *Node) SetAlign(v []Align) { n.attrs().align = v }

// SetTarget sets where a wikilink or embed points.
func (n *Node) SetTarget(v *Target) { n.attrs().target = v }

// SetCallout sets a block quote's Obsidian callout.
func (n *Node) SetCallout(v *Callout) { n.attrs().callout = v }

// noAttrs is what a node without values reads as. It is never written: setters go through attrs.
var noAttrs nodeAttrs

// attrsOrZero is n's values, or the zero values for a node that carries none, a nil node included,
// so every accessor answers its zero value instead of panicking.
func (n *Node) attrsOrZero() *nodeAttrs {
	if n == nil || n.a == nil {
		return &noAttrs
	}
	return n.a
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

	nodes nodeSlab // where the parse's nodes are made; released when the parse is done
}

// nodeSlab hands out one parse's Nodes from chunks it allocates, so a document of n nodes costs a
// few dozen allocations instead of n, and its tree lies together in memory. A chunk starts at 8
// nodes (a one-line parse, as the highlighter makes, stays small) and doubles up to 1024. A node is
// never moved: a chunk is only appended to within its capacity, and a full one is left as it is
// for a new one. Each Parse has its own (on its Document), so nothing is shared between parses.
type nodeSlab struct {
	chunks [][]Node
	attrs  [][]nodeAttrs // the values of the nodes that carry them, chunked the same way
}

// carriesAttrs are the kinds every node of which carries values: their nodes take them from the
// slab when made. A node of another kind that comes to carry one (a paragraph a setext underline
// makes a heading, a block quote that is a callout) takes it when it is first set.
var carriesAttrs = [...]bool{
	KindHeading: true, KindCodeBlock: true, KindHTMLBlock: true, KindList: true, KindItem: true,
	KindLink: true, KindImage: true, KindAutolink: true, KindLinkRefDef: true, KindTag: true,
	KindTable: true, KindWikilink: true, KindEmbed: true,
}

// new places n in the slab and returns where it lies.
func (s *nodeSlab) new(n Node) *Node {
	last := len(s.chunks) - 1
	if last < 0 || len(s.chunks[last]) == cap(s.chunks[last]) {
		size := 8
		if last >= 0 {
			size = min(2*cap(s.chunks[last]), 1024)
		}
		s.chunks = append(s.chunks, make([]Node, 0, size))
		last++
	}
	if n.a == nil && int(n.Kind) < len(carriesAttrs) && carriesAttrs[n.Kind] {
		n.a = s.newAttrs(nodeAttrs{})
	}
	s.chunks[last] = append(s.chunks[last], n)
	c := s.chunks[last]
	return &c[len(c)-1]
}

// newWith places n, carrying the values a, in the slab.
func (s *nodeSlab) newWith(n Node, a nodeAttrs) *Node {
	n.a = s.newAttrs(a)
	return s.new(n)
}

// newAttrs places a in the slab's value chunks, which grow as the node chunks do.
func (s *nodeSlab) newAttrs(a nodeAttrs) *nodeAttrs {
	last := len(s.attrs) - 1
	if last < 0 || len(s.attrs[last]) == cap(s.attrs[last]) {
		size := 8
		if last >= 0 {
			size = min(2*cap(s.attrs[last]), 1024)
		}
		s.attrs = append(s.attrs, make([]nodeAttrs, 0, size))
		last++
	}
	s.attrs[last] = append(s.attrs[last], a)
	c := s.attrs[last]
	return &c[len(c)-1]
}

// release ends the parse. A node the tree holds (reached from root by FirstChild and Next, as every
// reader of the tree walks it) keeps its value and drops its block state, which only the parse
// needed. A node the parse made and then let go (a paragraph that became a table, a delimiter run
// consumed by emphasis) is zeroed whole. A chunk lives as long as any of its nodes, so without this
// a dead node's block state, a whole paragraph's content, would outlive the parse.
func (s *nodeSlab) release(root *Node) {
	markLive(root)
	for _, c := range s.chunks {
		for i := range c {
			if c[i].blk == &liveMark {
				c[i].blk = nil
			} else {
				if c[i].a != nil {
					*c[i].a = nodeAttrs{} // its values may share a chunk with a live node's
				}
				c[i] = Node{}
			}
		}
	}
	s.chunks, s.attrs = nil, nil
}

// liveMark is release's mark for a node the tree holds; no parse-time block state is ever it.
var liveMark blockState

func markLive(n *Node) {
	for c := n; c != nil; c = c.Next {
		c.blk = &liveMark
		if c.FirstChild != nil {
			markLive(c.FirstChild)
		}
	}
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
	d.nodes.release(d.Root) // block state was kept for the inline phase; a node let go goes too
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
