package mermaid

import "strings"

// MindmapDiagram is a parsed mind map: its nodes in source order, the root first.
type MindmapDiagram struct {
	Nodes []MindNode
}

func (*MindmapDiagram) Kind() Kind { return Mindmap }

// MindNode is one node of a mind map.
type MindNode struct {
	ID     string
	Label  string // plain: quotes, Markdown marks and entities resolved; the ID's text when none
	Shape  MindShape
	Parent int // the parent's index in Nodes; -1 for the root
	Span   [2]int
}

// MindShape is a mind map node's outline.
type MindShape uint8

const (
	MindDefault MindShape = iota // text
	MindSquare                   // id[text]
	MindRounded                  // id(text)
	MindCircle                   // id((text))
	MindBang                     // id))text((
	MindCloud                    // id)text(
	MindHexagon                  // id{{text}}
)

var mindShapeNames = [...]string{MindDefault: "default", MindSquare: "square", MindRounded: "rounded",
	MindCircle: "circle", MindBang: "bang", MindCloud: "cloud", MindHexagon: "hexagon"}

func (s MindShape) String() string               { return enumName(mindShapeNames[:], int(s)) }
func (s MindShape) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// mindmapDelims are the shapes' delimiters, the two-character ones first so "((" is never read
// as "(".
var mindmapDelims = []struct {
	open, close string
	shape       MindShape
}{
	{"((", "))", MindCircle},
	{"))", "((", MindBang},
	{"{{", "}}", MindHexagon},
	{"[", "]", MindSquare},
	{"(", ")", MindRounded},
	{")", "(", MindCloud},
}

// mindmapParser reads a mind map's lines: each node's parent is the nearest line above it that is
// indented less.
type mindmapParser struct {
	p     *parser
	d     *MindmapDiagram
	stack []mindmapOpen // the nodes a deeper line may hang from, outermost first
}

type mindmapOpen struct{ indent, node int }

// mindmap parses a mind map's body: rest is its header line after the keyword, at restOff.
func (p *parser) mindmap(rest string, restOff int) (Diagram, error) {
	m := &mindmapParser{p: p, d: &MindmapDiagram{}}
	if r := strings.TrimSpace(rest); r != "" && !strings.HasPrefix(r, "%%") {
		p.fail(restOff, "unexpected %q after mindmap", r)
	}
	src := p.src
	start := restOff + len(rest)
	for start < len(src) {
		end := strings.IndexByte(src[start:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += start
		}
		m.line(src[start:end], start)
		if p.unsupported != nil {
			break
		}
		start = end + 1
	}
	return p.done(m.d)
}

// line is one line of the body at off: blank, a comment, or a node.
func (m *mindmapParser) line(raw string, off int) {
	text := strings.TrimRight(raw, " \t\r")
	t := strings.TrimLeft(text, " \t")
	if t == "" || strings.HasPrefix(t, "%%") {
		return
	}
	at := off + len(text) - len(t)
	switch {
	case strings.HasPrefix(t, "::icon("):
		m.p.unsupport(at, "icons")
		return
	case strings.Contains(t, ":::"):
		m.p.unsupport(at, "classes")
		return
	}
	indent := len(text) - len(t)
	for n := len(m.stack); n > 0 && m.stack[n-1].indent >= indent; n = len(m.stack) {
		m.stack = m.stack[:n-1]
	}
	parent := -1
	if len(m.stack) > 0 {
		parent = m.stack[len(m.stack)-1].node
	} else if len(m.d.Nodes) > 0 {
		m.p.fail(at, "a second root: indent it under the first")
		return
	}
	id, label, shape, ok := m.node(t, at)
	if !ok {
		return
	}
	m.stack = append(m.stack, mindmapOpen{indent, len(m.d.Nodes)})
	m.d.Nodes = append(m.d.Nodes, MindNode{ID: id, Label: label, Shape: shape, Parent: parent, Span: [2]int{at, at + len(t)}})
}

// node reads a node's text: an id and a shape's delimiters round its label, or text alone, which
// is both. An id holds no space, so "Ideas (more)" is text, not an id and a shape.
func (m *mindmapParser) node(t string, at int) (id, label string, shape MindShape, ok bool) {
	if i := strings.IndexAny(t, "[({)"); i >= 0 && !strings.ContainsAny(t[:i], " \t") {
		rest := t[i:]
		for _, d := range mindmapDelims {
			if !strings.HasPrefix(rest, d.open) {
				continue
			}
			if len(rest) < len(d.open)+len(d.close) || !strings.HasSuffix(rest, d.close) {
				m.p.fail(at+i, "%q opens a shape that does not close with %q", d.open, d.close)
				return "", "", 0, false
			}
			inner := rest[len(d.open) : len(rest)-len(d.close)]
			text, err := m.p.label(inner, at+i+len(d.open))
			if err != nil {
				return "", "", 0, false
			}
			id = t[:i]
			if id == "" {
				id = text
			}
			return id, text, d.shape, true
		}
	}
	text, err := m.p.label(t, at)
	if err != nil {
		return "", "", 0, false
	}
	return text, text, MindDefault, true
}
