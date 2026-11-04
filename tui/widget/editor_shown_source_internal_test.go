package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
)

// keywordLines highlights every line whole as a keyword: one style, so a line is styled or not.
type keywordLines struct{ tag highlight.Style }

func (k keywordLines) HighlightBlock(line string, previous highlight.State) ([]highlight.Span, highlight.State) {
	return []highlight.Span{{Start: 0, End: len(line), Style: k.tag}}, previous
}

// styledFirst reports whether c draws its shown buffer's first line with st.
func styledFirst(c *EditorCore, st highlight.Style) bool {
	f := c.BeginHighlight(0)
	defer f.Close()
	got := f.Styles(0)
	return c.Highlighting() && len(got) > 0 && got[0] == st
}

// An editor's highlighting reaches every buffer it shows: a buffer with none of its own, shown after
// another, takes the highlighting the editor set last (a host's SyntaxHighlighter applies its
// definition once, to the buffer shown then, and a switch between two buffers of one kind sets
// nothing again). A buffer returned to is drawn as it was, its lines not read again. One given the
// editor's under a definition since replaced takes the current one when shown; a buffer opened in
// a closed one's place takes it too; one with a factory of its own keeps it throughout; and an
// editor whose highlighting was turned off draws a buffer it gave its highlighting plain.
func TestAShownBufferTakesTheEditorsHighlighting(t *testing.T) {
	made := 0
	markdown := func() highlight.Source {
		made++
		return highlight.Source{Highlighter: keywordLines{highlight.Keyword}}
	}
	yaml := func() highlight.Source { return highlight.Source{Highlighter: keywordLines{highlight.Function}} }
	c := NewEditorCore(CoreSourceFactory(markdown))
	c.SetValue("# first\nbody")
	if !styledFirst(c, highlight.Keyword) {
		t.Fatal("fixture: the first buffer isn't highlighted")
	}
	first := c.Buffer()
	second, third := NewBuffer(BufferText("# second\nbody")), NewBuffer(BufferText("# third\nbody"))
	own := NewBuffer(BufferText("# own\nbody"), BufferSourceFactory(func() highlight.Source {
		return highlight.Source{Highlighter: keywordLines{highlight.String}}
	}))
	c.SetBuffer(second)
	if !styledFirst(c, highlight.Keyword) {
		t.Fatal("a buffer shown after another, with no highlighting of its own, is drawn plain")
	}
	c.SetBuffer(third)
	if !styledFirst(c, highlight.Keyword) {
		t.Fatal("a third buffer is drawn plain")
	}
	before := made
	c.SetBuffer(first)
	if !styledFirst(c, highlight.Keyword) {
		t.Fatal("the first buffer, shown again, is drawn plain")
	}
	if made != before {
		t.Errorf("showing a buffer that has its highlighting made %d sources, want none: its lines would be read again", made-before)
	}
	c.SetBuffer(own)
	if !styledFirst(c, highlight.String) {
		t.Fatal("a buffer with a factory of its own isn't drawn with it")
	}

	// the definition changes while the first is shown: the second, given the old one, takes the new
	c.SetBuffer(first)
	c.SetSourceFactory(yaml)
	if !styledFirst(c, highlight.Function) {
		t.Fatal("fixture: the shown buffer didn't take the new definition")
	}
	c.SetBuffer(second)
	if !styledFirst(c, highlight.Function) {
		t.Fatal("a buffer given the editor's old highlighting kept it after the definition changed")
	}
	c.SetBuffer(own)
	if !styledFirst(c, highlight.String) {
		t.Fatal("a buffer with a factory of its own lost it to the editor's new definition")
	}
	// a buffer closed and another opened in its place
	c.SetBuffer(NewBuffer(BufferText("# reopened\nbody")))
	if !styledFirst(c, highlight.Function) {
		t.Fatal("a buffer opened in a closed one's place is drawn plain")
	}

	// highlighting turned off: a buffer the editor gave its highlighting is drawn plain when shown
	c.SetHighlighter(nil)
	c.SetBuffer(third)
	if c.Highlighting() {
		t.Fatal("an editor whose highlighting was turned off still drew a buffer it had given it")
	}
	c.SetBuffer(NewBuffer(BufferText("# plain\nbody")))
	if c.Highlighting() {
		t.Fatal("an editor whose highlighting was turned off gave a new buffer the factory it set before")
	}
	c.SetBuffer(own)
	if !styledFirst(c, highlight.String) {
		t.Fatal("a buffer with a factory of its own lost it when the editor's was turned off")
	}
}
