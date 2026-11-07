package widget

import (
	"slices"
	"sort"
	"strings"

	htmlparse "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
)

// MARKDOWN BLOCKS — how a Markdown buffer's lines split into the blocks a Rendered view lays out,
// and which of its clusters are metadata (HTML comments) a reading view hides. Both editors split
// a buffer here, so they agree on where a fence, a table, frontmatter or an indented code block
// starts and ends; each toolkit paints the blocks its own way, applying HiddenClusters to its own
// spans or cells.

// Block is lines [From, To) of a buffer.
type Block struct{ From, To int }

// MarkdownBlocks splits a Markdown buffer into blocks. Its zero value splits without hiding
// metadata. It belongs to one renderer or editor: Blocks prepares Meta for the lines it is given,
// and a painter reads Meta for the same lines.
type MarkdownBlocks struct {
	// HideMetadata runs closed comments into blocks of their own, so a reading view can omit them.
	HideMetadata bool
	// Meta is the lines' metadata, prepared by Blocks when HideMetadata is on.
	Meta MarkdownMetadata
}

// Blocks are the fenced code blocks, each a block of its lines (an unclosed one runs to the end),
// frontmatter, tables, indented code blocks, runs of hidden metadata, and every other line alone;
// those overlapping [from, to), clipped to it.
func (m *MarkdownBlocks) Blocks(lines []string, from, to int) []Block {
	if m.HideMetadata {
		m.Meta.Prepare(lines)
	}
	var out []Block
	front, _ := MarkdownFrontmatterEnd(lines)
	for i := 0; i < len(lines); {
		end := i + 1
		if i == 0 && front > 0 {
			end = front
		} else if m.HideMetadata && m.Meta.Hidden(i) {
			for end < len(lines) && m.Meta.Hidden(end) {
				end++
			}
		} else if m.HideMetadata && m.Meta.Continued(i) {
			// The prefix is a comment begun on an earlier line, not a Markdown block opener.
		} else if f, ok := mdOpenFence(lines[i]); ok {
			for end < len(lines) && !markdown.FenceCloses(lines[end], f.ch, f.n) {
				end++
			}
			end = min(end+1, len(lines))
		} else if n := MarkdownTableEnd(lines, i); n > 0 {
			end = n
		} else if MarkdownStartsIndentedCode(lines, i) {
			// an indented code block: it cannot interrupt a paragraph, so it starts at the top or
			// after a blank line, and runs while lines stay indented or blank, its trailing
			// blanks left out
			for end < len(lines) && (MarkdownIndentedCode(lines[end]) || mdIsBlank(lines[end])) {
				end++
			}
			for end > i+1 && mdIsBlank(lines[end-1]) {
				end--
			}
		}
		if end > from && i < to {
			out = append(out, Block{From: max(i, from), To: min(end, to)})
		}
		i = end
	}
	return out
}

// ReflowFrom includes earlier lines whose metadata presentation changed in the last source update.
// Repeated Blocks calls for that same source keep the answer (InCode also calls Blocks).
func (m *MarkdownBlocks) ReflowFrom(changedLine int) int {
	if m.HideMetadata && m.Meta.changed >= 0 {
		return min(changedLine, m.Meta.changed)
	}
	return changedLine
}

// InCode reports whether line ln is inside a code block, fenced or indented, or frontmatter, as
// the blocks split the lines: there "- x" is code, not a list item.
func (m *MarkdownBlocks) InCode(lines []string, ln int) bool {
	if front, _ := MarkdownFrontmatterEnd(lines); ln < front {
		return true
	}
	for _, b := range m.Blocks(lines, 0, len(lines)) { // the whole document: a block's From is its start
		if b.From <= ln && ln < b.To {
			_, fenced := mdOpenFence(lines[b.From])
			return fenced || MarkdownStartsIndentedCode(lines, b.From)
		}
	}
	return false
}

