package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// * # g* g#, as Vim's nv_ident: the expectations from headless Neovim where it was asked, the
// rest from the same rules.
func TestVimStar(t *testing.T) {
	const s = "foo bar foo foobar foo"
	runEdits(t, []editCase{
		{what: "*", text: s, seq: "*", want: s, cursor: true, wantCol: 8},
		{what: "2* skips foobar", text: s, seq: "2*", want: s, cursor: true, wantCol: 19},
		{what: "# wraps back", text: s, seq: "#", want: s, cursor: true, wantCol: 19},
		{what: "g*", text: s, seq: "g*", want: s, cursor: true, wantCol: 8},
		{what: "2g* finds foo in foobar", text: s, seq: "2g*", want: s, cursor: true, wantCol: 12},
		{what: "g# back from foobar's foo", text: s, col: 19, seq: "g#", want: s, cursor: true, wantCol: 12},
		{what: "the keyword after the cursor", text: "  foo x foo", seq: "*", want: "  foo x foo", cursor: true, wantCol: 8},
		{what: "a keyword after wins over a run under", text: "x ... y ...", seq: "w*", want: "x ... y ...", cursor: true, wantCol: 6},
		{what: "a non-keyword run with no keyword after", text: "a ++ ++", col: 2, seq: "*", want: "a ++ ++", cursor: true, wantCol: 5},
		{what: "case is matched", text: "abc ABC abc", seq: "*", want: "abc ABC abc", cursor: true, wantCol: 8},
		{what: "a word ends at a class change (Hangul)", text: "한국 한국어 한국", seq: "*", want: "한국 한국어 한국", cursor: true, wantCol: 7},
		{what: "n after *", text: "foo x foo y foo", seq: "*n", want: "foo x foo y foo", cursor: true, wantCol: 12},
		{what: "N after * stays whole-word", text: "foo foobar foo", seq: "*N", want: "foo foobar foo", cursor: true},
		{what: "* from inside the word", text: "foo bar foo", col: 1, seq: "*", want: "foo bar foo", cursor: true, wantCol: 8},
		{what: "N after *", text: "foo x foo y foo", seq: "*N", want: "foo x foo y foo", cursor: true},
		{what: "3#", text: "foo x foo y foo", col: 12, seq: "3#", want: "foo x foo y foo", cursor: true, wantCol: 12},
		{what: "* is a jump: Ctrl-o goes back", text: "  foo x foo", seq: "*<cto>", want: "  foo x foo", cursor: true},
		{what: "nothing on the line", text: "   \nfoo", seq: "*", want: "   \nfoo", cursor: true},
	})
}

// A host that keeps its own find takes * and #: the core keeps no find of its own, and the cursor
// waits on the word's start.
func TestVimStarHost(t *testing.T) {
	c, _ := coreWith(t, "  foo x foo")
	var got []widget.FindRequest
	c.SetFindHost(func(r widget.FindRequest) bool { got = append(got, r); return true })
	vimSeq(c, "2#")
	if len(got) != 1 || got[0].Term != "foo" || !got[0].Options.WholeWord || !got[0].Backward || got[0].Count != 2 {
		t.Fatalf("the host was asked %+v", got)
	}
	if term, _ := c.FindTerm(); term != "" {
		t.Fatalf("the core kept a find of its own: %q", term)
	}
	if _, col := c.Line(); col != 2 {
		t.Fatalf("the cursor is at %d, want the word's start", col)
	}
	vimSeq(c, "g*")
	if len(got) != 2 || got[1].Options.WholeWord {
		t.Fatalf("g*: %+v", got)
	}
	declined, _ := coreWith(t, "foo x foo")
	declined.SetFindHost(func(widget.FindRequest) bool { return false })
	vimSeq(declined, "*")
	if term, o := declined.FindTerm(); term != "foo" || !o.WholeWord {
		t.Fatalf("a host that declined: the core's find is %q %+v", term, o)
	}
}

// WholeWord through the API: FindAll and the marks' rule.
func TestFindWholeWord(t *testing.T) {
	c, _ := coreWith(t, "foo foobar _foo foo.bar 漢字abc")
	var cols []int
	for _, r := range c.FindAll("foo", widget.FindOptions{WholeWord: true}) {
		cols = append(cols, r.From.Col)
	}
	if len(cols) != 2 || cols[0] != 0 || cols[1] != 16 {
		t.Fatalf("whole foo at %v, want [0 16]", cols)
	}
	if n := len(c.FindAll("abc", widget.FindOptions{WholeWord: true})); n != 1 {
		t.Fatalf("abc after 漢字: %d matches, want 1 (a class change ends a word)", n)
	}
	if !widget.IsKeyword("a") || !widget.IsKeyword("_") || !widget.IsKeyword("漢") || widget.IsKeyword(".") || widget.IsKeyword(" ") {
		t.Fatal("IsKeyword")
	}
}

// A viewer finds too: * changes nothing.
func TestVimStarOnAViewer(t *testing.T) {
	c, _ := coreWith(t, "foo x foo", widget.CoreReadOnly(true))
	vimSeq(c, "*")
	if _, col := c.Line(); col != 6 {
		t.Fatalf("a viewer's *: col %d", col)
	}
}
