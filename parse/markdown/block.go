package markdown

import "bytes"

// blockState is what an open block needs while lines are still being added to it.
type blockState struct {
	open          bool
	lastLineBlank bool
	content       []byte // the text lines added so far, each ending in '\n'
	segs          []seg  // where content came from, for mapping inline spans back to the source
	fenceLen      int
	fenceOffset   int // columns of indent before the opening fence
	startedBlank  bool
}

// seg records that content[at : at+pad+n] is pad spaces (the remainder of a partly consumed tab)
// followed by Source[src : src+n], and then a '\n' that stands for the line's end.
type seg struct {
	at, pad, src, n int
}

// line is one input line without its terminator.
type line struct {
	start, end int // Source offsets
}

type blockParser struct {
	doc  *Document
	cfg  *config
	src  []byte
	tip  *Node // the deepest open block
	root *Node

	// the current line and a cursor into it
	ln                   line
	text                 []byte
	offset               int // byte index into text
	column               int // column of offset, tabs expanded to multiples of 4
	nextNonspace         int
	nextNonspaceColumn   int
	indent               int
	indented             bool
	blank                bool
	partiallyConsumedTab bool

	lineConsumed         bool // a start used the whole line (an opening fence): add nothing
	allClosed            bool
	lastMatchedContainer *Node
	oldtip               *Node
}

func newBlockParser(d *Document, cfg *config) *blockParser {
	root := &Node{Kind: KindDocument, blk: &blockState{open: true}}
	d.Root = root
	return &blockParser{doc: d, cfg: cfg, src: d.Source, tip: root, root: root}
}

func (p *blockParser) run() {
	src := p.src
	for start := 0; start < len(src); {
		end := start
		for end < len(src) && !isLineEnd(src[end]) {
			end++
		}
		p.incorporate(line{start, end})
		next := end
		if next < len(src) {
			if src[next] == '\r' && next+1 < len(src) && src[next+1] == '\n' {
				next += 2
			} else {
				next++
			}
		}
		start = next
	}
	for p.tip != nil {
		p.finalize(p.tip, -1)
	}
	p.root.Span = Span{0, len(src)}
}

func clearState(n *Node) {
	for c := n; c != nil; c = c.Next {
		c.blk = nil
		if c.FirstChild != nil {
			clearState(c.FirstChild)
		}
	}
}

// findNextNonspace measures the indentation ahead of the cursor without moving it.
func (p *blockParser) findNextNonspace() {
	i, cols := p.offset, p.column
	for i < len(p.text) {
		switch p.text[i] {
		case ' ':
			i++
			cols++
			continue
		case '\t':
			i++
			cols += 4 - cols%4
			continue
		}
		break
	}
	p.blank = i >= len(p.text)
	p.nextNonspace = i
	p.nextNonspaceColumn = cols
	p.indent = cols - p.column
	p.indented = p.indent >= 4
}

// advanceOffset moves the cursor count bytes, or count columns when columns is set. A tab that is
// only partly consumed by a column count leaves partiallyConsumedTab set, so its remainder becomes
// spaces in the content (https://spec.commonmark.org/0.31.2/#tabs).
func (p *blockParser) advanceOffset(count int, columns bool) {
	for count > 0 && p.offset < len(p.text) {
		if p.text[p.offset] == '\t' {
			toTab := 4 - p.column%4
			if columns {
				p.partiallyConsumedTab = toTab > count
				step := min(toTab, count)
				p.column += step
				if !p.partiallyConsumedTab {
					p.offset++
				}
				count -= step
			} else {
				p.partiallyConsumedTab = false
				p.column += toTab
				p.offset++
				count--
			}
			continue
		}
		p.partiallyConsumedTab = false
		p.offset++
		p.column++
		count--
	}
}

func (p *blockParser) advanceNextNonspace() {
	p.offset = p.nextNonspace
	p.column = p.nextNonspaceColumn
	p.partiallyConsumedTab = false
}