// InTable reports whether line ln is a row of a table: its source is not a list, whatever it
// starts with.
func (m *MarkdownBlocks) InTable(lines []string, ln int) bool {
	for _, b := range m.Blocks(lines, 0, len(lines)) {
		if b.From <= ln && ln < b.To {
			return MarkdownTableEnd(lines, b.From) > 0
		}
	}
	return false
}

// MarkdownFrontmatterEnd is where a document's frontmatter ends, past its closing line, as the
// Raw view's highlighter reads it: a "---" first line, closed by "---" or "...". Unclosed (while it
// is being written), it runs to the end. 0: none.
func MarkdownFrontmatterEnd(lines []string) (end int, closed bool) {
	if len(lines) == 0 || !markdown.FrontmatterOpens(lines[0]) {
		return 0, false
	}
	for k := 1; k < len(lines); k++ {
		if markdown.FrontmatterCloses(lines[k]) {
			return k + 1, true
		}
	}
	return len(lines), false
}

// MarkdownTableEnd is where a table starting at line i ends, past its last row, as GFM reads one: a
// header row with a pipe, a delimiter row with as many cells, then body rows until a blank line or
// the start of another block (a heading, a quote, a fence, a rule, a list item), a row with no pipe
// included. 0: no table there.
func MarkdownTableEnd(lines []string, i int) int {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") || MarkdownIndentedCode(lines[i]) || mdStartsBlock(lines[i]) {
		return 0
	}
	delim, _ := MarkdownGFMRow(lines[i+1])
	aligns, ok := MarkdownDelimiterAligns(delim)
	if !ok {
		return 0
	}
	head, _ := MarkdownGFMRow(lines[i])
	cells, ok := MarkdownTableCells(head)
	if !ok {
		cells = nil // gui's tableCells: a row it can't split has no cells
	}
	if len(aligns) != len(cells) {
		return 0
	}
	end := i + 2
	for end < len(lines) && !mdIsBlank(lines[end]) && !mdStartsBlock(lines[end]) {
		end++
	}
	return end
}

// MarkdownGFMRow is a table row with its outer pipes, as GFM reads one whose pipes are optional,
// and how many bytes the opening pipe added before the line's own text (0 or 1).
func MarkdownGFMRow(line string) (row string, shift int) {
	t := strings.TrimRight(line, " \t")
	if !strings.HasPrefix(strings.TrimLeft(t, " \t"), "|") {
		line, shift = "|"+line, 1
	}
	if !strings.HasSuffix(t, "|") || strings.HasSuffix(t, "\\|") {
		line += "|" // no closing pipe, or an escaped one: the row's text runs to its end
	}
	return line, shift
}

// MarkdownQuoteDepth reads a line's blockquote marks: how many '>' (each with the space after it)
// before its text, and where the text starts.
func MarkdownQuoteDepth(line string) (depth, off int) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	for i < len(line) && line[i] == '>' {
		depth++
		i++
		if i < len(line) && line[i] == ' ' {
			i++
		}
	}
	if depth == 0 {
		return 0, 0
	}
	return depth, i
}

// MarkdownStartsIndentedCode reports whether line i starts an indented code block: indented as
// code, at the top or after a blank line, and not inside a list, where indentation nests an item
// or continues one.
func MarkdownStartsIndentedCode(lines []string, i int) bool {
	if !MarkdownIndentedCode(lines[i]) || (i > 0 && !mdIsBlank(lines[i-1])) {
		return false
	}
	for j := i - 1; j >= 0; j-- {
		switch {
		case mdIsBlank(lines[j]):
		case mdIsListItem(lines[j]):
			return false
		case lines[j][0] == ' ' || lines[j][0] == '\t':
		default:
			return true
		}
	}
	return true
}

