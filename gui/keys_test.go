package gui

import (
	"runtime"
	"testing"

	"gioui.org/io/key"
	"github.com/yongjohnlee80/golib/tui"
)

func TestTranslateKey(t *testing.T) {
	press := func(n key.Name, m key.Modifiers) key.Event { return key.Event{Name: n, Modifiers: m} }
	cases := []struct {
		name string
		in   key.Event
		want tui.KeyEvent
		ok   bool
	}{
		{"enter", press(key.NameReturn, 0), tui.KeyEvent{Code: tui.KeyEnter}, true},
		{"keypad enter", press(key.NameEnter, 0), tui.KeyEvent{Code: tui.KeyEnter}, true},
		{"backspace", press(key.NameDeleteBackward, 0), tui.KeyEvent{Code: tui.KeyBackspace}, true},
		{"delete", press(key.NameDeleteForward, 0), tui.KeyEvent{Code: tui.KeyDelete}, true},
		{"escape", press(key.NameEscape, 0), tui.KeyEvent{Code: tui.KeyEscape}, true},
		{"shift-tab", press(key.NameTab, key.ModShift), tui.KeyEvent{Code: tui.KeyTab, Mods: tui.ModShift}, true},
		{"ctrl-up", press(key.NameUpArrow, key.ModCtrl), tui.KeyEvent{Code: tui.KeyUp, Mods: tui.ModCtrl}, true},
		{"f12", press(key.NameF12, 0), tui.KeyEvent{Code: tui.KeyF12}, true},
		// Ctrl+I is not Tab: the kitty keyboard protocol keeps them apart, and so does the window.
		{"ctrl-i", press("I", key.ModCtrl), tui.KeyEvent{Code: 'i', Mods: tui.ModCtrl}, true},
		{"ctrl-shift-a", press("A", key.ModCtrl|key.ModShift), tui.KeyEvent{Code: 'a', Mods: tui.ModCtrl | tui.ModShift}, true},
		{"alt-x", press("X", key.ModAlt), tui.KeyEvent{Code: 'x', Mods: tui.ModAlt}, true},
		{"super-s is Super", press("S", key.ModCommand), tui.KeyEvent{Code: 's', Mods: tui.ModSuper}, true},
		{"ctrl-space", press(key.NameSpace, key.ModCtrl), tui.KeyEvent{Code: ' ', Mods: tui.ModCtrl}, true},
		{"ctrl-slash", press("/", key.ModCtrl), tui.KeyEvent{Code: '/', Mods: tui.ModCtrl}, true},
		// Text keys arrive as edit events; their key events are dropped so nothing types twice.
		{"plain a", press("A", 0), tui.KeyEvent{}, false},
		{"shift a", press("A", key.ModShift), tui.KeyEvent{}, false},
		{"plain space", press(key.NameSpace, 0), tui.KeyEvent{}, false},
		{"lone ctrl", press(key.NameCtrl, key.ModCtrl), tui.KeyEvent{}, false},
		{"lone shift", press(key.NameShift, key.ModShift), tui.KeyEvent{}, false},
	}
	for _, c := range cases {
		got, ok := translateKey(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: translateKey = %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestTranslateKeyRelease(t *testing.T) {
	got, ok := translateKey(key.Event{Name: key.NameEscape, State: key.Release})
	if !ok || got.Kind != tui.KeyRelease || got.Code != tui.KeyEscape {
		t.Fatalf("escape release = %+v, %v; want a KeyRelease of KeyEscape", got, ok)
	}
	if _, ok := translateKey(key.Event{Name: "A", State: key.Release}); ok {
		t.Fatal("a plain text key's release was reported; tui/term reports none for text")
	}
}

func TestTextKeys(t *testing.T) {
	got := textKeys("a한😀")
	want := []tui.KeyEvent{{Code: 'a', Text: "a"}, {Code: '한', Text: "한"}, {Code: '😀', Text: "😀"}}
	if len(got) != len(want) {
		t.Fatalf("textKeys = %+v; want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("textKeys[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}
}

func TestPasteChord(t *testing.T) {
	linux := key.Event{Name: "V", Modifiers: key.ModCtrl | key.ModShift}
	mac := key.Event{Name: "V", Modifiers: key.ModCommand}
	plainCtrlV := key.Event{Name: "V", Modifiers: key.ModCtrl}
	if isPasteChord(plainCtrlV) {
		t.Error("Ctrl+V is the app's key (vim's visual block), not the window's paste")
	}
	if runtime.GOOS == "darwin" {
		if !isPasteChord(mac) || isPasteChord(linux) {
			t.Error("on macOS the paste chord is Cmd+V")
		}
		return
	}
	if !isPasteChord(linux) || isPasteChord(mac) {
		t.Error("off macOS the paste chord is Ctrl+Shift+V")
	}
	if isPasteChord(key.Event{Name: "V", Modifiers: key.ModCtrl | key.ModShift, State: key.Release}) {
		t.Error("a release is not a paste")
	}
}
