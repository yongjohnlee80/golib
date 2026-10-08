package mermaid

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Parse reads a diagram. ErrUnsupported (errors.Is) is a type or construct golib does not draw;
// ErrTooLarge a source or label past its limit; a *SyntaxError a diagram that is wrong.
func Parse(src string) (Diagram, error) {
	if len(src) > MaxSource {
		return nil, fmt.Errorf("%w: %d bytes of source, more than %d", ErrTooLarge, len(src), MaxSource)
	}
	p := &parser{src: src}
	p.lineStarts()
	return p.parse()
}

type parser struct {
	src    string
	starts []int // byte offset of each line

	fc          *FlowchartDiagram
	nodes       map[string]int // id → index in fc.Nodes
	subgraphs   map[string]int
	stack       []string // open subgraphs, outermost first
	classOf     []classUse
	styles      map[string]Style
	unsupported error
	syntax      *SyntaxError
}

type classUse struct{ id, class string }

func (p *parser) lineStarts() {
	p.starts = []int{0}
	for i := 0; i < len(p.src); i++ {
		if p.src[i] == '\n' {
			p.starts = append(p.starts, i+1)
		}
	}
}

// pos turns a byte offset into a 1-based line and character column.
func (p *parser) pos(off int) (line, col int) {
	i, _ := slices.BinarySearch(p.starts, off+1)
	start := p.starts[i-1]
	return i, utf8.RuneCountInString(p.src[start:off]) + 1
}

// fail records the first syntax error; the scan goes on, since a later unsupported construct
// would make the diagram another renderer's.
func (p *parser) fail(off int, format string, args ...any) {
	if p.syntax == nil {
		l, c := p.pos(off)
		p.syntax = &SyntaxError{Line: l, Col: c, Msg: fmt.Sprintf(format, args...)}
	}
}

func (p *parser) unsupport(off int, what string) {
	if p.unsupported == nil {
		l, _ := p.pos(off)
		p.unsupported = fmt.Errorf("%w: %s (line %d)", ErrUnsupported, what, l)
	}
}

// stmt is one statement: its text and where it starts in the source.
type stmt struct {
	text string
	off  int
}

// statements splits src[from:] into statements at newlines and semicolons, never inside quotes
// (a quoted label may span lines), brackets or a |link text|. A line starting with %% is a
// comment, %%{ … }%% directives included.
func (p *parser) statements(from int) []stmt {
	var out []stmt
	s := p.src
	i := from
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n' || s[i] == ';') {
			i++
		}
		if i >= len(s) {
			break
		}
		if strings.HasPrefix(s[i:], "%%") {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		start, depth, quote, pipe := i, 0, false, false
		for i < len(s) {
			c := s[i]
			if quote {
				if c == '"' {
					quote = false
				}
				i++
				continue
			}
			if c == '\n' || (c == ';' && depth == 0 && !pipe) {
				break
			}
			switch c {
			case '"':
				quote = true
			case '[', '(', '{':
				depth++
			case ']', ')', '}':
				depth = max(depth-1, 0)
			case '|':
				pipe = !pipe
			}
			i++
		}
		text := strings.TrimRight(s[start:i], " \t\r")
		out = append(out, stmt{text, start})
	}
	return out
}

// header finds the diagram's type line and returns its keyword, the rest of that line, and where
// the rest starts.
func (p *parser) header() (kw, rest string, restOff int, err error) {
	s := p.src
	i := 0
	for i < len(s) {
		j := strings.IndexByte(s[i:], '\n')
		end := len(s)
		if j >= 0 {
			end = i + j
		}
		line := strings.TrimSpace(s[i:end])
		switch {
		case line == "" || strings.HasPrefix(line, "%%"):
		case line == "---":
			return "", "", 0, fmt.Errorf("%w: front matter (line 1)", ErrUnsupported)
		default:
			lead := i + strings.Index(s[i:end], line)
			kw = line
			if k := strings.IndexAny(line, " \t;"); k >= 0 {
				kw = line[:k]
			}
			return kw, line[len(kw):], lead + len(kw), nil
		}
		i = end + 1
	}
	return "", "", 0, &SyntaxError{Line: 1, Col: 1, Msg: "no diagram: the source is empty"}
}

