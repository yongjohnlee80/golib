package widget_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/golang"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

var vimToken = regexp.MustCompile(`<[a-z]+(:[^>]*)?>`)

// vimSeq types s into the core: plain text as keys, and <esc> <cr> <left> <pgdn> <bs> as those keys,
// <redo> as Ctrl-r, <ctw> and <ctu> as Ctrl-w and Ctrl-u,
// <tick> as the escape chord's timeout, <paste:TEXT> as a paste (\n for a line break).
func vimSeq(c *widget.EditorCore, s string) {
	for len(s) > 0 {
		loc := vimToken.FindStringIndex(s)
		if loc == nil {
			keys(c, s)
			return
		}
		keys(c, s[:loc[0]])
		tok := s[loc[0]:loc[1]]
		switch {
		case tok == "<esc>":
			c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
		case tok == "<cr>":
			c.HandleKey(tui.KeyEvent{Code: tui.KeyEnter})
		case tok == "<left>":
			c.HandleKey(tui.KeyEvent{Code: tui.KeyLeft})
		case tok == "<pgdn>":
			c.HandleKey(tui.KeyEvent{Code: tui.KeyPageDown})
		case tok == "<bs>":
			c.HandleKey(tui.KeyEvent{Code: tui.KeyBackspace})
		case tok == "<redo>":
			c.HandleKey(tui.KeyEvent{Code: 'r', Mods: tui.ModCtrl})
		case tok == "<ctw>":
			c.HandleKey(tui.KeyEvent{Code: 'w', Mods: tui.ModCtrl})
		case tok == "<ctu>":
			c.HandleKey(tui.KeyEvent{Code: 'u', Mods: tui.ModCtrl})
		case tok == "<tick>":
			c.HandleTick()
		case strings.HasPrefix(tok, "<paste:"):
			c.HandlePaste(strings.ReplaceAll(tok[len("<paste:"):len(tok)-1], `\n`, "\n"))
		}
		s = s[loc[1]:]
	}
}

type editCase struct {
	what    string
	text    string
	ln, col int
	seq     string
	want    string
	wantLn  int
	wantCol int
	cursor  bool
	opts    []widget.CoreOption
}

func runEdits(t *testing.T, cases []editCase) {
	t.Helper()
	for _, e := range cases {
		c, _ := coreWith(t, e.text, e.opts...)
		c.SetLine(e.ln, e.col)
		vimSeq(c, e.seq)
		if got := c.Value(); got != e.want {
			t.Errorf("%s: %q by %q: %q, want %q", e.what, e.text, e.seq, got, e.want)
			continue
		}
		if e.cursor {
			if ln, col := c.Line(); ln != e.wantLn || col != e.wantCol {
				t.Errorf("%s: cursor (%d,%d), want (%d,%d)", e.what, ln, col, e.wantLn, e.wantCol)
			}
		}
	}
}

// r replaces count clusters with the typed one (a digit, a space), or a line break; with too few
// left nothing changes.
func TestVimReplaceChar(t *testing.T) {
	runEdits(t, []editCase{
		{what: "rx", text: "abc", seq: "rx", want: "xbc", cursor: true},
		{what: "2rx", text: "abc", seq: "2rx", want: "xxc", cursor: true, wantCol: 1},
		{what: "3rx with two left", text: "abc", col: 1, seq: "3rx", want: "abc"},
		{what: "r and a space", text: "a b", seq: "r ", want: "  b"},
		{what: "r3: the digit is the char", text: "abc", seq: "r3", want: "3bc"},
		{what: "r Enter", text: "abcd", col: 1, seq: "r<cr>", want: "a\ncd", cursor: true, wantLn: 1},
		{what: "r then Esc cancels", text: "abc", seq: "r<esc>x", want: "bc"},
	})
}

// J joins with one blank (none after a blank, before ')' or with an empty line), gJ as is.
func TestVimJoin(t *testing.T) {
	runEdits(t, []editCase{
		{what: "J", text: "a\n  b", seq: "J", want: "a b", cursor: true, wantCol: 1},
		{what: "J after a trailing blank", text: "a \nb", seq: "J", want: "a b"},
		{what: "J before )", text: "f(\n)", seq: "J", want: "f()"},
		{what: "J with an empty line", text: "a\n\nb", seq: "J", want: "a\nb"},
		{what: "3J", text: "a\nb\nc\nd", seq: "3J", want: "a b c\nd"},
		{what: "J on the last line", text: "a\nb", ln: 1, seq: "J", want: "a\nb"},
		{what: "5J past the end", text: "a\nb", seq: "5J", want: "a b"},
		{what: "gJ", text: "a\n  b", seq: "gJ", want: "a  b"},
	})
}

