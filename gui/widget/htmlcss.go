package widget

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/gui/internal/csscolor"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// THE CSS SUBSET — what HTMLView's pixel layout reads of a page's styles: the page's <style>
// elements, a host's stylesheet, and style="" attributes, over golib's own user-agent sheet.
//
// Selectors: type, .class, #id, :root, compounds of them (pre.mermaid), the descendant and child
// combinators, and lists (th,td); specificity as CSS orders it, then source order. Values: px, em,
// rem, ch, % and unitless line-heights; #rgb[a], #rrggbb[aa], rgb()/rgba() and named colours;
// custom properties and var() with a fallback. Properties: colour and font (with the font
// shorthand), line-height, text-align, text-decoration, white-space, display, margin, padding,
// border (solid, with its sides and its radius), background, width, max-width (margin: auto
// centres), border-collapse and overflow. Anything else is ignored, as a browser ignores an
// unknown property: never an error.

// cssLen is a length as written: resolved against a font size, the root's, or a width at layout.
type cssLen struct {
	v    float32
	unit byte     // 'p' px, 'e' em, 'r' rem, 'c' ch, '%', 'w' vw, 'h' vh, 'a' auto, 'x' an expression, 0 unset
	expr *cssExpr // calc(), min(), max(), clamp() (htmlcss_values.go)
}

func (l cssLen) set() bool  { return l.unit != 0 }
func (l cssLen) auto() bool { return l.unit == 'a' }

// px is l in pixels: em and ch against font, rem against root, % against of, vw and vh against
// the view.
func (l cssLen) px(font, root, of float32, view gui.Size) float32 {
	switch l.unit {
	case 'x':
		return l.expr.eval(font, root, of, view)
	case 'w':
		return l.v / 100 * view.W
	case 'h':
		return l.v / 100 * view.H
	case 'p':
		return l.v
	case 'e':
		return l.v * font
	case 'r':
		return l.v * root
	case 'c':
		return l.v * font * 0.55
	case '%':
		return l.v / 100 * of
	}
	return 0
}

type cssBorder struct {
	w     cssLen
	color color.NRGBA
	set   bool // a colour was given; else the text's (currentColor)
	none  bool
}

// computed is an element's style after the cascade and inheritance.
type computed struct {
	// inherited
	color      color.NRGBA
	fontSize   float32 // px
	bold       bool
	italic     bool
	mono       bool
	family     string  // font-family as written, a CSS list; "" the window's typeface
	transform  string  // text-transform: uppercase, lowercase, capitalize; "" none
	spacing    cssLen  // letter-spacing; unset is normal
	lineHeight float32 // a multiple of the font size
	align      flow.Align
	white      flow.WhiteSpace
	vars       map[string]string // custom properties, shared with the parent until one is set here
	ownVars    bool
	link       string // the enclosing <a>'s href

	// not inherited
	display        string
	background     color.NRGBA
	underline      bool
	strike         bool
	margin         [4]cssLen // top, right, bottom, left
	padding        [4]cssLen
	border         [4]cssBorder
	radius         cssLen
	width          cssLen
	maxWidth       cssLen
	collapse       bool // border-collapse: collapse
	overflowScroll bool
	borderBox      bool      // box-sizing: border-box: width and height hold padding and border
	height         cssLen    // height; unset or auto: its content's
	minWidth       cssLen    // min-width
	minHeight      cssLen    // min-height
	maxHeight      cssLen    // max-height
	position       string    // "", relative, absolute or fixed (fixed is laid out as absolute)
	inset          [4]cssLen // top, right, bottom, left of a positioned box
	objectFit      string    // an image's: contain, cover, fill (the default), none
	invisible      bool      // visibility: hidden; inherited, so a child can show again
	// flex and grid (htmlview_flex.go)
	flexDir    string    // "row" (the default) or "column"
	flexWrap   bool      // flex-wrap: wrap
	gap        [2]cssLen // row-gap, column-gap
	justify    string    // justify-content
	alignItems string    // align-items
	alignSelf  string    // align-self
	grow       float32   // flex-grow
	shrink     float32   // flex-shrink; 1 initially
	basis      cssLen    // flex-basis; unset or auto: the width, else the content's
	gridCols   string    // grid-template-columns, as written
}

