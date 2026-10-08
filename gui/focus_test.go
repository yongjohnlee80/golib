package gui

import (
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"github.com/yongjohnlee80/golib/tui"
)

// The window sends Tab and Shift+Tab as system events, for its own focus traversal, and a
// catch-all key filter never matches one. The backend must ask for them by name, or a dialog's
// Tab never reaches the App.
func TestTabReachesTheApp(t *testing.T) {
	r := new(input.Router)
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	b.readInput(r.Source(), tag, metrics{}) // registers the filters
	r.Source().Execute(key.FocusCmd{Tag: tag})
	b.readInput(r.Source(), tag, metrics{})
	b.q.queue = nil

	r.Queue(input.SystemEvent{Event: key.Event{Name: key.NameTab, State: key.Press}})
	r.Queue(input.SystemEvent{Event: key.Event{Name: key.NameTab, Modifiers: key.ModShift, State: key.Press}})
	b.readInput(r.Source(), tag, metrics{})

	want := []tui.Event{
		tui.KeyEvent{Code: tui.KeyTab},
		tui.KeyEvent{Code: tui.KeyTab, Mods: tui.ModShift},
	}
	if len(b.q.queue) != len(want) {
		t.Fatalf("queued %#v, want %#v", b.q.queue, want)
	}
	for i := range want {
		if b.q.queue[i] != want[i] {
			t.Errorf("event %d = %#v, want %#v", i, b.q.queue[i], want[i])
		}
	}
}
