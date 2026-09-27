package markdown

import "bytes"

type startResult int

const (
	startNone      startResult = iota
	startContainer             // a container opened; keep looking for starts inside it
	startLeaf                  // a leaf opened (or a line was consumed); stop
)

// tryStarts tries every block start, in the order the spec gives them precedence, on the rest of
// the current line. container is the deepest block that matched the line.
//
// https://spec.commonmark.org/0.31.2/#precedence
func (p *blockParser) tryStarts(container *Node) (startResult, *Node) {
	ns := p.nextNonspace
	c := p.peek(ns)
	rest := p.text[ns:]

	if !p.indented {
		switch c {
		case '>':
			p.advanceNextNonspace()
			p.advanceOffset(1, false)
			if isSpaceOrTab(p.peek(p.offset)) {
				p.advanceOffset(1, true)
			}
			p.closeUnmatched()
			return startContainer, p.addChild(KindBlockQuote, ns)
		case '#':
			if level, content, ok := atxHeading(rest); ok {
				p.advanceNextNonspace()
				p.closeUnmatched()
				h := p.addChild(KindHeading, ns)
				h.Level = level
				h.blk.content = append([]byte(nil), content...)
				h.blk.segs = []seg{{at: 0, src: p.ln.start + ns + offsetIn(rest, content), n: len(content)}}
				h.Span.End = p.ln.end
				p.finalize(h, p.ln.end)
				p.offset = len(p.text)
				return startLeaf, h
			}
		case '`', '~':
			if n, info, ok := openingFence(rest, c); ok {
				p.closeUnmatched()
				cb := p.addChild(KindCodeBlock, ns)
				cb.Fence = c
				cb.blk.fenceLen = n
				cb.blk.fenceOffset = p.indent
				cb.Info = decodeInline(info)
				p.extendTo(cb, p.ln.end)
				p.lineConsumed = true
				return startLeaf, cb
			}
		case '<':
			// type 7 cannot interrupt a paragraph, including one this line would continue lazily
			inParagraph := container.Kind == KindParagraph || (!p.allClosed && p.tip.Kind == KindParagraph)
			if kind := htmlBlockStart(rest, inParagraph); kind != 0 {
				p.closeUnmatched()
				hb := p.addChild(KindHTMLBlock, 0)
				hb.HTML = kind
				hb.Span.Start = p.ln.start + ns
				// the line's content, indentation included, is added as-is
				return startLeaf, hb
			}
		}
		// a setext underline turns the paragraph above it into a heading
		if container.Kind == KindParagraph && (c == '=' || c == '-') {
			if level, ok := setextUnderline(rest); ok {
				p.closeUnmatched()
				p.extractLinkRefs(container) // definitions above the underline are not heading text
				if len(bytes.TrimSpace(container.blk.content)) == 0 {
					// only definitions were above it: no heading, and the underline is paragraph
					// text, not a thematic break (as the reference implementation, cmark, reads it)
					return startNone, nil
				}
				container.Kind = KindHeading
				container.Level = level
				container.Span.End = p.ln.end
				p.finalize(container, p.ln.end)
				p.offset = len(p.text)
				return startLeaf, container
			}
		}
		if thematicBreak(rest) {
			p.closeUnmatched()
			tb := p.addChild(KindThematicBreak, ns)
			tb.Span.End = p.ln.end
			p.finalize(tb, p.ln.end)
			p.offset = len(p.text)
			return startLeaf, tb
		}
	}

	if !p.indented {
		if data, ok := p.listMarker(container); ok {
			p.closeUnmatched()
			if p.tip.Kind != KindList || !listMatches(p.tip.List, data) {
				l := p.addChild(KindList, ns)
				l.List = data
				l.List.Tight = true
			}
			item := p.addChild(KindItem, ns)
			item.List = data
			if p.cfg.gfm {
				if res, n, ok := p.taskMarker(item); ok {
					return res, n
				}
			}
			return startContainer, item
		}
	}

	if p.indented && p.tip.Kind != KindParagraph && !p.blank {
		p.advanceOffset(4, true)
		p.closeUnmatched()
		cb := p.addChild(KindCodeBlock, p.offset)
		return startLeaf, cb
	}

	if p.cfg.gfm && !p.indented && container.Kind == KindParagraph {
		return p.tableStart(container)
	}
	return startNone, nil
}

func offsetIn(outer, inner []byte) int {
	if len(inner) == 0 {
		return 0
	}
	for i := range outer {
		if &outer[i] == &inner[0] {
			return i
		}
	}
	return 0
}