// inherit is a child's starting style: what CSS inherits, the rest at its initial value.
func (c *computed) inherit() *computed {
	return &computed{
		color: c.color, fontSize: c.fontSize, bold: c.bold, italic: c.italic, mono: c.mono,
		family: c.family, transform: c.transform, spacing: c.spacing, invisible: c.invisible,
		lineHeight: c.lineHeight, align: c.align, white: c.white, vars: c.vars, link: c.link,
		display: "inline", shrink: 1,
		// text-decoration propagates to inline descendants as drawn decoration
		underline: c.underline, strike: c.strike,
	}
}

type cssDecl struct {
	prop, value string
	important   bool
}

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
	name, op, value string
	fold            bool // the i flag: the value is compared case-insensitively
}

type cssSelector struct {
	parts []cssCompound
	combs []byte // between parts: ' ' descendant, '>' child
	spec  int
}

type cssRule struct {
	sels  []cssSelector
	decls []cssDecl
	order int
}

// mediaEnv is what a media query is asked about: the view's size in pixels and its theme.
type mediaEnv struct {
	w, h float32
	dark bool
}

// parseCSS reads a stylesheet's rules: an @media block's when its query matches env; any other
// @-rule is skipped with its block.
func parseCSS(src string, order *int, env mediaEnv) []cssRule {
	src = stripComments(src)
	var rules []cssRule
	for i := 0; i < len(src); {
		open := strings.IndexByte(src[i:], '{')
		if open < 0 {
			break
		}
		head := strings.TrimSpace(src[i : i+open])
		body, next := block(src, i+open)
		i = next
		if strings.HasPrefix(head, "@") {
			if q, ok := strings.CutPrefix(strings.ToLower(head), "@media"); ok && mediaMatches(q, env) {
				rules = append(rules, parseCSS(body, order, env)...)
			}
			continue // @font-face, @supports and the like: a page that needs them still shows its text
		}
		var sels []cssSelector
		for _, s := range strings.Split(head, ",") {
			if sel, ok := parseSelector(strings.TrimSpace(s)); ok {
				sels = append(sels, sel)
			}
		}
		if len(sels) == 0 {
			continue
		}
		*order++
		rules = append(rules, cssRule{sels: sels, decls: parseDecls(body), order: *order})
	}
	return rules
}

// block is the text inside the braces opening at src[open], and the index past them.
func block(src string, open int) (string, int) {
	depth := 0
	for j := open; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : j], j + 1
			}
		}
	}
	return src[open+1:], len(src)
}

func stripComments(s string) string {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+2+j+2:]
	}
}

// parseDecls reads "prop: value; …" (a rule's body, or a style attribute).
func parseDecls(s string) []cssDecl {
	var out []cssDecl
	for _, d := range splitTop(s, ';') {
		prop, value, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		prop = strings.ToLower(strings.TrimSpace(prop))
		value = strings.TrimSpace(value)
		imp := false
		if i := strings.LastIndex(strings.ToLower(value), "!important"); i >= 0 {
			value, imp = strings.TrimSpace(value[:i]), true
		}
		if prop != "" && value != "" {
			out = append(out, cssDecl{prop: prop, value: value, important: imp})
		}
	}
	return out
}

