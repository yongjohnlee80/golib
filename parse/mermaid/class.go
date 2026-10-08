package mermaid

import (
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ClassDiagram is a parsed class diagram.
type ClassDiagram struct {
	Dir        Dir
	Classes    []ClassBox // in order of first mention
	Relations  []Relation // in source order
	Namespaces []Namespace
	Notes      []ClassNote
	Styles     map[string]Style // classDef; "default" applies to every class
}

func (*ClassDiagram) Kind() Kind { return Class }

// ClassBox is a class: its name, annotations and members.
type ClassBox struct {
	ID string
	// Label is the name drawn: its ["label"], or its id, generics written ~T~ drawn <T>.
	Label       string
	Annotations []string // <<interface>> and the like, without the brackets
	Attributes  []string // members as written, generics drawn <T>
	Methods     []string // members with a (
	Namespace   string   // "" outside one
	Classes     []string // cssClass and :::, in the order given
	Style       Style    // resolved: classDef default, each class, then style statements
	Span        [2]int   // its first mention
}

// Relation is a line between two classes. From is the class written left of the arrow.
type Relation struct {
	From, To         string
	FromEnd, ToEnd   RelEnd
	Line             LineKind // Solid (--) or Dotted (..)
	FromCard, ToCard string   // the "cardinality" written beside each end
	Label            string
	Span             [2]int
}

// RelEnd is what a relation's end is drawn as.
type RelEnd uint8

const (
	RelNone        RelEnd = iota
	RelInheritance        // <| or |>: a hollow triangle
	RelComposition        // *: a filled diamond
	RelAggregation        // o: a hollow diamond
	RelAssociation        // < or >: an arrowhead
)

var relEndNames = [...]string{RelNone: "none", RelInheritance: "inheritance", RelComposition: "composition",
	RelAggregation: "aggregation", RelAssociation: "association"}

func (e RelEnd) String() string               { return enumName(relEndNames[:], int(e)) }
func (e RelEnd) MarshalText() ([]byte, error) { return []byte(e.String()), nil }

// Namespace is a box round the classes declared in it.
type Namespace struct {
	Name string
	Span [2]int
}

// ClassNote is a note, on a class (For) or on the diagram.
type ClassNote struct {
	For, Text string
	Span      [2]int
}

// classParser is a class diagram being read.
type classParser struct {
	p         *parser
	d         *ClassDiagram
	index     map[string]int // id → index in d.Classes
	namespace string         // the open namespace, "" outside one
	block     int            // the class whose { … } is open, -1 when none
	cssOf     []classUse
	styles    map[string]Style
}

// class parses a class diagram's body: rest is its header line after the keyword, at restOff.
func (p *parser) class(rest string, restOff int) (Diagram, error) {
	c := &classParser{p: p, d: &ClassDiagram{Dir: TB, Styles: map[string]Style{}}, index: map[string]int{},
		block: -1, styles: map[string]Style{}}
	for _, st := range p.statements(restOff) {
		if c.block >= 0 {
			c.member(st)
		} else {
			c.statement(st)
		}
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	if c.block >= 0 {
		p.fail(len(p.src), "class %q has no closing }", c.d.Classes[c.block].ID)
	}
	if c.namespace != "" {
		p.fail(len(p.src), "namespace %q has no closing }", c.namespace)
	}
	c.resolve()
	return p.done(c.d)
}

func (c *classParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch {
	case w == "class":
		c.classStmt(rest, restOff, st)
	case w == "namespace":
		c.openNamespace(rest, restOff, st)
	case st.text == "}":
		if c.namespace == "" {
			c.p.fail(st.off, "} closes nothing")
			return
		}
		c.d.Namespaces[len(c.d.Namespaces)-1].Span[1] = st.off + len(st.text)
		c.namespace = ""
	case w == "direction":
		d, ok := parseDir(rest)
		if !ok {
			c.p.fail(restOff, "unknown direction %q", rest)
			return
		}
		c.d.Dir = d
	case w == "note":
		c.note(rest, restOff, st)
	case w == "classDef":
		names, props := firstWord(rest)
		style := c.p.styleProps(props, st.off+len(st.text)-len(props))
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				c.d.Styles[n] = merge(c.d.Styles[n], style)
			}
		}
	case w == "cssClass":
		c.cssClass(rest, restOff)
	case w == "style":
		id, props := firstWord(rest)
		c.styles[id] = merge(c.styles[id], c.p.styleProps(props, st.off+len(st.text)-len(props)))
	case w == "click" || w == "callback" || w == "link":
		c.p.unsupport(st.off, w)
	case w == "accTitle:" || w == "accDescr:":
		// accessibility text: nothing drawn
	case w == "accTitle" || w == "accDescr":
		if !strings.HasPrefix(rest, ":") {
			c.p.unsupport(st.off, w+" block")
		}
	case strings.HasPrefix(st.text, "<<"):
		c.annotation(st.text, st.off)
	case relationAt(st.text) >= 0:
		c.relation(st)
	default:
		if n := classIDLen(st.text); n > 0 {
			if t := strings.TrimLeft(st.text[n:], " \t"); strings.HasPrefix(t, ":") {
				c.memberOf(st.text[:n], t[1:], st.off+len(st.text)-len(t)+1)
				return
			}
		}
		c.p.unsupport(st.off, "the statement "+quoteShort(st.text))
	}
}

