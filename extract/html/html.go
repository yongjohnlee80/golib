// Package html extracts a web page's content as Markdown, as an extract.Extractor: headings,
// paragraphs, lists, tables, code and the text of links and images, without tags, scripts, styles
// or the page's chrome.
//
// A page with a main element is read as that element alone; else its first article; else its
// body without nav, header, footer and aside. Scripts, styles, noscript, templates, SVG, canvases,
// frames, objects, forms, comments and anything hidden (the hidden attribute, aria-hidden="true")
// are dropped with what they hold, and their text never reaches the writer. Link targets are
// dropped: an embedding carries meaning, not URLs.
//
// It parses with parse/html under its limits; a page over them is refused with ErrTooLarge.
package html

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/extract"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// ErrTooLarge is a page over a parse limit: its source, its node count, a tag's attributes.
var ErrTooLarge = phtml.ErrTooLarge

// Extractor reads HTML. Its zero value parses under parse/html's DefaultLimits.
type Extractor struct {
	Limits phtml.Limits
}

// ID names this extractor for a derived cache's key.
func (Extractor) ID() string { return "golib/html" }

// Version is the version of its output; a change re-derives every page.
func (Extractor) Version() string { return "1" }

// Extract writes the page at r, of size bytes, to w as Markdown. Info.Title is the page's title
// element, else its first h1.
func (e Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (extract.Info, error) {
	lim := e.Limits
	if lim.MaxSource <= 0 {
		lim.MaxSource = phtml.DefaultLimits.MaxSource
	}
	switch {
	case size < 0:
		return extract.Info{}, errs.Wrap(errs.ErrInvalidArgument, "extract html: negative size %d", size)
	case size > int64(lim.MaxSource):
		return extract.Info{}, fmt.Errorf("extract html: %d bytes, limit %d: %w", size, lim.MaxSource, ErrTooLarge)
	}
	src := make([]byte, size)
	if n, err := r.ReadAt(src, 0); n < len(src) {
		if err == nil || err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return extract.Info{}, fmt.Errorf("extract html: read %d of %d bytes: %w", n, size, err)
	}
	doc, err := phtml.ParseLimited(ctx, src, lim)
	if err != nil {
		return extract.Info{}, fmt.Errorf("extract html: %w", err)
	}
	p := &page{memo: map[*phtml.Node]bool{}}
	root, chrome := p.content(doc)
	// what is dropped changes with the chrome, so the answers kept so far go with it
	p.chrome, p.memo = chrome, map[*phtml.Node]bool{}
	var blocks []string
	p.blocks(root, &blocks)
	info := extract.Info{Title: p.title(doc)}
	if info.Title == "" {
		info.Title = p.firstHeading(root)
	}
	for i, b := range blocks {
		if err := ctx.Err(); err != nil {
			return extract.Info{}, err
		}
		if i > 0 {
			b = "\n" + b
		}
		if _, err := io.WriteString(w, b+"\n"); err != nil {
			return extract.Info{}, err
		}
	}
	return info, nil
}

// dropped are the elements read for nothing, with everything they hold.
var dropped = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true, "canvas": true,
	"iframe": true, "object": true, "form": true, "head": true, "title": true,
}

// chromeNames are the page furniture a body without main or article is read without.
var chromeNames = map[string]bool{"nav": true, "header": true, "footer": true, "aside": true}

// block elements end the inline run before them and start a block of their own.
var block = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "body": true, "caption": true,
	"center": true, "dd": true, "details": true, "dialog": true, "div": true, "dl": true, "dt": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
	"html": true, "li": true, "main": true, "menu": true, "nav": true, "ol": true, "p": true,
	"pre": true, "section": true, "summary": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
}

type page struct {
	chrome bool                 // drop nav, header, footer, aside: the body is the content
	memo   map[*phtml.Node]bool // a flattened encloser: whether it or one outside it is dropped
}

// self reports whether n alone is dropped: its name, hidden, or chrome when chrome is dropped.
func (p *page) self(n *phtml.Node) bool {
	if n.Kind != phtml.StartTag {
		return false
	}
	if dropped[n.Name] || p.chrome && chromeNames[n.Name] {
		return true
	}
	if _, ok := n.Attr("hidden"); ok {
		return true
	}
	v, _ := n.Attr("aria-hidden")
	return strings.EqualFold(strings.TrimSpace(v), "true")
}

// gone reports whether n is dropped by itself or by an element that encloses it past the parse's
// depth limit. Each encloser's answer is kept, so the chains, which share their tails, are walked
// once in all.
func (p *page) gone(n *phtml.Node) bool {
	if p.self(n) {
		return true
	}
	var walked []*phtml.Node
	outer := false
	for f := range n.Flattened() {
		if v, ok := p.memo[f]; ok {
			outer = v
			break
		}
		walked = append(walked, f)
	}
	for i := len(walked) - 1; i >= 0; i-- {
		outer = outer || p.self(walked[i])
		p.memo[walked[i]] = outer
	}
	return outer
}

