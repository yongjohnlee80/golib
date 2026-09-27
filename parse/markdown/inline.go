package markdown

import "bytes"

// parseInlines runs the inline phase over every paragraph and heading, now that every block is
// closed and every link reference definition is known.
func parseInlines(d *Document, cfg *config) {
	var walk func(n *Node)
	walk = func(n *Node) {
		for c := n.FirstChild; c != nil; {
			next := c.Next
			switch c.Kind {
			case KindParagraph, KindHeading:
				if c.blk != nil {
					text, segs := leafContent(c)
					ip := &inlineParser{doc: d, cfg: cfg, src: d.Source, text: text, segs: segs, parent: c}
					ip.parse()
				}
			default:
				walk(c)
			}
			c = next
		}
	}
	walk(d.Root)
}

// leafContent is a paragraph's or heading's text for inline parsing: its lines joined, without the
// final line ending and without trailing spaces or tabs.
func leafContent(n *Node) ([]byte, []seg) {
	b := n.blk
	text := bytes.TrimRight(b.content, " \t\n")
	return text, b.segs
}

type delim struct {
	node         *Node
	char         byte
	num, origNum int
	canOpen      bool
	canClose     bool
	prev, next   *delim
}

type bracket struct {
	node         *Node // the "[" or "![" text node
	image        bool
	pos          int // content offset just past the opening bracket
	prevDelim    *delim
	active       bool
	bracketAfter bool
	prev         *bracket
}

type inlineParser struct {
	doc    *Document
	cfg    *config
	src    []byte
	text   []byte
	segs   []seg
	pos    int
	parent *Node

	delims   *delim
	brackets *bracket

	ticks   map[int][]int // backtick runs by length: their start offsets, ascending
	tickIdx map[int]int   // per length, the first run not yet behind the scan
}

func (p *inlineParser) srcAt(off int) int { return mapOffset(p.segs, off) }

func (p *inlineParser) parse() {
	for p.pos < len(p.text) {
		switch c := p.text[p.pos]; c {
		case '\n':
			p.lineEnd()
		case '\\':
			p.backslash()
		case '`':
			p.codeSpan()
		case '*', '_':
			p.delimRun(c)
		case '[':
			p.openBracket(false, 1)
		case '!':
			if p.pos+1 < len(p.text) && p.text[p.pos+1] == '[' {
				p.openBracket(true, 2)
			} else {
				p.textRun(p.pos, p.pos+1)
			}
		case ']':
			p.closeBracket()
		case '<':
			p.angle()
		case '&':
			p.entity()
		default:
			end := p.pos + 1
			for end < len(p.text) && !isInlineSpecial(p.text[end]) {
				end++
			}
			p.textRun(p.pos, end)
		}
	}
	p.processEmphasis(nil)
	p.mergeText(p.parent)
}

func isInlineSpecial(c byte) bool {
	switch c {
	case '\n', '\\', '`', '*', '_', '[', '!', ']', '<', '&':
		return true
	}
	return false
}

// textRun appends content[from:to] as text and moves past it.
func (p *inlineParser) textRun(from, to int) *Node {
	n := p.textNode(from, to, p.text[from:to])
	p.pos = to
	return n
}

// textNode appends a text node for content[from:to] whose text is lit.
func (p *inlineParser) textNode(from, to int, lit []byte) *Node {
	n := &Node{Kind: KindText, Span: Span{p.srcAt(from), p.srcAt(to)}}
	lit = replaceNUL(lit)
	if !bytes.Equal(lit, p.src[n.Span.Start:n.Span.End]) {
		n.Literal = append([]byte(nil), lit...)
	}
	p.parent.AppendChild(n)
	return n
}

func replaceNUL(b []byte) []byte {
	if bytes.IndexByte(b, 0) < 0 {
		return b
	}
	return bytes.ReplaceAll(b, []byte{0}, []byte("�"))
}

