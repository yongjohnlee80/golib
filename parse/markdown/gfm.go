package markdown

import "bytes"

// The kinds GitHub Flavored Markdown adds. A table's first row is its header row; the rest are its
// body. Task list items are KindItem nodes with Checked set, and extended autolinks are KindAutolink.
//
// https://github.github.com/gfm/
const (
	KindTable Kind = KindExtension + iota
	KindTableRow
	KindTableCell
	KindStrikethrough
)

// Align is a table column's alignment, from the colons of its delimiter row.
type Align uint8

const (
	AlignNone Align = iota
	AlignLeft
	AlignCenter
	AlignRight
)

// Extension is an optional syntax Parse can read. GFMExtension describes the one the package defines.
type Extension struct {
	Name string
	// Recognizes reports whether src holds any syntax the extension reads. On a source it does not
	// recognize, Parse builds the same tree with the extension as without it.
	Recognizes func(src []byte) bool

	enable func(*config)
}

// Option is the option that enables the extension.
func (e Extension) Option() Option { return e.enable }

// GFMExtension describes GitHub Flavored Markdown: tables, task list items, strikethrough and
// extended autolinks.
func GFMExtension() Extension {
	return Extension{Name: "gfm", Recognizes: gfmRecognizes, enable: func(c *config) { c.gfm = true }}
}

// GFM enables GitHub Flavored Markdown.
func GFM() Option { return GFMExtension().Option() }

// gfmRecognizes is true when src has a byte sequence one of the GFM rules starts from: a pipe or
// a colon beside a hyphen (the delimiter rows setext cannot take), two tildes, a task marker, or
// what an extended autolink needs.
func gfmRecognizes(src []byte) bool {
	for _, s := range []string{"|", ":-", "-:", "~~", "[ ]", "[\t]", "[x]", "[X]", "www.", "://", "@"} {
		if bytes.Contains(src, []byte(s)) {
			return true
		}
	}
	return false
}

// tableStart opens a table when the current line is a delimiter row under a paragraph whose last
// line, after any definitions are taken out, has as many cells. Lines of the paragraph above the
// header stay a paragraph. It is tried after every CommonMark block start, so a line one of those
// takes (a setext underline, a thematic break, a list item) is never a delimiter row.
//
// https://github.github.com/gfm/#tables-extension-
func (p *blockParser) tableStart(para *Node) (startResult, *Node) {
	aligns, ok := delimiterRow(p.text[p.nextNonspace:])
	if !ok {
		return startNone, nil
	}
	p.extractLinkRefs(para)
	b := para.blk
	if len(b.segs) == 0 {
		return startNone, nil
	}
	last := b.segs[len(b.segs)-1]
	header := b.content[last.at : last.at+last.pad+last.n]
	if len(splitRow(header)) != len(aligns) {
		return startNone, nil
	}
	parent := para.Parent
	if len(b.segs) == 1 {
		para.Unlink() // the paragraph was only the header row
		p.tip = parent
	} else {
		b.content = b.content[:last.at]
		b.segs = b.segs[:len(b.segs)-1]
		prev := b.segs[len(b.segs)-1]
		para.Span.End = prev.src + prev.n
		p.finalize(para, -1)
	}
	t := &Node{Kind: KindTable, Span: Span{last.src, last.src + last.n}, Align: aligns, blk: &blockState{open: true}}
	t.blk.content = append(append([]byte(nil), header...), '\n')
	t.blk.segs = []seg{{at: 0, pad: last.pad, src: last.src, n: last.n}}
	parent.AppendChild(t)
	p.tip = t
	p.addLine() // the delimiter row
	p.lineConsumed = true
	return startLeaf, t
}

// delimiterRow reads a table delimiter row: cells of hyphens, each with an optional colon at either
// end giving the column's alignment.
func delimiterRow(rest []byte) ([]Align, bool) {
	cells := splitRow(rest)
	aligns := make([]Align, 0, len(cells))
	for _, c := range cells {
		cell := rest[c[0]:c[1]]
		left := len(cell) > 0 && cell[0] == ':'
		right := len(cell) > 1 && cell[len(cell)-1] == ':'
		dashes := cell
		if left {
			dashes = dashes[1:]
		}
		if right {
			dashes = dashes[:len(dashes)-1]
		}
		if len(dashes) == 0 || len(bytes.Trim(dashes, "-")) != 0 {
			return nil, false
		}
		switch {
		case left && right:
			aligns = append(aligns, AlignCenter)
		case left:
			aligns = append(aligns, AlignLeft)
		case right:
			aligns = append(aligns, AlignRight)
		default:
			aligns = append(aligns, AlignNone)
		}
	}
	return aligns, len(aligns) > 0
}

