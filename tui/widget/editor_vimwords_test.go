package widget_test

import "testing"

// wordCase is a run of motion keys from a start, with the cursor after each.
type wordCase struct {
	text    string
	ln, col int // the start
	keys    []string
	want    [][2]int
	what    string
}

func runWords(t *testing.T, cases []wordCase) {
	t.Helper()
	for _, wc := range cases {
		c, _ := coreWith(t, wc.text)
		c.SetLine(wc.ln, wc.col)
		for i, k := range wc.keys {
			keys(c, k)
			if ln, col := c.Line(); ln != wc.want[i][0] || col != wc.want[i][1] {
				t.Errorf("%q from (%d,%d), %s: after %q (#%d): (%d, %d), want %v",
					wc.text, wc.ln, wc.col, wc.what, k, i+1, ln, col, wc.want[i])
				break
			}
		}
	}
}

func p(ln, col int) [2]int { return [2]int{ln, col} }

// w b e and ge stop where Vim's do: at each change between keyword, punctuation and blank, so
// foo.bar is three words; at each CJK script's change, and an emoji run's; on an empty line (not
// e); and at the text's ends.
func TestVimWordsFollowTheKeywordClasses(t *testing.T) {
	runWords(t, []wordCase{
		{"foo.bar baz", 0, 0, []string{"w", "w", "w", "w"}, [][2]int{p(0, 3), p(0, 4), p(0, 8), p(0, 10)}, "w"},
		{"foo.bar baz", 0, 10, []string{"b", "b", "b", "b", "b"}, [][2]int{p(0, 8), p(0, 4), p(0, 3), p(0, 0), p(0, 0)}, "b"},
		{"foo.bar baz", 0, 0, []string{"e", "e", "e", "e", "e"}, [][2]int{p(0, 2), p(0, 3), p(0, 6), p(0, 10), p(0, 10)}, "e"},
		{"foo.bar baz", 0, 10, []string{"ge", "ge", "ge", "ge"}, [][2]int{p(0, 6), p(0, 3), p(0, 2), p(0, 0)}, "ge"},
		{"foo  bar", 0, 0, []string{"w", "e", "b", "ge"}, [][2]int{p(0, 5), p(0, 7), p(0, 5), p(0, 2)}, "w e b ge"},
		{"a-b_c", 0, 0, []string{"w", "w", "w"}, [][2]int{p(0, 1), p(0, 2), p(0, 4)}, "w: _ is a keyword cluster"},
		{"a-b_c", 0, 0, []string{"e", "e"}, [][2]int{p(0, 1), p(0, 4)}, "e"},
		{"a-b_c", 0, 4, []string{"b", "b", "b"}, [][2]int{p(0, 2), p(0, 1), p(0, 0)}, "b"},
		{"x9y z", 0, 0, []string{"w"}, [][2]int{p(0, 4)}, "w: a digit is a keyword cluster"},
		{"漢字かなカナー end", 0, 0, []string{"w", "w", "w"}, [][2]int{p(0, 2), p(0, 4), p(0, 8)}, "w across scripts"},
		{"漢字かなカナー end", 0, 0, []string{"e", "e", "e"}, [][2]int{p(0, 1), p(0, 3), p(0, 6)}, "e across scripts"},
		{"漢字かなカナー end", 0, 8, []string{"b", "b"}, [][2]int{p(0, 4), p(0, 2)}, "b across scripts"},
		{"한국어 말", 0, 0, []string{"w"}, [][2]int{p(0, 4)}, "w over Hangul"},
		{"abc한국 x", 0, 0, []string{"w", "w"}, [][2]int{p(0, 3), p(0, 6)}, "w: Hangul is not a Latin word's class"},
		{"x々y z", 0, 0, []string{"w", "w", "w"}, [][2]int{p(0, 1), p(0, 2), p(0, 4)}, "w: the iteration mark is CJK punctuation"},
		{"a😀😀b c", 0, 0, []string{"w", "w", "w"}, [][2]int{p(0, 1), p(0, 3), p(0, 5)}, "w over emoji"},
		{"+∑😀+ ©.", 0, 0, []string{"w", "w", "w", "w"}, [][2]int{p(0, 2), p(0, 3), p(0, 5), p(0, 6)}, "w: ∑ is punctuation with +, an emoji is not, © is Latin-1 punctuation"},
		{"©. x", 0, 0, []string{"w"}, [][2]int{p(0, 3)}, "w: © (Emoji, but Latin-1) is punctuation with ."},
		{"x☀✅y", 0, 0, []string{"w", "w"}, [][2]int{p(0, 1), p(0, 3)}, "w: pictographs outside the emoji blocks"},
		{"foo\n\nbar\n\n\nbaz", 0, 0, []string{"w", "w", "w", "w", "w", "w"}, [][2]int{p(1, 0), p(2, 0), p(3, 0), p(4, 0), p(5, 0), p(5, 2)}, "w stops on each empty line"},
		{"foo\n\nbar\n\n\nbaz", 5, 0, []string{"b", "b", "b", "b", "b"}, [][2]int{p(4, 0), p(3, 0), p(2, 0), p(1, 0), p(0, 0)}, "b stops on each empty line"},
		{"foo\n\nbar\n\n\nbaz", 0, 0, []string{"e", "e", "e"}, [][2]int{p(0, 2), p(2, 2), p(5, 2)}, "e passes empty lines"},
		{"foo\n\nbar\n\n\nbaz", 5, 2, []string{"ge", "ge", "ge", "ge"}, [][2]int{p(4, 0), p(3, 0), p(2, 2), p(1, 0)}, "ge stops on each empty line"},
		{"foo\n\nbar\n\n\nbaz", 0, 0, []string{"2w", "3w", "9w"}, [][2]int{p(2, 0), p(5, 0), p(5, 2)}, "counted w"},
		{"foo\n\nbar\n\n\nbaz", 5, 2, []string{"2b", "9b"}, [][2]int{p(4, 0), p(0, 0)}, "counted b"},
		{"ab cd", 0, 3, []string{"w", "e"}, [][2]int{p(0, 4), p(0, 4)}, "w e at the text's end"},
		{"ab  ", 0, 0, []string{"w"}, [][2]int{p(0, 3)}, "w over trailing blanks to the end"},
		{"ab cd", 0, 0, []string{"b", "ge"}, [][2]int{p(0, 0), p(0, 0)}, "b ge at the text's start"},
		{"ab\ncd", 0, 1, []string{"w", "b", "ge"}, [][2]int{p(1, 0), p(0, 0), p(0, 0)}, "a line's end is blank"},
		{"ab\ncd", 1, 1, []string{"ge"}, [][2]int{p(0, 1)}, "ge to the previous line's end"},
	})
}

