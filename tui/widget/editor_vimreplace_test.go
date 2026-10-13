package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// R, Vim's Replace mode: every expectation is headless Neovim's on the same text and keys.
func TestVimReplace(t *testing.T) {
	runEdits(t, []editCase{
		{what: "3R repeats as replacement", text: "123456789", seq: "3Rab<esc>", want: "ababab789", cursor: true, wantCol: 5},
		{what: "past the end appends", text: "12", seq: "Rabcd<esc>", want: "abcd", cursor: true, wantCol: 3},
		{what: "Backspace restores, then only moves", text: "1234", seq: "Rab<bs><bs><bs>x<esc>", want: "x234"},
		{what: "Backspace before the start moves left", text: "1234", seq: "lRab<bs><bs><bs><esc>", want: "1234", cursor: true},
		{what: "Backspace deletes what it appended", text: "12", seq: "Rabc<bs><bs><esc>", want: "a2"},
		{what: "Enter breaks, overwriting nothing", text: "1234", seq: "Ra<cr>b<esc>", want: "a\nb34", cursor: true, wantLn: 1},
		{what: "Backspace joins the break", text: "1234", seq: "Ra<cr>b<bs><bs><esc>", want: "a234"},
		{what: ". repeats it", text: "1234\n5678", seq: "Rab<esc>j.", want: "ab34\n5ab8", cursor: true, wantLn: 1, wantCol: 2},
		{what: "Ctrl-w restores", text: "1234", seq: "Rab<ctw>x<esc>", want: "x234"},
		{what: "Ctrl-u restores", text: "1234 5678", seq: "wRab<ctu><esc>", want: "1234 5678", cursor: true, wantCol: 4},
		{what: "u undoes it whole", text: "1234", seq: "Rab<esc>u", want: "1234"},
		{what: "Ctrl-o resumes Replace", text: "1234", seq: "Ra<cto>lb<esc>", want: "a2b4", cursor: true, wantCol: 2},
		{what: "an arrow starts a new stack", text: "12345", seq: "Rab<left>c<bs><bs><esc>", want: "ab345", cursor: true},
		{what: "Backspace right after an arrow only moves", text: "12345", seq: "Rab<left><bs><esc>", want: "ab345", cursor: true},
		{what: "Tab overwrites", text: "12345", seq: "Ra\tb<esc>", want: "a\tb45", cursor: true, wantCol: 2},
		{what: "the count's text is what is left", text: "123456789", seq: "3Rab<bs>c<esc>", want: "acacac789", cursor: true, wantCol: 5},
		{what: "an arrow spends the count", text: "123456789", seq: "2Rab<left>c<esc>", want: "ac3456789", cursor: true, wantCol: 1},
		{what: "an arrow splits undo", text: "1234", seq: "Rab<left>c<esc>u", want: "ab34"},
		{what: "2. replaces twice", text: "1234", seq: "Rab<esc>2.", want: "aabab", cursor: true, wantCol: 4},
		{what: "3. past the end", text: "1234567", seq: "Rab<esc>l3.", want: "abababab", cursor: true, wantCol: 7},
	})
}

// Replace's mode: R reports it, Ctrl-o's command returns to it, Esc leaves; a paste overwrites; a
// viewer's R bubbles.
func TestVimReplaceMode(t *testing.T) {
	c, _ := coreWith(t, "1234")
	vimSeq(c, "R")
	if c.Mode() != widget.ModeReplace || c.Mode().String() != "REPLACE" {
		t.Fatalf("R: mode %v", c.Mode())
	}
	c.HandlePaste("ab")
	if c.Value() != "ab34" {
		t.Fatalf("a paste in Replace: %q", c.Value())
	}
	vimSeq(c, "<cto>")
	if c.Mode() != widget.ModeNormal || c.OneShotMode() != widget.ModeReplace {
		t.Fatalf("Ctrl-o in Replace: mode %v, one-shot %v", c.Mode(), c.OneShotMode())
	}
	vimSeq(c, "l")
	if c.Mode() != widget.ModeReplace || c.OneShotMode() != widget.ModeNormal {
		t.Fatalf("after Ctrl-o's command: mode %v", c.Mode())
	}
	vimSeq(c, "<esc>")
	if c.Mode() != widget.ModeNormal {
		t.Fatalf("Esc: mode %v", c.Mode())
	}
	if c.OneShotMode() != widget.ModeNormal {
		t.Fatal("a one-shot reported with none running")
	}
	i, _ := coreWith(t, "1234")
	vimSeq(i, "i<cto>")
	if i.OneShotMode() != widget.ModeInsert {
		t.Fatalf("Ctrl-o from Insert: %v", i.OneShotMode())
	}
	v, _ := coreWith(t, "1234", widget.CoreReadOnly(true))
	if v.HandleKey(tui.KeyEvent{Code: 'R', Text: "R"}) || v.Mode() != widget.ModeNormal {
		t.Fatalf("a viewer's R: mode %v", v.Mode())
	}
}

// The terminal Editor's caret: an underline in Replace.
func TestVimReplaceCaret(t *testing.T) {
	ed := widget.NewEditor(widget.WithInitialText("1234"))
	ed.Core().HandleKey(tui.KeyEvent{Code: 'R', Text: "R"})
	if got := ed.CursorShape(); got != tui.CursorShapeUnderline {
		t.Fatalf("Replace's caret: %v", got)
	}
	ed.Core().HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
	ed.Core().HandleKey(tui.KeyEvent{Code: 'i', Text: "i"})
	if got := ed.CursorShape(); got != tui.CursorShapeBar {
		t.Fatalf("Insert's caret: %v", got)
	}
}