// Dropped reports whether a reader of a page skips n with everything it holds: a script, a style
// or one of the other elements read for nothing, or anything hidden (the hidden attribute,
// aria-hidden="true"), itself or through an element that encloses it past the parse's depth limit.
// A page's chrome (nav, header, footer, aside) is not dropped here: that is the extractor's choice
// for a body with no main, not a view's. tui/widget's HTMLView draws pages by this rule.
func Dropped(n *phtml.Node) bool {
	p := page{memo: map[*phtml.Node]bool{}}
	return p.gone(n)
}

// Block reports whether an element named name (lower-cased) starts a block of its own, ending the
// inline run before it.
func Block(name string) bool { return block[name] }

// content is the part of the page to read: the first main, else the first article, else the
// body (or the whole document) with its chrome dropped.
func (p *page) content(doc *phtml.Node) (*phtml.Node, bool) {
	var article, body *phtml.Node
	stack := []*phtml.Node{doc}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind != phtml.StartTag || p.gone(n) {
			continue
		}
		switch n.Name {
		case "main":
			return n, false
		case "article":
			if article == nil {
				article = n
			}
		case "body":
			if body == nil {
				body = n
			}
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.Children[i])
		}
	}
	switch {
	case article != nil:
		return article, false
	case body != nil:
		return body, true
	}
	return doc, true
}

// title is the first title element's text, anywhere in the page.
func (p *page) title(doc *phtml.Node) string {
	stack := []*phtml.Node{doc}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == phtml.StartTag && n.Name == "title" {
			return collapse(rawText(n))
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.Children[i])
		}
	}
	return ""
}

// firstHeading is the text of the first h1 in what is read.
func (p *page) firstHeading(root *phtml.Node) string {
	stack := []*phtml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind != phtml.StartTag || p.gone(n) {
			continue
		}
		if n.Name == "h1" {
			var in inline
			p.inline(n, &in)
			return in.text()
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.Children[i])
		}
	}
	return ""
}

// blocks appends n's content as Markdown blocks: runs of inline content become paragraphs, and
// block children their own blocks.
func (p *page) blocks(n *phtml.Node, out *[]string) {
	var in inline
	flush := func() {
		if t := in.text(); t != "" {
			*out = append(*out, t)
		}
		in = inline{}
	}
	for _, c := range n.Children {
		if p.gone(c) {
			continue
		}
		if c.Kind != phtml.StartTag || !block[c.Name] {
			p.inline(c, &in)
			continue
		}
		flush()
		p.block(c, out)
	}
	flush()
}

// block appends the block element n.
func (p *page) block(n *phtml.Node, out *[]string) {
	switch n.Name {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		var in inline
		p.inline(n, &in)
		if t := in.text(); t != "" {
			*out = append(*out, strings.Repeat("#", int(n.Name[1]-'0'))+" "+t)
		}
	case "hr":
		*out = append(*out, "---")
	case "pre":
		*out = append(*out, fence(p.preText(n), language(n)))
	case "ul", "ol", "menu":
		if l := p.list(n); l != "" {
			*out = append(*out, l)
		}
	case "table":
		if t := p.table(n); t != "" {
			*out = append(*out, t)
		}
	case "blockquote":
		var inner []string
		p.blocks(n, &inner)
		if len(inner) > 0 {
			*out = append(*out, prefixLines(strings.Join(inner, "\n\n"), "> ", "> "))
		}
	default:
		p.blocks(n, out)
	}
}

// list renders a list's items, nested lists indented under their item.
func (p *page) list(n *phtml.Node) string {
	ordered := n.Name == "ol"
	num := 1
	if v, ok := n.Attr("start"); ok {
		if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			num = s
		}
	}
	var items []string
	for _, li := range n.Children {
		if li.Kind != phtml.StartTag || p.gone(li) {
			continue
		}
		var body []string
		if li.Name == "li" {
			p.blocks(li, &body)
		} else {
			p.block(li, &body)
		}
		marker := "- "
		if ordered {
			marker = strconv.Itoa(num) + ". "
			num++
		}
		text := strings.Join(body, "\n")
		if text == "" {
			continue
		}
		items = append(items, prefixLines(text, marker, strings.Repeat(" ", len(marker))))
	}
	return strings.Join(items, "\n")
}

