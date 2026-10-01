package term

import (
	"reflect"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// decodeFolded runs the decoder with the Option fold on.
func decodeFolded(t *testing.T, input string) []tui.Event {
	t.Helper()
	var out []tui.Event
	d := &decoder{emit: func(ev tui.Event) { out = append(out, ev) }, optionFold: true}
	d.feedBytes([]byte(input))
	return out
}

func TestOptionFoldTurnsComposedLettersIntoAlt(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []tui.Event
	}{
		{"option f", "ƒ", []tui.Event{key('f', tui.ModAlt)}},
		{"option o", "ø", []tui.Event{key('o', tui.ModAlt)}},
		{"option s", "ß", []tui.Event{key('s', tui.ModAlt)}},
		{"option z", "Ω", []tui.Event{key('z', tui.ModAlt)}},
		{"a run", "©√", []tui.Event{key('g', tui.ModAlt), key('v', tui.ModAlt)}},
		// kitty: an unmodified key reported by code point folds the same way
		{"kitty option f", "\x1b[402u", []tui.Event{key('f', tui.ModAlt)}},
		{"kitty option f, text", "\x1b[402;1;402u", []tui.Event{key('f', tui.ModAlt)}},
		// not Option-composed on a US layout, or already modified: unchanged
		{"plain letter", "f", []tui.Event{tui.KeyEvent{Code: 'f', Text: "f"}}},
		{"accented letter", "é", []tui.Event{tui.KeyEvent{Code: 'é', Text: "é"}}},
		{"esc-prefixed alt", "\x1bf", []tui.Event{key('f', tui.ModAlt)}},
		{"kitty ctrl+ƒ stays", "\x1b[402;5u", []tui.Event{key('ƒ', tui.ModCtrl)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeFolded(t, tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decode %q:\n got %#v\nwant %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestOptionFoldIsOffByDefault(t *testing.T) {
	want := []tui.Event{tui.KeyEvent{Code: 'ƒ', Text: "ƒ"}}
	if got := decodeEvents(t, "ƒ"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestOptionFoldLeavesPastedTextAlone(t *testing.T) {
	want := []tui.Event{tui.PasteEvent{Text: "ƒ©ø"}}
	if got := decodeFolded(t, "\x1b[200~ƒ©ø\x1b[201~"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

// TestWithOptionFoldReachesTheBackend: the option, given to Open, folds what the terminal sends.
func TestWithOptionFoldReachesTheBackend(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
		want tui.Event
	}{
		{"on", []Option{WithOptionFold()}, key('f', tui.ModAlt)},
		{"off", nil, tui.KeyEvent{Code: 'ƒ', Text: "ƒ"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScript(t, tc.opts...)
			s.respond("\x1b[?62c")
			if err := s.start(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.write("ƒ")
			ev, ok := s.waitEvent(2 * time.Second)
			if !ok {
				t.Fatal("no event")
			}
			if !reflect.DeepEqual(ev, tc.want) {
				t.Fatalf("got %#v want %#v", ev, tc.want)
			}
		})
	}
}
