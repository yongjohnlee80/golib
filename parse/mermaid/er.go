package mermaid

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ERDiagram is a parsed entity-relationship diagram.
type ERDiagram struct {
	Dir           Dir
	Entities      []Entity       // in order of first mention
	Relationships []Relationship // in source order
	Classes       map[string]Style
}

func (*ERDiagram) Kind() Kind { return ER }

// Entity is an ER diagram's entity.
type Entity struct {
	ID         string
	Label      string // its alias (p[Person]), else the ID
	Attributes []Attribute
	Classes    []string
	// Style is resolved: classDef default, then each class, then style statements.
	Style Style
	Span  [2]int // its first mention
}

// Attribute is one row of an entity's block.
type Attribute struct {
	Type, Name string
	Keys       []string // PK, FK and UK, as written
	Comment    string
	Span       [2]int
}

// Cardinality is how many of an entity a relationship allows at one end.
type Cardinality uint8

const (
	ExactlyOne Cardinality = iota + 1 // ||, only one, 1
	ZeroOrOne                         // |o, o|, zero or one, one or zero
	OneOrMore                         // }|, |{, one or more, one or many, many(1), 1+
	ZeroOrMore                        // }o, o{, zero or more, zero or many, many(0), 0+
)

var cardNames = [...]string{ExactlyOne: "exactly-one", ZeroOrOne: "zero-or-one", OneOrMore: "one-or-more", ZeroOrMore: "zero-or-more"}

func (c Cardinality) String() string               { return enumName(cardNames[:], int(c)) }
func (c Cardinality) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

// Relationship joins two entities: FromCard is the cardinality at From's end, ToCard at To's.
type Relationship struct {
	From, To         string
	FromCard, ToCard Cardinality
	Identifying      bool // -- or "to"; .., .-, -. and "optionally to" are not
	Label            string
	Span             [2]int
}

// erParser is the parse of one ER diagram. It holds the parser rather than embedding it, so the
// parser's own statement and resolve stay the flowchart's.
type erParser struct {
	p       *parser
	d       *ERDiagram
	index   map[string]int // entity id → index in d.Entities
	block   int            // the entity whose { … } block is open; -1 when none
	classOf []classUse
	styles  map[string]Style
}

// er parses an ER diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) er(rest string, restOff int) (Diagram, error) {
	e := &erParser{p: p, d: &ERDiagram{Dir: TB, Classes: map[string]Style{}}, index: map[string]int{}, block: -1,
		styles: map[string]Style{}}
	body := restOff
	if t := strings.TrimSpace(rest); t != "" {
		if !strings.HasPrefix(t, ";") {
			e.p.fail(restOff+strings.Index(rest, t), "unexpected %q after erDiagram", t)
		}
		body = restOff + strings.Index(rest, t)
	}
	for _, st := range e.p.erStatements(body) {
		if e.block >= 0 {
			e.inBlock(st)
		} else {
			e.statement(st)
		}
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	if e.block >= 0 {
		e.p.fail(len(p.src), "the block of %q has no }", e.d.Entities[e.block].ID)
	}
	e.resolve()
	return p.done(e.d)
}

// erStatements splits src[from:] at newlines and at semicolons outside quotes. A line starting
// with %% is a comment.
func (p *parser) erStatements(from int) []stmt {
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
		start, quote := i, false
		for i < len(s) && s[i] != '\n' && (quote || s[i] != ';') {
			if s[i] == '"' {
				quote = !quote
			}
			i++
		}
		out = append(out, stmt{strings.TrimRight(s[start:i], " \t\r"), start})
	}
	return out
}