// table renders a GFM table: its first row is the header, short rows are padded.
func (p *page) table(n *phtml.Node) string {
	var rows [][]string
	var walk func(n *phtml.Node)
	walk = func(n *phtml.Node) {
		for _, c := range n.Children {
			if c.Kind != phtml.StartTag || p.gone(c) {
				continue
			}
			switch c.Name {
			case "thead", "tbody", "tfoot":
				walk(c)
			case "tr":
				var row []string
				for _, cell := range c.Children {
					if cell.Kind != phtml.StartTag || p.gone(cell) || cell.Name != "td" && cell.Name != "th" {
						continue
					}
					var in inline
					p.inline(cell, &in)
					row = append(row, strings.ReplaceAll(in.text(), "|", `\|`))
				}
				if len(row) > 0 {
					rows = append(rows, row)
				}
			}
		}
	}
	walk(n)
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	var b strings.Builder
	line := func(cells []string) {
		b.WriteString("|")
		for i := range cols {
			c := ""
			if i < len(cells) {
				c = cells[i]
			}
			b.WriteString(" " + c + " |")
		}
		b.WriteString("\n")
	}
	line(rows[0])
	line(slicesRepeat("---", cols))
	for _, r := range rows[1:] {
		line(r)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func slicesRepeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// preText is a pre element's text as written, less one leading newline, as HTML drops it.
func (p *page) preText(n *phtml.Node) string {
	var b strings.Builder
	stack := []*phtml.Node{n}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if c != n && p.gone(c) {
			continue
		}
		if c.Kind == phtml.Text {
			b.WriteString(c.Data)
		}
		for i := len(c.Children) - 1; i >= 0; i-- {
			stack = append(stack, c.Children[i])
		}
	}
	s := strings.TrimPrefix(b.String(), "\n")
	return strings.TrimRight(s, "\n")
}

// language is a code block's language: "language-x" (or "lang-x") on the pre or its code.
func language(n *phtml.Node) string {
	nodes := []*phtml.Node{n}
	for _, c := range n.Children {
		if c.Kind == phtml.StartTag && c.Name == "code" {
			nodes = append(nodes, c)
		}
	}
	for _, x := range nodes {
		cls, _ := x.Attr("class")
		for _, f := range strings.Fields(cls) {
			for _, pre := range []string{"language-", "lang-"} {
				if l, ok := strings.CutPrefix(f, pre); ok && l != "" {
					return l
				}
			}
		}
	}
	return ""
}

// fence is code in a fenced block, its fence longer than any backtick run inside it.
func fence(code, lang string) string {
	f := "```"
	for strings.Contains(code, f) {
		f += "`"
	}
	return f + lang + "\n" + code + "\n" + f
}

// collapse is s with its whitespace runs as single spaces, trimmed.
func collapse(s string) string { return strings.Join(strings.FieldsFunc(s, isSpace), " ") }

// rawText is n's text children as written.
func rawText(n *phtml.Node) string {
	var b strings.Builder
	for _, c := range n.Children {
		if c.Kind == phtml.Text {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// inline is a paragraph's text as it is built: HTML's whitespace collapsed to single spaces.
type inline struct {
	b     strings.Builder
	space bool // a space is owed before the next word
}

// add writes s with HTML's whitespace rules: runs collapse, and a space between two pieces
// survives only when one of them had it.
func (in *inline) add(s string) {
	if s == "" {
		return
	}
	if isSpace(rune(s[0])) {
		in.space = true
	}
	fields := strings.FieldsFunc(s, isSpace)
	for i, f := range fields {
		if (in.space || i > 0) && in.b.Len() > 0 {
			in.b.WriteByte(' ')
		}
		in.b.WriteString(f)
		in.space = false
	}
	if isSpace(rune(s[len(s)-1])) {
		in.space = true
	}
}

// raw appends s as it is, owing any pending space first.
func (in *inline) raw(s string) {
	if in.space && in.b.Len() > 0 {
		in.b.WriteByte(' ')
	}
	in.b.WriteString(s)
	in.space = false
}

func (in *inline) text() string { return strings.TrimSpace(in.b.String()) }

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }

// inline appends n's inline content: text, emphasis, code spans, link text and image alts. A block
// element met inside inline content contributes its text, set off by spaces.
func (p *page) inline(n *phtml.Node, in *inline) {
	if p.gone(n) {
		return
	}
	switch n.Kind {
	case phtml.Text:
		in.add(n.Data)
		return
	case phtml.StartTag:
	default:
		return
	}
	switch n.Name {
	case "br":
		in.raw("\n")
		in.space = false
		return
	case "img":
		if alt, ok := n.Attr("alt"); ok {
			in.add(" " + alt + " ")
		}
		return
	case "strong", "b":
		p.marked(n, in, "**")
		return
	case "em", "i":
		p.marked(n, in, "*")
		return
	case "code", "kbd", "samp":
		p.marked(n, in, "`")
		return
	}
	if block[n.Name] {
		in.space = true
	}
	for _, c := range n.Children {
		p.inline(c, in)
	}
	if block[n.Name] {
		in.space = true
	}
}

// marked wraps n's inline content in mark, keeping the spaces around it outside the marks.
func (p *page) marked(n *phtml.Node, in *inline, mark string) {
	var inner inline
	for _, c := range n.Children {
		p.inline(c, &inner)
	}
	s := inner.b.String()
	core := strings.TrimSpace(s)
	if core == "" {
		if s != "" {
			in.space = true
		}
		return
	}
	if mark == "`" && strings.Contains(core, "`") {
		mark = "``"
		core = " " + core + " "
	}
	if s != "" && isSpace(rune(s[0])) {
		in.space = true
	}
	in.raw(mark + core + mark)
	if isSpace(rune(s[len(s)-1])) || inner.space {
		in.space = true
	}
}

// prefixLines prefixes s's first line with first and every later line with rest.
func prefixLines(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		pre := rest
		if i == 0 {
			pre = first
		}
		if l == "" && i > 0 {
			lines[i] = strings.TrimRight(pre, " ")
			continue
		}
		lines[i] = pre + l
	}
	return strings.Join(lines, "\n")
}
