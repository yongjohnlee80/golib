package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestFindMovesOnAndWraps(t *testing.T) {
	c, _ := coreWith(t, "a cat\nthe Cat sat\ncat")
	r, ok := c.Find("cat", pos(0, 2), widget.FindOptions{})
	if !ok || r != rng(2, 0, 2, 3) {
		t.Fatalf("Find from a match = %v, %v; want the next (case-sensitive, so not Cat)", r, ok)
	}
	if _, ok := c.Find("cat", pos(2, 0), widget.FindOptions{}); ok {
		t.Fatal("Find past the last match found one without Wrap")
	}
	if r, ok := c.Find("cat", pos(2, 0), widget.FindOptions{Wrap: true}); !ok || r != rng(0, 2, 0, 5) {
		t.Fatalf("Find with Wrap = %v, %v", r, ok)
	}
	if r, ok := c.Find("cat", pos(2, 0), widget.FindOptions{Backward: true, IgnoreCase: true}); !ok || r != rng(1, 4, 1, 7) {
		t.Fatalf("Find backward, folding case = %v, %v; want Cat", r, ok)
	}
	if got := len(c.FindAll("cat", widget.FindOptions{IgnoreCase: true})); got != 3 {
		t.Fatalf("FindAll folding case = %d matches, want 3", got)
	}
	// whole clusters: e does not match the e of é
	c.SetValue("café cafe")
	if all := c.FindAll("cafe", widget.FindOptions{}); len(all) != 1 || all[0] != rng(0, 5, 0, 9) {
		t.Fatalf("FindAll inside a cluster = %v", all)
	}
	if all := c.FindAll("", widget.FindOptions{}); len(all) != 0 {
		t.Fatalf("an empty term matched %v", all)
	}
}

func TestReplaceAllIsOneUndo(t *testing.T) {
	c, _ := coreWith(t, "one two one\none")
	n, err := c.ReplaceAll("one", "uno", widget.FindOptions{})
	if err != nil || n != 3 || c.Value() != "uno two uno\nuno" {
		t.Fatalf("ReplaceAll = %d, %v; text %q", n, err, c.Value())
	}
	undo(c)
	if c.Value() != "one two one\none" {
		t.Fatalf("one undo after ReplaceAll = %q", c.Value())
	}
	if n, err := c.ReplaceAll("zzz", "y", widget.FindOptions{}); n != 0 || err != nil || !c.CanRedo() {
		t.Fatalf("ReplaceAll with no match = %d, %v, and must not touch history", n, err)
	}
}

func TestNAndNStepThroughTheActiveFindAndBubbleWithout(t *testing.T) {
	c, _ := coreWith(t, "x\nfoo\nbar foo\n")
	if c.HandleKey(tui.KeyEvent{Code: 'n', Text: "n"}) {
		t.Fatal("n with no active find was consumed; it must reach the application")
	}
	c.SetFind("foo", widget.FindOptions{})
	keys(c, "n")
	if got := c.Cursor(); got != pos(1, 0) {
		t.Fatalf("n = %v, want (1,0)", got)
	}
	keys(c, "n")
	keys(c, "n") // wraps
	if got := c.Cursor(); got != pos(1, 0) {
		t.Fatalf("n n = %v, want (1,0) after wrapping", got)
	}
	c.HandleKey(tui.KeyEvent{Code: 'N', Text: "N"})
	if got := c.Cursor(); got != pos(2, 4) {
		t.Fatalf("N = %v, want (2,4) wrapping backward", got)
	}
	c.SetFind("", widget.FindOptions{})
	if c.HandleKey(tui.KeyEvent{Code: 'N', Text: "N"}) {
		t.Fatal("N after clearing the find was consumed")
	}
}

func TestTheActiveFindIsMarkedOverTheHostsOverlay(t *testing.T) {
	c, _ := coreWith(t, "say hello\nhello")
	// a core with no highlighter still marks
	c.SetFind("hello", widget.FindOptions{})
	f := c.BeginHighlight(0)
	st := f.Styles(0)
	f.Close()
	if len(st) != 9 || st[4] != highlight.Alert || st[8] != highlight.Alert || st[3] == highlight.Alert {
		t.Fatalf("styles of line 0 = %v; want hello marked Alert", st)
	}
	// the host's overlay stays under the marks
	c.SetHighlightOverlay(func(line string, spans []highlight.Span) []highlight.Span {
		return []highlight.Span{{Start: 0, End: 3, Style: highlight.Keyword}}
	})
	f = c.BeginHighlight(0)
	st = f.Styles(0)
	f.Close()
	if st[0] != highlight.Keyword || st[4] != highlight.Alert {
		t.Fatalf("styles with the host's overlay = %v", st)
	}
	c.SetFind("", widget.FindOptions{})
	f = c.BeginHighlight(0)
	st = f.Styles(0)
	f.Close()
	for i, s := range st {
		if s == highlight.Alert {
			t.Fatalf("cluster %d still marked after clearing the find: %v", i, st)
		}
	}
}