func quoteShort(s string) string {
	if len(s) > 40 {
		s = s[:40] + "…"
	}
	return `"` + s + `"`
}

func isIdent(r rune) bool { return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// classIDLen is the length of the class id at the start of s: an identifier, with any generic
// ~T~ suffix (no spaces in it).
func classIDLen(s string) int {
	i := 0
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !isIdent(r) {
			break
		}
		i += n
	}
	if i == 0 || i >= len(s) || s[i] != '~' {
		return i
	}
	j := i
	for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '[' && s[j] != ':' && s[j] != '{' && s[j] != '"' {
		j++
	}
	if k := strings.LastIndexByte(s[i:j], '~'); k > 0 {
		return i + k + 1
	}
	return i
}

// splitID is a written class id's id and its drawn name: ~T~ becomes <T>.
func splitID(s string) (id, name string) {
	if k := strings.IndexByte(s, '~'); k >= 0 {
		return s[:k], generics(s)
	}
	return s, s
}

// generics draws Mermaid's ~T~ as <T>: a ~ followed by an identifier opens, any other closes.
func generics(s string) string {
	if !strings.Contains(s, "~") {
		return s
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '~' {
			b.WriteByte(s[i])
			continue
		}
		r, _ := utf8.DecodeRuneInString(s[i+1:])
		opens := i+1 < len(s) && isIdent(r)
		if depth > 0 && !opens {
			b.WriteByte('>')
			depth--
			continue
		}
		b.WriteByte('<')
		depth++
	}
	return b.String()
}

// mention is class written's index, adding it on its first mention.
func (c *classParser) mention(written string, span [2]int) int {
	id, name := splitID(written)
	if i, ok := c.index[id]; ok {
		if name != id && c.d.Classes[i].Label == id {
			c.d.Classes[i].Label = name
		}
		return i
	}
	c.index[id] = len(c.d.Classes)
	c.d.Classes = append(c.d.Classes, ClassBox{ID: id, Label: name, Namespace: c.namespace, Span: span})
	return len(c.d.Classes) - 1
}

