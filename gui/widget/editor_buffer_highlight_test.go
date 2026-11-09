package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// wholeLineKeyword highlights every line whole as a keyword.
type wholeLineKeyword struct{}

func (wholeLineKeyword) HighlightBlock(line string, previous highlight.State) ([]highlight.Span, highlight.State) {
	return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Keyword}}, previous
}

// TestAGUIEditorsRawViewHighlightsEachBufferItShows: the GUI's Editor paints its Raw view from its
// core's highlighting (rawColors), which is the shown buffer's. A second buffer shown, with no
// highlighting of its own, is drawn with the editor's, as the first was: a switch between two
// Markdown notes, whose declared definition doesn't change, leaves neither plain.
func TestAGUIEditorsRawViewHighlightsEachBufferItShows(t *testing.T) {
	e := NewEditor(WithCore(editor.CoreSourceFactory(func() highlight.Source {
		return highlight.Source{Highlighter: wholeLineKeyword{}}
	})))
	c := e.Core()
	c.SetValue("# first\nbody")
	for _, text := range []string{"# second\nbody", "# third\nbody"} {
		c.SetBuffer(editor.NewBuffer(editor.BufferText(text)))
		if v := e.ShownView(); v != editor.ViewRaw {
			t.Fatalf("the editor shows view %v, not Raw", v)
		}
		f := c.BeginHighlight(0)
		got := f.Styles(0)
		f.Close()
		if !c.Highlighting() || len(got) == 0 || got[0] != highlight.Keyword {
			t.Fatalf("%q shown after another buffer: highlighting %v, line 0 styles %v; want the editor's", text, c.Highlighting(), got)
		}
	}
}