func (p *parser) parse() (Diagram, error) {
	kw, rest, restOff, err := p.header()
	if err != nil {
		return nil, err
	}
	switch kw {
	case "flowchart", "graph":
		return p.flowchart(rest, restOff)
	case "sequenceDiagram":
		return p.sequence(rest, restOff)
	case "classDiagram", "classDiagram-v2":
		return p.class(rest, restOff)
	case "stateDiagram", "stateDiagram-v2":
		return p.state(rest, restOff)
	case "erDiagram":
		return p.er(rest, restOff)
	}
	return nil, fmt.Errorf("%w: %q diagrams", ErrUnsupported, kw)
}

// done is a parse's answer: an unsupported construct first (the diagram is another renderer's,
// whatever else is wrong in it), then a syntax error, then d.
func (p *parser) done(d Diagram) (Diagram, error) {
	switch {
	case p.unsupported != nil:
		return nil, p.unsupported
	case p.syntax != nil:
		return nil, p.syntax
	}
	return d, nil
}

// flowchart parses a flowchart's body: rest is its header line after the keyword, at restOff.
func (p *parser) flowchart(rest string, restOff int) (Diagram, error) {
	p.fc = &FlowchartDiagram{Dir: TB, Classes: map[string]Style{}}
	p.nodes, p.subgraphs, p.styles = map[string]int{}, map[string]int{}, map[string]Style{}

	// The header line: an optional direction, then perhaps statements after a semicolon.
	body := restOff
	trimmed := strings.TrimLeft(rest, " \t")
	dirWord := trimmed
	if k := strings.IndexAny(trimmed, " \t;\r"); k >= 0 {
		dirWord = trimmed[:k]
	}
	if dirWord != "" {
		d, ok := parseDir(dirWord)
		if !ok {
			p.fail(restOff+len(rest)-len(trimmed), "unknown direction %q", dirWord)
		}
		p.fc.Dir = d
		body = restOff + len(rest) - len(trimmed) + len(dirWord)
	}
	for _, st := range p.statements(body) {
		p.statement(st)
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	if len(p.stack) > 0 {
		p.fail(len(p.src), "subgraph %q has no end", p.stack[len(p.stack)-1])
	}
	p.resolve()
	return p.done(p.fc)
}

func parseDir(w string) (Dir, bool) {
	switch w {
	case "TB", "TD":
		return TB, true
	case "BT":
		return BT, true
	case "LR":
		return LR, true
	case "RL":
		return RL, true
	}
	return TB, false
}

// firstWord is a statement's leading word and the text after it.
func firstWord(s string) (string, string) {
	if k := strings.IndexAny(s, " \t"); k >= 0 {
		return s[:k], strings.TrimLeft(s[k:], " \t")
	}
	return s, ""
}

func (p *parser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "subgraph":
		p.openSubgraph(rest, restOff, st.off)
	case "end":
		if rest != "" {
			p.fail(restOff, "unexpected %q after end", rest)
		}
		if len(p.stack) == 0 {
			p.fail(st.off, "end without a subgraph")
			return
		}
		sg := &p.fc.Subgraphs[p.subgraphs[p.stack[len(p.stack)-1]]]
		sg.Span[1] = st.off + len(st.text)
		p.stack = p.stack[:len(p.stack)-1]
	case "direction":
		d, ok := parseDir(rest)
		if !ok {
			p.fail(restOff, "unknown direction %q", rest)
			return
		}
		if len(p.stack) == 0 {
			p.fc.Dir = d
			return
		}
		p.fc.Subgraphs[p.subgraphs[p.stack[len(p.stack)-1]]].Dir = d
	case "classDef":
		names, props := firstWord(rest)
		style := p.styleProps(props, st.off+len(st.text)-len(props))
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				p.fc.Classes[n] = merge(p.fc.Classes[n], style)
			}
		}
	case "class":
		ids, cls := firstWord(rest)
		if cls == "" || strings.ContainsAny(cls, " \t") {
			p.fail(restOff, "class wants node ids and one class name")
			return
		}
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				p.classOf = append(p.classOf, classUse{id, cls})
			}
		}
	case "style":
		id, props := firstWord(rest)
		p.styles[id] = merge(p.styles[id], p.styleProps(props, st.off+len(st.text)-len(props)))
	case "click", "linkStyle", "call", "href":
		p.unsupport(st.off, w)
	case "accTitle:", "accDescr:":
		// accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if strings.HasPrefix(rest, ":") {
			return
		}
		p.unsupport(st.off, w+" block")
	default:
		p.chain(st)
	}
}

