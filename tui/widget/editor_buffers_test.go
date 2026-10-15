package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func esc(c *widget.EditorCore) { c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape}) }

func cursorAt(t *testing.T, c *widget.EditorCore, ln, col int, what string) {
	t.Helper()
	if l, k := c.Line(); l != ln || k != col {
		t.Fatalf("%s: cursor (%d, %d), want (%d, %d)", what, l, k, ln, col)
	}
}

// Each buffer keeps its own undo, cursor and Modified across a switch: u in A undoes A's edit, and
// B's text and history are untouched by it.
func TestABufferKeepsItsStateAcrossASwitch(t *testing.T) {
	c, _ := coreWith(t, "alpha\nbeta")
	a := c.Buffer()
	keys(c, "jx") // A: "alpha\neta", cursor (1, 0)
	b := widget.NewBuffer(widget.BufferText("one\ntwo\nthree"))
	c.SetBuffer(b)
	cursorAt(t, c, 0, 0, "B, first shown")
	keys(c, "jjdd") // B: "one\ntwo"
	if c.Value() != "one\ntwo" || !b.Modified() {
		t.Fatalf("B after dd: %q, modified %v", c.Value(), b.Modified())
	}
	c.SetBuffer(a)
	cursorAt(t, c, 1, 0, "A, shown again")
	if !a.Modified() || c.Value() != "alpha\neta" {
		t.Fatalf("A shown again: %q, modified %v", c.Value(), a.Modified())
	}
	keys(c, "u")
	if c.Value() != "alpha\nbeta" || a.Modified() {
		t.Fatalf("u in A: %q, modified %v", c.Value(), a.Modified())
	}
	if b.Value() != "one\ntwo" || !b.Modified() {
		t.Fatalf("u in A reached B: %q, modified %v", b.Value(), b.Modified())
	}
	c.SetBuffer(b)
	keys(c, "u")
	if b.Value() != "one\ntwo\nthree" {
		t.Fatalf("B's own u: %q", b.Value())
	}
}

// Showing a buffer is not an edit: no onChange, so a host never marks a buffer dirty by showing it.
func TestSetBufferIsNotAnEdit(t *testing.T) {
	changes := 0
	c, _ := coreWith(t, "a", widget.CoreOnChange(func() { changes++ }))
	a := c.Buffer()
	b := widget.NewBuffer(widget.BufferText("b"))
	c.SetBuffer(b)
	c.SetBuffer(a)
	c.SetBuffer(a) // the shown buffer: nothing
	if changes != 0 {
		t.Fatalf("switching buffers called onChange %d times", changes)
	}
	if a.Modified() || b.Modified() {
		t.Fatal("a switch left a buffer modified")
	}
}

// An editor's place in a buffer it left follows edits made there through another editor (a
// headless one, as a host edits a buffer no widget shows): lines inserted above keep it on its text,
// and its line deleted leaves it on that line number, which the next line's text joins (a deleted
// line is logged as the next one joining it).
func TestAPlaceFollowsEditsMadeElsewhere(t *testing.T) {
	c, _ := coreWith(t, "one\ntwo\nthree\nfour")
	b := c.Buffer()
	keys(c, "jjl") // on "three", col 1
	c.SetBuffer(widget.NewBuffer())

	h := widget.NewEditorCore(widget.CoreBuffer(b))
	if _, err := h.Insert(widget.TextPos{}, "zero\nhalf\n"); err != nil {
		t.Fatal(err)
	}
	c.SetBuffer(b)
	cursorAt(t, c, 4, 1, "after two lines inserted above")
	if line := strings.Split(c.Value(), "\n")[4]; line != "three" {
		t.Fatalf("the place moved off its text: line 4 is %q", line)
	}

	c.SetBuffer(widget.NewBuffer())
	if err := h.Delete(widget.TextRange{From: widget.TextPos{Line: 4}, To: widget.TextPos{Line: 5}}); err != nil {
		t.Fatal(err)
	}
	c.SetBuffer(b)
	cursorAt(t, c, 4, 1, "after its line was deleted")
	if line := strings.Split(c.Value(), "\n")[4]; line != "four" {
		t.Fatalf("after the delete, line 4 is %q, want four", line)
	}
	h.Detach()
}

