package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/gui/internal/css"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// SELECTORS — the subset HTMLView matches: their parse, their specificity, and their match
// against an element and its ancestors.

type cssCompound struct {
	tag     string
	id      string
	classes []string
	attrs   []cssAttr
	root    bool
	first   bool          // :first-child
	last    bool          // :last-child
	not     []cssCompound // :not(…), each must not match
	never   bool          // a state the view never has (:hover, :focus, …): the rule never applies
}

// cssAttr is an attribute selector: [name], or [name op value] with op one of = ~= |= ^= $= *=.
type cssAttr struct {
	name, value string
	op          attrOp
	fold        bool // the i flag: the value is compared case-insensitively
}

// attrOp is an attribute selector's comparison, as written between its name and its value.
type attrOp string

const (
	attrPresent   attrOp = ""   // [name]
	attrEquals    attrOp = "="  // [name=v]
	attrWord      attrOp = "~=" // a whitespace-separated word is v
	attrDashed    attrOp = "|=" // v, or v followed by "-"
	attrPrefix    attrOp = "^=" // starts with v
	attrSuffix    attrOp = "$=" // ends with v
	attrSubstring attrOp = "*=" // contains v
)

type cssSelector struct {
	parts []cssCompound
	combs []byte // between parts: ' ' descendant, '>' child
	spec  int
}

// parseSelector reads one selector of the subset: compounds joined by descendant and child
// combinators. A sibling combinator, or a pseudo-class or pseudo-element outside the subset,
// refuses the selector, so its rule matches nothing.
func parseSelector(s string) (cssSelector, bool) {
	var sel cssSelector
	toks, ok := selectorTokens(s)
	if !ok {
		return cssSelector{}, false
	}
	pendingChild := false
	for _, tok := range toks {
		switch tok {
		case ">":
			pendingChild = true
			continue
		case "+", "~":
			return cssSelector{}, false
		}
		c, ok := parseCompound(tok)
		if !ok {
			return cssSelector{}, false
		}
		if len(sel.parts) > 0 {
			if pendingChild {
				sel.combs = append(sel.combs, '>')
			} else {
				sel.combs = append(sel.combs, ' ')
			}
		}
		pendingChild = false
		sel.parts = append(sel.parts, c)
		sel.spec += c.specificity()
	}
	return sel, len(sel.parts) > 0 && !pendingChild
}

// specificity is a compound's: an id 100, a class, an attribute or a pseudo-class 10, a type 1;
// :not() counts what it holds.
func (c cssCompound) specificity() int {
	n := 10 * (len(c.classes) + len(c.attrs))
	if c.id != "" {
		n += 100
	}
	if c.root || c.first || c.last || c.never {
		n += 10
	}
	if c.tag != "" && c.tag != "*" {
		n++
	}
	for _, x := range c.not {
		n += x.specificity()
	}
	return n
}

// selectorTokens splits a selector into compounds and combinators (">", "+", "~"), keeping what is
// inside brackets, parentheses and quotes whole; false for an unbalanced one.
func selectorTokens(s string) ([]string, bool) {
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == '[' || c == '(':
			depth++
			cur.WriteByte(c)
		case c == ']' || c == ')':
			if depth == 0 {
				return nil, false
			}
			depth--
			cur.WriteByte(c)
		case depth > 0:
			cur.WriteByte(c)
		case c == '>' || c == '+' || c == '~':
			flush()
			toks = append(toks, string(c))
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	if depth != 0 || quote != 0 {
		return nil, false
	}
	flush()
	return toks, true
}

// neverPseudo are the states a page shown in the view is never in: a rule for them parses and never
// applies, where an unknown pseudo-class drops its selector.
var neverPseudo = map[string]bool{"hover": true, "focus": true, "focus-visible": true, "focus-within": true,
	"active": true, "visited": true, "target": true, "checked": true, "disabled": true}

// parseCompound reads tag, #id, .class, [attr…] and :pseudo parts.
func parseCompound(tok string) (cssCompound, bool) {
	var c cssCompound
	i := 0
	for i < len(tok) && !strings.ContainsRune(".#:[", rune(tok[i])) {
		i++
	}
	c.tag = strings.ToLower(tok[:i])
	for _, r := range c.tag {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '*') {
			return c, false
		}
	}
	for i < len(tok) {
		kind := tok[i]
		switch kind {
		case '[':
			end := strings.IndexByte(tok[i:], ']')
			if end < 0 {
				return c, false
			}
			a, ok := parseAttrSelector(tok[i+1 : i+end])
			if !ok {
				return c, false
			}
			c.attrs = append(c.attrs, a)
			i += end + 1
			continue
		case ':':
			if i+1 < len(tok) && tok[i+1] == ':' {
				return c, false // a pseudo-element: not drawn
			}
			j := i + 1
			for j < len(tok) && tok[j] != '.' && tok[j] != '#' && tok[j] != ':' && tok[j] != '[' && tok[j] != '(' {
				j++
			}
			name := strings.ToLower(tok[i+1 : j])
			arg := ""
			if j < len(tok) && tok[j] == '(' {
				end := matchingParen(tok, j)
				if end < 0 {
					return c, false
				}
				arg, j = tok[j+1:end], end+1
			}
			switch {
			case name == "root" && arg == "":
				c.root = true
			case name == "first-child" && arg == "":
				c.first = true
			case name == "last-child" && arg == "":
				c.last = true
			case name == "not":
				for _, part := range css.SplitTop(arg, ',') {
					x, ok := parseCompound(strings.TrimSpace(part))
					if !ok || strings.TrimSpace(part) == "" {
						return c, false
					}
					c.not = append(c.not, x)
				}
			case neverPseudo[name] && arg == "":
				c.never = true
			default:
				return c, false
			}
			i = j
			continue
		}
		j := i + 1
		for j < len(tok) && !strings.ContainsRune(".#:[", rune(tok[j])) {
			j++
		}
		name := tok[i+1 : j]
		if name == "" {
			return c, false
		}
		switch kind {
		case '.':
			c.classes = append(c.classes, name)
		case '#':
			c.id = name
		default:
			return c, false
		}
		i = j
	}
	return c, true
}

