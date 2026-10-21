package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A combining mark typed after a letter joins it: "e" then U+0301 is the one grapheme "é". Two Lefts
// from the end of "éx" therefore land before "é", and a letter typed there goes before it. Were the
// mark a column of its own, the second Left would stop between "e" and its accent, and the typed
// letter would split them, taking the accent.
func TestATextInputJoinsACombiningMarkToTheLetterBeforeIt(t *testing.T) {
	h, in, sh := focusedInput(t)
	defer h.stop()
	typeRune := func(s string) {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: []rune(s)[0], Text: s})
	}
	typeRune("e")
	typeRune("\u0301")
	typeRune("x")
	h.inject(key(tui.KeyLeft), key(tui.KeyLeft))
	typeRune("y")
	h.barrier(sh)
	var v string
	h.onLoop(func() { v = in.Value() })
	if want := "ye\u0301x"; v != want {
		t.Errorf("e, U+0301, x, Left, Left, y: value %q; want %q, the y before the whole é", v, want)
	}
}

// The same inside the line: a mark typed between "e" and "x" joins the "e", and the cursor stays
// after the "é" it made, before the "x". Counted on its own, the mark would add a column and put
// the cursor past the "x".
func TestATextInputKeepsTheCursorAfterAMarkTypedInsideTheLine(t *testing.T) {
	h, in, sh := focusedInput(t)
	defer h.stop()
	typeRune := func(s string) {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: []rune(s)[0], Text: s})
	}
	typeRune("e")
	typeRune("x")
	h.inject(key(tui.KeyLeft))
	typeRune("\u0301")
	typeRune("y")
	h.barrier(sh)
	var v string
	h.onLoop(func() { v = in.Value() })
	if want := "e\u0301yx"; v != want {
		t.Errorf("e, x, Left, U+0301, y: value %q; want %q, the y between the é and the x", v, want)
	}
}