// statement is one statement outside a block: a keyword, an entity (perhaps opening its block)
// or a relationship.
func (e *erParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "direction":
		d, ok := parseDir(rest)
		if !ok {
			e.p.fail(restOff, "unknown direction %q", rest)
			return
		}
		e.d.Dir = d
		return
	case "classDef":
		names, props := firstWord(rest)
		style := e.p.styleProps(props, st.off+len(st.text)-len(props))
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				e.d.Classes[n] = merge(e.d.Classes[n], style)
			}
		}
		return
	case "class":
		ids, cls := firstWord(rest)
		if cls == "" || strings.ContainsAny(cls, " \t") {
			e.p.fail(restOff, "class wants entity names and one class name")
			return
		}
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				e.classOf = append(e.classOf, classUse{id, cls})
			}
		}
		return
	case "style":
		id, props := firstWord(rest)
		e.styles[id] = merge(e.styles[id], e.p.styleProps(props, st.off+len(st.text)-len(props)))
		return
	case "click", "linkStyle", "call", "callback", "href", "title":
		e.p.unsupport(st.off, w)
		return
	case "accTitle:", "accDescr:":
		return // accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if strings.HasPrefix(rest, ":") {
			return
		}
		e.p.unsupport(st.off, w+" block")
		return
	}
	if strings.HasPrefix(st.text, "}") {
		e.p.fail(st.off, "} without an entity's block")
		return
	}
	c := &cursor{p: e.p, s: st.text, off: st.off}
	from, ok := e.entityRef(c)
	if !ok {
		return
	}
	c.skipSpace()
	switch {
	case c.i == len(c.s):
		return
	case c.s[c.i] == '{':
		e.openBlock(from, c)
		return
	}
	r := Relationship{From: from, Span: [2]int{st.off, st.off + len(st.text)}}
	if r.FromCard, ok = e.card(c); !ok {
		return
	}
	c.skipSpace()
	if r.Identifying, ok = e.relation(c); !ok {
		return
	}
	c.skipSpace()
	if r.ToCard, ok = e.card(c); !ok {
		return
	}
	c.skipSpace()
	if r.To, ok = e.entityRef(c); !ok {
		return
	}
	c.skipSpace()
	if c.i < len(c.s) {
		if c.s[c.i] != ':' {
			c.fail("unexpected %q after the relationship", c.s[c.i:])
			return
		}
		lbl, err := e.p.label(c.s[c.i+1:], c.at()+1)
		if err != nil {
			return
		}
		r.Label = lbl
	}
	e.d.Relationships = append(e.d.Relationships, r)
}

// openBlock reads the { after an entity, and whatever follows it in the same statement: the
// first attribute, perhaps the closing }.
func (e *erParser) openBlock(id string, c *cursor) {
	e.block = e.index[id]
	c.i++
	c.skipSpace()
	if c.i < len(c.s) {
		e.inBlock(stmt{c.s[c.i:], c.at()})
	}
}

var (
	attrType = regexp.MustCompile(`^[\pL_][\pL\pN_\-\[\]\(\),.]*$`)
	attrName = regexp.MustCompile(`^\*?[\pL_][\pL\pN_\-\[\]\(\)]*$`)
)

// inBlock reads a statement inside an entity's block: an attribute, the closing }, or both.
func (e *erParser) inBlock(st stmt) {
	text := st.text
	closed := false
	if k := closingBrace(text); k >= 0 {
		if tail := strings.TrimSpace(text[k+1:]); tail != "" {
			e.p.fail(st.off+k+1, "unexpected %q after }", tail)
		}
		text, closed = strings.TrimRight(text[:k], " \t"), true
	}
	if strings.TrimSpace(text) != "" {
		e.attribute(stmt{text, st.off})
	}
	if closed {
		e.block = -1
	}
}

// closingBrace is the index of the first } outside quotes in s, or -1.
func closingBrace(s string) int {
	quote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quote = !quote
		case '}':
			if !quote {
				return i
			}
		}
	}
	return -1
}

