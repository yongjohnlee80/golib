package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// The names the key map moved into tui/key or extended still compile from outside golib and mean
// what they did: tui's modifier type, its methods and values, the key codes, decl's handler
// option, and a keyed KeyChord literal.
func TestTheOldKeyAPICompilesAndMeansTheSame(t *testing.T) {
	var m tui.Mods = tui.ModCtrl | tui.ModShift | tui.ModNumLock
	if m.Chord() != tui.ModCtrl|tui.ModShift {
		t.Errorf("Chord: %v", m.Chord())
	}
	if got := (tui.ModCtrl | tui.ModAlt).String(); got != "alt+ctrl" {
		t.Errorf("String: %q", got)
	}
	if tui.LockMods != tui.ModCapsLock|tui.ModNumLock {
		t.Error("LockMods")
	}
	for _, c := range []struct {
		name string
		got  rune
		want rune
	}{{"Enter", tui.KeyEnter, 13}, {"Tab", tui.KeyTab, 9}, {"Escape", tui.KeyEscape, 27},
		{"Up", tui.KeyUp, 57352}, {"F10", tui.KeyF10, 57373}, {"Menu", tui.KeyMenu, 57363}} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	_ = tuidecl.Commands(map[string]func() error{"App.x": func() error { return nil }})
	kc := widget.KeyChord{Mode: widget.ModeNormal, Code: 'x', Ctrl: true}
	if kc.String() != "Ctrl+x" {
		t.Errorf("KeyChord.String: %q", kc.String())
	}
}

// An old keyed overlay literal on a functional key meant the key with or without Shift, because the
// editor read Shift+Down as Down; replayed over a scheme-built table it still does.
func TestAnOldFunctionalKeyOverlayKeepsItsShiftTwin(t *testing.T) {
	h, e, sh := mountedEditor(t, mustKeymap(t, `<mapping/>`), widget.WithCore(widget.CoreKeymap(widget.Keymap{
		{Mode: widget.ModeNormal, Code: tui.KeyDown}: widget.ActLineEnd,
	})))
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown, Mods: tui.ModShift}, ch('i'), ch('!'))
	h.barrier(sh)
	if got := editorValue(h, e); got != "one two thre!e" {
		t.Fatalf("Shift+Down is not the overlay's LineEnd: %q", got)
	}
}
