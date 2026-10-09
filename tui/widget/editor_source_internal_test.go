package widget

import (
	"fmt"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/parse/shell"
	"strings"
	"testing"
	"testing/fstest"
)

type sourceTrace struct {
	t        *testing.T
	refs     map[highlight.State]int
	calls    int
	released int
}

func (s *sourceTrace) HighlightBlock(line string, previous highlight.State) ([]highlight.Span, highlight.State) {
	s.calls++
	return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Keyword}}, previous + 1
}
func (s *sourceTrace) Retain(id highlight.State) {
	if id != 0 {
		s.refs[id]++
	}
}
func (s *sourceTrace) Release(id highlight.State) {
	if id == 0 {
		return
	}
	if s.refs[id] <= 0 {
		s.t.Fatalf("state %d released without a lease", id)
	}
	s.refs[id]--
	s.released++
}
func (s *sourceTrace) Collect(int) bool { return false }
func (s *sourceTrace) Indent(r indent.Request) (indent.Decision, bool) {
	if !r.StateKnown {
		return indent.Decision{}, false
	}
	return indent.Decision{Prefix: "  "}, true
}

func TestSourceStateLeasesEndOnFramesAndDocumentReplacement(t *testing.T) {
	var traces []*sourceTrace
	c := NewEditorCore(CoreSourceFactory(func() highlight.Source {
		s := &sourceTrace{t: t, refs: map[highlight.State]int{}}
		traces = append(traces, s)
		return highlight.Source{Highlighter: s, Indenter: s, States: s}
	}))
	c.SetValue("one\ntwo\nthree")
	f := c.BeginHighlight(0)
	f.Styles(2)
	f.Close()
	old := traces[len(traces)-1]
	c.SetValue("new")
	for id, n := range old.refs {
		if n != 0 {
			t.Fatalf("old document state %d still has %d leases", id, n)
		}
	}
	f = c.BeginHighlight(0)
	f.Styles(0)
	f.Close()
	current := traces[len(traces)-1]
	c.SetHighlighter(nil)
	for id, n := range current.refs {
		if n != 0 {
			t.Fatalf("cleared source state %d has %d leases", id, n)
		}
	}
}

func TestFindPaintDoesNotChangeDeepImmediateIndent(t *testing.T) {
	created := 0
	var trace *sourceTrace
	c := NewEditorCore(CoreModal(false), CoreAutoIndent(true), CoreSourceFactory(func() highlight.Source {
		created++
		trace = &sourceTrace{t: t, refs: map[highlight.State]int{}}
		return highlight.Source{Highlighter: trace, Indenter: trace, States: trace}
	}))
	c.SetValue(strings.Repeat("line\n", 2500) + "tail")
	for i := 0; i < 3; i++ {
		f := c.BeginHighlight(2500)
		f.Styles(2500)
		f.Close()
	}
	c.SetLine(2500, 4)
	beforeCreated, beforeCalls := created, trace.calls
	c.SetHighlightOverlay(func(line string, semantic []highlight.Span) []highlight.Span {
		return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Alert}}
	})
	c.InvalidateHighlightPaint()
	if created != beforeCreated || trace.calls != beforeCalls || c.hl.valid < 2500 {
		t.Fatal("paint invalidation reset semantic context")
	}
	c.insertIndentedNewline()
	if got := c.buf.lines[2501]; got != "  " {
		t.Fatalf("immediate newline got %q, want language indentation", got)
	}
}

func TestPaintOverlayCannotMutateSemanticSpansOrEscapeByteBounds(t *testing.T) {
	c := NewEditorCore(CoreHighlighter(highlight.HighlighterFunc(func(line string, _ highlight.State) ([]highlight.Span, highlight.State) {
		return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Keyword}}, 0
	})))
	c.SetValue("éx")
	c.SetHighlightOverlay(func(line string, spans []highlight.Span) []highlight.Span {
		spans[0].Style = highlight.Alert
		return append(spans, highlight.Span{Start: -1, End: 100, Style: highlight.Error})
	})
	f := c.BeginHighlight(0)
	got := f.Styles(0)
	f.Close()
	if got[0] != highlight.Alert || c.hl.lines[0].semantic[0].Style != highlight.Keyword {
		t.Fatal(got, c.hl.lines[0])
	}
	c.SetHighlightOverlay(nil)
	f = c.BeginHighlight(0)
	got = f.Styles(0)
	f.Close()
	if got[0] != highlight.Keyword {
		t.Fatal(got)
	}
}

func TestFactoryPreviewOwnsStatesAndPaletteDoesNotRenewSource(t *testing.T) {
	var traces []*sourceTrace
	d := highlight.Definition{Name: "Preview source", Extensions: []string{"*.custom"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		trace := &sourceTrace{t: t, refs: map[highlight.State]int{}}
		traces = append(traces, trace)
		return highlight.Source{Highlighter: trace, States: trace}
	}}
	catalog := highlight.NewRepository(d).Snapshot()
	preview := NewFilePreview(FilePaneStyles{}, false)
	preview.SetSourceHighlighting(catalog, SyntaxStyles{})
	files := FileSource{FS: fstest.MapFS{"one.custom": &fstest.MapFile{Data: []byte("one\ntwo")}, "two.custom": &fstest.MapFile{Data: []byte("next")}}}
	preview.Show(files, "one.custom", false)
	f := preview.view.Core().BeginHighlight(0)
	f.Styles(1)
	f.Close()
	old := traces[len(traces)-1]
	count := len(traces)
	preview.SetSourceHighlighting(catalog, SyntaxStyles{})
	if len(traces) != count {
		t.Fatal("palette rebuilt semantic source")
	}
	preview.Show(files, "two.custom", false)
	for id, n := range old.refs {
		if n != 0 {
			t.Fatal("preview switch leaked old roots", id, n)
		}
	}
}

func TestProvisionalEditChurnReclaimsStatesAndEventuallyCatchesUp(t *testing.T) {
	d := shell.Definition()
	c := NewEditorCore(CoreSourceFactory(func() highlight.Source { return d.NewSource(nil) }))
	c.SetValue("cat <<EOF\n" + strings.Repeat("body\n", 3000))
	stats := c.source.Highlighter.(interface{ RetainedStates() int })
	for i := 0; i < 100; i++ {
		c.buf.lines[0] = fmt.Sprintf("cat <<EOF%d", i)
		c.buf.touch(0)
		c.changed()
		f := c.BeginHighlight(2500)
		f.Styles(2500)
		f.Close()
		if n := stats.RetainedStates(); n > 6 {
			t.Fatalf("provisional churn retained %d historical states", n)
		}
	}
	caught := false
	for i := 0; i < 20; i++ {
		f := c.BeginHighlight(2500)
		f.Styles(2500)
		caught = !f.Behind
		f.Close()
		if caught {
			break
		}
	}
	if !caught {
		t.Fatal("collection starved semantic catch-up")
	}
}
