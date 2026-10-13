package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// vimType types s into the core, <esc> as Escape.
func vimType(c *widget.EditorCore, s string) {
	for i, part := range strings.Split(s, "<esc>") {
		if i > 0 {
			c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
		}
		keys(c, part)
	}
}

// opCase is keys typed from a cursor, and the text and register they leave.
type opCase struct {
	what      string
	text      string
	ln, col   int
	keys      string
	want      string
	reg       string // "" when the register is not checked
	linewise  bool
	wantLn    int
	wantCol   int
	checkCurs bool
}

func runOps(t *testing.T, cases []opCase) {
	t.Helper()
	for _, oc := range cases {
		c, _ := coreWith(t, oc.text)
		c.SetLine(oc.ln, oc.col)
		vimType(c, oc.keys)
		if got := c.Value(); got != oc.want {
			t.Errorf("%s: %q from (%d,%d) by %q: %q, want %q", oc.what, oc.text, oc.ln, oc.col, oc.keys, got, oc.want)
			continue
		}
		if oc.reg != "" {
			if reg, lw := c.Register(); reg != oc.reg || lw != oc.linewise {
				t.Errorf("%s: register %q linewise %v, want %q %v", oc.what, reg, lw, oc.reg, oc.linewise)
			}
		}
		if oc.checkCurs {
			if ln, col := c.Line(); ln != oc.wantLn || col != oc.wantCol {
				t.Errorf("%s: cursor (%d,%d), want (%d,%d)", oc.what, ln, col, oc.wantLn, oc.wantCol)
			}
		}
	}
}

// d over the three kinds of motion, Vim's special cases, and ranges of one cluster or one line,
// which are edits, not empty.
func TestVimDeleteOverMotions(t *testing.T) {
	runOps(t, []opCase{
		{what: "dw, exclusive", text: "foo bar baz", keys: "dw", want: "bar baz", reg: "foo "},
		{what: "de, inclusive", text: "foo bar baz", keys: "de", want: " bar baz", reg: "foo"},
		{what: "d$ to the end", text: "foo bar baz", col: 4, keys: "d$", want: "foo ", reg: "bar baz", checkCurs: true, wantCol: 3},
		{what: "dj, linewise", text: "one\ntwo\nthree", keys: "dj", want: "three", reg: "one\ntwo", linewise: true},
		{what: "d2j", text: "a\nb\nc\nd", keys: "d2j", want: "d"},
		{what: "3dd", text: "a\nb\nc\nd", keys: "3dd", want: "d", reg: "a\nb\nc", linewise: true},
		{what: "2d3w: counts multiply", text: "a b c d e f g h", keys: "2d3w", want: "g h"},
		{what: "dl on the last cluster", text: "abc", col: 2, keys: "dl", want: "ab", reg: "c"},
		{what: "dw on the text's last word", text: "foo bar", col: 4, keys: "dw", want: "foo ", reg: "bar"},
		{what: "dw on a line's last word does not join", text: "foo bar\nbaz", col: 4, keys: "dw", want: "foo \nbaz"},
		{what: "dw on a one-word line", text: "foo\nbar", keys: "dw", want: "\nbar"},
		{what: "d}: exclusive-linewise", text: "a\nb\n\nc", keys: "d}", want: "\nc", linewise: true, reg: "a\nb"},
		{what: "d} from mid-line: to the line's end", text: "xa\nb\n\nc", col: 1, keys: "d}", want: "x\n\nc"},
		{what: "dtb: an inclusive range of one cluster", text: "abc", keys: "dtb", want: "bc", reg: "a"},
		{what: "d$ on the last cluster", text: "abc", col: 2, keys: "d$", want: "ab", reg: "c"},
		{what: "de across the line's end", text: "ab cd\nx", col: 4, keys: "de", want: "ab c"},
		{what: "dd on a one-line text", text: "abc", keys: "dd", want: "", reg: "abc", linewise: true},
		{what: "d$ on an empty line", text: "\nx", keys: "d$", want: "\nx"},
		{what: "df,", text: "a,b,c", keys: "df,", want: "b,c", reg: "a,"},
		{what: "dt,", text: "a,b,c", keys: "dt,", want: ",b,c"},
		{what: "dF, back, exclusive", text: "a,b,c", col: 4, keys: "dF,", want: "a,bc"},
		{what: "d; repeats the find", text: "a,b,c,d", keys: "f,0d;", want: "b,c,d"},
		{what: "dge", text: "foo bar", col: 6, keys: "dge", want: "fo"},
		{what: "dgg", text: "a\nb\nc", ln: 2, keys: "dgg", want: ""},
		{what: "d2G: G's count is a line", text: "a\nb\nc\nd", ln: 3, keys: "d2G", want: "a"},
		{what: "d2gg", text: "a\nb\nc\nd", keys: "d2gg", want: "c\nd"},
		{what: "2dgg: the operator's count is gg's line", text: "a\nb\nc\nd", keys: "2dgg", want: "c\nd"},
		{what: "d50%: % with a count is a percentage", text: "a\nb\nc\nd", keys: "d50%", want: "c\nd"},
		{what: "d%", text: "f(a(b)) z", col: 1, keys: "d%", want: "f z"},
		{what: "d2$: $ with a count crosses lines", text: "ab\ncd\nef", col: 1, keys: "d2$", want: "a\nef"},
		{what: "3d^: ^ ignores a count", text: "  abc", col: 4, keys: "3d^", want: "  c"},
		{what: "d0", text: "abc", col: 2, keys: "d0", want: "c"},
		{what: "dB", text: "foo.bar baz", col: 8, keys: "dB", want: "baz"},
		{what: "dk", text: "a\nb\nc", ln: 1, keys: "dk", want: "c"},
		{what: "a failing motion changes nothing", text: "abc", keys: "dfz", want: "abc"},
		{what: "dj on the last line fails", text: "a\nb", ln: 1, keys: "dj", want: "a\nb"},
	})
}

