package mermaid

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// identLen is the length of the node id at the start of s: letters, digits and _, and a - that
// sits between two of them (node-1) rather than starting a link (A-->B).
func identLen(s string) int {
	i := 0
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			i += n
		case r == '-' && i > 0 && i+1 < len(s):
			nr, _ := utf8.DecodeRuneInString(s[i+1:])
			if nr == '_' || unicode.IsLetter(nr) || unicode.IsDigit(nr) {
				i++
				continue
			}
			return i
		default:
			return i
		}
	}
	return i
}

type shapeSyntax struct {
	open   string
	closes []shapeClose
}

type shapeClose struct {
	close string
	shape Shape
}

// shapes in the order they must be tried: a longer opener before its prefix.
var shapes = []shapeSyntax{
	{"(((", []shapeClose{{")))", DoubleCircle}}},
	{"((", []shapeClose{{"))", Circle}}},
	{"([", []shapeClose{{"])", Stadium}}},
	{"(", []shapeClose{{")", Round}}},
	{"[[", []shapeClose{{"]]", Subroutine}}},
	{"[(", []shapeClose{{")]", Cylinder}}},
	{"[/", []shapeClose{{"/]", Parallelogram}, {`\]`, Trapezoid}}},
	{`[\`, []shapeClose{{`\]`, ParallelogramAlt}, {"/]", TrapezoidAlt}}},
	{"[", []shapeClose{{"]", Rect}}},
	{"{{", []shapeClose{{"}}", Hexagon}}},
	{"{", []shapeClose{{"}", Diamond}}},
	{">", []shapeClose{{"]", Asymmetric}}},
}

// cursor walks one statement.
type cursor struct {
	p   *parser
	s   string
	off int // the statement's offset in the source
	i   int
	bad bool
}

func (c *cursor) at() int { return c.off + c.i }

func (c *cursor) skipSpace() {
	for c.i < len(c.s) && (c.s[c.i] == ' ' || c.s[c.i] == '\t') {
		c.i++
	}
}

func (c *cursor) fail(format string, args ...any) {
	c.p.fail(c.at(), format, args...)
	c.bad = true
}

// chain reads "A & B --> C -.text.-> D": node groups joined by links, one edge per pair.
func (p *parser) chain(st stmt) {
	c := &cursor{p: p, s: st.text, off: st.off}
	left := c.nodeGroup()
	for !c.bad {
		c.skipSpace()
		if c.i >= len(c.s) {
			return
		}
		start := c.at()
		spec, ok := c.link()
		if !ok {
			return
		}
		c.skipSpace()
		right := c.nodeGroup()
		if c.bad {
			return
		}
		for _, a := range left {
			for _, b := range right {
				e := spec
				e.From, e.To = a, b
				e.Span = [2]int{start, c.at()}
				p.fc.Edges = append(p.fc.Edges, e)
			}
		}
		left = right
	}
}

func (c *cursor) nodeGroup() []string {
	var ids []string
	for {
		id, ok := c.node()
		if !ok {
			return nil
		}
		ids = append(ids, id)
		c.skipSpace()
		if c.i < len(c.s) && c.s[c.i] == '&' {
			c.i++
			c.skipSpace()
			continue
		}
		return ids
	}
}

// node reads one node mention: an id, perhaps a shape with its text, perhaps :::class.
func (c *cursor) node() (string, bool) {
	start := c.i
	n := identLen(c.s[c.i:])
	if n == 0 {
		c.fail("expected a node id")
		return "", false
	}
	id := c.s[c.i : c.i+n]
	c.i += n
	if c.i < len(c.s) && c.s[c.i] == '@' {
		c.p.unsupport(c.at(), "the @ syntax")
		c.bad = true
		return "", false
	}
	shape, label, hasShape := Rect, id, false
	save := c.i
	c.skipSpace()
	for _, sh := range shapes {
		if !strings.HasPrefix(c.s[c.i:], sh.open) {
			continue
		}
		textStart := c.i + len(sh.open)
		raw, close, end, ok := c.shapeText(textStart, sh)
		if !ok {
			return "", false
		}
		lbl, err := c.p.label(raw, c.off+textStart)
		if err != nil {
			c.bad = true
			return "", false
		}
		shape, label, hasShape = close.shape, lbl, true
		c.i = end
		break
	}
	if !hasShape {
		c.i = save
	}
	var classes []string
	for strings.HasPrefix(c.s[c.i:], ":::") {
		c.i += 3
		k := identLen(c.s[c.i:])
		if k == 0 {
			c.fail("::: wants a class name")
			return "", false
		}
		classes = append(classes, c.s[c.i:c.i+k])
		c.i += k
	}
	c.p.mention(id, label, shape, hasShape, classes, [2]int{c.off + start, c.at()})
	return id, true
}

// shapeText finds where sh's text ends: a quoted string then the closer, or the first closer.
func (c *cursor) shapeText(from int, sh shapeSyntax) (raw string, close shapeClose, end int, ok bool) {
	s := c.s
	if strings.HasPrefix(s[from:], `"`) {
		q := strings.IndexByte(s[from+1:], '"')
		if q < 0 {
			c.i = from
			c.fail("a quoted label is not closed")
			return "", shapeClose{}, 0, false
		}
		after := from + 1 + q + 1
		for _, cl := range sh.closes {
			if strings.HasPrefix(s[after:], cl.close) {
				return s[from:after], cl, after + len(cl.close), true
			}
		}
		c.i = after
		c.fail("expected %q after the label", sh.closes[0].close)
		return "", shapeClose{}, 0, false
	}
	best, bestAt := shapeClose{}, -1
	for _, cl := range sh.closes {
		if k := strings.Index(s[from:], cl.close); k >= 0 && (bestAt < 0 || k < bestAt) {
			best, bestAt = cl, k
		}
	}
	if bestAt < 0 {
		c.i = from
		c.fail("the shape opened by %q is not closed", sh.open)
		return "", shapeClose{}, 0, false
	}
	return s[from : from+bestAt], best, from + bestAt + len(best.close), true
}