// Two editors showing one buffer: an edit through one moves the other's cursor with its text. Undo
// through either puts that editor at the change, as Vim's u does, and moves the other with the text.
func TestTwoEditorsShowOneBuffer(t *testing.T) {
	e1, l1 := coreWith(t, "a\nb\nc")
	b := e1.Buffer()
	e2, l2 := coreWith(t, "")
	e2.SetBuffer(b)
	keys(e2, "jj") // e2 on "c"
	l2.changed = nil
	keys(e1, "Onew") // e1 opens a line above "a"
	esc(e1)
	keys(e1, "j") // e1 on "a", line 1
	cursorAt(t, e2, 3, 0, "e2 after e1 added a line above")
	if len(l2.changed) == 0 {
		t.Fatal("e2's layout was not told of e1's edit")
	}
	_ = l1
	keys(e2, "u")
	if b.Value() != "a\nb\nc" {
		t.Fatalf("u through e2: %q", b.Value())
	}
	cursorAt(t, e2, 0, 0, "e2 after its own u (at the change)")
	if l, _ := e1.Line(); l != 0 {
		t.Fatalf("e1 after e2's u: line %d, want 0 (\"a\" moved up with the undo)", l)
	}
}

// . recorded in one buffer repeats in another; an Insert interrupted by a switch is its buffer's
// last change.
func TestDotAcrossASwitch(t *testing.T) {
	c, _ := coreWith(t, "abc")
	keys(c, "x") // A: "bc"; . is x
	b := widget.NewBuffer(widget.BufferText("xyz"))
	c.SetBuffer(b)
	keys(c, ".")
	if b.Value() != "yz" {
		t.Fatalf(". in B: %q, want yz", b.Value())
	}
	keys(c, "iQ") // an Insert, interrupted by a switch
	c.SetBuffer(widget.NewBuffer(widget.BufferText("mn")))
	if c.Mode() != widget.ModeNormal {
		t.Fatalf("after a switch from Insert: mode %v", c.Mode())
	}
	keys(c, ".")
	if c.Value() != "Qmn" {
		t.Fatalf(". after the interrupted Insert: %q, want Qmn", c.Value())
	}
}

// j and k keep their column across a switch away and back: the sticky column is the editor's
// place, as the cursor is.
func TestTheStickyColumnSurvivesASwitch(t *testing.T) {
	c, _ := coreWith(t, "abcdef\nab\nabcdef")
	a := c.Buffer()
	keys(c, "$j") // col 5 latched, on the short line at col 1
	c.SetBuffer(widget.NewBuffer())
	c.SetBuffer(a)
	keys(c, "j")
	cursorAt(t, c, 2, 5, "j after the switch")
}

// SetValue replaces the shown buffer's text as a new document: another editor's place in it goes
// to the start.
func TestSetValueResetsThePlacesInIt(t *testing.T) {
	c, _ := coreWith(t, "a\nb\nc")
	b := c.Buffer()
	keys(c, "jj")
	c.SetBuffer(widget.NewBuffer())
	h := widget.NewEditorCore(widget.CoreBuffer(b))
	h.SetValue("x\ny\nz")
	c.SetBuffer(b)
	cursorAt(t, c, 0, 0, "after SetValue in another editor")
}

// ReplaceFollowing leaves the editor's cursor on its text, wherever the replaced block is.
func TestReplaceFollowingKeepsTheCursorOnItsText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to widget.TextPos
		with     string
		ln, col  int
	}{
		{"above", widget.TextPos{Line: 0}, widget.TextPos{Line: 1}, "x\ny\n", 3, 2},
		{"below", widget.TextPos{Line: 3}, widget.TextPos{Line: 4}, "", 2, 2},
		{"over", widget.TextPos{Line: 2}, widget.TextPos{Line: 3}, "", 2, 2}, // its line takes the next line's text
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := coreWith(t, "zero\none\ntwo two\nthree\nfour")
			keys(c, "jjll") // (2, 2)
			if err := c.ReplaceFollowing(widget.TextRange{From: tc.from, To: tc.to}, tc.with); err != nil {
				t.Fatal(err)
			}
			cursorAt(t, c, tc.ln, tc.col, "after ReplaceFollowing "+tc.name)
			keys(c, "u")
			if c.Value() != "zero\none\ntwo two\nthree\nfour" {
				t.Fatalf("one u did not undo it: %q", c.Value())
			}
		})
	}
}

