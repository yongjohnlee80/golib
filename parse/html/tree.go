package html

import (
	"context"
	"iter"
)

// Node is one node of a parsed document: an element (StartTag), text, a comment or a doctype.
// The document itself is an element with no name, whose children are the top level.
type Node struct {
	Kind     TokenKind
	Name     string // an element's, lower-cased
	Attrs    []Attr
	Data     string // Text, Comment and Doctype
	Span     [2]int // its bytes in the source: an element's from its start tag to its end
	Children []*Node

	// flat is the chain of elements, innermost first, that were opened past MaxDepth around
	// this node and so are not its ancestors in the tree. Nodes share the chain's tails.
	flat *flatLink
}

type flatLink struct {
	n    *Node
	next *flatLink
}

// Attr is the value of the attribute name, and whether the element has it.
func (n *Node) Attr(name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// Flattened yields, innermost first, the elements that enclose n in the source but were opened
// past MaxDepth, so the tree holds them as n's preceding siblings or cousins rather than its
// ancestors. A reader that decides by an ancestor (a hidden element, a script) asks these too;
// within MaxDepth it yields nothing.
func (n *Node) Flattened() iter.Seq[*Node] {
	return func(yield func(*Node) bool) {
		for l := n.flat; l != nil; l = l.next {
			if !yield(l.n) {
				return
			}
		}
	}
}

// Parse builds the tree of src under DefaultLimits.
func Parse(src []byte) (*Node, error) { return ParseLimited(context.Background(), src, Limits{}) }

// void elements never have content.
var void = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}

// scopeEdge are the elements an implied end tag never closes across.
var scopeEdge = map[string]bool{
	"applet": true, "button": true, "caption": true, "html": true, "marquee": true, "object": true,
	"table": true, "td": true, "template": true, "th": true,
}

// closesP are the start tags that end an open paragraph.
var closesP = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
	"dialog": true, "div": true, "dl": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "header": true, "hgroup": true, "hr": true, "main": true, "menu": true, "nav": true,
	"ol": true, "p": true, "pre": true, "section": true, "table": true, "ul": true, "li": true,
	"dd": true, "dt": true,
}

// implied is, for a start tag, the open elements it ends, the elements it stops at, and the
// scope edges it passes on the way (a new row closes the open row through its open cell).
type implied struct{ ends, stops, through map[string]bool }

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

var (
	paragraph = set("p")
	cells     = set("td", "th")
	sections  = set("thead", "tbody", "tfoot")
)

var impliedEnds = map[string]implied{
	"li":       {set("li"), set("ul", "ol", "menu"), nil},
	"dt":       {set("dt", "dd"), set("dl"), nil},
	"dd":       {set("dt", "dd"), set("dl"), nil},
	"tr":       {set("tr"), set("table", "thead", "tbody", "tfoot"), cells},
	"td":       {set("td", "th"), set("tr", "table"), nil},
	"th":       {set("td", "th"), set("tr", "table"), nil},
	"option":   {set("option"), set("select", "datalist", "optgroup"), nil},
	"optgroup": {set("optgroup", "option"), set("select", "datalist"), nil},
	"thead":    {sections, set("table"), cells},
	"tbody":    {sections, set("table"), cells},
	"tfoot":    {sections, set("table"), cells},
}

// builder holds the open elements: the tree's (stack) and, past MaxDepth, the flattened ones.
type builder struct {
	lim   Limits
	src   []byte
	nodes int
	stack []*Node        // open elements in the tree; stack[0] is the document
	flat  *flatLink      // open elements past MaxDepth, innermost first
	nflat int            // the chain's length
	open  map[string]int // how many of each name the chain holds
}

