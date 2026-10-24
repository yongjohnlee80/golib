package widget

import (
	"slices"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/gui/flow"
	htmlparse "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
)

// markdownMetadata caches parser-identified comments in source-cluster coordinates. It never
// strips the buffer: a span after a comment still addresses the same source line and cluster.
// It belongs to one renderer/editor: LayOut reads the mask its Blocks call prepared.
type markdownMetadata struct {
	source    string
	ready     bool
	lines     []string
	changed   int
	clusters  map[int][]bool
	hidden    map[int]bool
	continued map[int]bool
}

func (m *markdownMetadata) prepare(lines []string) {
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
						mask := make([]bool, clusters(lines[ln]))
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
	*m = markdownMetadata{source: source, ready: true, lines: slices.Clone(lines), changed: -1, clusters: map[int][]bool{}, hidden: map[int]bool{}, continued: map[int]bool{}}
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

// plainEdit is deliberately conservative: changes to block starts, indentation, HTML, escaping
// or delimiter punctuation need the parser. Unchanged inline markup need not reparse the note
// when only its surrounding words changed. Line insertions/deletions take the parser path.
func (m *markdownMetadata) plainEdit(lines []string, first, last int) bool {
	for ln := first; ln <= last; ln++ {
		a, b := m.lines[ln], lines[ln]
		if a == b {
			continue
		}
		if isBlank(a) != isBlank(b) || startsBlock(a) || startsBlock(b) || indentedCode(a) || indentedCode(b) {
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

func (m *markdownMetadata) mask(spans []flow.Span, ln int) []flow.Span {
	mask, ok := m.clusters[ln]
	if !ok {
		return spans
	}
	var out []flow.Span
	for _, sp := range spans {
		if sp.Text == "" || sp.Atom != nil {
			out = append(out, sp)
			continue
		}
		start, offset, col := 0, 0, sp.Col
		hidden := sp.Hidden || (col < len(mask) && col >= 0 && mask[col])
		for c := range tui.Graphemes(sp.Text) {
			next := sp.Hidden || (col < len(mask) && col >= 0 && mask[col])
			if next != hidden {
				s := sp
				s.Text = sp.Text[start:offset]
				s.Col = sp.Col + clusters(sp.Text[:start])
				s.Hidden = hidden
				if start > 0 {
					s.Room = 0
				}
				out = append(out, s)
				start, hidden = offset, next
			}
			offset += len(c)
			col++
		}
		s := sp
		s.Text = sp.Text[start:]
		s.Col = sp.Col + clusters(sp.Text[:start])
		s.Hidden = hidden
		if start > 0 {
			s.Room = 0
		}
		out = append(out, s)
	}
	return out
}