// mention records a node at its first mention, and updates its shape and text when a later one
// gives them. Its subgraph is the last one it is mentioned in; a mention at top level leaves it
// where it is.
func (p *parser) mention(id, label string, shape Shape, hasShape bool, classes []string, span [2]int) {
	sub := ""
	if len(p.stack) > 0 {
		sub = p.stack[len(p.stack)-1]
	}
	i, ok := p.nodes[id]
	if !ok {
		i = len(p.fc.Nodes)
		p.nodes[id] = i
		p.fc.Nodes = append(p.fc.Nodes, Node{ID: id, Label: id, Shape: Rect, Span: span})
	}
	n := &p.fc.Nodes[i]
	if sub != "" {
		n.Subgraph = sub
	}
	if hasShape {
		n.Label, n.Shape, n.Span = label, shape, span
	}
	n.Classes = append(n.Classes, classes...)
}

// link reads one link: its ends, its line, its length, and its text in either form
// ("-- text -->" or "-->|text|").
func (c *cursor) link() (Edge, bool) {
	s := c.s
	e := Edge{MinLen: 1}
	start := c.i
	if n := identLen(s[c.i:]); n > 0 && c.i+n < len(s) && s[c.i+n] == '@' {
		c.p.unsupport(c.at(), "an edge id")
		c.bad = true
		return Edge{}, false
	}
	if c.i < len(s) {
		switch {
		case s[c.i] == '<':
			e.Tail = Head
			c.i++
		case (s[c.i] == 'o' || s[c.i] == 'x') && c.i+1 < len(s) && strings.IndexByte("-=.", s[c.i+1]) >= 0:
			e.Tail = arrowOf(s[c.i])
			c.i++
		}
	}
	if c.i >= len(s) {
		c.fail("expected a link")
		return Edge{}, false
	}
	run := func(ch byte) int {
		k := 0
		for c.i+k < len(s) && s[c.i+k] == ch {
			k++
		}
		return k
	}
	head := func() Arrow {
		if c.i < len(s) {
			if a := arrowOf(s[c.i]); a != None {
				if s[c.i] == '>' || c.i+1 >= len(s) || s[c.i+1] == ' ' || s[c.i+1] == '\t' || s[c.i+1] == '|' || identLen(s[c.i+1:]) > 0 {
					c.i++
					return a
				}
			}
		}
		return None
	}
	textForm := false
	switch ch := s[c.i]; ch {
	case '~':
		k := run('~')
		if k < 3 {
			c.fail("an invisible link is ~~~")
			return Edge{}, false
		}
		c.i += k
		e.Line, e.MinLen = Invisible, k-2
		if e.Tail != None {
			c.fail("an invisible link has no ends")
			return Edge{}, false
		}
	case '-', '=':
		k := run(ch)
		c.i += k
		e.Line = Solid
		if ch == '=' {
			e.Line = Thick
		}
		if ch == '-' && c.i < len(s) && s[c.i] == '.' {
			// dotted: -.-> -..-> -.- or "-. text .->"
			e.Line = Dotted
			d := run('.')
			c.i += d
			if c.i < len(s) && s[c.i] == '-' {
				c.i++
				e.Head = head()
				e.MinLen = d
				break
			}
			textForm = true
			if !c.textLink(&e, ".") {
				return Edge{}, false
			}
			break
		}
		if a := head(); a != None {
			if k < 2 {
				c.fail("a link is at least two %c", ch)
				return Edge{}, false
			}
			e.Head, e.MinLen = a, k-1
			break
		}
		switch {
		case k >= 3:
			e.MinLen = k - 2
		case k == 2:
			textForm = true
			if !c.textLink(&e, string(ch)) {
				return Edge{}, false
			}
		default:
			c.i = start
			c.fail("expected a link")
			return Edge{}, false
		}
	default:
		c.fail("expected a link")
		return Edge{}, false
	}
	if e.Tail != None && e.Head == None {
		c.i = start
		c.fail("a link with an end at its start needs one at its end")
		return Edge{}, false
	}
	c.skipSpace()
	if c.i < len(s) && s[c.i] == '|' {
		if textForm {
			c.fail("a link has text in one form, not both")
			return Edge{}, false
		}
		k := strings.IndexByte(s[c.i+1:], '|')
		if k < 0 {
			c.fail("link text |…| is not closed")
			return Edge{}, false
		}
		lbl, err := c.p.label(s[c.i+1:c.i+1+k], c.at()+1)
		if err != nil {
			c.bad = true
			return Edge{}, false
		}
		e.Label = lbl
		c.i += k + 2
	}
	return e, true
}

