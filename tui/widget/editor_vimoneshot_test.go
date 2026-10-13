package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Insert's Ctrl-o: one Normal-mode command, then Insert again where it left the cursor. A command
// half-typed waits; one that enters Insert is the end; Visual waits for its operator or Esc.
func TestVimInsertOneShot(t *testing.T) {
	runEdits(t, []editCase{
		{what: "a motion", text: "abc def", seq: "A<cto>0X<esc>", want: "Xabc def"},
		{what: "an edit", text: "a\nb", seq: "i<cto>ddX<esc>", want: "Xb"},
		{what: "2dw finishes first", text: "a b c d", seq: "i<cto>2dwX<esc>", want: "Xc d"},
		{what: "dt. finishes first", text: "ab.cd", seq: "i<cto>dt.X<esc>", want: "X.cd"},
		{what: `"ayy finishes first`, text: "ab", seq: `i<cto>"ayyX<esc>"ap`, want: "Xab\nab"},
		{what: "gU$ finishes first", text: "ab cd", col: 3, seq: "i<cto>gU$X<esc>", want: "ab XCD"},
		{what: "o's Insert is the command's: Esc stays in Normal", text: "a", seq: "A<cto>ob<esc>x", want: "a\n"},
		{what: "Visual's operator ends it", text: "abcd", seq: "i<cto>vldX<esc>", want: "Xcd"},
		{what: "Visual's Esc ends it", text: "abcd", seq: "i<cto>vl<esc>X<esc>", want: "aXbcd"},
		{what: "Esc right after cancels", text: "abc", seq: "i<cto><esc>X<esc>", want: "Xabc"},
		{what: "Esc half-typed cancels", text: "abc", seq: "i<cto>2d<esc>X<esc>", want: "Xabc"},
		{what: "at the end, unmoved, back past it", text: "abc", seq: "A<cto>lX<esc>", want: "abcX"},
		{what: "at the end, Esc, back past it", text: "abc", seq: "A<cto><esc>X<esc>", want: "abcX"},
		{what: "at the end, moved", text: "abc", seq: "A<cto>hX<esc>", want: "aXbc"},
		{what: "at the end, onto another line's last", text: "ab\ncd", seq: "A<cto>jX<esc>", want: "ab\ncXd"},
		{what: "$ wants the end", text: "abc", seq: "i<cto>$X<esc>", want: "abcX"},
		{what: ". inside it", text: "a b c", seq: "dwi<cto>.X<esc>", want: "Xc"},
		{what: "@a plays whole as the command", text: "ab\ncd\nef", seq: "qaxjqggi<cto>@aX<esc>", want: "\nXcd\nef"},
		{what: "a macro holding Ctrl-o", text: "abc\ndef", seq: "qaA<cto>0X<esc>qj@a", want: "Xabc\nXdef"},
	})
}

// Ctrl-o splits undo: the Insert before, the command's own change, and the Insert after are
// separate steps.
func TestVimInsertOneShotUndo(t *testing.T) {
	const seq = "ifoo<cto>ddbar<esc>"
	runEdits(t, []editCase{
		{what: "all of it", text: "x\ny", seq: seq, want: "bary"},
		{what: "u: the Insert after", text: "x\ny", seq: seq + "u", want: "y"},
		{what: "uu: the command", text: "x\ny", seq: seq + "uu", want: "foox\ny"},
		{what: "uuu: the Insert before", text: "x\ny", seq: seq + "uuu", want: "x\ny"},
	})
}

// . after Ctrl-o: the Insert before is the last change; the command, when it changes the text,
// replaces it; the Insert after records afresh, as an i.
func TestVimInsertOneShotRepeat(t *testing.T) {
	runEdits(t, []editCase{
		{what: "the Insert before", text: "ab\ncd", seq: "ifoo<cto>j<esc>.", want: "fooab\nfoocd"},
		{what: "the command's change", text: "abc\nabc", seq: "ifoo<cto>x<esc>j.", want: "foobc\nab"},
		{what: "the Insert after, as an i", text: "ab\ncd", seq: "i<cto>lbar<esc>j.", want: "abarb\ncbard"},
	})
}