// MarkdownIndentedCode reports a line indented as code: four spaces or a tab before any text.
func MarkdownIndentedCode(line string) bool {
	if mdIsBlank(line) {
		return false
	}
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

// mdFence is a fenced code block's opening: its character and its length.
type mdFence struct {
	ch byte
	n  int
}

// mdOpenFence reads a line as a fence opening (markdown.FenceOpens).
func mdOpenFence(line string) (mdFence, bool) {
	ch, n, ok := markdown.FenceOpens(line)
	return mdFence{ch: ch, n: n}, ok
}

// mdStartsBlock reports a line that starts a block of its own, which ends a table: a heading, a
// quote, a fence, a thematic break or a list item.
func mdStartsBlock(line string) bool {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 {
		return false
	}
	if _, ok := mdOpenFence(line); ok {
		return true
	}
	if d, _ := MarkdownQuoteDepth(line); d > 0 {
		return true
	}
	if n := len(s) - len(strings.TrimLeft(s, "#")); n >= 1 && n <= 6 && (n == len(s) || s[n] == ' ') {
		return true
	}
	return markdown.ThematicBreak(strings.TrimSpace(line)) || mdIsListItem(line)
}

// mdIsListItem reports whether the line begins with a valid Markdown list item marker.
func mdIsListItem(line string) bool {
	_, _, ok := MarkdownListItem(line)
	return ok
}

// mdIsBlank reports whether the line contains only whitespace characters.
func mdIsBlank(line string) bool { return strings.TrimSpace(line) == "" }

// MarkdownMetadata caches parser-identified comments in source-cluster coordinates. It never
// strips the buffer: a span after a comment still addresses the same source line and cluster. It
// belongs to one renderer or editor: a painter reads the mask its Blocks call prepared.
type MarkdownMetadata struct {
	source    string
	ready     bool
	lines     []string
	changed   int
	clusters  map[int][]bool
	hidden    map[int]bool
	continued map[int]bool
}

// Hidden reports whether line ln is wholly metadata: every cluster that isn't blank is hidden.
func (m *MarkdownMetadata) Hidden(ln int) bool { return m.hidden[ln] }

// Continued reports whether line ln starts inside a comment begun on an earlier line.
func (m *MarkdownMetadata) Continued(ln int) bool { return m.continued[ln] }

// Changed is the first line whose metadata presentation changed in the last Prepare, or -1.
func (m *MarkdownMetadata) Changed() int { return m.changed }

// HiddenClusters is line ln's mask, one entry a source cluster (tui.Graphemes), true where the
// cluster is metadata; ok false when the line holds none.
func (m *MarkdownMetadata) HiddenClusters(ln int) (mask []bool, ok bool) {
	mask, ok = m.clusters[ln]
	return mask, ok
}

// Prepare reads lines' metadata, again only when they changed since the last call.
func (m *MarkdownMetadata) Prepare(lines []string) {
	first, last := 0, len(lines)-1
	if m.ready {
		for first < min(len(m.lines), len(lines)) && m.lines[first] == lines[first] {
			first++
		}
		if first == len(lines) && len(m.lines) == len(lines) {
			return
		}
		if len(m.lines) == len(lines) {
			for last >= first && m.lines[last] == lines[last] {
				last--
			}
			if m.plainEdit(lines, first, last) {
				// Ordinary text edits cannot change the comment delimiters or block context.
				// A line wholly inside a comment only needs its cluster mask resized.
				m.changed = -1
				for ln := first; ln <= last; ln++ {
					if m.hidden[ln] {
						mask := make([]bool, len(clusters(lines[ln])))
						for i := range mask {
							mask[i] = true
						}
						if !slices.Equal(mask, m.clusters[ln]) && m.changed < 0 {
							m.changed = ln
						}
						m.clusters[ln] = mask
					}
				}
				m.lines = slices.Clone(lines)
				return
			}
		}
	}
	old := *m
	source := strings.Join(lines, "\n")
	*m = MarkdownMetadata{source: source, ready: true, lines: slices.Clone(lines), changed: -1, clusters: map[int][]bool{}, hidden: map[int]bool{}, continued: map[int]bool{}}
	starts := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		starts[i] = starts[i-1] + len(lines[i-1]) + 1
	}
	type byteRange struct{ from, to int }
	byLine := map[int][]byteRange{}
	add := func(start, end int) {
		ln := max(sort.Search(len(starts), func(i int) bool { return starts[i] > start })-1, 0)
		for ; ln < len(lines) && starts[ln] < end; ln++ {
			from, to := max(start-starts[ln], 0), min(end-starts[ln], len(lines[ln]))
			byLine[ln] = append(byLine[ln], byteRange{from, to})
			if start < starts[ln] {
				m.continued[ln] = true
			}
		}
	}
	src := []byte(source)
	doc := markdown.Parse(src, markdown.WithGFM(), markdown.WithObsidian())
	var walk func(*markdown.Node)
	walk = func(n *markdown.Node) {
		switch n.Kind {
		case markdown.KindRawHTML:
			if strings.HasPrefix(string(src[n.Span.Start:n.Span.End]), "<!--") {
				add(n.Span.Start, n.Span.End)
			}
		case markdown.KindHTMLBlock:
			part := src[n.Span.Start:n.Span.End]
			// HTML's tokenizer knows comments from quoted attributes and script/style raw text.
			tokens := htmlparse.NewTokenizer(part, htmlparse.Limits{MaxSource: max(len(part), 1)})
			for tok, ok := tokens.Next(); ok; tok, ok = tokens.Next() {
				if tok.Kind == htmlparse.Comment && strings.HasPrefix(string(part[tok.Span[0]:tok.Span[1]]), "<!--") {
					add(n.Span.Start+tok.Span[0], n.Span.Start+tok.Span[1])
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.Next {
			walk(c)
		}
	}
	walk(doc.Root)
	for ln, ranges := range byLine {
		var mask []bool
		at, whole := 0, true
		for c := range tui.Graphemes(lines[ln]) {
			end := at + len(c)
			hidden := false
			for _, r := range ranges {
				if r.from < end && at < r.to {
					hidden = true
					break
				}
			}
			mask = append(mask, hidden)
			if !hidden && strings.TrimSpace(c) != "" {
				whole = false
			}
			at = end
		}
		m.clusters[ln], m.hidden[ln] = mask, whole
	}
	if old.ready {
		for ln := 0; ln < max(len(old.lines), len(lines)); ln++ {
			if old.hidden[ln] != m.hidden[ln] || old.continued[ln] != m.continued[ln] || !slices.Equal(old.clusters[ln], m.clusters[ln]) {
				m.changed = ln
				break
			}
		}
	}
}

// plainEdit is deliberately conservative: changes to block starts, indentation, HTML, escaping or
// delimiter punctuation need the parser. Unchanged inline markup need not reparse the note when
// only its surrounding words changed. Line insertions and deletions take the parser path.
func (m *MarkdownMetadata) plainEdit(lines []string, first, last int) bool {
	for ln := first; ln <= last; ln++ {
		a, b := m.lines[ln], lines[ln]
		if a == b {
			continue
		}
		if mdIsBlank(a) != mdIsBlank(b) || mdStartsBlock(a) || mdStartsBlock(b) || MarkdownIndentedCode(a) || MarkdownIndentedCode(b) {
			return false
		}
		// Existing pipes matter too: typing only dashes between them can turn this
		// line into a table delimiter and change comment parsing on earlier lines.
		if strings.ContainsAny(a+b, "<>\"'|") {
			return false
		}
		if strings.Trim(a, "= \t") == "" || strings.Trim(b, "= \t") == "" {
			return false
		}
		if a[:len(a)-len(strings.TrimLeft(a, " \t"))] != b[:len(b)-len(strings.TrimLeft(b, " \t"))] {
			return false
		}
		from := 0
		for from < min(len(a), len(b)) && a[from] == b[from] {
			from++
		}
		endA, endB := len(a), len(b)
		for endA > from && endB > from && a[endA-1] == b[endB-1] {
			endA--
			endB--
		}
		if strings.ContainsAny(a[from:endA]+b[from:endB], "`~\\[]()|:\n\r") {
			return false
		}
		// Removing/inserting the gap between punctuation can join or split a delimiter run.
		if from > 0 && endA < len(a) && strings.ContainsRune("`~\\[]()", rune(a[from-1])) && strings.ContainsRune("`~\\[]()", rune(a[endA])) {
			return false
		}
	}
	return true
}