// classStmt reads "class Name", "class Name~T~", "class Name["Label"]", "class Name:::css",
// "class Name <<annotation>>", each perhaps followed by "{" opening its members, or "{ … }".
func (c *classParser) classStmt(rest string, off int, st stmt) {
	n := classIDLen(rest)
	if n == 0 {
		c.p.fail(off, "class wants a name")
		return
	}
	i := c.mention(rest[:n], [2]int{st.off, st.off + len(st.text)})
	if c.namespace != "" {
		c.d.Classes[i].Namespace = c.namespace
	}
	t := strings.TrimLeft(rest[n:], " \t")
	tOff := func() int { return off + len(rest) - len(t) }
	if strings.HasPrefix(t, "[") {
		k := strings.IndexByte(t, ']')
		if k < 0 {
			c.p.fail(tOff(), "the class's label is not closed")
			return
		}
		lbl, err := c.p.label(t[1:k], tOff()+1)
		if err != nil {
			return
		}
		c.d.Classes[i].Label = lbl
		t = strings.TrimLeft(t[k+1:], " \t")
	}
	if strings.HasPrefix(t, ":::") {
		css := t[3:]
		k := 0
		for k < len(css) && !strings.ContainsRune(" \t{<", rune(css[k])) {
			k++
		}
		if k == 0 {
			c.p.fail(tOff(), "::: wants a class name")
			return
		}
		c.cssOf = append(c.cssOf, classUse{c.d.Classes[i].ID, css[:k]})
		t = strings.TrimLeft(css[k:], " \t")
	}
	if strings.HasPrefix(t, "<<") {
		k := strings.Index(t, ">>")
		if k < 0 {
			c.p.fail(tOff(), "the annotation is not closed")
			return
		}
		c.d.Classes[i].Annotations = append(c.d.Classes[i].Annotations, strings.TrimSpace(t[2:k]))
		t = strings.TrimLeft(t[k+2:], " \t")
	}
	switch {
	case t == "":
	case t == "{":
		c.block = i
	case strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}"):
		if m := strings.TrimSpace(t[1 : len(t)-1]); m != "" {
			c.addMember(i, m, tOff()+1)
		}
	default:
		c.p.fail(tOff(), "unexpected %s after the class name", quoteShort(t))
	}
}

// member reads a line inside a class's { … }: a member, an annotation, or the closing }.
func (c *classParser) member(st stmt) {
	t := st.text
	if t == "}" {
		c.block = -1
		return
	}
	if strings.HasSuffix(t, "}") { // the last member and the } on one line
		c.member(stmt{strings.TrimSpace(t[:len(t)-1]), st.off})
		c.block = -1
		return
	}
	if strings.HasPrefix(t, "<<") {
		k := strings.Index(t, ">>")
		if k < 0 {
			c.p.fail(st.off, "the annotation is not closed")
			return
		}
		c.d.Classes[c.block].Annotations = append(c.d.Classes[c.block].Annotations, strings.TrimSpace(t[2:k]))
		return
	}
	if t != "" {
		c.addMember(c.block, t, st.off)
	}
}

// memberOf reads "Name : member".
func (c *classParser) memberOf(written, m string, off int) {
	i := c.mention(written, [2]int{off, off + len(m)})
	m = strings.TrimSpace(m)
	if m == "" {
		c.p.fail(off, "the member is empty")
		return
	}
	if strings.HasPrefix(m, "<<") && strings.HasSuffix(m, ">>") {
		c.d.Classes[i].Annotations = append(c.d.Classes[i].Annotations, strings.TrimSpace(m[2:len(m)-2]))
		return
	}
	c.addMember(i, m, off)
}

// addMember adds a member to class i: a method if it has a (, else an attribute.
func (c *classParser) addMember(i int, m string, off int) {
	txt, err := c.p.label(m, off)
	if err != nil {
		return
	}
	txt = generics(txt)
	if strings.Contains(m, "(") {
		c.d.Classes[i].Methods = append(c.d.Classes[i].Methods, txt)
		return
	}
	c.d.Classes[i].Attributes = append(c.d.Classes[i].Attributes, txt)
}