func (p *blockParser) peek(i int) byte {
	if i < len(p.text) {
		return p.text[i]
	}
	return 0
}

// incorporate runs one line through the three steps of the block phase: continuation of the open
// blocks, new block starts, and the rest of the line.
//
// https://spec.commonmark.org/0.31.2/#appendix-a-parsing-strategy
func (p *blockParser) incorporate(ln line) {
	p.ln = ln
	p.text = p.src[ln.start:ln.end]
	p.offset, p.column = 0, 0
	p.blank, p.partiallyConsumedTab = false, false
	p.oldtip = p.tip
	p.allClosed = true
	p.lineConsumed = false

	// 1. continuation
	container := p.root
	for {
		last := container.LastChild
		if last == nil || !last.blk.open {
			break
		}
		container = last
		p.findNextNonspace()
		switch p.continues(container) {
		case continueMatched:
			continue
		case continueFailed:
			container = container.Parent
		case continueLineDone:
			return
		}
		break
	}
	p.allClosed = container == p.oldtip
	p.lastMatchedContainer = container
	// A matched block quote spans this line, since its '>' is here. Every other block gains the line
	// only when content lands in it (addLine, or a leaf finalized on the line): a list, or a
	// paragraph that a heading or a break then interrupts, matches without keeping the line.
	for c := container; c != nil && c != p.root; c = c.Parent {
		if c.Kind == KindBlockQuote {
			p.extendTo(c, p.ln.end)
			break
		}
	}

	// 2. new block starts, unless the matched block takes raw lines
	// a paragraph or a table takes the line only if no block starts on it
	matchedLeaf := container.Kind != KindParagraph && container.Kind != KindTable && acceptsLines(container)
	var opened *Node // the deepest container this line opened, which spans its marker
	markerEnd := 0
	for !matchedLeaf {
		p.findNextNonspace()
		c := p.peek(p.nextNonspace)
		// a GFM delimiter row may also begin with '|' or ':', under a paragraph
		gfmRow := p.cfg.gfm && container.Kind == KindParagraph && (c == '|' || c == ':')
		if !p.indented && !mayStartBlock(c) && !gfmRow {
			p.advanceNextNonspace()
			break
		}
		res, nc := p.tryStarts(container)
		if res == startNone {
			p.advanceNextNonspace()
			break
		}
		container = nc
		if res == startContainer {
			opened, markerEnd = nc, p.ln.start+p.offset
		}
		if res == startLeaf {
			break
		}
		if acceptsLines(container) {
			break
		}
	}

	// one walk for all the containers opened here, rather than one per marker
	if opened != nil && opened.blk.open {
		p.extendTo(opened, markerEnd)
	}

	// 3. the rest of the line
	if p.lineConsumed {
		return
	}
	if !p.allClosed && !p.blank && p.tip.Kind == KindParagraph {
		p.addLine() // a lazy continuation line
		return
	}
	p.closeUnmatched()
	if p.blank && container.LastChild != nil {
		container.LastChild.blk.lastLineBlank = true
	}
	t := container.Kind
	lastLineBlank := p.blank &&
		!(t == KindBlockQuote || (t == KindCodeBlock && container.Fence != 0) ||
			(t == KindItem && container.FirstChild == nil && container.Span.Start >= p.ln.start))
	for c := container; c != nil; c = c.Parent {
		c.blk.lastLineBlank = lastLineBlank
		lastLineBlank = false
	}
	switch {
	case acceptsLines(container):
		p.addLine()
		if container.Kind == KindHTMLBlock && container.HTML >= 1 && container.HTML <= 5 &&
			htmlBlockEnds(container.HTML, p.text[p.offset:]) {
			p.lastLineLengthClose(container)
		}
	case p.offset < len(p.text) && !p.blank:
		p.addChild(KindParagraph, p.offset)
		p.advanceNextNonspace()
		p.addLine()
	}
}