// lineEnd turns a line ending into a hard break (two or more spaces directly before it) or a soft
// break, dropping the spaces and tabs at the end of this line and the start of the next.
//
// https://spec.commonmark.org/0.31.2/#hard-line-breaks
func (p *inlineParser) lineEnd() {
	spaces := 0
	if last := p.parent.LastChild; last != nil && last.Kind == KindText {
		t := last.Text(p.src)
		spaces = len(t) - len(bytes.TrimRight(t, " "))
		trimmed := bytes.TrimRight(t, " \t")
		if cut := len(t) - len(trimmed); cut > 0 {
			last.Literal = append([]byte(nil), trimmed...)
			last.Span.End -= min(cut, last.Span.End-last.Span.Start)
			if bytes.Equal(last.Literal, p.src[last.Span.Start:last.Span.End]) {
				last.Literal = nil
			}
			if len(trimmed) == 0 {
				last.Unlink()
			}
		}
	}
	kind := KindSoftBreak
	if spaces >= 2 {
		kind = KindHardBreak
	}
	start := p.srcAt(p.pos)
	p.parent.AppendChild(&Node{Kind: kind, Span: Span{start, start}})
	p.pos++
	for p.pos < len(p.text) && isSpaceOrTab(p.text[p.pos]) {
		p.pos++
	}
}

// backslash handles an escape: a backslash before ASCII punctuation is that character, before a line
// ending it is a hard break, and otherwise it is itself.
//
// https://spec.commonmark.org/0.31.2/#backslash-escapes
func (p *inlineParser) backslash() {
	i := p.pos
	if i+1 < len(p.text) {
		switch next := p.text[i+1]; {
		case next == '\n':
			start := p.srcAt(i)
			p.parent.AppendChild(&Node{Kind: KindHardBreak, Span: Span{start, p.srcAt(i + 1)}})
			p.pos = i + 2
			for p.pos < len(p.text) && isSpaceOrTab(p.text[p.pos]) {
				p.pos++
			}
			return
		case isASCIIPunct(next):
			p.textNode(i, i+2, p.text[i+1:i+2])
			p.pos = i + 2
			return
		}
	}
	p.textRun(i, i+1)
}

func (p *inlineParser) entity() {
	if v, n, ok := entityAt(p.text, p.pos); ok {
		p.textNode(p.pos, p.pos+n, v)
		p.pos += n
		return
	}
	p.textRun(p.pos, p.pos+1)
}

// codeSpan matches a backtick run with the next run of the same length. Every run's position is
// indexed once by length, so finding a closer never rescans the text.
//
// https://spec.commonmark.org/0.31.2/#code-spans
func (p *inlineParser) codeSpan() {
	if p.ticks == nil {
		p.indexTicks()
	}
	start := p.pos
	n := 0
	for start+n < len(p.text) && p.text[start+n] == '`' {
		n++
	}
	after := start + n
	runs := p.ticks[n]
	i := p.tickIdx[n]
	for i < len(runs) && runs[i] < after {
		i++
	}
	p.tickIdx[n] = i
	if i >= len(runs) {
		p.textRun(start, after) // no closer of this length: literal backticks
		return
	}
	close := runs[i]
	p.tickIdx[n] = i + 1
	content := bytes.ReplaceAll(p.text[after:close], []byte{'\n'}, []byte{' '})
	if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' &&
		len(bytes.Trim(content, " ")) > 0 {
		content = content[1 : len(content)-1]
	}
	node := &Node{Kind: KindCodeSpan, Span: Span{p.srcAt(start), p.srcAt(close + n)},
		Literal: replaceNUL(append([]byte{}, content...))}
	p.parent.AppendChild(node)
	p.pos = close + n
}

func (p *inlineParser) indexTicks() {
	p.ticks = map[int][]int{}
	p.tickIdx = map[int]int{}
	for i := 0; i < len(p.text); {
		if p.text[i] != '`' {
			i++
			continue
		}
		// a backtick escaped by a backslash does not open a run, but a run may still close
		j := i
		for j < len(p.text) && p.text[j] == '`' {
			j++
		}
		p.ticks[j-i] = append(p.ticks[j-i], i)
		i = j
	}
}

