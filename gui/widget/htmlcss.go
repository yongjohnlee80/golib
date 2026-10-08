package widget

import (
	"image/color"
	"strconv"
	"strings"

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
	unit byte // 'p' px, 'e' em, 'r' rem, 'c' ch, '%', 'a' auto, 0 unset
}

func (l cssLen) set() bool  { return l.unit != 0 }
func (l cssLen) auto() bool { return l.unit == 'a' }

// px is l in pixels: em and ch against font, rem against root, % against of.
func (l cssLen) px(font, root, of float32) float32 {
	switch l.unit {
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
}

// inherit is a child's starting style: what CSS inherits, the rest at its initial value.
func (c *computed) inherit() *computed {
	return &computed{
		color: c.color, fontSize: c.fontSize, bold: c.bold, italic: c.italic, mono: c.mono,
		lineHeight: c.lineHeight, align: c.align, white: c.white, vars: c.vars, link: c.link,
		display: "inline",
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
	root    bool
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

// parseCSS reads a stylesheet's rules; @-rules are skipped with their blocks.
func parseCSS(src string, order *int) []cssRule {
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
			continue // @media and the like: a page that needs them still shows its text
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

// parseSelector reads one selector of the subset; any other (attributes, pseudo-classes other
// than :root, siblings) is refused, so its rule matches nothing.
func parseSelector(s string) (cssSelector, bool) {
	var sel cssSelector
	s = strings.ReplaceAll(s, ">", " > ")
	pendingChild := false
	for _, tok := range strings.Fields(s) {
		if tok == ">" {
			pendingChild = true
			continue
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
		if c.id != "" {
			sel.spec += 100
		}
		sel.spec += 10 * len(c.classes)
		if c.root {
			sel.spec += 10
		}
		if c.tag != "" && c.tag != "*" {
			sel.spec++
		}
	}
	return sel, len(sel.parts) > 0 && !pendingChild
}

func parseCompound(tok string) (cssCompound, bool) {
	var c cssCompound
	i := 0
	for i < len(tok) && tok[i] != '.' && tok[i] != '#' && tok[i] != ':' {
		i++
	}
	c.tag = strings.ToLower(tok[:i])
	for i < len(tok) {
		kind := tok[i]
		j := i + 1
		for j < len(tok) && tok[j] != '.' && tok[j] != '#' && tok[j] != ':' {
			j++
		}
		name := tok[i+1 : j]
		switch kind {
		case '.':
			c.classes = append(c.classes, name)
		case '#':
			c.id = name
		case ':':
			if strings.ToLower(name) != "root" {
				return c, false
			}
			c.root = true
		}
		i = j
	}
	for _, r := range c.tag {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '*') {
			return c, false
		}
	}
	return c, true
}

// matchesCompound reports whether element n is c; root is whether n is the document's root
// element.
func matchesCompound(c cssCompound, n *phtml.Node, root bool) bool {
	if c.root && !root {
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
	return true
}

// matches reports whether sel matches the last of chain, the element and its ancestors outermost
// first.
func (sel cssSelector) matches(chain []*phtml.Node) bool {
	return matchFrom(sel, len(sel.parts)-1, chain, len(chain)-1)
}

func matchFrom(sel cssSelector, part int, chain []*phtml.Node, at int) bool {
	if at < 0 || !matchesCompound(sel.parts[part], chain[at], at == 0) {
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
	units := []struct {
		suf  string
		unit byte
	}{{"px", 'p'}, {"rem", 'r'}, {"em", 'e'}, {"ch", 'c'}, {"%", '%'}}
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
func (c *computed) apply(d cssDecl, parent *computed, rootPx float32) {
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
		for _, f := range strings.Fields(v) {
			if col, ok := parseColor(f, c.color); ok {
				c.background = col
				break
			}
		}
		if lv == "none" {
			c.background = color.NRGBA{}
		}
	case "font-size":
		c.fontSize = fontSize(lv, parent.fontSize, rootPx)
	case "font-weight":
		c.bold = lv == "bold" || lv == "bolder" || (len(lv) == 3 && lv >= "600")
	case "font-style":
		c.italic = lv == "italic" || lv == "oblique"
	case "font-family":
		c.mono = monoFamily(lv)
	case "font":
		c.font(lv, parent.fontSize, rootPx)
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
		if l, ok := parseLen(strings.Fields(lv + " 0")[0]); ok {
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
	f := strings.Fields(v)
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
	var out []string
	for _, f := range splitTop(v, ' ') {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func fontSize(v string, parent, root float32) float32 {
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
		return l.px(parent, root, parent)
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
		return l.px(font, font, font) / font
	}
	return 1.2
}

func monoFamily(v string) bool {
	return strings.Contains(v, "mono") || strings.Contains(v, "courier") || strings.Contains(v, "consolas")
}

// font reads the font shorthand: [style] [weight] size[/line-height] family.
func (c *computed) font(v string, parent, root float32) {
	f := strings.Fields(v)
	c.italic, c.bold = false, false
	for i, tok := range f {
		switch tok {
		case "italic", "oblique":
			c.italic = true
			continue
		case "bold", "bolder", "600", "700", "800", "900":
			c.bold = true
			continue
		case "normal", "400", "lighter", "300":
			continue
		}
		size, lh, _ := strings.Cut(tok, "/")
		c.fontSize = fontSize(size, parent, root)
		if lh != "" {
			c.lineHeight = lineHeight(lh, c.fontSize)
		}
		c.mono = monoFamily(strings.Join(f[i+1:], " "))
		return
	}
}