// attribute reads "type name [PK|FK|UK[, …]] ["comment"]".
func (e *erParser) attribute(st stmt) {
	text := st.text
	a := Attribute{Span: [2]int{st.off, st.off + len(st.text)}}
	if q := strings.IndexByte(text, '"'); q >= 0 {
		end := strings.IndexByte(text[q+1:], '"')
		if end < 0 {
			e.p.fail(st.off+q, "the comment is not closed")
			return
		}
		closeAt := q + 1 + end + 1
		if tail := strings.TrimSpace(text[closeAt:]); tail != "" {
			e.p.fail(st.off+closeAt, "unexpected %q after the comment", tail)
			return
		}
		lbl, err := e.p.label(text[q:closeAt], st.off+q)
		if err != nil {
			return
		}
		a.Comment = lbl
		text = text[:q]
	}
	fields := strings.Fields(text)
	if len(fields) < 2 {
		e.p.fail(st.off, "an attribute wants a type and a name")
		return
	}
	a.Type, a.Name = fields[0], fields[1]
	if !attrType.MatchString(a.Type) {
		e.p.fail(st.off, "%q is not an attribute type", a.Type)
		return
	}
	if !attrName.MatchString(a.Name) {
		e.p.fail(st.off+strings.Index(st.text, a.Name), "%q is not an attribute name", a.Name)
		return
	}
	keys := strings.Join(fields[2:], " ")
	for _, k := range strings.FieldsFunc(keys, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		switch k {
		case "PK", "FK", "UK":
			a.Keys = append(a.Keys, k)
		default:
			e.p.fail(st.off+strings.LastIndex(st.text, k), "%q is not a key: PK, FK or UK", k)
			return
		}
	}
	ent := &e.d.Entities[e.block]
	ent.Attributes = append(ent.Attributes, a)
}

// entityRef reads an entity at the cursor: a name or a quoted name, then perhaps an alias in
// brackets and :::classes. Reading it mentions it, so its first mention fixes its place.
func (e *erParser) entityRef(c *cursor) (string, bool) {
	start := c.i
	var id string
	if c.i < len(c.s) && c.s[c.i] == '"' {
		end := strings.IndexByte(c.s[c.i+1:], '"')
		if end < 0 {
			c.fail("the entity name is not closed")
			return "", false
		}
		lbl, err := e.p.label(c.s[c.i:c.i+end+2], c.at())
		if err != nil {
			return "", false
		}
		id = lbl
		c.i += end + 2
	} else {
		n := entityLen(c.s[c.i:])
		if n == 0 {
			if start == 0 && !strings.ContainsRune("|}-.:[{", rune(c.s[0])) { // not this grammar's: another renderer's
				e.p.unsupport(c.at(), "a statement that is not an entity or a relationship")
			} else {
				c.fail("an entity name was expected")
			}
			return "", false
		}
		id = c.s[c.i : c.i+n]
		c.i += n
	}
	alias := ""
	if c.i < len(c.s) && c.s[c.i] == '[' {
		end := strings.IndexByte(c.s[c.i:], ']')
		if end < 0 {
			c.fail("the alias is not closed")
			return "", false
		}
		lbl, err := e.p.label(c.s[c.i+1:c.i+end], c.at()+1)
		if err != nil {
			return "", false
		}
		alias = lbl
		c.i += end + 1
	}
	var classes []string
	if strings.HasPrefix(c.s[c.i:], ":::") {
		c.i += 3
		n := 0
		for c.i+n < len(c.s) && !strings.ContainsRune(" \t{", rune(c.s[c.i+n])) {
			n++
		}
		if n == 0 {
			c.fail("::: wants a class name")
			return "", false
		}
		for _, cl := range strings.Split(c.s[c.i:c.i+n], ",") {
			if cl = strings.TrimSpace(cl); cl != "" {
				classes = append(classes, cl)
			}
		}
		c.i += n
	}
	i, ok := e.index[id]
	if !ok {
		i = len(e.d.Entities)
		e.index[id] = i
		e.d.Entities = append(e.d.Entities, Entity{ID: id, Label: id, Span: [2]int{c.off + start, c.at()}})
	}
	if alias != "" {
		e.d.Entities[i].Label = alias
	}
	e.d.Entities[i].Classes = append(e.d.Entities[i].Classes, classes...)
	return id, true
}