// delimRun scans a run of '*' or '_' and records whether it can open and close emphasis.
//
// https://spec.commonmark.org/0.31.2/#emphasis-and-strong-emphasis
func (p *inlineParser) delimRun(c byte) {
	start := p.pos
	end := start
	for end < len(p.text) && p.text[end] == c {
		end++
	}
	before := runeBefore(p.text, start)
	after := runeAt(p.text, end)
	beforeWS, afterWS := isUnicodeWhitespace(before), isUnicodeWhitespace(after)
	beforeP, afterP := isUnicodePunct(before), isUnicodePunct(after)
	left := !afterWS && (!afterP || beforeWS || beforeP)
	right := !beforeWS && (!beforeP || afterWS || afterP)
	canOpen, canClose := left, right
	if c == '_' {
		canOpen = left && (!right || beforeP)
		canClose = right && (!left || afterP)
	}
	node := p.textRun(start, end)
	if canOpen || canClose {
		d := &delim{node: node, char: c, num: end - start, origNum: end - start,
			canOpen: canOpen, canClose: canClose, prev: p.delims}
		if p.delims != nil {
			p.delims.next = d
		}
		p.delims = d
	}
}

func (p *inlineParser) openBracket(image bool, width int) {
	node := p.textRun(p.pos, p.pos+width)
	if p.brackets != nil {
		p.brackets.bracketAfter = true
	}
	p.brackets = &bracket{node: node, image: image, pos: p.pos, prevDelim: p.delims,
		active: true, prev: p.brackets}
}

// closeBracket tries to make a link or image of the nearest open bracket: an inline link, then a full,
// collapsed or shortcut reference.
//
// https://spec.commonmark.org/0.31.2/#links
func (p *inlineParser) closeBracket() {
	closePos := p.pos
	opener := p.brackets
	if opener == nil {
		p.textRun(closePos, closePos+1)
		return
	}
	if !opener.active {
		p.brackets = opener.prev
		p.textRun(closePos, closePos+1)
		return
	}
	var dest, title []byte
	matched := false
	end := closePos + 1
	if d, t, e, ok := p.inlineLinkTail(closePos + 1); ok {
		dest, title, end, matched = d, t, e, true
	} else {
		var label []byte
		lab, e, ok := scanLinkLabel(p.text, closePos+1)
		switch {
		case ok:
			label, end = lab, e
		case !opener.bracketAfter:
			label = p.text[opener.pos:closePos] // collapsed ("[]") or shortcut
			if bytes.HasPrefix(p.text[closePos+1:], []byte("[]")) {
				end = closePos + 3
			}
		}
		if label != nil && len(label) <= 999 {
			if ref, found := p.doc.Refs[normalizeLabel(label)]; found {
				dest, title, matched = ref.Dest, ref.Title, true
			}
		}
		if !matched {
			end = closePos + 1
		}
	}
	if !matched {
		p.brackets = opener.prev
		p.textRun(closePos, closePos+1)
		return
	}
	kind := KindLink
	if opener.image {
		kind = KindImage
	}
	link := &Node{Kind: kind, Dest: dest, Title: title,
		Span: Span{opener.node.Span.Start, p.srcAt(end)}}
	for c := opener.node.Next; c != nil; {
		next := c.Next
		c.Unlink()
		link.AppendChild(c)
		c = next
	}
	opener.node.InsertBefore(link)
	p.processEmphasis(opener.prevDelim)
	opener.node.Unlink()
	p.brackets = opener.prev
	if !opener.image {
		for b := p.brackets; b != nil; b = b.prev {
			if !b.image {
				b.active = false // links may not contain links
			}
		}
	}
	p.pos = end
}

// inlineLinkTail parses "(destination title)" at i.
//
// https://spec.commonmark.org/0.31.2/#inline-link
func (p *inlineParser) inlineLinkTail(i int) ([]byte, []byte, int, bool) {
	b := p.text
	if i >= len(b) || b[i] != '(' {
		return nil, nil, 0, false
	}
	j := skipSpaceAtMostOneLine(b, i+1)
	var dest []byte
	if j < len(b) && b[j] != ')' {
		d, k, ok := scanLinkDest(b, j)
		if !ok {
			return nil, nil, 0, false
		}
		dest = unescapeAll(d)
		j = k
	} else {
		dest = []byte{}
	}
	var title []byte
	t := skipSpaceAtMostOneLine(b, j)
	if t > j && t < len(b) && b[t] != ')' {
		ttl, k, ok := scanLinkTitle(b, t)
		if !ok {
			return nil, nil, 0, false
		}
		title = unescapeAll(ttl)
		t = skipSpaceAtMostOneLine(b, k)
	}
	if t >= len(b) || b[t] != ')' {
		return nil, nil, 0, false
	}
	return dest, title, t + 1, true
}

