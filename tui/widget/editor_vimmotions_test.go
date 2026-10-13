package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// fakeViewport is a fakeLayout that shows lines first..last and records its scrolls.
type fakeViewport struct {
	fakeLayout
	first, last int
	scrolls     []int
}

func (v *fakeViewport) VisibleLines() (int, int) { return v.first, v.last }
func (v *fakeViewport) ScrollLines(d int)        { v.scrolls = append(v.scrolls, d) }

func viewportCore(t *testing.T, text string, first, last, page int) (*widget.EditorCore, *fakeViewport) {
	t.Helper()
	v := &fakeViewport{fakeLayout: fakeLayout{per: 24, page: page}, first: first, last: last}
	c := widget.NewEditorCore(widget.CoreInitialText(text))
	c.Bind(nil, v)
	return c, v
}

func special(c *widget.EditorCore, code rune) bool { return c.HandleKey(tui.KeyEvent{Code: code}) }

func ctrlKey(c *widget.EditorCore, r rune) bool {
	return c.HandleKey(tui.KeyEvent{Code: r, Mods: tui.ModCtrl})
}

func at(t *testing.T, c *widget.EditorCore, what string, ln, col int) {
	t.Helper()
	if l, cc := c.Line(); l != ln || cc != col {
		t.Fatalf("%s: cursor (%d, %d), want (%d, %d)", what, l, cc, ln, col)
	}
}

// Enter, + and - go count lines down or up to the first non-blank; ^ goes to it on the line, and
// on a blank-only line that is its last cluster, as in Vim.
func TestVimLineMotionsLandOnTheFirstNonBlank(t *testing.T) {
	c, _ := coreWith(t, "  alpha\n\tbeta\ngamma\n   \nend")
	special(c, tui.KeyEnter)
	at(t, c, "Enter", 1, 1)
	keys(c, "+")
	at(t, c, "+", 2, 0)
	keys(c, "+")
	at(t, c, "+ onto a blank-only line", 3, 2)
	keys(c, "3-")
	at(t, c, "3-", 0, 2)
	keys(c, "$^")
	at(t, c, "$ then ^", 0, 2)
	keys(c, "9")
	special(c, tui.KeyEnter)
	at(t, c, "9 Enter stops at the last line", 4, 0)
	keys(c, "9-")
	at(t, c, "9- stops at the first", 0, 2)
	keys(c, "2+")
	at(t, c, "2+", 2, 0)
}

// Backspace goes count clusters left, onto the previous line's last from a line's start.
func TestVimBackspaceWrapsOntoThePreviousLine(t *testing.T) {
	c, _ := coreWith(t, "ab\ncd\nef")
	keys(c, "jj")
	special(c, tui.KeyBackspace)
	at(t, c, "BS at a line's start", 1, 1)
	keys(c, "3")
	special(c, tui.KeyBackspace)
	at(t, c, "3 BS", 0, 0)
	if !special(c, tui.KeyBackspace) {
		t.Fatal("BS at the text's start was not consumed")
	}
	at(t, c, "BS at the text's start", 0, 0)
}

// f F t T find the countth character on the line, a digit or a space included; ; and , repeat the
// last find either way, and a repeated till moves past the match it stopped short of.
func TestVimCharacterFinds(t *testing.T) {
	const line = "a,b,c,d 3 x3"
	c, _ := coreWith(t, line)
	keys(c, "f,")
	at(t, c, "f,", 0, 1)
	keys(c, "2f,")
	at(t, c, "2f,", 0, 5)
	keys(c, ";")
	at(t, c, "; past the last comma", 0, 5)
	keys(c, ",")
	at(t, c, ", reverses", 0, 3)
	keys(c, "0f3")
	at(t, c, "f3: the digit is the target, not a count", 0, 8)
	keys(c, "02f3")
	at(t, c, "2f3", 0, 11)
	keys(c, "0f ")
	at(t, c, "f and a space", 0, 7)
	keys(c, "09f,")
	at(t, c, "9f, with three commas", 0, 0)

	keys(c, "t,")
	at(t, c, "t, with the comma next", 0, 0)
	keys(c, ";")
	at(t, c, "; after t skips the adjacent comma", 0, 2)
	keys(c, "$F,")
	at(t, c, "F,", 0, 5)
	keys(c, "T,")
	at(t, c, "T,", 0, 4)
	keys(c, "T,")
	at(t, c, "T, with the comma next stays", 0, 4)
	keys(c, ";")
	at(t, c, "; after T skips it", 0, 2)
	keys(c, ",")
	at(t, c, ", after T is a forward till", 0, 4)
	keys(c, "02,")
	at(t, c, "2, after T", 0, 4)
	if c.Value() != line {
		t.Fatalf("finds changed the text: %q", c.Value())
	}
}

