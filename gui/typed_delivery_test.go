package gui

import (
	"testing"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"github.com/yongjohnlee80/golib/tui"
)

// readKeys drives the window's input read: focus, queue the platform events in
// one frame's read, then the next. Edit events commit at the end of a frame's
// read, so each frame's edit events become that frame's key events.
func readKeys(t *testing.T, events ...event.Event) []tui.Event {
	t.Helper()
	r := new(input.Router)
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	b.readInput(r.Source(), tag, metrics{})
	r.Source().Execute(key.FocusCmd{Tag: tag})
	b.readInput(r.Source(), tag, metrics{})
	b.q.queue = nil
	for _, e := range events {
		r.Queue(e)
	}
	b.readInput(r.Source(), tag, metrics{})
	out := append([]tui.Event(nil), b.q.queue...)
	return out
}

// Typed text — a letter, a space, a letter held so the platform delivers its
// edit event twice, as macOS's insertText and Wayland's xkb both do — reaches
// the App as one KeyEvent per rune, in typing order, with nothing held back.
// Each edit's range is the selection the previous edit left.
func TestTypedTextIsDeliveredPerRune(t *testing.T) {
	got := readKeys(t,
		key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "a"},
		key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: " "},
		key.EditEvent{Range: key.Range{Start: 2, End: 2}, Text: "b"},
		key.EditEvent{Range: key.Range{Start: 3, End: 3}, Text: "b"}, // the repeat
	)
	want := []tui.Event{
		tui.KeyEvent{Code: 'a', Text: "a"},
		tui.KeyEvent{Code: ' ', Text: " "},
		tui.KeyEvent{Code: 'b', Text: "b"},
		tui.KeyEvent{Code: 'b', Text: "b"},
	}
	if len(got) != len(want) {
		t.Fatalf("delivered %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}