// annotation reads "<<interface>> Name".
func (c *classParser) annotation(t string, off int) {
	k := strings.Index(t, ">>")
	if k < 0 {
		c.p.fail(off, "the annotation is not closed")
		return
	}
	name := strings.TrimSpace(t[k+2:])
	if name == "" || classIDLen(name) != len(name) {
		c.p.fail(off+k+2, "an annotation wants one class name after it")
		return
	}
	i := c.mention(name, [2]int{off, off + len(t)})
	c.d.Classes[i].Annotations = append(c.d.Classes[i].Annotations, strings.TrimSpace(t[2:k]))
}

func (c *classParser) openNamespace(rest string, off int, st stmt) {
	if c.namespace != "" {
		c.p.unsupport(st.off, "a namespace in a namespace")
		return
	}
	name := strings.TrimSpace(strings.TrimSuffix(rest, "{"))
	if name == "" || !strings.HasSuffix(rest, "{") {
		c.p.fail(off, "namespace wants a name and {")
		return
	}
	c.namespace = name
	c.d.Namespaces = append(c.d.Namespaces, Namespace{Name: name, Span: [2]int{st.off, st.off + len(st.text)}})
}

// note reads `note "text"` and `note for Name "text"`; \n in the text breaks a line.
func (c *classParser) note(rest string, off int, st stmt) {
	n := ClassNote{Span: [2]int{st.off, st.off + len(st.text)}}
	t := rest
	if w, after := firstWord(rest); w == "for" {
		id, text := firstWord(after)
		if id == "" || classIDLen(id) != len(id) {
			c.p.fail(off, "note for wants a class")
			return
		}
		n.For, _ = splitID(id)
		c.mention(id, n.Span)
		t = text
	}
	if len(t) < 2 || t[0] != '"' || t[len(t)-1] != '"' {
		c.p.fail(off+len(rest)-len(t), "a note's text is quoted")
		return
	}
	txt, err := c.p.label(strings.ReplaceAll(t, `\n`, "\n"), off+len(rest)-len(t))
	if err != nil {
		return
	}
	n.Text = txt
	c.d.Notes = append(c.d.Notes, n)
}

// cssClass reads `cssClass "A,B" name`.
func (c *classParser) cssClass(rest string, off int) {
	if !strings.HasPrefix(rest, `"`) {
		c.p.fail(off, `cssClass wants "ids" and a class`)
		return
	}
	k := strings.IndexByte(rest[1:], '"')
	if k < 0 {
		c.p.fail(off, "the ids are not closed")
		return
	}
	css := strings.TrimSpace(rest[k+2:])
	if css == "" {
		c.p.fail(off+k+2, "cssClass wants a class name")
		return
	}
	for _, id := range strings.Split(rest[1:k+1], ",") {
		if id = strings.TrimSpace(id); id != "" {
			c.cssOf = append(c.cssOf, classUse{id, css})
		}
	}
}

// relationAt is where a relation's -- or .. starts in s, outside quotes and before any ':', or -1.
func relationAt(s string) int {
	quote := false
	for i := 0; i+1 < len(s); i++ {
		switch {
		case s[i] == '"':
			quote = !quote
		case quote:
		case strings.HasPrefix(s[i:], ":::"): // a css class on an end
			i += 2
		case s[i] == ':':
			return -1 // a member's text, not a relation
		case (s[i] == '-' && s[i+1] == '-') || (s[i] == '.' && s[i+1] == '.'):
			return i
		}
	}
	return -1
}