// splitTop splits s at sep outside parentheses.
func splitTop(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
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
				for _, part := range splitTop(arg, ',') {
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
	a := cssAttr{op: "="}
	name := s[:eq]
	if eq > 0 && strings.ContainsRune("~|^$*", rune(s[eq-1])) {
		a.op = s[eq-1 : eq+1]
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
	case "":
		return true
	case "=":
		return have == want
	case "~=":
		for _, f := range strings.Fields(have) {
			if f == want {
				return true
			}
		}
		return false
	case "|=":
		return have == want || strings.HasPrefix(have, want+"-")
	case "^=":
		return want != "" && strings.HasPrefix(have, want)
	case "$=":
		return want != "" && strings.HasSuffix(have, want)
	case "*=":
		return want != "" && strings.Contains(have, want)
	}
	return false
}

// matches reports whether sel matches the last of chain, the element and its ancestors outermost
// first.
func (sel cssSelector) matches(chain []*phtml.Node) bool {
	return matchFrom(sel, len(sel.parts)-1, chain, len(chain)-1)
}

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

// uaCSS is golib's user-agent stylesheet, in the subset: what an unstyled page looks like.
const uaCSS = `
html,body,div,p,ul,ol,dl,dt,dd,pre,blockquote,figure,figcaption,section,article,aside,header,footer,nav,main,address,details,summary,h1,h2,h3,h4,h5,h6,hr,table,caption,center,fieldset,form,hgroup,menu,legend{display:block}
head,script,style,title,meta,link,template,noscript,base{display:none}
li{display:list-item}
table{display:table}
thead,tbody,tfoot{display:table-row-group}
tr{display:table-row}
td,th{display:table-cell;padding:2px 6px}
caption{text-align:center}
body{margin:8px}
p,ul,ol,dl,pre,blockquote,table,figure{margin:1em 0}
ul,ol,menu{padding-left:2em}
li ul,li ol{margin:0}
dd{margin-left:2em}
h1{font-size:2em;font-weight:bold;margin:.67em 0}
h2{font-size:1.5em;font-weight:bold;margin:.83em 0}
h3{font-size:1.17em;font-weight:bold;margin:1em 0}
h4{font-weight:bold;margin:1.33em 0}
h5{font-size:.83em;font-weight:bold;margin:1.67em 0}
h6{font-size:.67em;font-weight:bold;margin:2.33em 0}
b,strong,th,dt{font-weight:bold}
i,em,cite,dfn,var,address{font-style:italic}
pre,code,kbd,samp,tt{font-family:monospace}
pre{white-space:pre}
a,u,ins{text-decoration:underline}
s,del,strike{text-decoration:line-through}
blockquote{padding-left:1em;margin-left:0;margin-right:0}
hr{margin:.5em 0;border-top:1px solid}
.callout{margin:1em 0;padding:.5em 1em;border-left:4px solid}
.callout-title{font-weight:bold}
`

// parseColor reads a colour value; cur is currentColor's.
func parseColor(s string, cur color.NRGBA) (color.NRGBA, bool) { return csscolor.Parse(s, cur) }

// parseLen reads a length; allowAuto takes "auto".
func parseLen(s string) (cssLen, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "auto" {
		return cssLen{unit: 'a'}, true
	}
	if s == "0" {
		return cssLen{unit: 'p'}, true
	}
	if strings.HasSuffix(s, ")") {
		if e, ok := parseExpr(s); ok {
			return cssLen{unit: 'x', expr: e}, true
		}
		return cssLen{}, false
	}
	units := []struct {
		suf  string
		unit byte
	}{{"px", 'p'}, {"rem", 'r'}, {"em", 'e'}, {"ch", 'c'}, {"%", '%'}, {"vw", 'w'}, {"vh", 'h'}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suf) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suf), 32)
			if err != nil {
				return cssLen{}, false
			}
			return cssLen{v: float32(n), unit: u.unit}, true
		}
	}
	return cssLen{}, false
}