// A command run on a headless editor sharing the shown editor's registry gives the text the same
// command gives on the shown editor: the user's and a host's edits run one code path.
func TestAHeadlessEditorRunsTheUsersCommands(t *testing.T) {
	shown, _ := coreWith(t, "hello world")
	if err := shown.Commands().Set(widget.Command{Name: "upper", Run: func(cc widget.CommandContext) error {
		ln, _ := cc.Core.Line()
		line := strings.Split(cc.Core.Value(), "\n")[ln]
		_, err := cc.Core.Replace(widget.TextRange{From: widget.TextPos{Line: ln}, To: widget.TextPos{Line: ln, Col: len([]rune(line))}}, strings.ToUpper(line))
		return err
	}}); err != nil {
		t.Fatal(err)
	}
	other := widget.NewBuffer(widget.BufferText("hello world"))
	h := widget.NewEditorCore(widget.CoreBuffer(other), widget.CoreCommands(shown.Commands()))
	if err := shown.RunCommand("upper"); err != nil {
		t.Fatal(err)
	}
	if err := h.RunCommand("upper"); err != nil {
		t.Fatal(err)
	}
	if shown.Value() != other.Value() || other.Value() != "HELLO WORLD" {
		t.Fatalf("shown %q, headless %q", shown.Value(), other.Value())
	}
}

// A buffer's highlighting is its own: switching back paints from its kept spans with no
// highlighter call, and another buffer's highlighter is not its.
func TestHighlightingStaysWithItsBuffer(t *testing.T) {
	calls := 0
	kw := highlight.HighlighterFunc(func(line string, in highlight.State) ([]highlight.Span, highlight.State) {
		calls++
		if i := strings.Index(line, "kw"); i >= 0 {
			return []highlight.Span{{Start: i, End: i + 2, Style: highlight.Keyword}}, in
		}
		return nil, in
	})
	c, _ := coreWith(t, "kw a\nb kw", widget.CoreHighlighter(kw))
	a := c.Buffer()
	render := func() [][]highlight.Style {
		f := c.BeginHighlight(0)
		defer f.Close()
		return [][]highlight.Style{f.Styles(0), f.Styles(1)}
	}
	render()
	b := widget.NewBuffer(widget.BufferText("kw"))
	c.SetBuffer(b)
	if c.Highlighting() {
		t.Fatal("B has A's highlighter")
	}
	c.SetBuffer(a)
	before := calls
	got := render()
	if calls != before {
		t.Fatalf("switching back highlighted %d lines again", calls-before)
	}
	if got[0][0] != highlight.Keyword || got[1][2] != highlight.Keyword {
		t.Fatalf("A's styles after the switch back: %v", got)
	}
}

// Two editors on one buffer paint from its shared spans in their own looks: when one edits a line
// and renders first, the other renders the new spans, never the old line's painted styles.
func TestTheSecondEditorPaintsTheNewSpans(t *testing.T) {
	kw := highlight.HighlighterFunc(func(line string, in highlight.State) ([]highlight.Span, highlight.State) {
		if i := strings.Index(line, "kw"); i >= 0 {
			return []highlight.Span{{Start: i, End: i + 2, Style: highlight.Keyword}}, in
		}
		return nil, in
	})
	e1, _ := coreWith(t, "kw", widget.CoreHighlighter(kw))
	e2, _ := coreWith(t, "")
	e2.SetBuffer(e1.Buffer())
	render := func(c *widget.EditorCore) []highlight.Style {
		f := c.BeginHighlight(0)
		defer f.Close()
		return f.Styles(0)
	}
	if s := render(e2); len(s) != 2 || s[0] != highlight.Keyword {
		t.Fatalf("e2 before the edit: %v", s)
	}
	keys(e1, "rx") // "xw": no keyword
	render(e1)     // e1 walks first and re-highlights the line
	if s := render(e2); len(s) != 2 || s[0] != highlight.Normal {
		t.Fatalf("e2 painted the old line's styles: %v", s)
	}
}

