package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/shell"
)

func TestSourceStateLeasesEndOnFramesAndDocumentReplacement(t *testing.T) {
	var traces []*widgettest.SourceTrace
	c := NewEditorCore(CoreSourceFactory(func() highlight.Source {
		s := &widgettest.SourceTrace{T: t, Refs: map[highlight.State]int{}}
		traces = append(traces, s)
		return highlight.Source{Highlighter: s, Indenter: s, States: s}
	}))
	c.SetValue("one\ntwo\nthree")
	f := c.BeginHighlight(0)
	f.Styles(2)
	f.Close()
	old := traces[len(traces)-1]
	c.SetValue("new")
	for id, n := range old.Refs {
		if n != 0 {
			t.Fatalf("old document state %d still has %d leases", id, n)
		}
	}
	f = c.BeginHighlight(0)
	f.Styles(0)
	f.Close()
	current := traces[len(traces)-1]
	c.SetHighlighter(nil)
	for id, n := range current.Refs {
		if n != 0 {
			t.Fatalf("cleared source state %d has %d leases", id, n)
		}
	}
}

func TestFindPaintDoesNotChangeDeepImmediateIndent(t *testing.T) {
	created := 0
	var trace *widgettest.SourceTrace
	c := NewEditorCore(CoreModal(false), CoreAutoIndent(true), CoreSourceFactory(func() highlight.Source {
		created++
		trace = &widgettest.SourceTrace{T: t, Refs: map[highlight.State]int{}}
		return highlight.Source{Highlighter: trace, Indenter: trace, States: trace}
	}))
	c.SetValue(strings.Repeat("line\n", 2500) + "tail")
	for i := 0; i < 3; i++ {
		f := c.BeginHighlight(2500)
		f.Styles(2500)
		f.Close()
	}
	c.SetLine(2500, 4)
	beforeCreated, beforeCalls := created, trace.Calls
	c.SetHighlightOverlay(func(line string, semantic []highlight.Span) []highlight.Span {
		return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Alert}}
	})
	c.InvalidateHighlightPaint()
	if created != beforeCreated || trace.Calls != beforeCalls || c.hl.text.valid < 2500 {
		t.Fatal("paint invalidation reset semantic context")
	}
	c.insertIndentedNewline()
	if got := c.buf.Lines[2501]; got != "  " {
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
	if got[0] != highlight.Alert || c.hl.text.lines[0].semantic[0].Style != highlight.Keyword {
		t.Fatal(got, c.hl.text.lines[0])
	}
	c.SetHighlightOverlay(nil)
	f = c.BeginHighlight(0)
	got = f.Styles(0)
	f.Close()
	if got[0] != highlight.Keyword {
		t.Fatal(got)
	}
}

func TestProvisionalEditChurnReclaimsStatesAndEventuallyCatchesUp(t *testing.T) {
	d := shell.Definition()
	c := NewEditorCore(CoreSourceFactory(func() highlight.Source { return d.NewSource(nil) }))
	c.SetValue("cat <<EOF\n" + strings.Repeat("body\n", 3000))
	stats := c.source.Highlighter.(interface{ RetainedStates() int })
	for i := 0; i < 100; i++ {
		c.buf.Lines[0] = fmt.Sprintf("cat <<EOF%d", i)
		c.buf.Touch(0)
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