// W B E and gE end words only at blanks; they stop on empty lines too.
func TestVimWORDsEndOnlyAtBlanks(t *testing.T) {
	runWords(t, []wordCase{
		{"foo.bar baz", 0, 0, []string{"W", "W"}, [][2]int{p(0, 8), p(0, 10)}, "W"},
		{"foo.bar baz", 0, 10, []string{"B", "B"}, [][2]int{p(0, 8), p(0, 0)}, "B"},
		{"foo.bar baz", 0, 0, []string{"E", "E"}, [][2]int{p(0, 6), p(0, 10)}, "E"},
		{"foo.bar baz", 0, 10, []string{"gE", "gE"}, [][2]int{p(0, 6), p(0, 0)}, "gE"},
		{"a-b_c 漢字かな x", 0, 0, []string{"W", "W"}, [][2]int{p(0, 6), p(0, 11)}, "W over mixed classes"},
		{"a.b\n\nc.d", 0, 0, []string{"W", "W", "B"}, [][2]int{p(1, 0), p(2, 0), p(1, 0)}, "W B stop on an empty line"},
	})
}

// A word motion extends a Visual selection by its cursor.
func TestVimWordsExtendVisual(t *testing.T) {
	c, _ := coreWith(t, "foo.bar baz")
	keys(c, "vw")
	if r, cc, er, ec, ok := c.SelectionRange(); !ok || r != 0 || cc != 0 || er != 0 || ec != 4 {
		t.Fatalf("v w: selection (%d,%d)-(%d,%d) ok=%v, want (0,0)-(0,4)", r, cc, er, ec, ok)
	}
	keys(c, "e")
	if _, _, _, ec, _ := c.SelectionRange(); ec != 7 {
		t.Fatalf("then e: selection ends at %d, want 7", ec)
	}
}