func (p *blockParser) lastLineLengthClose(b *Node) {
	p.finalize(b, p.ln.end)
}

// lineEndBefore is where the previous line ended.
func (p *blockParser) lineEndBefore() int {
	e := p.ln.start
	if e > 0 && p.src[e-1] == '\n' {
		e--
	}
	if e > 0 && p.src[e-1] == '\r' {
		e--
	}
	return e
}

type continueResult int

const (
	continueMatched continueResult = iota
	continueFailed
	continueLineDone // the block consumed the whole line (a closing code fence)
)

// continues decides whether the open block b continues on the current line, consuming its prefix.
func (p *blockParser) continues(b *Node) continueResult {
	switch b.Kind {
	case KindBlockQuote:
		if !p.indented && p.peek(p.nextNonspace) == '>' {
			p.advanceNextNonspace()
			p.advanceOffset(1, false)
			if isSpaceOrTab(p.peek(p.offset)) {
				p.advanceOffset(1, true)
			}
			return continueMatched
		}
		return continueFailed
	case KindItem:
		if p.blank {
			if b.FirstChild == nil {
				return continueFailed // an item can begin with at most one blank line
			}
			p.advanceNextNonspace()
			return continueMatched
		}
		if p.indent >= b.List.markerOffset+b.List.padding {
			p.advanceOffset(b.List.markerOffset+b.List.padding, true)
			return continueMatched
		}
		return continueFailed
	case KindHeading, KindThematicBreak:
		return continueFailed
	case KindCodeBlock:
		if b.Fence != 0 {
			if !p.indented && p.peek(p.nextNonspace) == b.Fence {
				if n := closingFence(p.text[p.nextNonspace:], b.Fence); n >= b.blk.fenceLen {
					p.finalize(b, p.ln.end)
					return continueLineDone
				}
			}
			for i := b.blk.fenceOffset; i > 0 && isSpaceOrTab(p.peek(p.offset)); i-- {
				p.advanceOffset(1, true)
			}
			return continueMatched
		}
		if p.indent >= 4 {
			p.advanceOffset(4, true)
			return continueMatched
		}
		if p.blank {
			p.advanceNextNonspace()
			return continueMatched
		}
		return continueFailed
	case KindHTMLBlock:
		if p.blank && (b.HTML == 6 || b.HTML == 7) {
			return continueFailed
		}
		return continueMatched
	case KindParagraph, KindTable:
		if p.blank {
			return continueFailed
		}
		return continueMatched
	case KindList, KindDocument:
		return continueMatched
	}
	return continueFailed
}

// acceptsLines reports the blocks whose content is added line by line rather than parsed further.
func acceptsLines(b *Node) bool {
	switch b.Kind {
	case KindParagraph, KindCodeBlock, KindHTMLBlock, KindTable:
		return true
	}
	return false
}