// c deletes and types in its place, one undo step; cw is ce; cc keeps the indent; C s S.
func TestVimChange(t *testing.T) {
	runOps(t, []opCase{
		{what: "cw is ce", text: "foo bar", keys: "cwX<esc>", want: "X bar"},
		{what: "cw on blanks is dw", text: "a   b", col: 1, keys: "cwX<esc>", want: "aXb"},
		{what: "cc keeps the indent", text: "  foo\nbar", keys: "ccX<esc>", want: "  X\nbar", reg: "  foo", linewise: true},
		{what: "cc on a one-line text", text: "abc", keys: "ccX<esc>", want: "X"},
		{what: "C", text: "foo bar", col: 4, keys: "CX<esc>", want: "foo X"},
		{what: "C on the last cluster", text: "abc", col: 2, keys: "CX<esc>", want: "abX"},
		{what: "2s", text: "abc", keys: "2sX<esc>", want: "Xc"},
		{what: "s on an empty line", text: "", keys: "sX<esc>", want: "X"},
		{what: "S", text: "  abc\nd", keys: "SX<esc>", want: "  X\nd"},
		{what: "c0 at the start types there", text: "abc", keys: "c0X<esc>", want: "Xabc"},
		{what: "c2j", text: "a\nb\nc\nd", keys: "c2jX<esc>", want: "X\nd"},
	})
	c, _ := coreWith(t, "foo bar")
	vimType(c, "cwhello world<esc>")
	if c.Value() != "hello world bar" {
		t.Fatalf("cw then typing: %q", c.Value())
	}
	vimType(c, "u")
	if c.Value() != "foo bar" {
		t.Fatalf("one u after a c: %q, want the text before it", c.Value())
	}
}