// ~ toggles and moves on; g~ gu gU are operators with doubled forms; marks of a cluster stay.
func TestVimCase(t *testing.T) {
	runEdits(t, []editCase{
		{what: "~", text: "aBc", seq: "~", want: "ABc", cursor: true, wantCol: 1},
		{what: "3~", text: "aBc", seq: "3~", want: "AbC", cursor: true, wantCol: 2},
		{what: "~ beyond letters", text: "émile", seq: "~", want: "Émile"},
		{what: "g~w", text: "foo bar", seq: "g~w", want: "FOO bar"},
		{what: "gUiw", text: "foo bar", col: 5, seq: "gUiw", want: "foo BAR", cursor: true, wantCol: 4},
		{what: "guu", text: "ABC\nDEF", seq: "guu", want: "abc\nDEF"},
		{what: "gUU", text: "abc\ndef", seq: "gUU", want: "ABC\ndef"},
		{what: "g~~", text: "aBc", seq: "g~~", want: "AbC"},
		{what: "g~g~", text: "aBc", seq: "g~g~", want: "AbC"},
		{what: "gugu", text: "ABC", seq: "gugu", want: "abc"},
		{what: "2gUU", text: "a\nb\nc", seq: "2gUU", want: "A\nB\nc"},
		{what: "a combining mark stays", text: "éx", seq: "gUU", want: "ÉX"},
	})
}

// > and < shift whole lines by one unit's width, rebuilt in the unit's style; blank lines and an
// unindented line going out stay.
func TestVimShift(t *testing.T) {
	runEdits(t, []editCase{
		{what: ">> with a unit set", text: "a", seq: ">>", want: "  a", opts: []widget.CoreOption{widget.CoreIndentUnit("  ")}},
		{what: ">> with the buffer's own unit", text: "x\n   y", seq: ">>", want: "   x\n   y"},
		{what: ">> with none: a tab's width", text: "x", seq: ">>", want: "    x", cursor: true, wantCol: 4},
		{what: ">j", text: "a\nb\nc", seq: ">j", want: "    a\n    b\nc"},
		{what: ">ip", text: "a\nb\n\nc", seq: ">ip", want: "    a\n    b\n\nc"},
		{what: "a blank line is not indented", text: "a\n\nb", seq: ">2j", want: "    a\n\n    b"},
		{what: "<< six columns", text: "      a", seq: "<<", want: "  a", opts: []widget.CoreOption{widget.CoreIndentUnit("    ")}},
		{what: "<< unindented", text: "a", seq: "<<", want: "a"},
		{what: "<< a mixed indent", text: "\t  a", seq: "<<", want: "  a", opts: []widget.CoreOption{widget.CoreIndentUnit("    ")}},
		{what: ">> in a tab unit", text: "  a", seq: ">>", want: "\t  a", opts: []widget.CoreOption{widget.CoreIndentUnit("\t")}},
		{what: "3>>", text: "a\nb\nc\nd", seq: "3>>", want: "    a\n    b\n    c\nd"},
	})
}

// . repeats the last change: Normal commands as keys, Insert as typed and pasted text and keys.
func TestVimRepeat(t *testing.T) {
	runEdits(t, []editCase{
		{what: "dw.", text: "a b c d", seq: "dw.", want: "c d"},
		{what: "x.", text: "abc", seq: "x.", want: "c"},
		{what: "ciw then .", text: "foo bar", seq: "ciwX<esc>w.", want: "X X"},
		{what: "A then .", text: "a\nb", seq: "AZ<esc>j.", want: "aZ\nbZ"},
		{what: ">> then .", text: "a\nb", seq: ">>j.", want: "    a\n    b"},
		{what: "r then .", text: "abc", seq: "rxl.", want: "xxc"},
		{what: "J then .", text: "a\nb\nc", seq: "J.", want: "a b c"},
		{what: "yyp then .", text: "a", seq: "yyp.", want: "a\na\na"},
		{what: "N. replaces the count", text: "a b c d e f", seq: "2dw3.", want: "f"},
		{what: "N. replaces an operator's inner count", text: "a b c d e f g h i j k l", seq: "2d3w4.", want: "k l"},
		{what: "a motion or a yank is not a change", text: "a b c d", seq: "dwyww.", want: "b d"},
		{what: "the chord's tick commits text, not an escape", text: "", seq: "ij<tick>k<esc>0.", want: "jkjk"},
		{what: "a paste in Insert", text: "x", seq: "a<paste:y)z><esc>.", want: "xy)zy)z"},
		{what: "a multiline paste and Enter", text: "", seq: "i<paste:a\nb><cr>c<esc>.", want: "a\nb\na\nb\ncc"},
		{what: "Neovim's ins-repeat: iab<Left>c<Esc>.", text: "", seq: "iab<left>c<esc>.", want: "aacbcb"},
		{what: "a jump splits the change", text: "", seq: "iab<pgdn>cd<esc>.", want: "abccdd"},
		{what: "u is not a change", text: "abc", seq: "xu.", want: "bc"},
		{what: "a replay that changes nothing is no undo step", text: "a", seq: "x.u", want: "a"},
		{what: "i<Esc> changes nothing: x stays the last change", text: "abc", seq: "xi<esc>.", want: "c"},
		{what: "a jump with nothing after it keeps the segment before", text: "", seq: "iab<pgdn><esc>.", want: "aabb"},
	})
	c, _ := coreWith(t, "a b c")
	vimSeq(c, "dw.")
	vimSeq(c, "u")
	if c.Value() != "b c" {
		t.Fatalf("one u after a .: %q, want the first dw kept", c.Value())
	}
	k, _ := coreWith(t, "abc")
	vimSeq(k, "x")
	k.SetKeyset(widget.KeysetStandard)
	k.SetKeyset(widget.KeysetVim)
	vimSeq(k, "<esc>.")
	if k.Value() != "bc" {
		t.Fatalf("a keyset switch kept the change to repeat: %q", k.Value())
	}
	p, _ := coreWith(t, "")
	vimSeq(p, "iab")
	p.PressAt(0, 0)
	vimSeq(p, "c<esc>$.")
	if p.Value() != "cacb" {
		t.Fatalf("a press in Insert splits the change: %q, want cacb (. inserts only the c typed after it)", p.Value())
	}
	n, _ := coreWith(t, "")
	vimSeq(n, "iab<left>c<esc>.u")
	if n.Value() != "acb" {
		t.Fatalf("one u after an Insert replayed with a move: %q, want acb", n.Value())
	}
}

