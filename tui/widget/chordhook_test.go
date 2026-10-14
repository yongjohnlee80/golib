package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// SetChordHook: a Normal or Visual chord with nothing pending goes to the hook first; one it takes
// is not the keymap's (x deletes nothing) and is not replayed by . or a macro. A chord with a count,
// an operator or an armed key pending, Esc, and Insert mode's keys never reach it; a chord it
// declines is the keymap's as before; nil removes it.
func TestAChordHookTakesUnpendingChords(t *testing.T) {
	c, _ := coreWith(t, "abcdef")
	var got []widget.KeyChord
	c.SetChordHook(func(kc widget.KeyChord) bool {
		got = append(got, kc)
		return kc.Code == 'x' || kc.Ctrl && kc.Code == 'k'
	})
	keys(c, "x")
	c.HandleKey(tui.KeyEvent{Code: 'k', Mods: tui.ModCtrl})
	if c.Value() != "abcdef" || len(got) != 2 || got[0] != (widget.KeyChord{Mode: widget.ModeNormal, Code: 'x'}) ||
		got[1] != (widget.KeyChord{Mode: widget.ModeNormal, Code: 'k', Ctrl: true}) {
		t.Fatalf("taken: text %q, hook saw %+v", c.Value(), got)
	}

	got = nil
	keys(c, "2x") // a count pending: x is the keymap's, twice
	keys(c, "dl") // an operator pending: l is the motion
	keys(c, "fe") // an armed f: e is its character
	if c.Value() != "def" {
		t.Fatalf("pending keys reached the hook: %q", c.Value())
	}
	for _, kc := range got {
		if kc.Code != '2' && kc.Code != 'd' && kc.Code != 'f' {
			t.Fatalf("the hook saw %q with a command pending: %+v", kc.Code, got)
		}
	}

	got = nil
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
	keys(c, "iZ")
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
	for _, kc := range got {
		if kc.Code == tui.KeyEscape || kc.Code == 'Z' {
			t.Fatalf("the hook saw Esc or an Insert key: %+v", got)
		}
	}

	// . repeats the last change (the Z insert), never a hook's chord
	c.SetValue("abc")
	keys(c, "ix")
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
	keys(c, "x.") // x is the hook's: . repeats the insert of x
	if c.Value() != "xxabc" {
		t.Fatalf(". after a hook's chord: %q", c.Value())
	}

	// a macro records no hook's chord: qa x l q, then @a moves one right and deletes nothing
	c.SetValue("abc")
	keys(c, "qaxlq@a")
	if c.Value() != "abc" {
		t.Fatalf("a macro replayed a hook's chord: %q", c.Value())
	}

	// declined: the keymap's; removed: the keymap's
	c.SetChordHook(func(widget.KeyChord) bool { return false })
	keys(c, "0x")
	c.SetChordHook(nil)
	keys(c, "x")
	if c.Value() != "c" {
		t.Fatalf("a declined or removed hook: %q", c.Value())
	}
}

// In Visual mode the hook sees the chord as Visual's.
func TestAChordHookSeesVisualChords(t *testing.T) {
	c, _ := coreWith(t, "abc")
	var got []widget.KeyChord
	c.SetChordHook(func(kc widget.KeyChord) bool { got = append(got, kc); return kc.Code == 'Q' })
	keys(c, "vQ")
	if len(got) != 2 || got[1] != (widget.KeyChord{Mode: widget.ModeVisual, Code: 'Q'}) || c.Mode() != widget.ModeVisual {
		t.Fatalf("visual: %+v, mode %v", got, c.Mode())
	}
}
