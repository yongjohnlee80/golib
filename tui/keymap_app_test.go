package tui

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

func mustScheme(t *testing.T, xml string) *keymap.Scheme {
	t.Helper()
	s, err := keymap.ParseScheme(strings.NewReader(xml), "keymap.xml")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustMap(t *testing.T, cmds []keymap.Command, schemes ...*keymap.Scheme) *keymap.Map {
	t.Helper()
	m, err := keymap.Build(cmds, schemes...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

var (
	cmdSave      = keymap.Command{ID: "t.save", Scope: "t", Keys: key.MustParse("Ctrl+S")}
	cmdSaveAll   = keymap.Command{ID: "t.saveAll", Scope: "t", Keys: key.MustParse("Ctrl+Shift+S")}
	cmdDown      = keymap.Command{ID: "t.down", Scope: "t", Keys: key.MustParse("Down", "J")}
	cmdQuestion  = keymap.Command{ID: "t.help", Scope: "t", Keys: key.MustParse("?")}
	testCommands = []keymap.Command{cmdSave, cmdSaveAll, cmdDown, cmdQuestion}
)

// keyCommand is the decision every widget's KeyCommand makes; these cells pin it.
func TestKeyCommandIsTheExactChordFirstThenTheFallback(t *testing.T) {
	for _, m := range []*keymap.Map{nil, mustMap(t, testCommands)} {
		for _, c := range []struct {
			name string
			ev   KeyEvent
			want string
		}{
			{"exact Ctrl+S", KeyEvent{Code: 's', Mods: ModCtrl}, "t.save"},
			{"exact Ctrl+Shift+S from kitty", KeyEvent{Code: 's', Mods: ModCtrl | ModShift}, "t.saveAll"},
			{"legacy capital with Ctrl is Ctrl+S", KeyEvent{Code: 'S', Mods: ModCtrl}, "t.save"},
			{"j", KeyEvent{Code: 'j', Text: "j"}, "t.down"},
			{"Down under Num Lock", KeyEvent{Code: KeyDown, Mods: ModNumLock}, "t.down"},
			{"a repeat matches", KeyEvent{Kind: KeyRepeat, Code: KeyDown}, "t.down"},
			{"kitty ?", KeyEvent{Code: '/', Mods: ModShift, Text: "?"}, "t.help"},
			{"legacy ?", KeyEvent{Code: '?', Text: "?"}, "t.help"},
			{"a release never matches", KeyEvent{Kind: KeyRelease, Code: KeyDown}, ""},
			{"Shift+Down is not Down", KeyEvent{Code: KeyDown, Mods: ModShift}, ""},
			{"Hyper+j matches nothing", KeyEvent{Code: 'j', Mods: ModHyper, Text: "j"}, ""},
			{"Super+j is not j", KeyEvent{Code: 'j', Mods: ModSuper}, ""},
		} {
			cmd, ok := keyCommand(m, c.ev, testCommands)
			if got := map[bool]string{true: cmd.ID}[ok]; got != c.want {
				t.Errorf("map %v, %s: %q, want %q", m != nil, c.name, got, c.want)
			}
		}
	}
	// With only the fallback bound, a kitty Ctrl+Shift+S reaches Ctrl+S.
	if cmd, ok := keyCommand(nil, KeyEvent{Code: 's', Mods: ModCtrl | ModShift}, []keymap.Command{cmdSave}); !ok || cmd.ID != "t.save" {
		t.Errorf("fallback: %v %v", cmd.ID, ok)
	}
}

func TestKeyCommandFollowsTheMap(t *testing.T) {
	m := mustMap(t, testCommands, mustScheme(t, `<mapping>
<shortcut id="t.down"><key value="Ctrl+N"/></shortcut>
<shortcut id="t.help"><key value=""/></shortcut>
</mapping>`))
	if cmd, ok := keyCommand(m, KeyEvent{Code: 'n', Mods: ModCtrl}, testCommands); !ok || cmd.ID != "t.down" {
		t.Errorf("the scheme's Ctrl+N: %v %v", cmd.ID, ok)
	}
	if _, ok := keyCommand(m, KeyEvent{Code: 'j', Text: "j"}, testCommands); ok {
		t.Error("j still moves after the scheme replaced it")
	}
	if _, ok := keyCommand(m, KeyEvent{Code: '?', Text: "?"}, testCommands); ok {
		t.Error("an unbound command still fires")
	}
}

// Tab traversal is the FocusNext and FocusPrev commands: a key map moves it, and the old key stops.
func TestFocusTraversalFollowsTheKeymap(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	m := mustMap(t, Commands(), mustScheme(t, `<mapping>
<shortcut id="tui.app.focusNext"><key value="Ctrl+N"/></shortcut>
<shortcut id="tui.app.focusPrev"><key value="Ctrl+P"/></shortcut>
</mapping>`))
	h := startApp(t, root, 8, 8, WithKeymap(m))
	h.inject(KeyEvent{Code: 'n', Mods: ModCtrl})
	waitFor(t, "Ctrl+N focuses a", func() bool { return focusedID(h) == a.nodeID() })
	h.inject(tabEv(false), KeyEvent{Code: 'n', Mods: ModCtrl})
	waitFor(t, "Tab no longer steps; Ctrl+N does", func() bool { return focusedID(h) == b.nodeID() })
	h.inject(KeyEvent{Code: 'p', Mods: ModCtrl})
	waitFor(t, "Ctrl+P steps back", func() bool { return focusedID(h) == a.nodeID() })
}

// A kitty terminal reports Num Lock as a modifier: Tab still steps (it did not before the key map,
// whose matching drops lock bits as every binding should).
func TestTabStepsWithNumLockOn(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 8)
	h.inject(KeyEvent{Code: KeyTab, Mods: ModNumLock})
	waitFor(t, "focus a", func() bool { return focusedID(h) == a.nodeID() })
	h.inject(KeyEvent{Code: KeyTab, Mods: ModNumLock | ModShift})
	waitFor(t, "Shift-Tab wraps to b", func() bool { return focusedID(h) == b.nodeID() })
}

func TestApplyKeymapInstallsAndClears(t *testing.T) {
	t.Parallel()
	h := startApp(t, newFocusProbe("a", Size{W: 2, H: 1}), 8, 8)
	m := mustMap(t, Commands())
	var got *keymap.Map
	h.onLoop(func() { h.app.ApplyKeymap(m); got = h.app.Keymap() })
	if got != m {
		t.Fatal("ApplyKeymap did not install the map")
	}
	h.app.SetKeymap(nil)
	waitFor(t, "SetKeymap(nil) clears", func() bool {
		var cur *keymap.Map
		h.onLoop(func() { cur = h.app.Keymap() })
		return cur == nil
	})
}
