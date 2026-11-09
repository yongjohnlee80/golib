package gui

import (
	"testing"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/gio/io/system"

	"github.com/yongjohnlee80/golib/tui"
)

// Window gives an App on a GUI backend its window's controls, and none on a terminal.
func TestWindowIsTheGUIBackendsAndNoneOnATerminal(t *testing.T) {
	if _, ok := Window(nil); ok {
		t.Fatal("a nil App has window controls")
	}
	if _, ok := Window(tui.NewApp(&stubPane{}, tui.WithBackend(tui.NewTestBackend(10, 2)))); ok {
		t.Fatal("an App on a terminal backend has window controls")
	}
	b := NewBackend()
	var got []system.Action
	b.performHook = func(a system.Action) { got = append(got, a) }
	w, ok := Window(tui.NewApp(&stubPane{}, tui.WithBackend(b)))
	if !ok {
		t.Fatal("an App on the GUI backend has no window controls")
	}
	w.Perform(system.ActionMaximize)
	w.Perform(system.ActionMinimize | system.ActionClose)
	if len(got) != 2 || got[0] != system.ActionMaximize || got[1] != system.ActionMinimize|system.ActionClose {
		t.Fatalf("the window was asked for %v; want maximize, then minimize and close together", got)
	}
}

// Maximized is what the window's last ConfigEvent said.
func TestMaximizedFollowsTheWindowsConfiguration(t *testing.T) {
	b := NewBackend()
	w, _ := Window(tui.NewApp(&stubPane{}, tui.WithBackend(b)))
	for _, c := range []struct {
		mode app.WindowMode
		want bool
	}{{app.Maximized, true}, {app.Windowed, false}, {app.Fullscreen, false}, {app.Maximized, true}} {
		b.configure(app.ConfigEvent{Config: app.Config{Mode: c.mode}})
		if w.Maximized() != c.want {
			t.Fatalf("after a %v configuration Maximized() = %v, want %v", c.mode, w.Maximized(), c.want)
		}
	}
}