// openSubgraph reads "subgraph id", "subgraph id [title]", "subgraph id["title"]" or
// "subgraph some title" (whose id is the title).
func (p *parser) openSubgraph(rest string, off, stmtOff int) {
	if rest == "" {
		p.fail(off, "subgraph wants an id or a title")
		return
	}
	id, title := rest, rest
	n := identLen(rest)
	switch {
	case n > 0 && n < len(rest) && strings.TrimLeft(rest[n:], " \t") != "" && strings.TrimLeft(rest[n:], " \t")[0] == '[':
		t := strings.TrimLeft(rest[n:], " \t")
		if !strings.HasSuffix(t, "]") {
			p.fail(off, "subgraph title %q is not closed", t)
			return
		}
		id = rest[:n]
		lbl, err := p.label(t[1:len(t)-1], off+len(rest)-len(t)+1)
		if err != nil {
			return
		}
		title = lbl
	case strings.HasPrefix(rest, "\""):
		lbl, err := p.label(rest, off)
		if err != nil {
			return
		}
		id, title = lbl, lbl
	}
	if _, dup := p.subgraphs[id]; dup {
		p.fail(off, "subgraph %q is defined twice", id)
		return
	}
	parent := ""
	if len(p.stack) > 0 {
		parent = p.stack[len(p.stack)-1]
	}
	p.subgraphs[id] = len(p.fc.Subgraphs)
	p.fc.Subgraphs = append(p.fc.Subgraphs, Subgraph{ID: id, Title: title, Parent: parent, Span: [2]int{stmtOff, off + len(rest)}})
	p.stack = append(p.stack, id)
}

// styleProps reads "fill:#f9f,stroke:#333,stroke-width:4px". Commas inside parentheses
// (rgb(1,2,3)) belong to the value.
func (p *parser) styleProps(s string, off int) Style {
	var out Style
	depth, start := 0, 0
	flush := func(end int) {
		part := strings.TrimSpace(s[start:end])
		if part == "" {
			return
		}
		k := strings.IndexByte(part, ':')
		if k <= 0 {
			p.fail(off+start, "style property %q has no name:value", part)
			return
		}
		out = append(out, StyleProp{strings.TrimSpace(part[:k]), strings.TrimSpace(part[k+1:])})
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(s))
	if len(out) == 0 {
		p.fail(off, "no style properties")
	}
	return out
}

// merge adds b's properties to a's; one of the same name replaces it in place.
func merge(a, b Style) Style {
	out := slices.Clone(a)
	for _, pr := range b {
		if i := slices.IndexFunc(out, func(x StyleProp) bool { return x.Name == pr.Name }); i >= 0 {
			out[i] = pr
			continue
		}
		out = append(out, pr)
	}
	return out
}

// resolve applies classes and styles, and refuses links to a subgraph: a subgraph is a box, not
// a node, and this model has no edge to a box.
func (p *parser) resolve() {
	for _, cu := range p.classOf {
		if i, ok := p.nodes[cu.id]; ok {
			p.fc.Nodes[i].Classes = append(p.fc.Nodes[i].Classes, cu.class)
		}
	}
	for i := range p.fc.Nodes {
		n := &p.fc.Nodes[i]
		st := slices.Clone(p.fc.Classes["default"])
		for _, c := range n.Classes {
			st = merge(st, p.fc.Classes[c])
		}
		st = merge(st, p.styles[n.ID])
		if len(st) > 0 {
			n.Style = st
		}
	}
	for _, e := range p.fc.Edges {
		for _, end := range []string{e.From, e.To} {
			if _, ok := p.subgraphs[end]; ok {
				p.unsupport(e.Span[0], "a link to subgraph "+end)
			}
		}
	}
}