// splitRow returns the trimmed ranges of a table row's cells: pipes separate them, a leading and a
// trailing pipe are optional, and an escaped pipe stays in its cell. A pipe is escaped when an odd
// run of backslashes precedes it: in "\\|" the backslash is the one escaped, and the pipe separates.
func splitRow(line []byte) [][2]int {
	i, end := 0, len(line)
	for i < end && isSpaceOrTab(line[i]) {
		i++
	}
	for end > i && isSpaceOrTab(line[end-1]) {
		end--
	}
	if i < end && line[i] == '|' {
		i++
	}
	if end > i && line[end-1] == '|' && !escapedPipe(line, end-1) {
		end--
	}
	var cells [][2]int
	start := i
	for j := i; j <= end; j++ {
		if j < end && (line[j] != '|' || escapedPipe(line, j)) {
			continue
		}
		s, e := start, j
		for s < e && isSpaceOrTab(line[s]) {
			s++
		}
		for e > s && isSpaceOrTab(line[e-1]) {
			e--
		}
		cells = append(cells, [2]int{s, e})
		start = j + 1
	}
	return cells
}

// escapedPipe reports whether the pipe at line[j] follows an odd run of backslashes.
func escapedPipe(line []byte, j int) bool {
	n := 0
	for j-n-1 >= 0 && line[j-n-1] == '\\' {
		n++
	}
	return n%2 == 1
}

// finalizeTable turns the table's lines into rows: the header, then the body rows, each with as
// many cells as the header (missing cells are empty, extra ones are dropped). The delimiter row
// becomes no node.
func finalizeTable(t *Node) {
	b := t.blk
	for i, s := range b.segs {
		if i == 1 {
			continue
		}
		line := b.content[s.at : s.at+s.pad+s.n]
		row := &Node{Kind: KindTableRow, Span: Span{s.src, s.src + s.n}}
		t.AppendChild(row)
		cells := splitRow(line)
		for col := range t.Align {
			cell := &Node{Kind: KindTableCell, Span: Span{row.Span.End, row.Span.End}, blk: &blockState{}}
			if col < len(cells) {
				tableCell(cell, line, cells[col], s)
			}
			row.AppendChild(cell)
		}
	}
}

// tableCell gives a cell its content and span: the cell's bytes, with the backslash that escapes
// each escaped pipe dropped, so that the pipe is text wherever it is, even in a code span.
func tableCell(cell *Node, line []byte, r [2]int, s seg) {
	src := func(off int) int { return s.src + max(0, off-s.pad) }
	cell.Span = Span{src(r[0]), src(r[1])}
	b := cell.blk
	from := r[0]
	for j := r[0]; j <= r[1]; j++ {
		if j < r[1] && !(line[j] == '\\' && j+1 < r[1] && line[j+1] == '|' && escapedPipe(line, j+1)) {
			continue
		}
		if j > from {
			b.segs = append(b.segs, seg{at: len(b.content), src: src(from), n: j - from})
			b.content = append(b.content, line[from:j]...)
		}
		from = j + 1
	}
}

// taskMarker makes item a task list item when its content starts with "[ ]", "[x]" or "[X]"
// followed by whitespace. The rest of the line is then the text of its first paragraph, which the
// marker begins, so no other block starts there.
//
// https://github.github.com/gfm/#task-list-items-extension-
func (p *blockParser) taskMarker(item *Node) (startResult, *Node, bool) {
	rest := p.text[p.offset:]
	if p.partiallyConsumedTab || len(rest) < 3 || rest[0] != '[' || rest[2] != ']' ||
		(rest[1] != ' ' && rest[1] != '\t' && rest[1] != 'x' && rest[1] != 'X') ||
		(len(rest) > 3 && !isSpaceOrTab(rest[3])) {
		return startNone, nil, false
	}
	checked := rest[1] == 'x' || rest[1] == 'X'
	item.Checked = &checked
	p.advanceOffset(3, false)
	p.extendTo(item, p.ln.start+p.offset)
	p.findNextNonspace()
	p.advanceNextNonspace()
	para := p.addChild(KindParagraph, p.offset)
	if !p.blank {
		p.addLine()
	}
	p.lineConsumed = true
	return startLeaf, para, true
}