// entityLen is the length of the entity name at the start of s: letters, digits, _ and -, where a
// - is not the start of a relationship's -- or -.
func entityLen(s string) int {
	i := 0
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			i += n
		case r == '-' && i > 0 && i+1 < len(s) && s[i+1] != '-' && s[i+1] != '.':
			i++
		default:
			return i
		}
	}
	return i
}

// cardSymbols are the cardinalities written as symbols; either side may use any of them.
var cardSymbols = []struct {
	s string
	c Cardinality
}{{"||", ExactlyOne}, {"|o", ZeroOrOne}, {"o|", ZeroOrOne}, {"}|", OneOrMore}, {"|{", OneOrMore}, {"}o", ZeroOrMore}, {"o{", ZeroOrMore}}

// cardWords are the cardinalities written as words, the longest first so that a prefix never
// matches early.
var cardWords = []struct {
	s string
	c Cardinality
}{
	{"zero or more", ZeroOrMore}, {"zero or many", ZeroOrMore}, {"one or more", OneOrMore}, {"one or many", OneOrMore},
	{"zero or one", ZeroOrOne}, {"one or zero", ZeroOrOne}, {"only one", ExactlyOne},
	{"many(0)", ZeroOrMore}, {"many(1)", OneOrMore}, {"0+", ZeroOrMore}, {"1+", OneOrMore}, {"1", ExactlyOne},
}

func (e *erParser) card(c *cursor) (Cardinality, bool) {
	rest := c.s[c.i:]
	for _, cs := range cardSymbols {
		if strings.HasPrefix(rest, cs.s) {
			c.i += len(cs.s)
			return cs.c, true
		}
	}
	for _, cw := range cardWords {
		if len(rest) >= len(cw.s) && strings.EqualFold(rest[:len(cw.s)], cw.s) && wordEnds(rest, len(cw.s)) {
			c.i += len(cw.s)
			return cw.c, true
		}
	}
	c.fail("a cardinality was expected: ||, |o, }|, }o or their words")
	return 0, false
}

// wordEnds says whether a word of length n at the start of s ends there, rather than running on
// into a longer word.
func wordEnds(s string, n int) bool {
	if n >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[n:])
	return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}

// relation reads the line between the cardinalities: identifying or not.
func (e *erParser) relation(c *cursor) (identifying, ok bool) {
	rest := c.s[c.i:]
	switch {
	case strings.HasPrefix(rest, "--"):
		c.i += 2
		return true, true
	case strings.HasPrefix(rest, ".."), strings.HasPrefix(rest, ".-"), strings.HasPrefix(rest, "-."):
		c.i += 2
		return false, true
	}
	lower := strings.ToLower(rest)
	switch {
	case strings.HasPrefix(lower, "optionally to") && wordEnds(rest, len("optionally to")):
		c.i += len("optionally to")
		return false, true
	case strings.HasPrefix(lower, "to") && wordEnds(rest, 2):
		c.i += 2
		return true, true
	}
	c.fail("a relationship was expected: --, .., to or optionally to")
	return false, false
}

// resolve applies classes and styles to the entities they name.
func (e *erParser) resolve() {
	for _, cu := range e.classOf {
		if i, ok := e.index[cu.id]; ok {
			e.d.Entities[i].Classes = append(e.d.Entities[i].Classes, cu.class)
		}
	}
	for i := range e.d.Entities {
		n := &e.d.Entities[i]
		st := slices.Clone(e.d.Classes["default"])
		for _, c := range n.Classes {
			st = merge(st, e.d.Classes[c])
		}
		st = merge(st, e.styles[n.ID])
		if len(st) > 0 {
			n.Style = st
		}
	}
}
