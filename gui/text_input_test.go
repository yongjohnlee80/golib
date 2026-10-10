package gui

import (
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"
)

// An input method attaches only to a window that has opened text input: on Wayland, Gio
// enables zwp_text_input_v3 only then, and fcitx5 or IBus cannot switch to Hangul in a window
// that never did. Gio closes text input whenever focus moves, so the backend must open it
// each time it takes focus, not once.
func TestTheWindowOpensTextInputWhenItTakesFocus(t *testing.T) {
	r := new(input.Router)
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	b.readInput(r.Source(), tag, metrics{})
	b.readInput(r.Source(), tag, metrics{})
	if got := r.TextInputState(); got != input.TextInputOpen {
		t.Fatalf("after taking focus, text input is %v, want TextInputOpen", got)
	}

	// The window loses and regains the system's focus, as Gio reports it (app/window.go queues
	// a key.FocusEvent): the backend must take focus again and reopen text input.
	r.Queue(key.FocusEvent{Focus: false})
	b.readInput(r.Source(), tag, metrics{})
	if b.gio.focused {
		t.Fatal("the backend did not see focus leave")
	}
	b.readInput(r.Source(), tag, metrics{})
	if got := r.TextInputState(); got != input.TextInputOpen {
		t.Fatalf("after focus came back, text input is %v, want TextInputOpen", got)
	}
}
