package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// reg is register name's content as "text" with a trailing "⏎" when it is whole lines, "∅" for none.
func reg(c *widget.EditorCore, name rune) string {
	text, linewise, ok := c.NamedRegister(name)
	switch {
	case !ok:
		return "∅"
	case linewise:
		return text + "⏎"
	}
	return text
}

type regCase struct {
	what string
	text string
	seq  string
	want string          // the text after, "" to skip
	regs map[rune]string // registers after, as reg shows them
	opts []widget.CoreOption
}

func runRegs(t *testing.T, cases []regCase) {
	t.Helper()
	for _, e := range cases {
		c, _ := coreWith(t, e.text, e.opts...)
		c.SetLine(0, 0)
		vimSeq(c, e.seq)
		if e.want != "" && c.Value() != e.want {
			t.Errorf("%s: %q by %q: %q, want %q", e.what, e.text, e.seq, c.Value(), e.want)
		}
		for name, want := range e.regs {
			if got := reg(c, name); got != want {
				t.Errorf("%s: %q by %q: register %q holds %q, want %q", e.what, e.text, e.seq, name, got, want)
			}
		}
	}
}

// Which registers a yank writes (Vim's get_yank_register): the one named, else 0; the unnamed
// register follows either way. A–Z append, whole lines when either part is.
func TestVimRegistersYank(t *testing.T) {
	runRegs(t, []regCase{
		{what: "yy: the unnamed and 0", text: "one\ntwo", seq: "yy",
			regs: map[rune]string{'"': "one⏎", '0': "one⏎", 'a': ""}},
		{what: `"ayy: a and the unnamed, not 0`, text: "one\ntwo", seq: `"ayyj"ap`, want: "one\ntwo\none",
			regs: map[rune]string{'a': "one⏎", '"': "one⏎", '0': ""}},
		{what: `"Ayy appends lines`, text: "one\ntwo", seq: `"ayyj"Ayy`,
			regs: map[rune]string{'a': "one\ntwo⏎", '"': "one\ntwo⏎"}},
		{what: `"Ayw appends text`, text: "ab cd", seq: `"ayww"Ayw`, regs: map[rune]string{'a': "ab cd"}},
		{what: `"Ayy onto text: whole lines`, text: "ab\ncd", seq: `"aylj"Ayy`, regs: map[rune]string{'a': "a\ncd⏎"}},
		{what: `"A onto nothing`, text: "ab", seq: `"Ayl`, regs: map[rune]string{'a': "a"}},
		{what: "a count before the name stays", text: "a\nb\nc", seq: `2"ayy`, regs: map[rune]string{'a': "a\nb⏎"}},
		{what: "a count after the name", text: "a\nb\nc", seq: `"a2yy`, regs: map[rune]string{'a': "a\nb⏎"}},
		{what: "the name lasts one command", text: "x\ny", seq: `"ajyy`,
			regs: map[rune]string{'a': "", '"': "y⏎", '0': "y⏎"}},
		{what: "Esc drops the name", text: "x", seq: `"a<esc>yy`, regs: map[rune]string{'a': "", '0': "x⏎"}},
		{what: "a key naming no register cancels it", text: "abc", seq: `"#x`, want: "bc", regs: map[rune]string{'-': "a"}},
		{what: `Visual "ay`, text: "abc", seq: `vl"ay`, regs: map[rune]string{'a': "ab", '0': ""}},
		{what: `"_yy keeps nothing`, text: "a\nb", seq: `yyj"_yy`, regs: map[rune]string{'"': "a⏎", '0': "a⏎"}},
		{what: `"+yy: + and the unnamed`, text: "one", seq: `"+yy`, regs: map[rune]string{'+': "one⏎", '"': "one⏎", '0': ""}},
		{what: `"*yy`, text: "one", seq: `"*yy`, regs: map[rune]string{'*': "one⏎", '+': ""}},
	})
}

// Which registers a delete writes (Vim's op_delete): the one named; then 1, the numbered ones
// shifting, for a line or more or a motion Vim keeps there (%, {, }); else - when none was named.
// The black hole keeps nothing, and the unnamed register keeps what it had.
func TestVimRegistersDelete(t *testing.T) {
	runRegs(t, []regCase{
		{what: "dd: 1 and the unnamed", text: "1\n2\n3", seq: "dd", want: "2\n3",
			regs: map[rune]string{'1': "1⏎", '"': "1⏎", '-': "", '0': ""}},
		{what: "dd dd: 1 shifts to 2", text: "1\n2\n3", seq: "dddd", want: "3",
			regs: map[rune]string{'1': "2⏎", '2': "1⏎", '"': "2⏎"}},
		{what: "nine shifts drop the oldest", text: "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11", seq: "dddddddddddddddddddd",
			regs: map[rune]string{'1': "10⏎", '9': "2⏎"}},
		{what: "x: the small delete register", text: "abc", seq: "x", want: "bc",
			regs: map[rune]string{'-': "a", '"': "a", '1': ""}},
		{what: "dw within a line: -", text: "ab cd", seq: "dw", regs: map[rune]string{'-': "ab ", '1': ""}},
		{what: "a delete across lines: 1", text: "ab\ncd", seq: "lvjd", regs: map[rune]string{'1': "b\ncd", '-': ""}},
		{what: "d% within a line: 1", text: "(x) y", seq: "d%", want: " y", regs: map[rune]string{'1': "(x)", '-': ""}},
		{what: "d} : 1", text: "a b", seq: "d}", regs: map[rune]string{'1': "a b", '-': ""}},
		{what: `"ax: a, not -`, text: "abc", seq: `"ax`, regs: map[rune]string{'a': "a", '-': "", '"': "a", '1': ""}},
		{what: `"add: a and 1`, text: "a\nb", seq: `"add`, regs: map[rune]string{'a': "a⏎", '1': "a⏎", '"': "a⏎"}},
		{what: `"Add: 1 holds the delete, the unnamed the appended a`, text: "a\nb\nc", seq: `"ayyj"Add`,
			regs: map[rune]string{'a': "a\nb⏎", '1': "b⏎", '"': "a\nb⏎"}},
		{what: `"_dd keeps nothing`, text: "a\nb", seq: `yyj"_ddp`, want: "a\na",
			regs: map[rune]string{'1': "", '-': "", '"': "a⏎"}},
		{what: `"_x keeps nothing`, text: "ab", seq: `"_x`, want: "b", regs: map[rune]string{'-': "", '"': ""}},
		{what: "cw: -", text: "ab cd", seq: "cwX<esc>", want: "X cd", regs: map[rune]string{'-': "ab"}},
		{what: "cc: 1", text: "ab\ncd", seq: "ccX<esc>", want: "X\ncd", regs: map[rune]string{'1': "ab⏎"}},
		{what: `"add then .: into a again`, text: "a\nb\nc", seq: `"add.`, want: "c", regs: map[rune]string{'a': "b⏎", '1': "b⏎", '2': "a⏎"}},
	})
}