// atxHeading recognizes 1–6 '#' followed by a space, a tab or the line's end, and returns the heading
// text without the optional closing sequence.
//
// https://spec.commonmark.org/0.31.2/#atx-headings
func atxHeading(rest []byte) (int, []byte, bool) {
	n := 0
	for n < len(rest) && rest[n] == '#' {
		n++
	}
	if n < 1 || n > 6 || (n < len(rest) && !isSpaceOrTab(rest[n])) {
		return 0, nil, false
	}
	content := bytes.TrimLeft(rest[n:], " \t")
	content = bytes.TrimRight(content, " \t")
	// an optional closing sequence: '#'s preceded by a space or tab, or the whole content
	j := len(content)
	for j > 0 && content[j-1] == '#' {
		j--
	}
	if j == 0 {
		content = content[:0]
	} else if j < len(content) && isSpaceOrTab(content[j-1]) {
		content = bytes.TrimRight(content[:j], " \t")
	}
	return n, content, true
}

// openingFence recognizes three or more backticks or tildes. A backtick fence's info string may
// not contain a backtick.
//
// https://spec.commonmark.org/0.31.2/#fenced-code-blocks
func openingFence(rest []byte, c byte) (int, []byte, bool) {
	n := 0
	for n < len(rest) && rest[n] == c {
		n++
	}
	if n < 3 {
		return 0, nil, false
	}
	info := bytes.TrimSpace(trimSpaceTab(rest[n:]))
	if c == '`' && bytes.IndexByte(info, '`') >= 0 {
		return 0, nil, false
	}
	return n, info, true
}

func trimSpaceTab(b []byte) []byte { return bytes.Trim(b, " \t") }

// closingFence returns the length of a closing fence of c at the start of rest, or 0: the fence may
// be followed only by spaces and tabs.
func closingFence(rest []byte, c byte) int {
	n := 0
	for n < len(rest) && rest[n] == c {
		n++
	}
	if n < 3 || len(trimSpaceTab(rest[n:])) != 0 {
		return 0
	}
	return n
}

// setextUnderline recognizes a line of '=' (level 1) or '-' (level 2), optionally followed by spaces.
//
// https://spec.commonmark.org/0.31.2/#setext-headings
func setextUnderline(rest []byte) (int, bool) {
	body := bytes.TrimRight(rest, " \t")
	if len(body) == 0 {
		return 0, false
	}
	c := body[0]
	for _, b := range body {
		if b != c {
			return 0, false
		}
	}
	if c == '=' {
		return 1, true
	}
	return 2, true
}

// thematicBreak recognizes three or more '*', '-' or '_' of one kind, with only spaces and tabs
// between them.
//
// https://spec.commonmark.org/0.31.2/#thematic-breaks
func thematicBreak(rest []byte) bool {
	var c byte
	n := 0
	for _, b := range rest {
		switch {
		case isSpaceOrTab(b):
		case (b == '*' || b == '-' || b == '_') && (c == 0 || b == c):
			c = b
			n++
		default:
			return false
		}
	}
	return n >= 3
}

// listMarker recognizes a bullet or ordered list marker at the cursor and measures the item's
// content indentation.
//
// https://spec.commonmark.org/0.31.2/#list-items
func (p *blockParser) listMarker(container *Node) (ListData, bool) {
	rest := p.text[p.nextNonspace:]
	var d ListData
	d.markerOffset = p.indent
	markerLen := 0
	switch c := p.peek(p.nextNonspace); {
	case c == '*' || c == '+' || c == '-':
		d.Type, d.Bullet, markerLen = ListBullet, c, 1
	case isDigit(c):
		n := 0
		for n < len(rest) && n < 10 && isDigit(rest[n]) {
			n++
		}
		if n > 9 || n >= len(rest) || (rest[n] != '.' && rest[n] != ')') {
			return d, false
		}
		start := 0
		for _, b := range rest[:n] {
			start = start*10 + int(b-'0')
		}
		d.Type, d.Start, d.Delimiter, markerLen = ListOrdered, start, rest[n], n+1
	default:
		return d, false
	}
	// the marker must be followed by a space, a tab or the line's end
	after := p.nextNonspace + markerLen
	if after < len(p.text) && !isSpaceOrTab(p.text[after]) {
		return d, false
	}
	// a list item interrupting a paragraph must not be empty, and an ordered one must start at 1
	if container.Kind == KindParagraph {
		if len(trimSpaceTab(p.text[after:])) == 0 {
			return d, false
		}
		if d.Type == ListOrdered && d.Start != 1 {
			return d, false
		}
	}
	// consume the marker, then measure the spaces that follow it
	p.advanceNextNonspace()
	p.advanceOffset(markerLen, true)
	spacesStartCol, spacesStartOff := p.column, p.offset
	for p.column-spacesStartCol < 5 && isSpaceOrTab(p.peek(p.offset)) {
		p.advanceOffset(1, true)
	}
	blankItem := p.offset >= len(p.text)
	spaces := p.column - spacesStartCol
	if spaces >= 5 || spaces < 1 || blankItem {
		// content indented five or more is an indented code block inside the item: the item's
		// content starts one space after the marker
		d.padding = markerLen + 1
		p.column, p.offset, p.partiallyConsumedTab = spacesStartCol, spacesStartOff, false
		if isSpaceOrTab(p.peek(p.offset)) {
			p.advanceOffset(1, true)
		}
	} else {
		d.padding = markerLen + spaces
	}
	return d, true
}