// A find's target is the typed cluster; a key with no text cancels the find and acts afresh; Esc
// cancels it, consumed; a command boundary (focus leaving, a press) drops an armed find.
func TestVimCharacterFindArgument(t *testing.T) {
	c, _ := coreWith(t, "xéy,z")
	keys(c, "f")
	c.HandleKey(tui.KeyEvent{Code: 'e', Text: "éq"})
	at(t, c, "f and a composed cluster", 0, 1)

	keys(c, "0f")
	special(c, tui.KeyRight)
	at(t, c, "an arrow after f moves", 0, 1)
	keys(c, "l")
	at(t, c, "the key after the arrow is not a target", 0, 2)

	keys(c, "0f")
	if !special(c, tui.KeyEscape) {
		t.Fatal("Esc after f bubbled")
	}
	keys(c, "l")
	at(t, c, "the key after Esc is not a target", 0, 1)

	keys(c, "0f")
	ctrlKey(c, 'd')
	keys(c, ",")
	at(t, c, "a Ctrl key cancels f", 0, 0)

	keys(c, "0f")
	c.FocusLost()
	keys(c, "l")
	at(t, c, "focus leaving drops f", 0, 1)
	keys(c, "f")
	c.PressAt(0, 0)
	keys(c, "l")
	at(t, c, "a press drops f", 0, 1)
	keys(c, "f")
	c.Select(widget.TextRange{From: widget.TextPos{Line: 0, Col: 0}, To: widget.TextPos{Line: 0, Col: 2}})
	keys(c, "l")
	at(t, c, "a mode change (Select, into Visual) drops f", 0, 2)
	special(c, tui.KeyEscape)
	keys(c, "0lf")
	keys(c, "i")
	at(t, c, "f then i finds no i", 0, 1)
	if c.Mode() != widget.ModeNormal {
		t.Fatalf("f i entered %v: i was the target", c.Mode())
	}
}

// % goes to the partner of the bracket under or after the cursor, counting nesting across lines;
// N% goes to line N% of the text, N past 100 taken as 100.
func TestVimMatchPairAndPercent(t *testing.T) {
	c, _ := coreWith(t, "f(a, (b)) z\n{\n  [x]\n}\nplain (((")
	keys(c, "%")
	at(t, c, "% to the bracket after the cursor's partner", 0, 8)
	keys(c, "%")
	at(t, c, "% back", 0, 1)
	keys(c, "4l%")
	at(t, c, "% from the inner (", 0, 7)
	keys(c, "j%")
	at(t, c, "% across lines", 3, 0)
	keys(c, "%")
	at(t, c, "% back across lines", 1, 0)
	keys(c, "j0%")
	at(t, c, "% from blanks to [ ... ]", 2, 4)
	keys(c, "Gl%")
	at(t, c, "% with no partner", 4, 1)
	keys(c, "0w%")
	at(t, c, "% on ((( unmatched", 4, 6)

	keys(c, "50%")
	at(t, c, "50% of 5 lines", 2, 2)
	keys(c, "1%")
	at(t, c, "1%", 0, 0)
	keys(c, "100%")
	at(t, c, "100%", 4, 0)
	keys(c, "gg999%")
	at(t, c, "999% clamps", 4, 0)
}

// H M L go to the view's top, middle and bottom lines, first non-blank, counted from the top for
// H and from the bottom for L; without a viewport the keys are consumed and nothing moves.
func TestVimViewLines(t *testing.T) {
	c, _ := viewportCore(t, strings.Repeat("  x\n", 19)+"  x", 5, 14, 10)
	keys(c, "H")
	at(t, c, "H", 5, 2)
	keys(c, "3H")
	at(t, c, "3H", 7, 2)
	keys(c, "M")
	at(t, c, "M", 9, 2)
	keys(c, "L")
	at(t, c, "L", 14, 2)
	keys(c, "3L")
	at(t, c, "3L", 12, 2)
	keys(c, "99H")
	at(t, c, "99H", 14, 2)
	keys(c, "99L")
	at(t, c, "99L", 5, 2)

	n, _ := coreWith(t, "a\nb\nc")
	keys(n, "j")
	for _, k := range "HML" {
		if !n.HandleKey(tui.KeyEvent{Code: k, Text: string(k)}) {
			t.Fatalf("%c with no viewport bubbled", k)
		}
	}
	at(t, n, "H M L with no viewport", 1, 0)
}

