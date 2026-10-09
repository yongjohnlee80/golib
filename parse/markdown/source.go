package markdown

import (
	"fmt"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/parse/internal/states"
	"github.com/yongjohnlee80/golib/parse/languages"
	"sort"
	"strings"
)

const embeddedState highlight.State = 1 << 20

type codeFence struct {
	character      byte
	length, offset int
	language       string
	child          highlight.State
}
type source struct {
	catalog      *highlight.Catalog
	children     map[string]highlight.Source
	store        *states.Store[codeFence]
	collectNext  int
	collectRound []string
	collectMore  bool
}

// Definition creates Markdown behavior that borrows source-language providers
// from the supplied immutable catalog. The legacy Highlighter remains unchanged.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "Markdown", Extensions: []string{"*.md", "*.markdown"}, Aliases: []string{"markdown", "md"}, DocumentAdapter: true, Highlighter: Highlighter(), SourceFactory: func(catalog *highlight.Catalog) highlight.Source {
		if catalog == nil {
			catalog = highlight.NewRepository(languages.Definitions()...).Snapshot()
		}
		s := &source{catalog: catalog, children: map[string]highlight.Source{}}
		s.store = states.New(func(f codeFence) string {
			return fmt.Sprintf("%d/%d/%d/%q/%d", f.character, f.length, f.offset, f.language, f.child)
		}, states.WithDependencies(s.retainChild, s.releaseChild))
		return highlight.Source{Highlighter: s, Indenter: s, States: s}
	}}
}

func (s *source) retainChild(f codeFence) {
	if child := s.children[f.language]; child.States != nil {
		child.States.Retain(f.child)
	}
}
func (s *source) releaseChild(f codeFence) {
	if child := s.children[f.language]; child.States != nil {
		child.States.Release(f.child)
	}
}
func (s *source) Retain(id highlight.State) {
	if id >= embeddedState {
		s.store.Retain(id - embeddedState)
	}
}
func (s *source) Release(id highlight.State) {
	if id >= embeddedState {
		s.store.Release(id - embeddedState)
	}
}
func (s *source) Collect(budget int) bool {
	if budget <= 0 {
		return true
	}
	if s.collectRound == nil {
		s.collectRound = []string{""}
		names := []string{}
		for name, child := range s.children {
			if child.States != nil {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		s.collectRound = append(s.collectRound, names...)
		s.collectNext = 0
		s.collectMore = false
	}
	remaining := len(s.collectRound) - s.collectNext
	for budget > 0 && s.collectNext < len(s.collectRound) {
		share := max(1, budget/remaining)
		name := s.collectRound[s.collectNext]
		if name == "" {
			s.collectMore = s.store.Collect(share) || s.collectMore
		} else {
			s.collectMore = s.children[name].States.Collect(share) || s.collectMore
		}
		s.collectNext++
		remaining--
		budget -= share
	}
	if s.collectNext < len(s.collectRound) {
		return true
	}
	more := s.collectMore
	s.collectRound = nil
	return more
}

func (s *source) child(name string) highlight.Source {
	if value, ok := s.children[name]; ok {
		return value
	}
	d, ok := s.catalog.Definition(name)
	if !ok {
		return highlight.Source{}
	}
	value := d.NewSource(s.catalog)
	s.children[name] = value
	return value
}

func codeOffset(line string, n int) int {
	at := 0
	for at < len(line) && at < n && line[at] == ' ' {
		at++
	}
	return at
}

func (s *source) HighlightBlock(line string, previous highlight.State) ([]highlight.Span, highlight.State) {
	if previous >= embeddedState {
		f, ok := s.store.Get(previous - embeddedState)
		if !ok {
			return highlightMarkdown(line, stateBody)
		}
		if fenceCloses(line, f.character, f.length) {
			return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Comment}}, stateBody
		}
		child := s.child(f.language)
		if child.Highlighter == nil {
			if line == "" {
				return nil, previous
			}
			return []highlight.Span{{Start: 0, End: len(line), Style: highlight.String}}, previous
		}
		offset := codeOffset(line, f.offset)
		spans, next := child.Highlighter.HighlightBlock(line[offset:], f.child)
		spans = append([]highlight.Span(nil), spans...)
		for i := range spans {
			spans[i].Start += offset
			spans[i].End += offset
		}
		f.child = next
		return spans, embeddedState + s.store.Intern(f)
	}
	if previous != stateFrontmatter && previous != stateComment {
		if ch, n, ok := fenceOpens(line); ok {
			offset := len(line) - len(strings.TrimLeft(line, " "))
			info := strings.Fields(line[offset+n:])
			name := ""
			if len(info) > 0 {
				if d, found := s.catalog.DefinitionForLanguage(info[0]); found && !d.DocumentAdapter {
					name = d.Name
					s.child(name)
				}
			}
			f := codeFence{character: ch, length: n, offset: offset, language: name}
			return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Comment}}, embeddedState + s.store.Intern(f)
		}
	}
	return highlightMarkdown(line, previous)
}

func (s *source) Indent(r indent.Request) (indent.Decision, bool) {
	if !r.StateKnown || highlight.State(r.PreviousState) < embeddedState {
		return indent.Decision{}, false
	}
	f, ok := s.store.Get(highlight.State(r.PreviousState) - embeddedState)
	if !ok || fenceCloses(r.Line, f.character, f.length) {
		return indent.Decision{}, false
	}
	child := s.child(f.language)
	if child.Indenter == nil {
		return indent.Decision{}, false
	}
	offset := codeOffset(r.Line, f.offset)
	prefix := r.Line[:offset]
	r.Line = r.Line[offset:]
	r.Column = max(0, r.Column-offset)
	r.PreviousState = int(f.child)
	d, ok := child.Indenter.Indent(r)
	if !ok {
		return d, false
	}
	d.Prefix = prefix + d.Prefix
	if d.SplitClosing {
		d.ClosingPrefix = prefix + d.ClosingPrefix
	}
	return d, true
}
