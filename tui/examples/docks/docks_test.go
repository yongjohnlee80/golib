package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// TestTheDocksQMLIsSound lints the document and the dock style it uses.
func TestTheDocksQMLIsSound(t *testing.T) {
	decltest.Check(t, programOptions(func(string) {}, func() {})...)
}

// TestEachPanelsDWinsInsideItAndTheWindowsOutside: two panels from one style, each opened by its
// key and focused; in each, d is the panel's, and on the page it is the Window's, as is n the page's.
// What a key said is read from App.say, not the screen: an open panel covers the status line.
func TestEachPanelsDWinsInsideItAndTheWindowsOutside(t *testing.T) {
	var mu sync.Mutex
	var said []string
	var s *decltest.Screen
	say := func(line string) {
		mu.Lock()
		said = append(said, line)
		mu.Unlock()
		_ = s.Program.Set("App.status", line)
	}
	last := func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(said) == 0 {
			return ""
		}
		return said[len(said)-1]
	}
	s = decltest.Run(t, 90, 14, programOptions(say, func() {})...)
	s.WaitForText(t, "the page")
	press := func(code rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: code} }
	says := func(keys []tui.Event, want string) {
		t.Helper()
		s.Keys(t, keys...)
		s.WaitFor(t, "App.say: "+want, func(string) bool { return strings.Contains(last(), want) })
	}
	says([]tui.Event{decltest.Rune('d')}, "the Window's")
	says([]tui.Event{decltest.Rune('n')}, "the page's own")
	s.Keys(t, press(tui.KeyF2))
	s.WaitForText(t, "README.md")
	says([]tui.Event{decltest.Rune('d')}, "delete, in the files panel")
	s.Keys(t, press(tui.KeyEscape), press(tui.KeyF3))
	s.WaitForText(t, "ship the docks example")
	says([]tui.Event{decltest.Rune('d')}, "done, in the notes panel")
	// the page's n is not live in a panel: n then d leave the panel's d the last said
	says([]tui.Event{decltest.Rune('n'), decltest.Rune('d')}, "done, in the notes panel")
	mu.Lock()
	for _, l := range said[len(said)-2:] {
		if strings.Contains(l, "page's own") {
			t.Errorf("the page's n fired from inside a panel: %v", said)
		}
	}
	mu.Unlock()
	says([]tui.Event{press(tui.KeyEscape), decltest.Rune('d')}, "the Window's")
	if strings.Contains(s.String(), "ship the docks example") {
		t.Fatalf("Esc left the notes panel open:\n%s", s)
	}
}
