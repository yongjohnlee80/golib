package markdown

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// The kinds the Obsidian syntax adds. A callout is a KindBlockQuote with Callout set, whose first
// child is its KindCalloutTitle.
//
// https://help.obsidian.md/obsidian-flavored-markdown
const (
	KindFrontmatter Kind = KindExtension + 0x20 + iota
	KindWikilink
	KindEmbed
	KindTag
	KindCalloutTitle
)

// Target is where a wikilink or an embed points, as written: [[Page#Heading|Alias]], or
// [[Page#^Block]]. Page is empty for a link within the same note. Heading keeps any nested
// "#Sub" parts. For an embed, Alias may be a size ("100x145"); what it means is the consumer's.
type Target struct {
	Page, Heading, Block, Alias []byte
}

// Callout is a block quote's callout marker: > [!Type]+ or > [!Type]-. Types compare
// case-insensitively; Fold is '+' (expanded), '-' (collapsed) or 0 (not foldable).
type Callout struct {
	Type []byte
	Fold byte
}

// ObsidianExtension describes the Obsidian syntax: frontmatter, wikilinks, embeds, tags and
// callouts. Its rules are read from Obsidian's help pages, which describe rather than specify;
// where they leave a case open, the rule cases pin the reading taken.
func ObsidianExtension() Extension {
	return Extension{Name: "obsidian", Recognizes: obsidianRecognizes, enable: func(c *config) { c.obsidian = true }}
}

// Obsidian enables the Obsidian syntax.
func Obsidian() Option { return ObsidianExtension().Option() }

// obsidianRecognizes is true when src starts with "---" (frontmatter) or has "[[" (a wikilink or
// an embed), "[!" (a callout) or a '#' followed by a byte a tag may hold.
func obsidianRecognizes(src []byte) bool {
	if bytes.HasPrefix(src, []byte("---")) || bytes.Contains(src, []byte("[[")) || bytes.Contains(src, []byte("[!")) {
		return true
	}
	for i := bytes.IndexByte(src, '#'); i >= 0 && i+1 < len(src); {
		if c := src[i+1]; c >= 0x80 || isASCIIAlnum(c) || c == '_' || c == '-' || c == '/' {
			return true
		}
		j := bytes.IndexByte(src[i+1:], '#')
		if j < 0 {
			break
		}
		i += 1 + j
	}
	return false
}

// frontmatter reads a frontmatter block at the start of the source: a line of "---", any lines,
// and a closing line of "---" (trailing spaces or tabs allowed on both). Without a closing line
// there is none, and the lines are ordinary Markdown. It returns where the Markdown begins.
//
// https://help.obsidian.md/properties
func (p *blockParser) frontmatter() int {
	src := p.src
	lineEnd := func(s int) (end, next int) {
		end = s
		for end < len(src) && !isLineEnd(src[end]) {
			end++
		}
		next = end
		if next < len(src) {
			next++
			if src[end] == '\r' && next < len(src) && src[next] == '\n' {
				next++
			}
		}
		return end, next
	}
	fence := func(s, e int) bool { return string(bytes.TrimRight(src[s:e], " \t")) == "---" }
	e, body := lineEnd(0)
	if !fence(0, e) {
		return 0
	}
	for s := body; s < len(src); {
		e, next := lineEnd(s)
		if fence(s, e) {
			fm := &Node{Kind: KindFrontmatter, Span: Span{0, e}, Literal: src[body:s:s], blk: &blockState{}}
			p.root.AppendChild(fm)
			return next
		}
		s = next
	}
	return 0
}

// calloutStart makes the block quote just opened a callout when its first line starts with
// "[!type]", optionally followed by '+' or '-'. The rest of that line is the callout's title,
// kept apart from the quote's body.
//
// https://help.obsidian.md/callouts
func (p *blockParser) calloutStart(bq *Node) (startResult, *Node, bool) {
	rest := p.text[p.offset:]
	if p.partiallyConsumedTab || len(rest) < 4 || rest[0] != '[' || rest[1] != '!' {
		return startNone, nil, false
	}
	k := 2
	for k < len(rest) && rest[k] != ']' && rest[k] > ' ' && rest[k] != 0x7f {
		k++
	}
	if k == 2 || k >= len(rest) || rest[k] != ']' {
		return startNone, nil, false
	}
	c := &Callout{Type: append([]byte(nil), rest[2:k]...)}
	n := k + 1
	if n < len(rest) && (rest[n] == '+' || rest[n] == '-') {
		c.Fold = rest[n]
		n++
	}
	bq.Callout = c
	p.advanceOffset(n, false)
	p.findNextNonspace()
	p.advanceNextNonspace()
	title := p.addChild(KindCalloutTitle, p.offset)
	content := p.text[p.offset:]
	title.blk.content = append([]byte(nil), content...)
	title.blk.segs = []seg{{at: 0, src: p.ln.start + p.offset, n: len(content)}}
	p.finalize(title, p.ln.end)
	p.offset = len(p.text)
	return startLeaf, title, true
}

