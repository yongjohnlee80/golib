package chunk

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/search"
)

// Version is how this package's chunkers cut, the same for each: a change that would cut the same
// document differently changes it.
const Version = "5"

// The chunk sizes, in estimated tokens (Tokens).
const (
	// DefaultTokens is the section budget of a limit of 0: no chunk, breadcrumb included, is
	// estimated above it.
	DefaultTokens = 512
	targetTokens  = 350 // a chunk closes once it reaches this share of the budget (350 of 512)
	maxTokens     = DefaultTokens
)

// unit is a block the packer places whole: a code block, a table and a list item are never split.
// holes are the link reference definitions inside it, which are not text.
type unit struct {
	start, end int
	holes      []markdown.Span
	tokens     int
	body       string // nonempty for a split block with synthetic table header or code fence
	kind       markdown.Kind
}

// Text cuts plain text into chunks: paragraphs, separated by blank lines, packed in order like a
// Markdown section's blocks, a paragraph over the budget split along its lines. The breadcrumb is
// title. A text with no words is one chunk of no body.
func Text(src []byte, title string, limit int) []search.Chunk {
	if limit <= 0 {
		limit = maxTokens
	}
	crumb := boundedCrumb(title, limit)
	budget := max(1, limit-tokensOf([]byte(crumb+"\n"), 0, len(crumb)+1))
	var units []unit
	start := -1
	for offset := 0; offset < len(src); {
		end := offset
		for end < len(src) && src[end] != '\n' {
			end++
		}
		if end < len(src) {
			end++
		}
		if len(bytes.TrimSpace(src[offset:end])) == 0 {
			if start >= 0 {
				piece := unit{start: start, end: offset, tokens: tokensOf(src, start, offset), kind: markdown.KindParagraph}
				units = append(units, splitOversized(src, piece, piece.kind, budget)...)
				start = -1
			}
		} else if start < 0 {
			start = offset
		}
		offset = end
	}
	if start >= 0 {
		piece := unit{start: start, end: len(src), tokens: tokensOf(src, start, len(src)), kind: markdown.KindParagraph}
		units = append(units, splitOversized(src, piece, piece.kind, budget)...)
	}
	out := packLimited(src, units, crumb, 0, limit)
	if len(out) == 0 {
		out = append(out, newChunk(0, crumb, "", 0, 0))
	}
	return out
}

// Markdown cuts a parsed document into heading-aware chunks of at most limit estimated tokens
// (DefaultTokens when 0). Sections split at every heading; a section's blocks are packed in order
// into chunks of about 350/512 of the limit; a code block, a table and a list item are never split
// unless one alone is over the limit, when it is split along its lines (a table repeating its
// header, a code block its fence). The breadcrumb names title and the headings above; chunks do not
// overlap, since the breadcrumb carries the context. Link reference definitions and frontmatter are
// not text. A document with no text is one chunk of no body, so its title can still be found.
func Markdown(doc *markdown.Document, title string, limit int) []search.Chunk {
	if limit <= 0 {
		limit = maxTokens
	}
	src := doc.Source
	var out []search.Chunk
	var stack []string // the heading texts above, by level
	var units []unit
	flush := func() {
		heads := nonEmpty(stack)
		if len(heads) > 0 && heads[0] == title {
			heads = heads[1:] // the title came from this heading: say it once
		}
		crumb := strings.Join(append([]string{title}, heads...), " > ")
		crumb = boundedCrumb(crumb, limit)
		budget := max(1, limit-tokensOf([]byte(crumb+"\n"), 0, len(crumb)+1)-2)
		var parts []unit
		for _, u := range units {
			parts = append(parts, splitOversized(src, u, u.kind, budget)...)
		}
		out = append(out, packLimited(src, parts, crumb, len(out), limit)...)
		units = units[:0]
	}
	for n := doc.Root.FirstChild; n != nil; n = n.Next {
		switch n.Kind {
		case markdown.KindLinkRefDef, markdown.KindFrontmatter:
			continue
		case markdown.KindHeading:
			flush()
			for len(stack) < n.Level {
				stack = append(stack, "")
			}
			stack = append(stack[:n.Level-1], plainText(n, src))
			continue
		case markdown.KindList:
			if tokensOf(src, n.Span.Start, n.Span.End) > limit {
				for it := n.FirstChild; it != nil; it = it.Next {
					units = append(units, unitOf(src, it))
				}
				continue
			}
		}
		units = append(units, unitOf(src, n))
	}
	flush()
	if len(out) == 0 {
		// a note with no text (a title, frontmatter) is still a note: one chunk of no body carries its
		// title, so it can be found
		out = append(out, newChunk(0, boundedCrumb(title, limit), "", 0, 0))
	}
	for i := range out {
		out[i].Ord = i
	}
	return out
}

