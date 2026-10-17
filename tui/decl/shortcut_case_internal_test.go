package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/key"
)

// A letter's case is its Shift, as in Qt: "C" is the key and "Shift+C" the
// capital, whichever way a terminal delivers the capital — the character
// itself, or the letter with Shift. With Ctrl or Alt, case is not relied on.
func TestAShortcutLettersCaseIsItsShift(t *testing.T) {
	press := func(c rune, m tui.Mods) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: c, Mods: m} }
	for _, c := range []struct {
		seq  string
		ev   tui.KeyEvent
		want bool
	}{
		{"C", press('c', 0), true},
		{"C", press('C', 0), false},
		{"c", press('C', 0), false},
		{"Shift+C", press('C', 0), true},
		{"Shift+C", press('c', tui.ModShift), true},
		{"Shift+C", press('c', 0), false},
		{"Ctrl+Q", press('q', tui.ModCtrl), true},
		{"Ctrl+Q", press('Q', tui.ModCtrl), true},
		{"Ctrl+Q", press('q', tui.ModCtrl|tui.ModShift), true},
		{"Alt+H", press('H', tui.ModAlt), true},
		{"?", press('?', 0), true},
	} {
		seq, err := key.Parse(c.seq)
		if err != nil {
			t.Fatal(err)
		}
		if got := seqMatches(seq, c.ev); got != c.want {
			t.Errorf("%q matches %q (mods %v): %v, want %v", c.seq, string(c.ev.Code), c.ev.Mods, got, c.want)
		}
	}
}