// angle handles '<': an autolink, raw HTML, or a literal '<'.
//
// https://spec.commonmark.org/0.31.2/#autolinks
func (p *inlineParser) angle() {
	rest := p.text[p.pos:]
	if n, dest := autolink(rest); n > 0 {
		// entity references decode in an autolink, as in any URL; backslash escapes do not
		node := &Node{Kind: KindAutolink, Dest: decodeEntities(dest), Span: Span{p.srcAt(p.pos), p.srcAt(p.pos + n)}}
		text := &Node{Kind: KindText, Span: Span{p.srcAt(p.pos + 1), p.srcAt(p.pos + n - 1)}}
		if lit := decodeEntities(rest[1 : n-1]); !bytes.Equal(lit, rest[1:n-1]) {
			text.Literal = lit
		}
		node.AppendChild(text)
		p.parent.AppendChild(node)
		p.pos += n
		return
	}
	if n := rawHTML(rest); n > 0 {
		node := &Node{Kind: KindRawHTML, Span: Span{p.srcAt(p.pos), p.srcAt(p.pos + n)},
			Literal: append([]byte(nil), rest[:n]...)}
		p.parent.AppendChild(node)
		p.pos += n
		return
	}
	p.textRun(p.pos, p.pos+1)
}

// autolink recognizes "<scheme:...>" and "<email>" at the start of b.
func autolink(b []byte) (int, []byte) {
	if len(b) < 3 || b[0] != '<' {
		return 0, nil
	}
	// URI autolink: a scheme of 2–32 characters, ':', then no spaces, controls, '<' or '>'
	i := 1
	if isASCIIAlpha(b[i]) {
		j := i + 1
		for j < len(b) && j-i < 32 && (isASCIIAlnum(b[j]) || b[j] == '+' || b[j] == '.' || b[j] == '-') {
			j++
		}
		if j-i >= 2 && j < len(b) && b[j] == ':' {
			k := j + 1
			for k < len(b) && b[k] > ' ' && b[k] != '<' && b[k] != '>' && b[k] != 0x7f {
				k++
			}
			if k < len(b) && b[k] == '>' {
				return k + 1, append([]byte(nil), b[1:k]...)
			}
		}
	}
	// email autolink
	j := 1
	for j < len(b) && (isASCIIAlnum(b[j]) || bytes.IndexByte([]byte(".!#$%&'*+/=?^_`{|}~-"), b[j]) >= 0) {
		j++
	}
	if j == 1 || j >= len(b) || b[j] != '@' {
		return 0, nil
	}
	k := j + 1
	labels := 0
	for {
		s := k
		for k < len(b) && k-s < 63 && (isASCIIAlnum(b[k]) || b[k] == '-') {
			k++
		}
		if k == s || b[s] == '-' || b[k-1] == '-' {
			return 0, nil
		}
		labels++
		if k < len(b) && b[k] == '.' {
			k++
			continue
		}
		break
	}
	if labels == 0 || k >= len(b) || b[k] != '>' {
		return 0, nil
	}
	return k + 1, append([]byte("mailto:"), b[1:k]...)
}

