package mermaid

import (
	"errors"
	"strconv"
	"strings"
)

// Block is the block diagram kind's value, declared here and registered in init (kind.go).
const Block Kind = 15

func init() {
	register(Block, "block", []string{"block-beta", "block"}, (*parser).block)
}

// BlockDiagram is a parsed block diagram: its blocks laid on a grid of columns, a group's own grid
// inside it, and the links between blocks.
type BlockDiagram struct {
	Root  BlockGroup
	Edges []Edge // From and To are blocks' ids; MinLen is unused
}

func (*BlockDiagram) Kind() Kind { return Block }

// BlockGroup is a grid of blocks: Columns of them a row (0: one row, as wide as its blocks), its
// blocks in order, each row filled left to right before the next.
type BlockGroup struct {
	Columns int
	Items   []BlockItem
}

// BlockItem is one cell of a grid, Width columns wide: a block (its id, label and shape), a space
// (Space: nothing drawn), or a group of its own (Group set, its id, when it has one, in ID).
type BlockItem struct {
	ID    string
	Label string
	Shape Shape
	Width int
	Space bool
	Group *BlockGroup
	Span  [2]int // its mention
}

// blockParser is the parse of one block diagram: the groups open, innermost last.
type blockParser struct {
	p     *parser
	d     *BlockDiagram
	open  []*BlockGroup
	ids   map[string]bool
	links []stmt // edges, checked against the blocks once all are read
}

// block parses a block diagram: its header takes nothing. Each line is `columns`, a group's
// `block` … `end`, a classDef, class or style (read, not drawn), a link between blocks, or a row of
// items. A block arrow is not read (ErrUnsupported). A link to a block that isn't there, an `end`
// with no group open, a group left open and a width that isn't a number above 0 are syntax errors.
func (p *parser) block(rest string, restOff int) (Diagram, error) {
	t := &blockParser{p: p, d: &BlockDiagram{}, ids: map[string]bool{}}
	t.open = []*BlockGroup{&t.d.Root}
	if s := strings.TrimSpace(rest); s != "" {
		bad, _ := firstWord(s)
		p.fail(restOff+strings.Index(rest, s), "unexpected %q after block", bad)
	}
	var last stmt
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		t.statement(st)
		last = st
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	if len(t.open) > 1 {
		p.fail(last.off, "a block group is not closed: end it with end")
	}
	for _, st := range t.links {
		t.link(st)
	}
	return p.done(t.d)
}

// group is the innermost group open.
func (t *blockParser) group() *BlockGroup { return t.open[len(t.open)-1] }

func (t *blockParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	head, _, _ := strings.Cut(w, ":")
	switch {
	case w == "columns":
		if rest == "auto" {
			t.group().Columns = 0
			return
		}
		n, err := strconv.Atoi(rest)
		if err != nil || n < 1 {
			t.p.fail(st.off, "columns wants a number above 0, or auto: %q", rest)
			return
		}
		t.group().Columns = n
	case head == "block" && w != "block-beta":
		t.openGroup(w, rest, st)
	case w == "end":
		if len(t.open) == 1 {
			t.p.fail(st.off, "end with no block group open")
			return
		}
		t.open = t.open[:len(t.open)-1]
	case w == "classDef" || w == "class" || w == "style":
		// styles: read, not drawn
	case w == "accTitle:" || w == "accDescr:":
		// accessibility text: nothing drawn
	case strings.Contains(st.text, "<[") && strings.Contains(st.text, "]>"):
		t.p.unsupport(st.off, "a block arrow")
	case blockLinked(st.text):
		t.links = append(t.links, st)
	default:
		t.row(st)
	}
}