// gfmInline handles the bytes GFM adds as inline triggers. It reports whether it consumed input;
// when it did not, the byte is ordinary text.
func (p *inlineParser) gfmInline(c byte) bool {
	switch c {
	case '~':
		p.delimRun(c)
		return true
	case '.':
		return p.extendedAutolink("www", "http://")
	case ':':
		for _, scheme := range []string{"https", "http", "ftp"} {
			if p.extendedAutolink(scheme, "") {
				return true
			}
		}
	}
	return false
}

// extendedAutolink recognizes a www autolink (lead "www", at the '.') or a URL autolink (lead a
// scheme, at the ':'), whose lead is the tail of the text node just written. It is not recognized
// inside link text, and only at the start of a line, after whitespace, or after '*', '_', '~' or '('.
//
// https://github.github.com/gfm/#autolinks-extension-
func (p *inlineParser) extendedAutolink(lead, prefix string) bool {
	start := p.pos - len(lead)
	if p.brackets != nil || start < 0 || string(p.text[start:p.pos]) != lead || !boundaryBefore(p.text, start) {
		return false
	}
	domain := p.pos + 1 // after "www."
	if prefix == "" {
		if !bytes.HasPrefix(p.text[p.pos:], []byte("://")) {
			return false
		}
		domain = p.pos + 3
	}
	last := p.parent.LastChild
	if last == nil || last.Kind != KindText || last.Literal != nil || last.Span.End != p.srcAt(p.pos) ||
		last.Span.End-last.Span.Start < len(lead) {
		return false
	}
	end := linkEnd(p.text, start, domain)
	if end <= domain {
		return false
	}
	shrinkEnd(last, len(lead))
	if last.Span.Start == last.Span.End {
		last.Unlink()
	}
	link := &Node{Kind: KindAutolink, Span: Span{p.srcAt(start), p.srcAt(end)},
		Dest: append([]byte(prefix), p.text[start:end]...)}
	link.AppendChild(&Node{Kind: KindText, Span: link.Span})
	p.parent.AppendChild(link)
	p.pos = end
	return true
}

// boundaryBefore reports whether an extended autolink may start at b[i]: at the start of a line,
// after whitespace, or after one of the delimiting characters '*', '_', '~' and '('.
func boundaryBefore(b []byte, i int) bool {
	if i == 0 {
		return true
	}
	r := runeBefore(b, i)
	return isUnicodeWhitespace(r) || r == '*' || r == '_' || r == '~' || r == '('
}

// linkEnd returns where an extended autolink starting at b[start] ends, given where its domain
// starts, or 0 if the domain is not valid. Past the domain, anything but whitespace and '<'
// continues it; then trailing punctuation, unbalanced closing parentheses and an entity-like
// "&name;" are taken back off.
func linkEnd(b []byte, start, domain int) int {
	end := validDomain(b, domain)
	if end == 0 {
		return 0
	}
	for end < len(b) && b[end] != '<' && !isUnicodeWhitespace(runeAt(b, end)) {
		end++
	}
	// counted once: each ')' taken off the end is one fewer, so a long tail of them stays linear
	open, closing := bytes.Count(b[start:end], []byte("(")), bytes.Count(b[start:end], []byte(")"))
	for end > domain {
		switch c := b[end-1]; c {
		case '?', '!', '.', ',', ':', '*', '_', '~':
			end--
			continue
		case ')':
			if closing > open {
				end--
				closing--
				continue
			}
		case ';':
			k := end - 2
			for k > domain && isASCIIAlnum(b[k]) {
				k--
			}
			if k < end-2 && b[k] == '&' {
				end = k // an entity-like "&name;" is not part of the link
			} else {
				end--
			}
			continue
		}
		break
	}
	return end
}

