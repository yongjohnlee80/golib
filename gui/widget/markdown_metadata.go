package widget

import (
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/gui/flow"
	htmlparse "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
)

// markdownMetadata caches parser-identified comments in source-cluster coordinates. It never
// strips the buffer: a span after a comment still addresses the same source line and cluster.
type markdownMetadata struct {
	source    string
	ready     bool
	clusters  map[int][]bool
	hidden    map[int]bool
	continued map[int]bool
}

func (m *markdownMetadata) prepare(lines []string) {
	source := strings.Join(lines, "\n")
	if m.ready && m.source == source {
		return
	}
	*m = markdownMetadata{source: source, ready: true, clusters: map[int][]bool{}, hidden: map[int]bool{}, continued: map[int]bool{}}
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
	doc := markdown.Parse(src, markdown.GFM(), markdown.Obsidian())
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