// processEmphasis pairs the delimiter runs above bottom into emphasis. openersBottom keeps, per
// class of closer (delimiter byte, whether the closer can also open, its length mod 3), the lowest
// point a failed search reached, so no later closer of that class searches the same openers again.
//
// https://spec.commonmark.org/0.31.2/#phase-2-inline-structure
func (p *inlineParser) processEmphasis(bottom *delim) {
	var openersBottom [2][2][3]*delim
	var set [2][2][3]bool
	slot := func(d *delim) (int, int, int) {
		c := 0
		if d.char == '_' {
			c = 1
		}
		o := 0
		if d.canOpen {
			o = 1
		}
		return c, o, d.origNum % 3
	}
	// the first delimiter above bottom (above the start when bottom is nil)
	closer := p.delims
	for closer != nil && closer.prev != bottom {
		closer = closer.prev
	}
	for closer != nil {
		if !closer.canClose {
			closer = closer.next
			continue
		}
		c, o, m := slot(closer)
		limit := bottom
		if set[c][o][m] {
			limit = openersBottom[c][o][m]
		}
		opener := closer.prev
		found := false
		for opener != nil && opener != bottom && opener != limit {
			if opener.char == closer.char && opener.canOpen {
				odd := (closer.canOpen || opener.canClose) && closer.origNum%3 != 0 &&
					(opener.origNum+closer.origNum)%3 == 0
				if !odd {
					found = true
					break
				}
			}
			opener = opener.prev
		}
		if !found {
			openersBottom[c][o][m], set[c][o][m] = closer.prev, true
			next := closer.next
			if !closer.canOpen {
				p.removeDelim(closer)
			}
			closer = next
			continue
		}
		use := 1
		if closer.num >= 2 && opener.num >= 2 {
			use = 2
		}
		kind := KindEmph
		if use == 2 {
			kind = KindStrong
		}
		on, cn := opener.node, closer.node
		emph := &Node{Kind: kind, Span: Span{on.Span.End - use, cn.Span.Start + use}}
		shrinkEnd(on, use)
		shrinkStart(cn, use)
		opener.num -= use
		closer.num -= use
		for n := on.Next; n != nil && n != cn; {
			next := n.Next
			n.Unlink()
			emph.AppendChild(n)
			n = next
		}
		cn.InsertBefore(emph)
		// delimiters between the pair can no longer match
		for d := closer.prev; d != nil && d != opener; {
			prev := d.prev
			p.removeDelim(d)
			d = prev
		}
		if opener.num == 0 {
			on.Unlink()
			p.removeDelim(opener)
		}
		if closer.num == 0 {
			next := closer.next
			cn.Unlink()
			p.removeDelim(closer)
			closer = next
		}
	}
	for p.delims != nil && p.delims != bottom {
		p.removeDelim(p.delims)
	}
}

func shrinkEnd(n *Node, k int) {
	n.Span.End -= k
	if n.Literal != nil {
		n.Literal = n.Literal[:len(n.Literal)-k]
	}
}

func shrinkStart(n *Node, k int) {
	n.Span.Start += k
	if n.Literal != nil {
		n.Literal = n.Literal[k:]
	}
}

func (p *inlineParser) removeDelim(d *delim) {
	if d.prev != nil {
		d.prev.next = d.next
	}
	if d.next != nil {
		d.next.prev = d.prev
	} else {
		p.delims = d.prev
	}
	d.prev, d.next = nil, nil
}

// mergeText joins adjacent text nodes whose spans touch, so delimiter runs that matched nothing and
// neighbouring text read as one node. A merged node keeps a Literal only when its text differs from
// the source it spans.
func (p *inlineParser) mergeText(parent *Node) {
	for c := parent.FirstChild; c != nil; c = c.Next {
		if c.FirstChild != nil {
			p.mergeText(c)
		}
		if c.Kind != KindText {
			continue
		}
		// find the run first and join it once: joining pairwise would recopy the run per node
		last, verbatim := c, c.Literal == nil
		for n := c.Next; n != nil && n.Kind == KindText && n.Span.Start == last.Span.End; n = n.Next {
			last, verbatim = n, verbatim && n.Literal == nil
		}
		if last == c {
			continue
		}
		var joined []byte
		if !verbatim {
			for n := c; ; n = n.Next {
				joined = append(joined, n.Text(p.src)...)
				if n == last {
					break
				}
			}
		}
		c.Span.End = last.Span.End
		c.Literal = joined
		if verbatim || bytes.Equal(joined, p.src[c.Span.Start:c.Span.End]) {
			c.Literal = nil
		}
		for stop := last.Next; c.Next != stop; {
			c.Next.Unlink()
		}
	}
}