// openGroup opens `block`, `block:id` or `block:id:width`, nested in the group open.
func (t *blockParser) openGroup(w, rest string, st stmt) {
	parts := strings.Split(w, ":")
	it := BlockItem{Width: 1, Group: &BlockGroup{}, Span: [2]int{st.off, st.off + len(w)}}
	if len(parts) > 1 {
		it.ID = parts[1]
		if it.ID != "" {
			if t.ids[it.ID] {
				t.p.fail(st.off, "block %q is defined twice", it.ID)
				return
			}
			t.ids[it.ID] = true
		}
	}
	if len(parts) > 2 {
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 1 {
			t.p.fail(st.off, "a block's width wants a number above 0: %q", parts[2])
			return
		}
		it.Width = n
	}
	if len(parts) > 3 || strings.TrimSpace(rest) != "" {
		t.p.fail(st.off, "unexpected %q after a block group's id and width", strings.TrimSpace(strings.Join(parts[min(len(parts), 3):], ":")+" "+rest))
		return
	}
	g := t.group()
	g.Items = append(g.Items, it)
	t.open = append(t.open, g.Items[len(g.Items)-1].Group)
}

// row reads a row's items: `id`, `id["label"]` (any of a flowchart's shapes), each perhaps `:width`,
// and `space` or `space:width`, separated by spaces.
func (t *blockParser) row(st stmt) {
	c := &cursor{p: t.p, s: st.text, off: st.off}
	for !c.bad {
		c.skipSpace()
		if c.i >= len(c.s) {
			return
		}
		start := c.i
		it := BlockItem{Width: 1}
		if w, _ := firstWord(c.s[c.i:]); w == "space" || strings.HasPrefix(w, "space:") {
			it.Space = true
			c.i += len("space")
		} else {
			id, label, shape, _, _, ok := c.shaped()
			if !ok {
				return
			}
			if t.ids[id] {
				t.p.fail(c.off+start, "block %q is defined twice", id)
				return
			}
			t.ids[id] = true
			it.ID, it.Label, it.Shape = id, label, shape
		}
		if strings.HasPrefix(c.s[c.i:], ":") && !strings.HasPrefix(c.s[c.i:], ":::") {
			c.i++
			k := 0
			for c.i+k < len(c.s) && c.s[c.i+k] >= '0' && c.s[c.i+k] <= '9' {
				k++
			}
			n, err := strconv.Atoi(c.s[c.i : c.i+k])
			if err != nil || n < 1 {
				c.fail("a block's width wants a number above 0")
				return
			}
			it.Width = n
			c.i += k
		}
		if c.i < len(c.s) && c.s[c.i] != ' ' && c.s[c.i] != '\t' {
			c.fail("unexpected %q after a block", string(c.s[c.i]))
			return
		}
		it.Span = [2]int{c.off + start, c.at()}
		g := t.group()
		g.Items = append(g.Items, it)
	}
}

// blockLinked reports whether a line holds a link: --, ==, -. or ~~~ outside quotes and brackets.
func blockLinked(s string) bool {
	depth, quoted := 0, false
	for i := 0; i+1 < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			quoted = !quoted
		case quoted:
		case strings.IndexByte("[({", c) >= 0:
			depth++
		case strings.IndexByte("])}", c) >= 0:
			depth--
		case depth == 0 && (s[i:i+2] == "--" || s[i:i+2] == "==" || s[i:i+2] == "-." || s[i:i+2] == "~~"):
			return true
		}
	}
	return false
}

// link reads a chain of links, `a --> b -- "text" --> c`, each between two blocks already defined.
func (t *blockParser) link(st stmt) {
	c := &cursor{p: t.p, s: st.text, off: st.off}
	from := t.endpoint(c)
	for from != "" && !c.bad {
		c.skipSpace()
		if c.i >= len(c.s) {
			return
		}
		start := c.at()
		e, ok := c.link()
		if !ok {
			return
		}
		c.skipSpace()
		to := t.endpoint(c)
		if to == "" {
			return
		}
		e.From, e.To, e.MinLen, e.Span = from, to, 0, [2]int{start, c.at()}
		t.d.Edges = append(t.d.Edges, e)
		from = to
	}
}

// endpoint reads a link's end: a block's id, which must be defined (a shape here would define a
// block a row never placed).
func (t *blockParser) endpoint(c *cursor) string {
	at := c.at()
	id, _, _, hasShape, _, ok := c.shaped()
	switch {
	case !ok:
		return ""
	case hasShape:
		t.p.fail(at, "a link's end names a block; %q is defined in a row, not on a link", id)
		return ""
	case !t.ids[id]:
		t.p.fail(at, "a link to %q, which is no block", id)
		return ""
	}
	return id
}