// obsidianInline handles the bytes the Obsidian syntax adds as inline triggers, reporting whether
// it consumed input.
func (p *inlineParser) obsidianInline(c byte) bool {
	switch c {
	case '[':
		return p.wikilink(false)
	case '!':
		return p.wikilink(true)
	case '#':
		return p.tag()
	}
	return false
}

// wikilink reads [[target|alias]], or ![[…]] as an embed, on one line, unless its content is blank
// or holds another "[[". A wikilink is a link, so as with an inline link the innermost one wins:
// no '[' before it can open a link around it. An embed, like an image, leaves them open.
//
// https://help.obsidian.md/links
func (p *inlineParser) wikilink(embed bool) bool {
	start, open := p.pos, p.pos+2
	lead := "[["
	if embed {
		open, lead = p.pos+3, "![["
	}
	if !bytes.HasPrefix(p.text[start:], []byte(lead)) {
		return false
	}
	// each search reuses the last one's answer while it lies ahead, so a line of unclosed "[["
	// is read once, not once per opener
	closeAt := p.wikiClose.next(p.text, open, "]]")
	if closeAt < 0 {
		return false
	}
	if nl := p.wikiLine.next(p.text, open, "\n"); nl >= 0 && nl < closeAt {
		return false
	}
	if again := p.wikiOpen.next(p.text, open, "[["); again >= 0 && again < closeAt {
		return false
	}
	inner := p.text[open:closeAt]
	if len(bytes.TrimSpace(inner)) == 0 {
		return false
	}
	t := &Target{}
	target, display := inner, [2]int{open, closeAt}
	if i := bytes.IndexByte(inner, '|'); i >= 0 {
		target, t.Alias = inner[:i], clone(inner[i+1:])
		if len(bytes.TrimSpace(t.Alias)) > 0 {
			display = [2]int{open + i + 1, closeAt}
		} else {
			display = [2]int{open, open + i}
		}
	}
	t.Page = target
	if i := bytes.IndexByte(target, '#'); i >= 0 {
		t.Page, t.Heading = target[:i], target[i+1:]
		if j := bytes.Index(t.Heading, []byte("^")); j >= 0 && (j == 0 || t.Heading[j-1] == '#') {
			t.Block = t.Heading[j+1:]
			t.Heading = bytes.TrimSuffix(t.Heading[:j], []byte("#"))
		}
	}
	t.Page, t.Heading, t.Block = clone(t.Page), clone(t.Heading), clone(t.Block)
	kind := KindWikilink
	if embed {
		kind = KindEmbed
	}
	n := &Node{Kind: kind, Span: Span{p.srcAt(start), p.srcAt(closeAt + 2)}, Target: t}
	n.AppendChild(&Node{Kind: KindText, Span: Span{p.srcAt(display[0]), p.srcAt(display[1])}})
	p.parent.AppendChild(n)
	p.pos = closeAt + 2
	if !embed {
		for b := p.brackets; b != nil; b = b.prev {
			if !b.image {
				b.active = false // links may not contain links
			}
		}
	}
	return true
}

// tag reads #name at the start of a line or after whitespace. A name holds letters, digits, '_',
// '-', '/' and other non-punctuation characters such as emoji, and at least one non-digit.
//
// https://help.obsidian.md/tags
func (p *inlineParser) tag() bool {
	start := p.pos
	if start > 0 && !isUnicodeWhitespace(runeBefore(p.text, start)) {
		return false
	}
	j, digitsOnly := start+1, true
	for j < len(p.text) {
		r, size := utf8.DecodeRune(p.text[j:])
		if !tagRune(r) {
			break
		}
		if r < '0' || r > '9' {
			digitsOnly = false
		}
		j += size
	}
	if j == start+1 || digitsOnly {
		return false
	}
	n := &Node{Kind: KindTag, Span: Span{p.srcAt(start), p.srcAt(j)}, Label: clone(p.text[start+1 : j])}
	n.AppendChild(&Node{Kind: KindText, Span: n.Span})
	p.parent.AppendChild(n)
	p.pos = j
	return true
}

// nextIndex finds the next occurrence of a pattern at or after a position. The positions asked for
// only move forward, so the previous answer is reused while it still lies ahead.
type nextIndex struct {
	from, at int // the last search's start and answer (-1: none from there on)
	valid    bool
}

func (x *nextIndex) next(text []byte, from int, pat string) int {
	if x.valid && from >= x.from && (x.at < 0 || x.at >= from) {
		return x.at
	}
	x.from, x.at, x.valid = from, -1, true
	if i := bytes.Index(text[from:], []byte(pat)); i >= 0 {
		x.at = from + i
	}
	return x.at
}

func tagRune(r rune) bool {
	if r < 0x80 {
		return isASCIIAlnum(byte(r)) || r == '_' || r == '-' || r == '/'
	}
	return r != utf8.RuneError && !isUnicodeWhitespace(r) && !unicode.IsPunct(r) && !unicode.IsControl(r)
}

func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte{}, b...)
}
