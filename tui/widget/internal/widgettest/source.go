package widgettest

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
)

type SourceTrace struct {
	T        TB
	Refs     map[highlight.State]int
	Calls    int
	released int
}

func (s *SourceTrace) HighlightBlock(line string, previous highlight.State) ([]highlight.Span, highlight.State) {
	s.Calls++
	return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Keyword}}, previous + 1
}

func (s *SourceTrace) Retain(id highlight.State) {
	if id != 0 {
		s.Refs[id]++
	}
}

func (s *SourceTrace) Release(id highlight.State) {
	if id == 0 {
		return
	}
	if s.Refs[id] <= 0 {
		s.T.Fatalf("state %d released without a lease", id)
	}
	s.Refs[id]--
	s.released++
}

func (s *SourceTrace) Collect(int) bool { return false }

func (s *SourceTrace) Indent(r indent.Request) (indent.Decision, bool) {
	if !r.StateKnown {
		return indent.Decision{}, false
	}
	return indent.Decision{Prefix: "  "}, true
}