// Esc in Normal mode cancels the active find; with none it bubbles, as before.
func TestEscapeCancelsTheActiveFind(t *testing.T) {
	c, _ := coreWith(t, "foo bar foo")
	esc := tui.KeyEvent{Code: tui.KeyEscape}
	c.SetFind("foo", widget.FindOptions{})
	if !c.HandleKey(esc) {
		t.Fatal("Esc over an active find was not consumed")
	}
	if term, _ := c.FindTerm(); term != "" {
		t.Fatalf("the find is still %q", term)
	}
	if c.HandleKey(esc) {
		t.Fatal("Esc with no find was consumed; it must reach the host")
	}
	// the host's own: asked only then, and only what it takes is consumed
	asked := 0
	c.SetOnEscape(func() bool { asked++; return asked == 1 })
	if !c.HandleKey(esc) || c.HandleKey(esc) || asked != 2 {
		t.Fatalf("the host's Esc: asked %d", asked)
	}
	keys(c, "i")
	c.HandleKey(esc) // Insert's Esc returns to Normal: not the host's
	if asked != 2 {
		t.Fatalf("the host was asked about Insert mode's Esc (%d)", asked)
	}
}

// A modeless editor's Esc (Text mode) cancels the find and asks the host, as Normal mode's does.
func TestModelessEscapeCancelsTheFindToo(t *testing.T) {
	c, _ := coreWith(t, "foo bar", widget.CoreKeyset(widget.KeysetStandard))
	esc := tui.KeyEvent{Code: tui.KeyEscape}
	c.SetFind("foo", widget.FindOptions{})
	if !c.HandleKey(esc) {
		t.Fatal("a modeless Esc over an active find was not consumed")
	}
	if term, _ := c.FindTerm(); term != "" {
		t.Fatalf("the find is still %q", term)
	}
	asked := false
	c.SetOnEscape(func() bool { asked = true; return true })
	if !c.HandleKey(esc) || !asked {
		t.Fatal("a modeless Esc did not reach the host's answer")
	}
}

// The current match is drawn whatever the focus and the mode; n sets it, and ending the find
// clears it.
func TestTheCurrentMatchIsHighlighted(t *testing.T) {
	c, _ := coreWith(t, "a foo b foo")
	c.SetFind("foo", widget.FindOptions{})
	keys(c, "n")
	if r, ok := c.CurrentMatch(); !ok || r != rng(0, 2, 0, 5) {
		t.Fatalf("CurrentMatch after n = %v, %v", r, ok)
	}
	if !c.Highlighted(0, 2, false) || !c.Highlighted(0, 4, false) || c.Highlighted(0, 5, false) {
		t.Fatal("the current match is not highlighted, unfocused, exactly")
	}
	if c.Highlighted(0, 8, false) {
		t.Fatal("another match is highlighted as the current one")
	}
	c.SetFind("", widget.FindOptions{})
	if _, ok := c.CurrentMatch(); ok || c.Highlighted(0, 2, false) {
		t.Fatal("ending the find kept the current match")
	}
	// a host's own find sets it directly
	c.SetCurrentMatch(rng(0, 8, 0, 11))
	if !c.Highlighted(0, 9, false) {
		t.Fatal("SetCurrentMatch did not highlight")
	}
	// a Visual selection by keys is drawn only while focused; Select's whatever the focus
	c.SetCurrentMatch(widget.TextRange{})
	keys(c, "0v")
	if c.Highlighted(0, 0, false) || !c.Highlighted(0, 0, true) {
		t.Fatal("a v selection is drawn unfocused, or not focused")
	}
	keys(c, "v")
	c.Select(rng(0, 0, 0, 1))
	if !c.Highlighted(0, 0, false) {
		t.Fatal("Select's selection is not drawn unfocused")
	}
}