// A typed closer is aligned when replayed, a pasted one stays as pasted.
func TestVimRepeatTypedAndPastedClosers(t *testing.T) {
	d := golang.Definition()
	src := widget.CoreSourceFactory(func() highlight.Source { return d.NewSource(nil) })
	// the indenter decides a closer's place from verified highlight state, as the editor's
	// highlighter leaves it: verify every line before each edit
	verified := func(c *widget.EditorCore) {
		f := c.BeginHighlight(0)
		f.Styles(c.LineCount())
		f.Close()
	}
	doc := "func f() {\n\tx\n\t\nfunc g() {\n\ty\n\t"
	typed, _ := coreWith(t, doc, widget.CoreAutoIndent(true), src)
	verified(typed)
	typed.SetLine(2, 0)
	vimSeq(typed, "A}<esc>")
	first := strings.Split(typed.Value(), "\n")[2]
	verified(typed)
	typed.SetLine(5, 0)
	vimSeq(typed, ".")
	if got := strings.Split(typed.Value(), "\n")[5]; got != first || got != "}" {
		t.Fatalf("a typed } replayed as %q (the original %q), want } aligned", got, first)
	}
	pasted, _ := coreWith(t, doc, widget.CoreAutoIndent(true), src)
	verified(pasted)
	pasted.SetLine(2, 0)
	vimSeq(pasted, "A<paste:}><esc>")
	verified(pasted)
	pasted.SetLine(5, 0)
	vimSeq(pasted, ".")
	if got := strings.Split(pasted.Value(), "\n")[5]; got != "\t}" {
		t.Fatalf("a pasted } replayed as %q, want it literal", got)
	}
}

// A viewer refuses the new edits, and they bubble to the host.
func TestVimEditsOnAViewer(t *testing.T) {
	ro := widget.NewEditorCore(widget.CoreInitialText("abc\ndef"), widget.CoreReadOnly(true))
	ro.Bind(nil, &fakeLayout{per: 1, page: 5})
	for _, r := range "rJ~><" {
		if ro.HandleKey(tui.KeyEvent{Code: r, Text: string(r)}) {
			t.Errorf("a viewer consumed %c", r)
		}
	}
	vimSeq(ro, "gUU")
	if ro.Value() != "abc\ndef" {
		t.Fatalf("a viewer changed: %q", ro.Value())
	}
}

// A replay that changes nothing at a full history gives back its snapshot, and the oldest step
// is still there: every one of the 64 can be undone.
func TestVimRepeatAtAFullHistory(t *testing.T) {
	full := strings.Repeat("x", 70)
	c, _ := coreWith(t, full+"\n")
	for range 64 {
		vimSeq(c, "x") // 64 steps: the history is full
	}
	vimSeq(c, "j.") // x again, on the empty line: no change
	if c.Value() != strings.Repeat("x", 6)+"\n" {
		t.Fatalf("the fixture: %q", c.Value())
	}
	vimSeq(c, "k")
	for range 64 {
		vimSeq(c, "u")
	}
	if got := strings.Split(c.Value(), "\n")[0]; got != full {
		t.Fatalf("after 64 u: %d x, want 70: the replay cost a step", len(got))
	}
}