// Ctrl-d and Ctrl-u scroll the view and move the cursor half a page (count lines with a count),
// keeping the sticky column; with no viewport the cursor moves alone.
func TestVimHalfPages(t *testing.T) {
	text := "abcdef\n" + strings.Repeat("ab\n", 9) + strings.Repeat("abcdef\n", 9) + "abcdef"
	c, v := viewportCore(t, text, 0, 9, 10)
	keys(c, "4l")
	ctrlKey(c, 'd')
	at(t, c, "Ctrl-d", 5, 1)
	ctrlKey(c, 'd')
	at(t, c, "Ctrl-d keeps the column", 10, 4)
	ctrlKey(c, 'u')
	at(t, c, "Ctrl-u", 5, 1)
	keys(c, "3")
	ctrlKey(c, 'u')
	at(t, c, "3 Ctrl-u", 2, 1)
	if got, want := fmt.Sprint(v.scrolls), "[5 5 -5 -3]"; got != want {
		t.Fatalf("scrolls %s, want %s", got, want)
	}
	keys(c, "99")
	ctrlKey(c, 'd')
	at(t, c, "99 Ctrl-d stops at the last line", 19, 4)

	n, _ := coreWith(t, strings.Repeat("x\n", 9)+"x")
	ctrlKey(n, 'd')
	at(t, n, "Ctrl-d with a 5-line page and no viewport", 2, 0)
	l, _ := viewportCore(t, "a\nb\nc", 0, 2, 1)
	ctrlKey(l, 'd')
	at(t, l, "Ctrl-d with a one-line page", 1, 0)
}

// W B E are w b e (the Editor's words are WORDs); ge and gE go back to the previous word's end.
func TestVimWORDsAndWordEndBack(t *testing.T) {
	c, _ := coreWith(t, "foo.bar baz\n\n  qux")
	keys(c, "W")
	at(t, c, "W", 0, 8)
	keys(c, "E")
	at(t, c, "E", 0, 10)
	keys(c, "B")
	at(t, c, "B", 0, 8)
	keys(c, "G$ge")
	at(t, c, "ge across a blank line", 0, 10)
	keys(c, "G$gE")
	at(t, c, "gE", 0, 10)
	keys(c, "G$2ge")
	at(t, c, "2ge", 0, 6)
	keys(c, "0lge")
	at(t, c, "ge in the first word", 0, 0)
	keys(c, "gl")
	at(t, c, "g then l: l moves", 0, 1)
}

// Every new motion extends a Visual selection, Ctrl-d and Ctrl-u included, and works on a viewer.
func TestVimNewMotionsExtendVisualAndWorkReadOnly(t *testing.T) {
	c, _ := viewportCore(t, "ab,cd\nef\ngh\nij", 0, 3, 4)
	keys(c, "vf,")
	if r, cc, er, ec, ok := c.SelectionRange(); !ok || r != 0 || cc != 0 || er != 0 || ec != 3 {
		t.Fatalf("v f,: selection (%d,%d)-(%d,%d) ok=%v", r, cc, er, ec, ok)
	}
	ctrlKey(c, 'd')
	if r, _, er, _, ok := c.SelectionRange(); !ok || r != 0 || er != 2 {
		t.Fatalf("v f, Ctrl-d: selection rows %d-%d ok=%v", r, er, ok)
	}
	keys(c, "L")
	if _, _, er, _, ok := c.SelectionRange(); !ok || er != 3 {
		t.Fatalf("then L: selection to row %d ok=%v", er, ok)
	}

	ro := widget.NewEditorCore(widget.CoreInitialText("a(b)\n c"), widget.CoreReadOnly(true))
	ro.Bind(nil, &fakeLayout{per: 1, page: 2})
	keys(ro, "%")
	at(t, ro, "% on a viewer", 0, 3)
	special(ro, tui.KeyEnter)
	at(t, ro, "Enter on a viewer", 1, 1)
	if ro.Value() != "a(b)\n c" {
		t.Fatalf("a viewer's text changed: %q", ro.Value())
	}
}

// A host may rebind the new motions like any other action: an overlay naming one does not panic,
// and the chord does the motion.
func TestVimNewMotionsRebind(t *testing.T) {
	c, _ := coreWith(t, "a\n  b\nc,d", widget.CoreKeymap(widget.Keymap{
		{Mode: widget.ModeNormal, Code: 'z'}: widget.ActLineDownFirst,
		{Mode: widget.ModeVisual, Code: 'z'}: widget.ActLineDownFirst,
		{Mode: widget.ModeNormal, Code: 'q'}: widget.ActFindCharForward,
		{Mode: widget.ModeNormal, Code: '+'}: widget.ActUnbound,
	}))
	keys(c, "z")
	at(t, c, "z rebound to LineDownFirst", 1, 2)
	keys(c, "vz")
	at(t, c, "z in Visual", 2, 0)
	special(c, tui.KeyEscape)
	keys(c, "q,")
	at(t, c, "q rebound to a find", 2, 1)
	if c.HandleKey(tui.KeyEvent{Code: '+', Text: "+"}) {
		t.Fatal("an unbound + was consumed")
	}
}