// y copies without changing the text; Y is y$ (Neovim's mapping); yy is the line.
func TestVimYank(t *testing.T) {
	runOps(t, []opCase{
		{what: "yw", text: "foo bar", keys: "yw", want: "foo bar", reg: "foo "},
		{what: "Y is y$", text: "foo bar", col: 4, keys: "Y", want: "foo bar", reg: "bar", checkCurs: true, wantCol: 4},
		{what: "yy", text: "foo bar\nx", col: 2, keys: "yy", want: "foo bar\nx", reg: "foo bar", linewise: true, checkCurs: true, wantCol: 2},
		{what: "yb moves to the start", text: "foo bar", col: 4, keys: "yb", want: "foo bar", reg: "foo ", checkCurs: true},
		{what: "yk moves up", text: "a\nb", ln: 1, keys: "yk", want: "a\nb", reg: "a\nb", linewise: true, checkCurs: true},
		{what: "y$ on the last cluster", text: "abc", col: 2, keys: "y$", want: "abc", reg: "c"},
	})
}

// An operator is cancelled by Esc, by a key that is no motion (processed afresh), and by a
// command boundary; a viewer refuses d and c and allows y; an overlay's motion works in an
// operator.
func TestVimOperatorPending(t *testing.T) {
	c, _ := coreWith(t, "abc")
	vimType(c, "d<esc>x")
	if c.Value() != "bc" {
		t.Fatalf("d Esc x: %q", c.Value())
	}
	vimType(c, "dv")
	if c.Value() != "bc" || c.Mode() != widget.ModeVisual {
		t.Fatalf("d v: %q in %v; want the operator cancelled and v to enter Visual", c.Value(), c.Mode())
	}
	vimType(c, "<esc>")
	vimType(c, "d")
	c.FocusLost()
	vimType(c, "l")
	if ln, col := c.Line(); c.Value() != "bc" || ln != 0 || col != 1 {
		t.Fatalf("d, focus lost, l: %q at (%d,%d); want l to move", c.Value(), ln, col)
	}

	ro := widget.NewEditorCore(widget.CoreInitialText("foo bar"), widget.CoreReadOnly(true))
	ro.Bind(nil, &fakeLayout{per: 1, page: 5})
	vimType(ro, "dwcwX<esc>CSs")
	if ro.Value() != "foo bar" {
		t.Fatalf("a viewer changed: %q", ro.Value())
	}
	vimType(ro, "0yw") // the refused operators' w keys moved the cursor
	if reg, _ := ro.Register(); reg != "foo " {
		t.Fatalf("a viewer's yw: register %q", reg)
	}

	bs, _ := coreWith(t, "abc")
	bs.SetLine(0, 2)
	vimType(bs, "d")
	special(bs, tui.KeyBackspace)
	if bs.Value() != "ac" {
		t.Fatalf("d Backspace: %q, want ac", bs.Value())
	}

	ov, _ := coreWith(t, "foo bar baz", widget.CoreKeymap(widget.Keymap{
		{Mode: widget.ModeNormal, Code: 'z'}: widget.ActWordForward,
	}))
	vimType(ov, "dz")
	if ov.Value() != "bar baz" {
		t.Fatalf("d over an overlay's motion: %q", ov.Value())
	}
}