// InsertOneShot reports the command running, the mode Normal; an unbound key bubbles and Insert
// resumes; Esc right after Ctrl-o is consumed.
func TestVimInsertOneShotState(t *testing.T) {
	c, _ := coreWith(t, "abc")
	vimSeq(c, "i<cto>")
	if !c.InsertOneShot() || c.Mode() != widget.ModeNormal {
		t.Fatalf("after Ctrl-o: one-shot %v, mode %v", c.InsertOneShot(), c.Mode())
	}
	if c.HandleKey(keyMod('x', tui.ModAlt)) {
		t.Fatal("Ctrl-o then an unbound key: consumed")
	}
	if c.InsertOneShot() || c.Mode() != widget.ModeInsert {
		t.Fatalf("after an unbound key: one-shot %v, mode %v", c.InsertOneShot(), c.Mode())
	}
	vimSeq(c, "<cto>")
	if !c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape}) || c.Mode() != widget.ModeInsert {
		t.Fatalf("Esc right after Ctrl-o: mode %v", c.Mode())
	}
	vimSeq(c, "<cto>2")
	if !c.InsertOneShot() || c.Mode() != widget.ModeNormal {
		t.Fatal("a count alone ended the one-shot")
	}
}

// : waits: Insert resumes once the prompt closes, its line run or cancelled.
func TestVimInsertOneShotCommandLine(t *testing.T) {
	c, _ := coreWith(t, "a\nb\nc")
	c.SetCommandLine(func() bool { return true })
	vimSeq(c, "i<cto>:")
	if !c.InsertOneShot() || c.Mode() != widget.ModeNormal {
		t.Fatalf("with the prompt open: one-shot %v, mode %v", c.InsertOneShot(), c.Mode())
	}
	if err := c.RunCommand("3"); err != nil {
		t.Fatal(err)
	}
	c.CommandLineClosed()
	vimSeq(c, "X<esc>")
	if got := c.Value(); got != "a\nb\nXc" {
		t.Fatalf(":3 from Ctrl-o, then X: %q", got)
	}
}

// The real prompt: Insert resumes once its line runs, and once Esc cancels it.
func TestVimInsertOneShotPrompt(t *testing.T) {
	h, ed, _ := cmdFixture(t, "one\ntwo\nthree")
	defer h.stop()
	mode := func() (m widget.EditorMode) {
		h.onLoop(func() { m = ed.Core().Mode() })
		return
	}
	typeIn(h, "i")
	h.inject(tui.KeyEvent{Code: 'o', Mods: tui.ModCtrl})
	typeIn(h, ":")
	h.waitFor("the prompt", func() bool { return promptOpen(h, ed) })
	typeIn(h, "3")
	enter(h)
	h.waitFor("Insert after the line ran", func() bool { return !promptOpen(h, ed) && mode() == widget.ModeInsert })
	h.inject(tui.KeyEvent{Code: 'o', Mods: tui.ModCtrl})
	typeIn(h, ":")
	h.waitFor("the prompt again", func() bool { return promptOpen(h, ed) })
	h.inject(tui.KeyEvent{Code: tui.KeyEscape})
	h.settle()
	h.waitFor("Insert after Esc", func() bool { return !promptOpen(h, ed) && mode() == widget.ModeInsert })
	typeIn(h, "X")
	var got string
	h.onLoop(func() { got = ed.Core().Value() })
	if got != "one\ntwo\nXthree" {
		t.Fatalf("after :3 and a cancelled prompt, X: %q", got)
	}
}

// A host's ActUnbound on Insert Ctrl-o leaves the key unconsumed, and Insert stays.
func TestVimInsertOneShotHostUnbinds(t *testing.T) {
	c, _ := coreWith(t, "abc", widget.CoreKeymap(widget.Keymap{{Mode: widget.ModeInsert, Code: 'o', Ctrl: true}: widget.ActUnbound}))
	keys(c, "i")
	if c.HandleKey(tui.KeyEvent{Code: 'o', Mods: tui.ModCtrl}) || c.Mode() != widget.ModeInsert {
		t.Fatalf("an unbound Ctrl-o: mode %v", c.Mode())
	}
}