// An Insert's undo group ends at a switch. It shows in a modeless editor, which stays in Insert
// across the switch: back in the buffer, what is typed next is a step of its own, so one undo takes
// it alone and leaves what was typed before the switch. (A modal editor returns to Normal, whose
// edits always begin a step.)
func TestAnInsertsUndoGroupEndsAtASwitch(t *testing.T) {
	c, _ := coreWith(t, "", widget.CoreModal(false))
	a := c.Buffer()
	keys(c, "Q")
	c.SetBuffer(widget.NewBuffer(widget.BufferText("other")))
	c.SetBuffer(a)
	keys(c, "Z")
	if c.Value() != "QZ" {
		t.Fatalf("typed before and after the switch: %q, want QZ", c.Value())
	}
	c.Undo()
	if c.Value() != "Q" {
		t.Fatalf("one undo after the switch back: %q, want Q (only what was typed after it)", c.Value())
	}
}

// A Batch that switches buffers announces each buffer's edits as that buffer's: A's when the switch
// comes (to A's other editors too), B's at the batch's end, and B's edit is an undo step of its own.
func TestABatchAcrossASwitchKeepsEachBuffersEdits(t *testing.T) {
	var c *widget.EditorCore
	var told []string
	c, _ = coreWith(t, "a", widget.CoreOnChange(func() { told = append(told, c.Value()) }))
	a := c.Buffer()
	other, ol := coreWith(t, "")
	other.SetBuffer(a)
	ol.changed = nil
	b := widget.NewBuffer(widget.BufferText("b"))
	err := c.Batch(func() error {
		if _, err := c.Insert(widget.TextPos{}, "A"); err != nil {
			return err
		}
		c.SetBuffer(b)
		_, err := c.Insert(widget.TextPos{}, "B")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(told) != 2 || told[0] != "Aa" || told[1] != "Bb" {
		t.Fatalf("onChange saw %q, want [Aa Bb]: each buffer's edits announced as its own", told)
	}
	if len(ol.changed) == 0 {
		t.Fatal("A's other editor was not told of A's edit")
	}
	c.Undo()
	if b.Value() != "b" || a.Value() != "Aa" {
		t.Fatalf("one undo in B: B %q, A %q; want b and Aa", b.Value(), a.Value())
	}
}

// The finds' plain highlighter is the buffer's, shared by the editors whose find needs it: one
// editor clearing its find, or leaving, keeps another's walk; and no find removes a highlighter the
// host installed.
func TestTheFindsPlainHighlighterIsShared(t *testing.T) {
	e1, _ := coreWith(t, "a b a")
	e2, _ := coreWith(t, "")
	e2.SetBuffer(e1.Buffer())
	e1.SetFind("a", widget.FindOptions{})
	e2.SetFind("a", widget.FindOptions{})
	e1.SetFind("", widget.FindOptions{})
	if !e2.Highlighting() {
		t.Fatal("e1 clearing its find took the walk e2's find paints on")
	}
	e1.SetFind("a", widget.FindOptions{})
	e1.SetBuffer(widget.NewBuffer())
	if !e2.Highlighting() {
		t.Fatal("e1 leaving took the walk e2's find paints on")
	}
	e2.SetFind("", widget.FindOptions{})
	if e2.Highlighting() {
		t.Fatal("the plain highlighter stayed with no find needing it")
	}

	kw := highlight.HighlighterFunc(func(string, highlight.State) ([]highlight.Span, highlight.State) { return nil, 0 })
	e1.SetBuffer(e2.Buffer())
	e1.SetFind("a", widget.FindOptions{}) // the plain one again
	e2.SetHighlighter(kw)                 // the host's replaces it
	e1.SetFind("", widget.FindOptions{})
	if !e2.Highlighting() {
		t.Fatal("clearing a find removed the host's highlighter")
	}
}
