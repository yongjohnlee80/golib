package gui

import (
	"testing"

	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/io/key"
	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
)

func TestRawKeyOfReportsEveryKey(t *testing.T) {
	cases := []struct {
		in   KeyInput
		code rune
		kind tui.KeyKind
	}{
		{KeyInput{Name: "W", Press: true}, 'w', tui.KeyPress},
		{KeyInput{Name: "W"}, 'w', tui.KeyRelease},
		{KeyInput{Name: string(key.NameSpace), Press: true}, ' ', tui.KeyPress},
		{KeyInput{Name: string(key.NameShift), Press: true}, tuikey.LeftShift, tui.KeyPress},
		{KeyInput{Name: string(key.NameCtrl), Press: true}, tuikey.LeftControl, tui.KeyPress},
		{KeyInput{Name: string(key.NameAlt), Press: true}, tuikey.LeftAlt, tui.KeyPress},
		{KeyInput{Name: string(key.NameCommand), Press: true}, tuikey.LeftSuper, tui.KeyPress},
		{KeyInput{Name: string(key.NameReturn), Press: true}, tuikey.Enter, tui.KeyPress},
		{KeyInput{Name: string(key.NameUpArrow)}, tuikey.Up, tui.KeyRelease},
	}
	for _, c := range cases {
		k, ok := rawKeyOf(c.in)
		if !ok || k.Code != c.code || k.Kind != c.kind {
			t.Errorf("%+v → %+v %v; want code %d kind %v", c.in, k, ok, c.code, c.kind)
		}
	}
}

// keyRig is a backend reading a Gio router's key events, with the keyboard focus.
type keyRig struct {
	t *testing.T
	r *input.Router
	b *Backend
}

func newKeyRig(t *testing.T) *keyRig {
	g := &keyRig{t: t, r: new(input.Router), b: NewBackend()}
	g.frame()
	g.r.Source().Execute(key.FocusCmd{Tag: &g.b.gio})
	g.frame()
	return g
}

func (g *keyRig) frame() { g.b.readInput(g.r.Source(), &g.b.gio, metrics{}) }

// keys is the key events pushed since the last call.
func (g *keyRig) keys() []tui.KeyEvent {
	g.b.q.mu.Lock()
	defer g.b.q.mu.Unlock()
	var out []tui.KeyEvent
	for _, ev := range g.b.q.queue {
		if k, ok := ev.(tui.KeyEvent); ok {
			out = append(out, k)
		}
	}
	g.b.q.queue = g.b.q.queue[:0]
	return out
}

func (g *keyRig) press(name key.Name, mods key.Modifiers) {
	g.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press})
	g.frame()
}

func TestRawModeReportsLettersAsKeysAndTextModeDoesNot(t *testing.T) {
	g := newKeyRig(t)
	g.press("W", 0)
	if got := g.keys(); len(got) != 0 {
		t.Fatalf("text mode pushed %+v for an unchorded W; its text comes from the input method", got)
	}
	g.b.SetRawKeys(true)
	g.frame()
	if s := g.r.TextInputState(); s == input.TextInputOpen {
		t.Fatalf("raw mode left text input %v; want it closed", s)
	}
	g.press("W", 0)
	g.press(key.NameShift, key.ModShift)
	got := g.keys()
	if len(got) != 2 || got[0].Code != 'w' || got[0].Kind != tui.KeyPress || got[1].Code != tuikey.LeftShift {
		t.Fatalf("raw W then Shift pushed %+v", got)
	}
}

// Keys held as the focus leaves, or as raw mode ends, are released: a game never sees one stuck.
func TestHeldRawKeysAreReleasedOnFocusLossAndOnLeavingRawMode(t *testing.T) {
	g := newKeyRig(t)
	g.b.SetRawKeys(true)
	g.frame()
	g.press("W", 0)
	g.press(key.NameShift, key.ModShift)
	g.keys()
	g.r.Source().Execute(key.FocusCmd{Tag: nil}) // the focus leaves
	g.frame()
	released := map[rune]bool{}
	for _, k := range g.keys() {
		if k.Kind == tui.KeyRelease {
			released[k.Code] = true
		}
	}
	if !released['w'] || !released[tuikey.LeftShift] {
		t.Fatalf("on focus loss released %v; want w and LeftShift", released)
	}
	g.r.Source().Execute(key.FocusCmd{Tag: &g.b.gio})
	g.frame()
	g.press("A", 0)
	g.keys()
	g.b.SetRawKeys(false)
	g.frame()
	if got := g.keys(); len(got) != 1 || got[0].Code != 'a' || got[0].Kind != tui.KeyRelease {
		t.Fatalf("leaving raw mode pushed %+v; want A's release", got)
	}
	if s := g.r.TextInputState(); s != input.TextInputOpen {
		t.Fatalf("text input %v after leaving raw mode; want open", s)
	}
}

// In raw mode the paste chord is a key, not a paste.
func TestRawModeSendsThePasteChordAsKeys(t *testing.T) {
	g := newKeyRig(t)
	g.b.SetRawKeys(true)
	g.frame()
	g.press("V", key.ModCtrl)
	got := g.keys()
	if len(got) != 1 || got[0].Code != 'v' || got[0].Mods&tui.ModCtrl == 0 {
		t.Fatalf("raw Ctrl+V pushed %+v; want the key", got)
	}
}

// Entering raw mode drops a composition in progress.
func TestEnteringRawModeDropsTheComposition(t *testing.T) {
	g := newKeyRig(t)
	g.b.gio.ime.edit(TextEdit{Start: 0, End: 0, Text: "하"})
	g.b.gio.ime.compose(Composition{Start: 0, End: 1})
	if !g.b.gio.ime.composing() {
		t.Fatal("fixture: no composition")
	}
	g.b.SetRawKeys(true)
	g.frame()
	if g.b.gio.ime.composing() || len(g.b.gio.ime.text) != 0 {
		t.Fatalf("raw mode kept the composition: %+v", g.b.gio.ime)
	}
}