// substituteVars replaces var(--x[, fallback]) in v from vars, to a bounded depth.
func substituteVars(v string, vars map[string]string) string {
	for range 8 {
		i := strings.Index(v, "var(")
		if i < 0 {
			return v
		}
		depth, j := 0, i+3
		for ; j < len(v); j++ {
			if v[j] == '(' {
				depth++
			} else if v[j] == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if j >= len(v) {
			return v
		}
		inner := v[i+4 : j]
		name, fallback, _ := strings.Cut(inner, ",")
		val, ok := vars[strings.TrimSpace(name)]
		if !ok {
			val = strings.TrimSpace(fallback)
		}
		v = v[:i] + val + v[j+1:]
	}
	return v
}

// apply sets one declaration on c; parent is the inherited style (its font size for em).
func (c *computed) apply(d cssDecl, parent *computed, rootPx float32, view gui.Size) {
	if strings.HasPrefix(d.prop, "--") {
		if !c.ownVars {
			nv := make(map[string]string, len(c.vars)+1)
			for k, v := range c.vars {
				nv[k] = v
			}
			c.vars, c.ownVars = nv, true
		}
		c.vars[d.prop] = d.value
		return
	}
	v := substituteVars(d.value, c.vars)
	lv := strings.ToLower(strings.TrimSpace(v))
	switch d.prop {
	case "color":
		if col, ok := parseColor(v, parent.color); ok {
			c.color = col
		}
	case "background", "background-color":
		for _, f := range splitFields(v) {
			if col, ok := parseColor(f, c.color); ok {
				c.background = col
				break
			}
		}
		if lv == "none" {
			c.background = color.NRGBA{}
		}
	case "font-size":
		c.fontSize = fontSize(lv, parent.fontSize, rootPx, view)
	case "font-weight":
		c.bold = boldWeight(lv)
	case "font-style":
		c.italic = lv == "italic" || lv == "oblique"
	case "font-family":
		c.mono, c.family = monoFamily(lv), familyList(v)
	case "text-transform":
		switch lv {
		case "uppercase", "lowercase", "capitalize":
			c.transform = lv
		default:
			c.transform = ""
		}
	case "letter-spacing":
		if lv == "normal" {
			c.spacing = cssLen{}
		} else if l, ok := parseLen(lv); ok && !l.auto() && l.unit != '%' {
			c.spacing = l
		}
	case "font":
		c.font(lv, parent.fontSize, rootPx, view)
	case "line-height":
		c.lineHeight = lineHeight(lv, c.fontSize)
	case "text-align":
		switch lv {
		case "center":
			c.align = flow.Center
		case "right", "end":
			c.align = flow.End
		default:
			c.align = flow.Start
		}
	case "text-decoration", "text-decoration-line":
		c.underline = strings.Contains(lv, "underline")
		c.strike = strings.Contains(lv, "line-through")
	case "white-space":
		switch lv {
		case "pre", "nowrap":
			c.white = flow.Pre
		case "pre-wrap", "pre-line", "break-spaces":
			c.white = flow.PreWrap
		default:
			c.white = flow.Normal
		}
	case "display":
		c.display = lv
	case "margin":
		boxSides(lv, &c.margin)
	case "padding":
		boxSides(lv, &c.padding)
	case "margin-top", "margin-right", "margin-bottom", "margin-left":
		if l, ok := parseLen(lv); ok {
			c.margin[side(d.prop)] = l
		}
	case "padding-top", "padding-right", "padding-bottom", "padding-left":
		if l, ok := parseLen(lv); ok {
			c.padding[side(d.prop)] = l
		}
	case "border":
		b := parseBorder(lv, c.color)
		c.border = [4]cssBorder{b, b, b, b}
	case "border-top", "border-right", "border-bottom", "border-left":
		c.border[side(d.prop)] = parseBorder(lv, c.color)
	case "border-color":
		if col, ok := parseColor(lv, c.color); ok {
			for i := range c.border {
				c.border[i].color, c.border[i].set = col, true
			}
		}
	case "border-width":
		if l, ok := parseLen(lv); ok {
			for i := range c.border {
				c.border[i].w = l
			}
		}
	case "border-radius":
		if l, ok := parseLen(splitFields(lv + " 0")[0]); ok {
			c.radius = l
		}
	case "width":
		if l, ok := parseLen(lv); ok {
			c.width = l
		}
	case "max-width":
		if l, ok := parseLen(lv); ok {
			c.maxWidth = l
		}
	case "border-collapse":
		c.collapse = lv == "collapse"
	case "box-sizing":
		c.borderBox = lv == "border-box"
	case "height", "min-width", "min-height", "max-height":
		if l, ok := parseLen(lv); ok {
			switch d.prop {
			case "height":
				c.height = l
			case "min-width":
				c.minWidth = l
			case "min-height":
				c.minHeight = l
			default:
				c.maxHeight = l
			}
		} else if lv == "none" && d.prop == "max-height" {
			c.maxHeight = cssLen{}
		}
	case "position":
		switch lv {
		case "relative", "absolute":
			c.position = lv
		case "fixed":
			c.position = "absolute"
		default:
			c.position = ""
		}
	case "top", "right", "bottom", "left":
		if l, ok := parseLen(lv); ok {
			c.inset[side("-"+d.prop)] = l
		}
	case "inset":
		boxSides(lv, &c.inset)
	case "visibility":
		c.invisible = lv == "hidden" || lv == "collapse"
	case "object-fit":
		c.objectFit = lv
	case "flex-direction":
		c.flexDir = "row"
		if strings.HasPrefix(lv, "column") {
			c.flexDir = "column"
		}
	case "flex-wrap":
		c.flexWrap = strings.HasPrefix(lv, "wrap")
	case "flex-flow":
		for _, f := range strings.Fields(lv) {
			switch {
			case strings.HasPrefix(f, "column"):
				c.flexDir = "column"
			case strings.HasPrefix(f, "row"):
				c.flexDir = "row"
			case strings.HasPrefix(f, "wrap"):
				c.flexWrap = true
			case f == "nowrap":
				c.flexWrap = false
			}
		}
	case "gap", "grid-gap":
		f := splitTop(lv, ' ')
		var vals []cssLen
		for _, s := range f {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			if l, ok := parseLen(s); ok && !l.auto() {
				vals = append(vals, l)
			}
		}
		switch len(vals) {
		case 1:
			c.gap = [2]cssLen{vals[0], vals[0]}
		case 2:
			c.gap = [2]cssLen{vals[0], vals[1]}
		}
	case "row-gap", "grid-row-gap", "column-gap", "grid-column-gap":
		if l, ok := parseLen(lv); ok && !l.auto() {
			c.gap[map[bool]int{true: 0, false: 1}[strings.Contains(d.prop, "row")]] = l
		}
	case "justify-content":
		c.justify = lv
	case "align-items":
		c.alignItems = lv
	case "align-self":
		c.alignSelf = lv
	case "flex-grow", "flex-shrink":
		if f, err := strconv.ParseFloat(lv, 32); err == nil && f >= 0 {
			if d.prop == "flex-grow" {
				c.grow = float32(f)
			} else {
				c.shrink = float32(f)
			}
		}
	case "flex-basis":
		if l, ok := parseLen(lv); ok {
			c.basis = l
		}
	case "flex":
		c.flex(lv)
	case "grid-template-columns":
		c.gridCols = lv
	case "overflow", "overflow-x":
		c.overflowScroll = lv == "auto" || lv == "scroll"
	}
}

// side is the index of a -top/-right/-bottom/-left property's side.
func side(prop string) int {
	switch {
	case strings.HasSuffix(prop, "-top"):
		return 0
	case strings.HasSuffix(prop, "-right"):
		return 1
	case strings.HasSuffix(prop, "-bottom"):
		return 2
	}
	return 3
}

// boxSides reads a margin or padding shorthand of one to four values.
func boxSides(v string, out *[4]cssLen) {
	f := splitFields(v)
	var l []cssLen
	for _, s := range f {
		x, ok := parseLen(s)
		if !ok {
			return
		}
		l = append(l, x)
	}
	switch len(l) {
	case 1:
		*out = [4]cssLen{l[0], l[0], l[0], l[0]}
	case 2:
		*out = [4]cssLen{l[0], l[1], l[0], l[1]}
	case 3:
		*out = [4]cssLen{l[0], l[1], l[2], l[1]}
	case 4:
		*out = [4]cssLen{l[0], l[1], l[2], l[3]}
	}
}

// parseBorder reads a border shorthand: a width, a style (solid draws; none and hidden do not;
// any other is drawn solid), a colour.
func parseBorder(v string, cur color.NRGBA) cssBorder {
	b := cssBorder{w: cssLen{v: 3, unit: 'p'}} // medium
	styled := false
	for _, f := range splitFields(v) {
		switch f {
		case "none", "hidden":
			b.none = true
			styled = true
		case "solid", "dashed", "dotted", "double", "groove", "ridge", "inset", "outset":
			styled = true
		case "thin":
			b.w = cssLen{v: 1, unit: 'p'}
		case "medium":
			b.w = cssLen{v: 3, unit: 'p'}
		case "thick":
			b.w = cssLen{v: 5, unit: 'p'}
		default:
			if l, ok := parseLen(f); ok {
				b.w = l
			} else if c, ok := parseColor(f, cur); ok {
				b.color, b.set = c, true
			}
		}
	}
	if !styled {
		b.none = true // a border without a style draws nothing, as CSS's
	}
	return b
}

// splitFields splits on spaces outside parentheses (rgb(1, 2, 3) is one field).
func splitFields(v string) []string {
	v = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			return ' '
		}
		return r
	}, v)
	var out []string
	for _, f := range splitTop(v, ' ') {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func fontSize(v string, parent, root float32, view gui.Size) float32 {
	switch v {
	case "smaller":
		return parent * 0.83
	case "larger":
		return parent * 1.2
	case "small":
		return root * 0.89
	case "medium":
		return root
	case "large":
		return root * 1.2
	}
	if l, ok := parseLen(v); ok && !l.auto() {
		if l.unit == '%' {
			return l.v / 100 * parent
		}
		return l.px(parent, root, parent, view)
	}
	return parent
}

func lineHeight(v string, font float32) float32 {
	if v == "normal" {
		return 1.2
	}
	if n, err := strconv.ParseFloat(v, 32); err == nil {
		return float32(n)
	}
	if l, ok := parseLen(v); ok && font > 0 {
		if l.unit == '%' {
			return l.v / 100
		}
		return l.px(font, font, font, gui.Size{}) / font
	}
	return 1.2
}

// flex reads the flex shorthand: none, auto, initial, or grow [shrink] [basis].
func (c *computed) flex(v string) {
	switch v {
	case "none":
		c.grow, c.shrink, c.basis = 0, 0, cssLen{unit: 'a'}
		return
	case "auto":
		c.grow, c.shrink, c.basis = 1, 1, cssLen{unit: 'a'}
		return
	case "initial":
		c.grow, c.shrink, c.basis = 0, 1, cssLen{unit: 'a'}
		return
	}
	nums := 0
	c.basis = cssLen{unit: 'p'} // a bare number gives a basis of 0
	for _, f := range splitFields(v) {
		if n, err := strconv.ParseFloat(f, 32); err == nil && n >= 0 && nums < 2 {
			if nums == 0 {
				c.grow, c.shrink = float32(n), 1
			} else {
				c.shrink = float32(n)
			}
			nums++
			continue
		}
		if l, ok := parseLen(f); ok {
			c.basis = l
		}
	}
}

// boldWeight reports whether a font-weight is bold: bold, bolder, or a number from 600.
func boldWeight(v string) bool {
	if v == "bold" || v == "bolder" {
		return true
	}
	n, err := strconv.ParseFloat(v, 32)
	return err == nil && n >= 600
}

// familyList is a font-family list as the shaper takes it: its names, quotes dropped, and
// system-ui (which the shaper does not know) read as sans-serif.
func familyList(v string) string {
	var out []string
	for _, f := range strings.Split(v, ",") {
		f = strings.Trim(strings.TrimSpace(f), "\"'")
		switch strings.ToLower(f) {
		case "":
			continue
		case "system-ui", "-apple-system", "blinkmacsystemfont", "ui-sans-serif":
			f = "sans-serif"
		case "ui-serif":
			f = "serif"
		case "ui-monospace":
			f = "monospace"
		}
		out = append(out, f)
	}
	return strings.Join(out, ", ")
}

func monoFamily(v string) bool {
	return strings.Contains(v, "mono") || strings.Contains(v, "courier") || strings.Contains(v, "consolas")
}

// font reads the font shorthand: [style] [weight] size[/line-height] family.
func (c *computed) font(v string, parent, root float32, view gui.Size) {
	f := splitFields(v)
	c.italic, c.bold = false, false
	for i, tok := range f {
		switch tok {
		case "italic", "oblique":
			c.italic = true
			continue
		case "bold", "bolder":
			c.bold = true
			continue
		case "normal", "lighter", "small-caps":
			continue
		}
		if n, err := strconv.Atoi(tok); err == nil && n >= 1 && n <= 1000 {
			c.bold = n >= 600
			continue
		}
		size, lh, _ := strings.Cut(tok, "/")
		c.fontSize = fontSize(size, parent, root, view)
		if lh != "" {
			c.lineHeight = lineHeight(lh, c.fontSize)
		}
		c.mono = monoFamily(strings.Join(f[i+1:], " "))
		c.family = familyList(strings.Join(f[i+1:], " "))
		return
	}
}

// mediaMatches evaluates a media query list: true when any query in it matches env. A query is
// [not|only] [all|screen|print|…] and (feature: value) conditions; the features read are min- and
// max-width and -height, width, height, orientation and prefers-color-scheme. Any other feature,
// and print, never matches.
func mediaMatches(list string, env mediaEnv) bool {
	for _, q := range splitTop(list, ',') {
		if mediaQuery(strings.TrimSpace(q), env) {
			return true
		}
	}
	return false
}

func mediaQuery(q string, env mediaEnv) bool {
	if q == "" {
		return true
	}
	not := false
	if rest, ok := strings.CutPrefix(q, "not "); ok {
		not, q = true, rest
	} else if rest, ok := strings.CutPrefix(q, "only "); ok {
		q = rest
	}
	ok := true
	for i, part := range splitAnd(q) {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "("):
			ok = ok && mediaFeature(strings.Trim(part, "() "), env)
		case i == 0 && (part == "all" || part == "screen"):
		default:
			ok = false // print, or a type the view is not
		}
	}
	return ok != not
}