// mayStartBlock is a quick filter: a non-indented block start begins with one of these bytes.
func mayStartBlock(c byte) bool {
	switch c {
	case '>', '#', '`', '~', '<', '*', '+', '-', '_', '=', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

// closeUnmatched finalizes the open blocks the current line did not continue.
func (p *blockParser) closeUnmatched() {
	if p.allClosed {
		return
	}
	for p.oldtip != p.lastMatchedContainer {
		parent := p.oldtip.Parent
		p.finalize(p.oldtip, -1)
		p.oldtip = parent
	}
	p.allClosed = true
}

// addChild opens a block of kind k at byte offset off of the current line, under the nearest open
// block that can contain it.
func (p *blockParser) addChild(k Kind, off int) *Node {
	for !canContain(p.tip.Kind, k) {
		p.finalize(p.tip, -1)
	}
	n := &Node{Kind: k, Span: Span{p.ln.start + off, p.ln.start + off}, blk: &blockState{open: true}}
	p.tip.AppendChild(n)
	p.tip = n
	return n
}

// extendTo stretches n and its ancestors to end at least at end.
// An ancestor already ending there stops the walk: a parent never ends before its children, so
// deep nesting costs nothing per line.
func (p *blockParser) extendTo(n *Node, end int) {
	if n == p.root {
		return
	}
	n.Span.End = max(n.Span.End, end)
	for a := n.Parent; a != nil && a != p.root && a.Span.End < end; a = a.Parent {
		a.Span.End = end
	}
}

func canContain(parent, child Kind) bool {
	switch parent {
	case KindDocument, KindBlockQuote, KindItem:
		return child != KindItem
	case KindList:
		return child == KindItem
	}
	return false
}

// addLine appends the rest of the current line to the tip's content.
func (p *blockParser) addLine() {
	b := p.tip.blk
	pad := 0
	if p.partiallyConsumedTab {
		p.offset++
		pad = 4 - p.column%4
	}
	s := seg{at: len(b.content), pad: pad, src: p.ln.start + p.offset, n: len(p.text) - p.offset}
	if len(b.segs) == 0 && p.tip.Kind == KindParagraph {
		// a paragraph starts at its first line of text, including one whose definitions were taken out
		p.tip.Span.Start = s.src
	}
	for i := 0; i < pad; i++ {
		b.content = append(b.content, ' ')
	}
	b.content = append(b.content, p.text[p.offset:]...)
	b.content = append(b.content, '\n')
	b.segs = append(b.segs, s)
	// blank lines at the end of an indented code block are not part of it, so they extend nothing
	// until a later line with content does
	if !p.blank || p.tip.Kind != KindCodeBlock || p.tip.Fence != 0 {
		p.extendTo(p.tip, p.ln.end)
	}
}

// finalize closes b. A block that ends on the current line (a closing code fence, the line that
// ends an HTML block) passes that line's end so its span includes it; everything else passes -1 and
// keeps the span its content gave it.
func (p *blockParser) finalize(b *Node, end int) {
	parent := b.Parent
	b.blk.open = false
	if end >= 0 {
		p.extendTo(b, end)
	}
	switch b.Kind {
	case KindParagraph:
		p.finalizeParagraph(b)
	case KindCodeBlock:
		finalizeCode(b)
	case KindHTMLBlock:
		b.Literal = bytes.TrimRight(b.blk.content, "\n")
		if len(b.blk.content) > 0 {
			b.Literal = append(b.Literal, '\n')
		}
	case KindList:
		finalizeList(b)
	case KindTable:
		finalizeTable(b)
	}
	p.tip = parent
}

func finalizeCode(b *Node) {
	c := b.blk.content
	if b.Fence != 0 {
		// the first line is the info string, kept apart at the start
		b.Literal = c
		return
	}
	// an indented code block drops trailing blank lines
	lines := bytes.SplitAfter(c, []byte{'\n'})
	for len(lines) > 0 && len(bytes.TrimSpace(lines[len(lines)-1])) == 0 {
		lines = lines[:len(lines)-1]
	}
	b.Literal = bytes.Join(lines, nil)
	if b.Literal == nil {
		b.Literal = []byte{}
	}
}

// finalizeList decides tightness: a list is loose when a blank line separates two of its items, or
// two blocks directly inside one item.
//
// https://spec.commonmark.org/0.31.2/#loose
func finalizeList(l *Node) {
	tight := true
	for item := l.FirstChild; item != nil && tight; item = item.Next {
		if endsWithBlankLine(item) && item.Next != nil {
			tight = false
			break
		}
		for sub := item.FirstChild; sub != nil; sub = sub.Next {
			if endsWithBlankLine(sub) && (item.Next != nil || sub.Next != nil) {
				tight = false
				break
			}
		}
	}
	l.List.Tight = tight
}

func endsWithBlankLine(b *Node) bool {
	for b != nil {
		if b.blk != nil && b.blk.lastLineBlank {
			return true
		}
		if b.Kind != KindList && b.Kind != KindItem {
			return false
		}
		b = b.LastChild
	}
	return false
}
