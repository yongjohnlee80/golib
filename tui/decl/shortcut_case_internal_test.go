package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A letter's case is its Shift, as in Qt: "C" is the key and "Shift+C" the
// capital, whichever way a terminal delivers the capital — the character
// itself, or the letter with Shift. With Ctrl or Alt, case is not relied on.
func TestAShortcutLettersCaseIsItsShift(t *testing.T) {
	key := func(c rune, m tui.Mods) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: c, Mods: m} }
	for _, c := range []struct {
		seq  string
		ev   tui.KeyEvent
		want bool
	}{
		{"C", key('c', 0), true},
		{"C", key('C', 0), false},
		{"c", key('C', 0), false},
		{"Shift+C", key('C', 0), true},
		{"Shift+C", key('c', tui.ModShift), true},
		{"Shift+C", key('c', 0), false},
		{"Ctrl+Q", key('q', tui.ModCtrl), true},
		{"Ctrl+Q", key('Q', tui.ModCtrl), true},
		{"Ctrl+Q", key('q', tui.ModCtrl|tui.ModShift), true},
		{"Alt+H", key('H', tui.ModAlt), true},
		{"?", key('?', 0), true},
	} {
		seq, err := parseSequence(c.seq)
		if err != nil {
			t.Fatal(err)
		}
		if got := seq.matches(c.ev); got != c.want {
			t.Errorf("%q matches %q (mods %v): %v, want %v", c.seq, string(c.ev.Code), c.ev.Mods, got, c.want)
		}
	}
}