// p and P put the register the command names, count times; nothing from an empty one.
func TestVimRegistersPut(t *testing.T) {
	runRegs(t, []regCase{
		{what: `"ap`, text: "x\ny", seq: `"ayyj"ap`, want: "x\ny\nx"},
		{what: `"aP`, text: "x\ny", seq: `"ayyj"aP`, want: "x\nx\ny"},
		{what: "3p lines", text: "a", seq: "yy3p", want: "a\na\na\na"},
		{what: "3p text", text: "ab", seq: "yl3p", want: "aaaab"},
		{what: `"0p after a delete`, text: "a\nb", seq: "yyjdd\"0p", want: "a\na"},
		{what: `"1p and "2p`, text: "1\n2\n3", seq: `dddd"2p"1p`, want: "3\n1\n2"},
		{what: `"-p`, text: "abc", seq: `x"-p`, want: "bac"},
		{what: `"+p puts what was last written there`, text: "ab", seq: `"+yl$"+p`, want: "aba"},
		{what: "an empty register puts nothing", text: "ab", seq: `"bp`, want: "ab"},
		{what: `"_p puts nothing`, text: "ab", seq: `yl"_p`, want: "ab"},
	})
}

// NamedRegister and SetNamedRegister beside Register and SetRegister, unchanged: the unnamed one.
func TestVimRegistersAPI(t *testing.T) {
	c, _ := coreWith(t, "x")
	if !c.SetNamedRegister('b', "hi", false) || reg(c, 'b') != "hi" || reg(c, '"') != "hi" || reg(c, '0') != "" {
		t.Fatalf("SetNamedRegister b: b %q, unnamed %q, 0 %q", reg(c, 'b'), reg(c, '"'), reg(c, '0'))
	}
	if !c.SetNamedRegister('B', "there", true) || reg(c, 'b') != "hi\nthere⏎" {
		t.Fatalf("SetNamedRegister B appends: %q", reg(c, 'b'))
	}
	for _, name := range []rune{'_', '"', '!'} {
		if c.SetNamedRegister(name, "z", false) {
			t.Errorf("SetNamedRegister(%q) set it", name)
		}
	}
	if reg(c, '_') != "∅" || reg(c, '!') != "∅" {
		t.Errorf("_ %q, ! %q: want no register", reg(c, '_'), reg(c, '!'))
	}
	c.SetRegister("plain", false)
	if text, lw := c.Register(); text != "plain" || lw || reg(c, '"') != "plain" || reg(c, 'b') != "hi\nthere⏎" {
		t.Errorf("SetRegister: Register %q %v, \" %q, b %q", text, lw, reg(c, '"'), reg(c, 'b'))
	}
	vimSeq(c, `"bp`)
	if c.Value() != "x\nhi\nthere" {
		t.Errorf(`"bp put %q`, c.Value())
	}
}

// A viewer names registers and yanks into them; a put from one is refused, and bubbles (#285).
func TestVimRegistersOnAViewer(t *testing.T) {
	c, _ := coreWith(t, "a\nb", widget.CoreReadOnly(true))
	vimSeq(c, `"ayy`)
	if reg(c, 'a') != "a⏎" {
		t.Fatalf("a viewer's \"ayy: a holds %q", reg(c, 'a'))
	}
	keys(c, `"a`)
	if c.HandleKey(key('p')) || c.Value() != "a\nb" {
		t.Fatalf("a viewer's \"ap was consumed or put: %q", c.Value())
	}
}

// What reaches the system clipboard: a delete only when + or * is named for it (a deleted secret
// never leaves on its own), and nothing from the black hole.
func TestVimRegistersClipboardExport(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 4)
	h.onLoop(func() { ed.SetValue("secret\nkeep\nlast") })
	h.barrier(sh)
	h.inject(key('g'), key('g'), key('d'), key('d')) // an unnamed delete
	h.barrier(sh)
	if got := h.tb.Clipboard(); got != nil {
		t.Fatalf("an unnamed dd reached the clipboard: %q", got)
	}
	h.inject(key('"'), key('_'), key('y'), key('y')) // the black hole
	h.barrier(sh)
	if got := h.tb.Clipboard(); got != nil {
		t.Fatalf(`"_yy reached the clipboard: %q`, got)
	}
	h.inject(key('"'), key('+'), key('d'), key('d')) // a delete named into +
	h.waitFor("the named delete on the clipboard", func() bool { return string(h.tb.Clipboard()) == "keep" })
}
