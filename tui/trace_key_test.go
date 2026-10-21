package tui

import (
	"slices"
	"sync"
	"testing"
)

// A key's trace line names it as the key package does: a functional key by its name (F12, Home,
// PgUp, Backspace), and a key that types a character by that character, as typed. Before, only six
// functional keys were named, and the rest were written as their private-use code points.
func TestAKeysTraceLineNamesIt(t *testing.T) {
	var mu sync.Mutex
	var got []string
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2, WithTrace(func(ev TraceEvent) {
		if ev.Kind == TraceKey {
			mu.Lock()
			got = append(got, ev.Detail)
			mu.Unlock()
		}
	}))
	h.inject(
		KeyEvent{Kind: KeyPress, Code: KeyF12},
		KeyEvent{Kind: KeyPress, Code: KeyHome},
		KeyEvent{Kind: KeyPress, Code: KeyBackspace},
		keyEv('j'),
		KeyEvent{Kind: KeyPress, Code: 'J', Text: "J", Mods: ModShift},
		KeyEvent{Kind: KeyPress, Code: 'l', Mods: ModCtrl},
		KeyEvent{Kind: KeyPress, Code: KeyEnter, Mods: ModAlt},
	)
	want := []string{"F12", "Home", "Backspace", "j", "Shift-J", "Ctrl-l", "Alt-Enter"}
	waitFor(t, "every key traced", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= len(want)
	})
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(got, want) {
		t.Errorf("trace details %q, want %q", got, want)
	}
}

// Every trace kind has its name, and a kind outside the set reads "?".
func TestEveryTraceKindHasItsName(t *testing.T) {
	for k, want := range map[TraceKind]string{
		TraceFocus: "focus", TraceFocusRepair: "focus-repair", TraceMount: "mount", TraceUnmount: "unmount",
		TraceKey: "key", TraceScope: "scope", TraceAction: "action", TraceCapture: "capture", TraceCapture + 1: "?",
	} {
		if got := k.String(); got != want {
			t.Errorf("TraceKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