// textLink reads "-- text -->", "== text ==>" or "-. text .->" after its opening, up to the
// closing run, which sets the link's end and length.
func (c *cursor) textLink(e *Edge, mark string) bool {
	s := c.s
	from := c.i
	for j := from; j < len(s); j++ {
		switch mark {
		case ".":
			if s[j] != '.' {
				continue
			}
			d := 0
			for j+d < len(s) && s[j+d] == '.' {
				d++
			}
			if j+d < len(s) && s[j+d] == '-' {
				c.i = j + d + 1
				e.Head = c.headAt()
				e.MinLen = d
				return c.setText(e, s[from:j], from)
			}
			j += d - 1
		default:
			ch := mark[0]
			if s[j] != ch {
				continue
			}
			k := 0
			for j+k < len(s) && s[j+k] == ch {
				k++
			}
			c.i = j + k
			if a := c.headAt(); a != None && k >= 2 {
				e.Head, e.MinLen = a, k-1
				return c.setText(e, s[from:j], from)
			}
			if k >= 3 {
				e.MinLen = k - 2
				return c.setText(e, s[from:j], from)
			}
			c.i = from
			j += k - 1
		}
	}
	c.i = from
	c.fail("the link's text has no closing link")
	return false
}

func (c *cursor) headAt() Arrow {
	if c.i < len(c.s) {
		if a := arrowOf(c.s[c.i]); a != None {
			c.i++
			return a
		}
	}
	return None
}

func (c *cursor) setText(e *Edge, raw string, from int) bool {
	lbl, err := c.p.label(strings.TrimSpace(raw), c.off+from)
	if err != nil {
		c.bad = true
		return false
	}
	e.Label = lbl
	return true
}

func arrowOf(b byte) Arrow {
	switch b {
	case '>':
		return Head
	case 'o':
		return Dot
	case 'x':
		return Cross
	}
	return None
}

var (
	entity   = regexp.MustCompile(`#([A-Za-z]+|[0-9]+);`)
	breakTg  = regexp.MustCompile(`(?i)<br\s*/?>`)
	tag      = regexp.MustCompile(`<[A-Za-z/!]`)
	icon     = regexp.MustCompile(`\bfa[bklrs]?:fa-`)
	mdStrong = regexp.MustCompile(`\*\*|__`)
	mdEm     = regexp.MustCompile(`(^|[^\pL\pN])[*_]|[*_]([^\pL\pN]|$)`)
)

// label turns written text into the plain text drawn: quotes removed, a Markdown string's marks
// removed, entity codes (#quot; #9829;) decoded, <br> made a line break. An icon or any other
// HTML is unsupported.
func (p *parser) label(raw string, off int) (string, error) {
	if len(raw) > MaxLabel {
		l, _ := p.pos(off)
		p.unsupported = fmt.Errorf("%w: a label of %d bytes on line %d, more than %d", ErrTooLarge, len(raw), l, MaxLabel)
		return "", p.unsupported
	}
	s := strings.TrimSpace(raw)
	markdown := false
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
		if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
			s, markdown = s[1:len(s)-1], true
		}
	}
	if icon.MatchString(s) {
		p.unsupport(off, "an icon")
		return "", ErrUnsupported
	}
	s = breakTg.ReplaceAllString(s, "\n")
	if tag.MatchString(s) {
		p.unsupport(off, "HTML in a label")
		return "", ErrUnsupported
	}
	if markdown {
		s = mdStrong.ReplaceAllString(s, "")
		s = mdEm.ReplaceAllString(s, "$1$2")
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = strings.TrimSpace(l)
		}
		s = strings.Join(lines, "\n")
	}
	s = entity.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if name[0] >= '0' && name[0] <= '9' {
			return html.UnescapeString("&#" + name + ";")
		}
		return html.UnescapeString("&" + name + ";")
	})
	return s, nil
}