// validDomain returns the end of a valid domain at b[i], or 0: segments of letters, digits, '_' and
// '-' separated by periods, at least one period, and no '_' in the last two segments.
func validDomain(b []byte, i int) int {
	segStart, periods := i, 0
	var underscore [2]bool // in the segment before the current one, and in the current one
	j := i
	for ; j < len(b); j++ {
		c := b[j]
		switch {
		case c == '.':
			if j == segStart || j+1 >= len(b) || !domainChar(b, j+1) {
				goto done
			}
			periods++
			underscore[0], underscore[1] = underscore[1], false
			segStart = j + 1
		case c == '_':
			underscore[1] = true
		case domainChar(b, j):
			if c >= 0x80 {
				j += utf8Len(b[j:]) - 1
			}
		default:
			goto done
		}
	}
done:
	if periods == 0 || j == segStart || underscore[0] || underscore[1] {
		return 0
	}
	return j
}

func domainChar(b []byte, j int) bool {
	c := b[j]
	if c < 0x80 {
		return isASCIIAlnum(c) || c == '-' || c == '_'
	}
	r := runeAt(b, j)
	return isUnicodeLetterOrDigit(r)
}

// emailAutolinks finds extended email autolinks in the text of n's inline content, outside links,
// images and autolinks. It runs after adjacent text is merged, since an address's '.', '_' and '-'
// may each have begun as their own text node.
func (p *inlineParser) emailAutolinks(n *Node) {
	for c := n.FirstChild; c != nil; c = c.Next {
		switch c.Kind {
		case KindLink, KindImage, KindAutolink:
			continue
		case KindText:
			if c.Literal == nil {
				c = p.splitEmails(c)
			}
		default:
			p.emailAutolinks(c)
		}
	}
}

// splitEmails links each address in text node t, returning the last node it leaves in t's place.
func (p *inlineParser) splitEmails(t *Node) *Node {
	for {
		text := p.src[t.Span.Start:t.Span.End]
		at, s, e := findEmail(text, func(i int) bool {
			if i > 0 {
				return boundaryBefore(text, i)
			}
			return p.lineStartOrBoundary(t.Span.Start)
		})
		if at < 0 {
			return t
		}
		base := t.Span.Start
		link := &Node{Kind: KindAutolink, Span: Span{base + s, base + e},
			Dest: append([]byte("mailto:"), text[s:e]...)}
		link.AppendChild(&Node{Kind: KindText, Span: link.Span})
		rest := &Node{Kind: KindText, Span: Span{base + e, t.Span.End}}
		t.Span.End = base + s
		if t.Next != nil {
			t.Next.InsertBefore(link)
		} else {
			t.Parent.AppendChild(link)
		}
		if link.Next != nil {
			link.Next.InsertBefore(rest)
		} else {
			link.Parent.AppendChild(rest)
		}
		if t.Span.Start == t.Span.End {
			t.Unlink()
		}
		if rest.Span.Start == rest.Span.End {
			rest.Unlink()
			return link
		}
		t = rest
	}
}

// lineStartOrBoundary is boundaryBefore for a source offset: true at the start of a line's content,
// else decided by the byte before it.
func (p *inlineParser) lineStartOrBoundary(off int) bool {
	for _, s := range p.segs {
		if s.src == off {
			return true
		}
	}
	return off == 0 || boundaryBefore(p.src, off)
}

// findEmail finds the first address in text whose start ok accepts, returning the offset of its
// '@' and its range, or -1. The local part is letters, digits, '.', '-', '_' and '+'; the domain is
// letters, digits, '-' and '_' in segments separated by periods, at least one, not ending in '-'
// or '_'; a final '.' is not part of the address.
//
// https://github.github.com/gfm/#extended-email-autolink
func findEmail(text []byte, ok func(int) bool) (at, start, end int) {
	for from := 0; ; {
		i := bytes.IndexByte(text[from:], '@')
		if i < 0 {
			return -1, 0, 0
		}
		at = from + i
		from = at + 1
		s := at
		for s > 0 && (isASCIIAlnum(text[s-1]) || bytes.IndexByte([]byte(".-_+"), text[s-1]) >= 0) {
			s--
		}
		if s == at || !ok(s) {
			continue
		}
		e, periods, segStart := at+1, 0, at+1
		for e < len(text) {
			c := text[e]
			if c == '.' {
				if e == segStart {
					break
				}
				periods++
				segStart = e + 1
			} else if !isASCIIAlnum(c) && c != '-' && c != '_' {
				break
			}
			e++
		}
		for e > at+1 && text[e-1] == '.' {
			e--
			periods--
		}
		if periods < 1 || e == at+1 || text[e-1] == '-' || text[e-1] == '_' {
			continue
		}
		return at, s, e
	}
}