// matchingParen is the index of the ) closing the ( at open; -1 for none.
func matchingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseAttrSelector reads the inside of [...]: name, name=value, with ~ | ^ $ * before the =, a
// value quoted or bare, and an i flag.
func parseAttrSelector(s string) (cssAttr, bool) {
	s = strings.TrimSpace(s)
	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		name := strings.ToLower(s)
		return cssAttr{name: name}, name != "" && !strings.ContainsAny(name, " \t\"'")
	}
	a := cssAttr{op: attrEquals}
	name := s[:eq]
	if eq > 0 && strings.ContainsRune("~|^$*", rune(s[eq-1])) {
		a.op = attrOp(s[eq-1 : eq+1])
		name = s[:eq-1]
	}
	a.name = strings.ToLower(strings.TrimSpace(name))
	val := strings.TrimSpace(s[eq+1:])
	if strings.HasSuffix(strings.ToLower(val), " i") {
		val, a.fold = strings.TrimSpace(val[:len(val)-2]), true
	}
	if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
		val = val[1 : len(val)-1]
	}
	a.value = val
	return a, a.name != ""
}

// matchesCompound reports whether element n is c; parent is n's parent element (nil for the
// root), and root is whether n is the document's root element.
func matchesCompound(c cssCompound, n, parent *phtml.Node, root bool) bool {
	if c.never || c.root && !root {
		return false
	}
	if c.tag != "" && c.tag != "*" && c.tag != n.Name {
		return false
	}
	if c.id != "" {
		if id, _ := n.Attr("id"); id != c.id {
			return false
		}
	}
	if len(c.classes) > 0 {
		cls, _ := n.Attr("class")
		have := strings.Fields(cls)
		for _, want := range c.classes {
			found := false
			for _, h := range have {
				if h == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, a := range c.attrs {
		if !a.matches(n) {
			return false
		}
	}
	if c.first || c.last {
		if parent == nil {
			return false
		}
		var first, last *phtml.Node
		for _, k := range parent.Children {
			if k.Kind == phtml.StartTag || k.Kind == phtml.SelfClosing {
				if first == nil {
					first = k
				}
				last = k
			}
		}
		if c.first && first != n || c.last && last != n {
			return false
		}
	}
	for _, x := range c.not {
		if matchesCompound(x, n, parent, root) {
			return false
		}
	}
	return true
}

// matches reports whether element n has the attribute as a selects it.
func (a cssAttr) matches(n *phtml.Node) bool {
	var have string
	found := false
	for _, at := range n.Attrs {
		if strings.EqualFold(at.Name, a.name) {
			have, found = at.Value, true
			break
		}
	}
	if !found {
		return false
	}
	want := a.value
	if a.fold {
		have, want = strings.ToLower(have), strings.ToLower(want)
	}
	switch a.op {
	case attrPresent:
		return true
	case attrEquals:
		return have == want
	case attrWord:
		for _, f := range strings.Fields(have) {
			if f == want {
				return true
			}
		}
		return false
	case attrDashed:
		return have == want || strings.HasPrefix(have, want+"-")
	case attrPrefix:
		return want != "" && strings.HasPrefix(have, want)
	case attrSuffix:
		return want != "" && strings.HasSuffix(have, want)
	case attrSubstring:
		return want != "" && strings.Contains(have, want)
	}
	return false
}

// matches reports whether sel matches the last of chain, the element and its ancestors outermost
// first.
func (sel cssSelector) matches(chain []*phtml.Node) bool {
	return matchFrom(sel, len(sel.parts)-1, chain, len(chain)-1)
}

// matchFrom recursively matches selector parts against the node chain from right to left.
func matchFrom(sel cssSelector, part int, chain []*phtml.Node, at int) bool {
	if at < 0 {
		return false
	}
	var parent *phtml.Node
	if at > 0 {
		parent = chain[at-1]
	}
	if !matchesCompound(sel.parts[part], chain[at], parent, at == 0) {
		return false
	}
	if part == 0 {
		return true
	}
	if sel.combs[part-1] == '>' {
		return matchFrom(sel, part-1, chain, at-1)
	}
	for a := at - 1; a >= 0; a-- {
		if matchFrom(sel, part-1, chain, a) {
			return true
		}
	}
	return false
}