// ParseLimited builds the tree of src under l (a zero Limits is DefaultLimits), checking ctx
// every 4096 tokens. A limit passed is ErrTooLarge; a cancelled ctx is its error.
func ParseLimited(ctx context.Context, src []byte, l Limits) (*Node, error) {
	t := NewTokenizer(src, l)
	b := &builder{lim: t.lim, src: src, open: map[string]int{}}
	doc := &Node{Kind: StartTag, Span: [2]int{0, len(src)}}
	b.stack = []*Node{doc}
	for count := 0; ; count++ {
		if count%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		tok, ok := t.Next()
		if !ok {
			break
		}
		if err := b.add(tok); err != nil {
			return nil, err
		}
	}
	if err := t.Err(); err != nil {
		return nil, err
	}
	for l := b.flat; l != nil; l = l.next {
		l.n.Span[1] = len(src)
	}
	for len(b.stack) > 1 {
		b.pop(len(src))
	}
	return doc, nil
}

func (b *builder) top() *Node { return b.stack[len(b.stack)-1] }

// node counts a new node against MaxNodes and attaches it under the open element.
func (b *builder) node(n *Node) error {
	b.nodes++
	if b.nodes > b.lim.MaxNodes {
		return ErrTooLarge
	}
	n.flat = b.flat
	p := b.top()
	p.Children = append(p.Children, n)
	return nil
}

func (b *builder) add(tok Token) error {
	switch tok.Kind {
	case Text, Comment, Doctype:
		return b.node(&Node{Kind: tok.Kind, Data: tok.Data, Span: tok.Span})
	case EndTag:
		b.end(tok)
		return nil
	}
	b.implyEnds(tok.Name, tok.Span[0])
	el := &Node{Kind: StartTag, Name: tok.Name, Attrs: tok.Attrs, Span: tok.Span}
	if err := b.node(el); err != nil {
		return err
	}
	if tok.Kind == SelfClosing || void[tok.Name] {
		return nil
	}
	if len(b.stack)-1 >= b.lim.MaxDepth {
		b.flat = &flatLink{n: el, next: b.flat}
		b.nflat++
		b.open[el.Name]++
		return nil
	}
	b.stack = append(b.stack, el)
	return nil
}

// implyEnds closes what the start tag name ends: an open paragraph before a block, an open list
// item before another, and the table and option families'.
func (b *builder) implyEnds(name string, at int) {
	if b.nflat > 0 {
		return // flattened elements carry no structure to repair
	}
	if closesP[name] {
		b.closeNearest(implied{ends: paragraph}, at)
	}
	if im, ok := impliedEnds[name]; ok {
		b.closeNearest(im, at)
	}
}

// closeNearest pops up to and including the nearest open element in im.ends, if one is open
// before an element in im.stops or a scope edge im.through does not pass.
func (b *builder) closeNearest(im implied, at int) {
	for i := len(b.stack) - 1; i > 0; i-- {
		name := b.stack[i].Name
		if im.ends[name] {
			for len(b.stack) > i {
				b.pop(at)
			}
			return
		}
		if im.stops[name] || scopeEdge[name] && !im.through[name] {
			return
		}
	}
}

// end closes the nearest open element the end tag names, and everything open inside it; an end
// tag naming no open element is ignored. A flattened element is looked for only when one of its
// name is open, and the walk to it pops what it passes, so the walks cost at most the pushes.
func (b *builder) end(tok Token) {
	if b.open[tok.Name] > 0 {
		for b.flat != nil {
			n := b.flat.n
			b.flat = b.flat.next
			b.nflat--
			b.open[n.Name]--
			if n.Name == tok.Name {
				n.Span[1] = tok.Span[1]
				return
			}
			n.Span[1] = tok.Span[0]
		}
	}
	for i := len(b.stack) - 1; i > 0; i-- {
		if b.stack[i].Name == tok.Name {
			for b.flat != nil { // everything flattened is inside it
				b.flat.n.Span[1] = tok.Span[0]
				b.flat = b.flat.next
			}
			b.nflat = 0
			clear(b.open)
			for len(b.stack) > i+1 {
				b.pop(tok.Span[0])
			}
			b.pop(tok.Span[1])
			return
		}
	}
}

// pop closes the innermost open element at the byte at.
func (b *builder) pop(at int) {
	n := b.top()
	n.Span[1] = max(at, n.Span[1])
	b.stack = b.stack[:len(b.stack)-1]
}