// relation reads `A "1" <|-- "*" B : label`.
func (c *classParser) relation(st stmt) {
	s := st.text
	at := relationAt(s)
	line := Solid
	if s[at] == '.' {
		line = Dotted
	}
	lo, hi := at, at+2
	left := RelNone
	switch l := s[:lo]; {
	case strings.HasSuffix(l, "()"):
		c.p.unsupport(st.off, "a lollipop interface")
		return
	case strings.HasSuffix(l, "<|"):
		left, lo = RelInheritance, lo-2
	case strings.HasSuffix(l, "*"):
		left, lo = RelComposition, lo-1
	case strings.HasSuffix(l, "<"):
		left, lo = RelAssociation, lo-1
	case strings.HasSuffix(l, " o"), strings.HasSuffix(l, `"o`):
		left, lo = RelAggregation, lo-1
	}
	right := RelNone
	switch r := s[hi:]; {
	case strings.HasPrefix(r, "()"):
		c.p.unsupport(st.off, "a lollipop interface")
		return
	case strings.HasPrefix(r, "|>"):
		right, hi = RelInheritance, hi+2
	case strings.HasPrefix(r, "*"):
		right, hi = RelComposition, hi+1
	case strings.HasPrefix(r, ">"):
		right, hi = RelAssociation, hi+1
	case strings.HasPrefix(r, "o "), strings.HasPrefix(r, `o"`):
		right, hi = RelAggregation, hi+1
	}
	rel := Relation{FromEnd: left, ToEnd: right, Line: line, Span: [2]int{st.off, st.off + len(s)}}

	lhs := strings.TrimSpace(s[:lo])
	if strings.HasSuffix(lhs, `"`) {
		k := strings.LastIndexByte(lhs[:len(lhs)-1], '"')
		if k < 0 {
			c.p.fail(st.off, "a cardinality is not closed")
			return
		}
		rel.FromCard = lhs[k+1 : len(lhs)-1]
		lhs = strings.TrimSpace(lhs[:k])
	}
	rhs := strings.TrimSpace(s[hi:])
	rhsOff := func() int { return st.off + len(s) - len(rhs) - (len(s[hi:]) - len(strings.TrimRight(s[hi:], " \t"))) }
	if strings.HasPrefix(rhs, `"`) {
		k := strings.IndexByte(rhs[1:], '"')
		if k < 0 {
			c.p.fail(rhsOff(), "a cardinality is not closed")
			return
		}
		rel.ToCard = rhs[1 : k+1]
		rhs = strings.TrimSpace(rhs[k+2:])
	}
	if k := strings.IndexByte(rhs, ':'); k >= 0 && !strings.HasPrefix(rhs[k:], ":::") {
		lbl, err := c.p.label(rhs[k+1:], rhsOff()+k+1)
		if err != nil {
			return
		}
		rel.Label = strings.TrimSpace(lbl)
		rhs = strings.TrimSpace(rhs[:k])
	}
	lhs, rhs = cssStrip(lhs), cssStrip(rhs)
	if lhs == "" || classIDLen(lhs) != len(lhs) {
		c.p.fail(st.off, "a relation wants a class before its arrow")
		return
	}
	if rhs == "" || classIDLen(rhs) != len(rhs) {
		c.p.fail(rhsOff(), "a relation wants a class after its arrow")
		return
	}
	rel.From = c.d.Classes[c.mention(lhs, rel.Span)].ID
	rel.To = c.d.Classes[c.mention(rhs, rel.Span)].ID
	c.d.Relations = append(c.d.Relations, rel)
}

// cssStrip drops a :::class from a written id; a relation's ends carry none in this subset.
func cssStrip(s string) string {
	if k := strings.Index(s, ":::"); k >= 0 {
		return s[:k]
	}
	return s
}

// resolve applies cssClass and :::, then the styles.
func (c *classParser) resolve() {
	for _, cu := range c.cssOf {
		if i, ok := c.index[cu.id]; ok {
			c.d.Classes[i].Classes = append(c.d.Classes[i].Classes, cu.class)
		}
	}
	for i := range c.d.Classes {
		k := &c.d.Classes[i]
		st := slices.Clone(c.d.Styles["default"])
		for _, cl := range k.Classes {
			st = merge(st, c.d.Styles[cl])
		}
		st = merge(st, c.styles[k.ID])
		if len(st) > 0 {
			k.Style = st
		}
	}
}