// Text objects after an operator: words, WORDs, paragraphs, quotes and brackets, with counts.
func TestVimTextObjects(t *testing.T) {
	runOps(t, []opCase{
		{what: "diw", text: "foo bar baz", col: 5, keys: "diw", want: "foo  baz", reg: "bar"},
		{what: "daw", text: "foo bar baz", col: 5, keys: "daw", want: "foo baz", reg: "bar "},
		{what: "ciw", text: "foo bar baz", col: 4, keys: "ciwX<esc>", want: "foo X baz"},
		{what: "diw on blanks", text: "foo   bar", col: 4, keys: "diw", want: "foobar"},
		{what: "daw on blanks", text: "foo   bar", col: 4, keys: "daw", want: "foo"},
		{what: "daw at the end: the blanks before", text: "foo bar", col: 5, keys: "daw", want: "foo"},
		{what: "d2aw", text: "a b c d", keys: "d2aw", want: "c d"},
		{what: "d3iw", text: "a b c d", keys: "d3iw", want: " c d"},
		{what: "diw stops at punctuation", text: "foo.bar", keys: "diw", want: ".bar"},
		{what: "diW", text: "foo.bar baz", keys: "diW", want: " baz"},
		{what: "dip", text: "a\nb\n\nc", keys: "dip", want: "\nc", linewise: true, reg: "a\nb"},
		{what: "dap", text: "a\nb\n\nc", keys: "dap", want: "c"},
		{what: `di"`, text: `say "hi there" ok`, col: 6, keys: `di"`, want: `say "" ok`, reg: "hi there"},
		{what: `da"`, text: `say "hi there" ok`, col: 6, keys: `da"`, want: "say ok"},
		{what: `di" before the quotes`, text: `say "hi" ok`, keys: `di"`, want: `say "" ok`},
		{what: `di" on the closing quote`, text: `say "hi" ok`, col: 7, keys: `di"`, want: `say "" ok`},
		{what: `di" past an escaped quote`, text: `a "b\"c" d`, col: 3, keys: `di"`, want: `a "" d`},
		{what: `ci" of empty quotes`, text: `a "" b`, col: 2, keys: `ci"X<esc>`, want: `a "X" b`},
		{what: `di" with no quotes`, text: "abc", keys: `di"`, want: "abc"},
		{what: "di'", text: "x 'y' z", col: 3, keys: "di'", want: "x '' z"},
		{what: "di(", text: "f(a, (b))", col: 6, keys: "di(", want: "f(a, ())"},
		{what: "dib", text: "f(a, (b))", col: 6, keys: "dib", want: "f(a, ())"},
		{what: "da(", text: "f(a, (b))", col: 6, keys: "da(", want: "f(a, )"},
		{what: "2di(", text: "f(a, (b))", col: 6, keys: "2di(", want: "f()"},
		{what: "di( on the open bracket", text: "f(a)", col: 1, keys: "di(", want: "f()"},
		{what: "di( on the close bracket", text: "f(a)", col: 3, keys: "di(", want: "f()"},
		{what: "di( across lines", text: "f(\n  x\n  y\n)", ln: 1, col: 2, keys: "di(", want: "f(\n)", linewise: true, reg: "  x\n  y"},
		{what: "di{", text: "{a {b} c}", col: 4, keys: "di{", want: "{a {} c}"},
		{what: "diB", text: "{a {b} c}", col: 1, keys: "diB", want: "{}"},
		{what: "di[", text: "[1, [2]]", col: 1, keys: "di[", want: "[]"},
		{what: "di<", text: "a<b<c>>", col: 4, keys: "di<", want: "a<b<>>"},
		{what: "2di<", text: "a<b<c>>", col: 4, keys: "2di<", want: "a<>"},
		{what: "di< with an unmatched <", text: "<a", col: 1, keys: "di<", want: "<a"},
		{what: "di( with none around", text: "abc", col: 1, keys: "di(", want: "abc"},
		{what: "yiw", text: "foo bar", col: 5, keys: "yiw", want: "foo bar", reg: "bar"},
	})
}

// In Visual, i and a select a text object; ip and ap select whole lines.
func TestVimVisualTextObjects(t *testing.T) {
	c, _ := coreWith(t, "foo bar baz")
	c.SetLine(0, 5)
	vimType(c, "viw")
	if r, cc, er, ec, ok := c.SelectionRange(); !ok || r != 0 || cc != 4 || er != 0 || ec != 7 {
		t.Fatalf("viw: (%d,%d)-(%d,%d) %v, want bar", r, cc, er, ec, ok)
	}
	vimType(c, "d")
	if c.Value() != "foo  baz" {
		t.Fatalf("viwd: %q", c.Value())
	}
	p, _ := coreWith(t, "a\nb\n\nc")
	vimType(p, "vip")
	if p.Mode() != widget.ModeVisualLine {
		t.Fatalf("vip: mode %v, want Visual line", p.Mode())
	}
	vimType(p, "d")
	if p.Value() != "\nc" {
		t.Fatalf("vipd: %q", p.Value())
	}
}