func listMatches(a, b ListData) bool {
	return a.Type == b.Type && a.Delimiter == b.Delimiter && a.Bullet == b.Bullet
}

// paragraphHasContent reports whether the paragraph would still have content once its leading link
// reference definitions are taken out; a setext underline cannot make a heading of definitions.
func (p *blockParser) paragraphHasContent(para *Node) bool {
	rest := para.blk.content
	for {
		n, _, _, _, ok := parseLinkRefDef(rest)
		if !ok {
			break
		}
		rest = rest[n:]
	}
	return len(bytes.TrimSpace(rest)) > 0
}

var htmlBlockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "base": true, "basefont": true,
	"blockquote": true, "body": true, "caption": true, "center": true, "col": true,
	"colgroup": true, "dd": true, "details": true, "dialog": true, "dir": true, "div": true,
	"dl": true, "dt": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "frame": true, "frameset": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "head": true, "header": true, "hr": true,
	"html": true, "iframe": true, "legend": true, "li": true, "link": true, "main": true,
	"menu": true, "menuitem": true, "nav": true, "noframes": true, "ol": true,
	"optgroup": true, "option": true, "p": true, "param": true, "search": true,
	"section": true, "summary": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "title": true, "tr": true, "track": true,
	"ul": true,
}

var htmlRawTags = []string{"pre", "script", "style", "textarea"}

// htmlBlockStart returns which of the seven HTML block start conditions rest meets, or 0. Every type
// but 7 may interrupt a paragraph.
//
// https://spec.commonmark.org/0.31.2/#html-blocks
func htmlBlockStart(rest []byte, inParagraph bool) int {
	lower := bytes.ToLower(rest)
	for _, t := range htmlRawTags {
		if bytes.HasPrefix(lower, []byte("<"+t)) {
			after := len(t) + 1
			if after >= len(rest) || isSpaceOrTab(rest[after]) || rest[after] == '>' {
				return 1
			}
		}
	}
	switch {
	case bytes.HasPrefix(rest, []byte("<!--")):
		return 2
	case bytes.HasPrefix(rest, []byte("<?")):
		return 3
	case len(rest) > 2 && rest[1] == '!' && isASCIIAlpha(rest[2]):
		return 4
	case bytes.HasPrefix(rest, []byte("<![CDATA[")):
		return 5
	}
	name := lower[1:]
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	j := 0
	for j < len(name) && isASCIIAlnum(name[j]) {
		j++
	}
	if j > 0 && htmlBlockTags[string(name[:j])] {
		if j >= len(name) || isSpaceOrTab(name[j]) || name[j] == '>' ||
			(name[j] == '/' && j+1 < len(name) && name[j+1] == '>') {
			return 6
		}
	}
	if !inParagraph {
		if n := htmlTag(rest); n > 0 && len(trimSpaceTab(rest[n:])) == 0 && !isRawTagName(lower) {
			return 7
		}
	}
	return 0
}

func isRawTagName(lower []byte) bool {
	name := lower[1:]
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	for _, t := range htmlRawTags {
		if bytes.HasPrefix(name, []byte(t)) && (len(name) == len(t) || !isASCIIAlnum(name[len(t)]) && name[len(t)] != '-') {
			return true
		}
	}
	return false
}

// htmlBlockEnds reports whether a line ends an HTML block of types 1–5.
func htmlBlockEnds(kind int, text []byte) bool {
	lower := bytes.ToLower(text)
	switch kind {
	case 1:
		for _, t := range htmlRawTags {
			if bytes.Contains(lower, []byte("</"+t+">")) {
				return true
			}
		}
	case 2:
		return bytes.Contains(text, []byte("-->"))
	case 3:
		return bytes.Contains(text, []byte("?>"))
	case 4:
		return bytes.IndexByte(text, '>') >= 0
	case 5:
		return bytes.Contains(text, []byte("]]>"))
	}
	return false
}