func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// unitOf is block n as a unit. A definition is a child of the block holding its paragraph (a list
// item, a block quote), so one can sit anywhere below n.
func unitOf(src []byte, n *markdown.Node) unit {
	u := unit{start: n.Span.Start, end: n.Span.End, tokens: tokensOf(src, n.Span.Start, n.Span.End), kind: n.Kind}
	var walk func(*markdown.Node)
	walk = func(n *markdown.Node) {
		for c := n.FirstChild; c != nil; c = c.Next {
			if c.Kind == markdown.KindLinkRefDef {
				u.holes = append(u.holes, c.Span)
				u.tokens -= tokensOf(src, c.Span.Start, c.Span.End)
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return u
}

// Tokens is the estimated tokens of text: the larger of 1.3 per word and one per four bytes. It is
// a stated estimate, the same for every model, not a count.
func Tokens(text []byte) int { return tokensOf(text, 0, len(text)) }

func tokensOf(src []byte, start, end int) int {
	b := src[start:end]
	return max(len(strings.Fields(string(b)))*13/10, (len(b)+3)/4)
}

// packLimited places units in order: a chunk closes once it reaches its share of the budget, or
// before a unit that would take it past the budget.
func packLimited(src []byte, units []unit, crumb string, base, limit int) []search.Chunk {
	crumb = boundedCrumb(crumb, limit)
	budget := max(1, limit-tokensOf([]byte(crumb+"\n"), 0, len(crumb)+1))
	var out []search.Chunk
	var cur []unit
	tokens := 0
	emit := func() {
		if len(cur) == 0 {
			return
		}
		start, end := cur[0].start, cur[len(cur)-1].end
		body := strings.TrimSpace(text(src, cur))
		if body != "" {
			out = append(out, newChunk(base+len(out), crumb, body, start, end))
		}
		cur, tokens = cur[:0], 0
	}
	for _, u := range units {
		if len(cur) > 0 && tokens+u.tokens > budget {
			emit()
		}
		cur = append(cur, u)
		tokens += u.tokens
		if tokens >= min(limit*targetTokens/maxTokens, budget) {
			emit()
		}
	}
	emit()
	return out
}

func boundedCrumb(crumb string, limit int) string {
	for tokensOf([]byte(crumb), 0, len(crumb)) >= limit && len(crumb) > 0 {
		_, size := utf8.DecodeLastRuneInString(crumb)
		crumb = crumb[:len(crumb)-size]
	}
	return crumb
}

// text is the units' source with their holes cut out. Whitespace between two units is kept; anything
// else there is a definition (a block of its own), which becomes a blank line.
func text(src []byte, units []unit) string {
	var b strings.Builder
	for i, u := range units {
		if i > 0 {
			if gap := src[units[i-1].end:u.start]; len(bytes.TrimSpace(gap)) == 0 {
				b.Write(gap)
			} else {
				b.WriteString("\n\n")
			}
		}
		at := u.start
		if u.body != "" {
			b.WriteString(u.body)
			continue
		}
		for _, h := range u.holes {
			b.Write(src[at:h.Start])
			at = h.End
		}
		b.Write(src[at:u.end])
	}
	return b.String()
}

// splitOversized retains source offsets for each fragment. Synthetic markdown syntax
// belongs in the embedded body, never in the fragment's source span.
func splitOversized(src []byte, u unit, kind markdown.Kind, limit int) []unit {
	if u.tokens <= limit {
		return []unit{u}
	}
	bodyText := text(src, []unit{u})
	prefix, suffix := "", ""
	if kind == markdown.KindTable {
		lines := strings.SplitAfter(bodyText, "\n")
		if len(lines) >= 3 {
			prefix = lines[0] + lines[1]
			if tokensOf([]byte(prefix), 0, len(prefix)) > limit/3 {
				// A header larger than the budget cannot be repeated in full.
				// Retain a short column-name excerpt; the original remains in its source span.
				header := strings.TrimSpace(lines[0])
				for len(header) > 0 && tokensOf([]byte(header), 0, len(header)) > limit/4 {
					_, n := utf8.DecodeLastRuneInString(header)
					header = header[:len(header)-n]
				}
				prefix = header + "… |\n|---|\n"
			}
		}
	} else if kind == markdown.KindCodeBlock {
		line, _, ok := strings.Cut(bodyText, "\n")
		if ok && (strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~")) {
			prefix, suffix = line+"\n", "\n"+strings.TrimSpace(line)[:3]
		}
	}
	// A table's first fragment already contains its header; subsequent fragments repeat it.
	var out []unit
	at := u.start
	for at < u.end {
		p := ""
		if len(out) > 0 {
			p = prefix
		}
		remaining := limit - tokensOf([]byte(p+suffix), 0, len(p+suffix))
		if remaining < 1 {
			remaining = 1
		}
		end := at
		for end < u.end {
			next := end
			if i := bytes.IndexByte(src[next:u.end], '\n'); i >= 0 {
				next += i + 1
			} else {
				next = u.end
			}
			if tokensOf(src, at, next) > remaining {
				break
			}
			end = next
		}
		if end == at { // An indivisible row/line: split at a UTF-8 boundary.
			for end < u.end {
				_, n := utf8.DecodeRune(src[end:u.end])
				if tokensOf(src, at, end+n) > remaining && end > at {
					break
				}
				end += n
				if tokensOf(src, at, end) >= remaining {
					break
				}
			}
		}
		if end <= at {
			end = min(at+1, u.end)
		}
		part := text(src, []unit{{start: at, end: end, holes: clippedHoles(u.holes, at, end)}})
		if kind == markdown.KindCodeBlock && prefix != "" {
			if len(out) > 0 {
				part = prefix + part
			}
			if !strings.HasSuffix(strings.TrimSpace(part), strings.TrimSpace(suffix)) {
				part += suffix
			}
		} else {
			part = p + part
		}
		out = append(out, unit{start: at, end: end, body: part, tokens: tokensOf([]byte(part), 0, len(part))})
		at = end
	}
	return out
}

func clippedHoles(holes []markdown.Span, start, end int) []markdown.Span {
	var out []markdown.Span
	for _, h := range holes {
		if h.End <= start || h.Start >= end {
			continue
		}
		out = append(out, markdown.Span{Start: max(h.Start, start), End: min(h.End, end)})
	}
	return out
}

func newChunk(ord int, crumb, body string, start, end int) search.Chunk {
	return search.Chunk{Ord: ord, Breadcrumb: crumb, Body: body, ByteStart: start, ByteEnd: end}
}

// snippetBytes is the most a Snippet keeps of a chunk's text.
const snippetBytes = 200

// Snippet is the start of a chunk's text, at most 200 bytes, cut at a rune boundary and marked
// with "…" when cut: a semantic hit's snippet.
func Snippet(body string) string {
	if len(body) <= snippetBytes {
		return body
	}
	cut := snippetBytes
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return body[:cut] + "…"
}

// plainText is a node's text without markup: what a heading says.
func plainText(n *markdown.Node, src []byte) string {
	var b strings.Builder
	var walk func(*markdown.Node)
	walk = func(n *markdown.Node) {
		for c := n.FirstChild; c != nil; c = c.Next {
			switch c.Kind {
			case markdown.KindText, markdown.KindCodeSpan:
				b.Write(c.Text(src))
			case markdown.KindSoftBreak, markdown.KindHardBreak:
				b.WriteByte(' ')
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}