// splitAnd splits a query at its top-level "and"s.
func splitAnd(q string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(q); i++ {
		switch q[i] {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case 'a':
			if depth == 0 && strings.HasPrefix(q[i:], "and") && (i == 0 || q[i-1] == ' ') && (i+3 == len(q) || q[i+3] == ' ' || q[i+3] == '(') {
				out = append(out, q[start:i])
				start = i + 3
				i += 2
			}
		}
	}
	return append(out, q[start:])
}

// mediaFeature evaluates one "name: value" (or a bare name) against env.
func mediaFeature(f string, env mediaEnv) bool {
	name, value, _ := strings.Cut(f, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	px := func() (float32, bool) {
		l, ok := parseLen(value)
		if !ok || l.unit == '%' || l.unit == 'a' {
			return 0, false
		}
		return l.px(16, 16, 0, gui.Size{W: env.w, H: env.h}), true // em in a query is the initial font size
	}
	switch name {
	case "min-width", "max-width", "width", "min-height", "max-height", "height":
		v, ok := px()
		if !ok {
			return false
		}
		have := env.w
		if strings.HasSuffix(name, "height") {
			have = env.h
		}
		switch {
		case strings.HasPrefix(name, "min-"):
			return have >= v
		case strings.HasPrefix(name, "max-"):
			return have <= v
		}
		return have == v
	case "orientation":
		return value == "portrait" && env.h >= env.w || value == "landscape" && env.w > env.h
	case "prefers-color-scheme":
		return value == "dark" && env.dark || value == "light" && !env.dark
	}
	return false
}
