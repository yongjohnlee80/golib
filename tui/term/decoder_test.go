package term

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// decodeEvents runs the full byte→event path synchronously (no goroutines):
// the whole decode surface, asserted directly.
func decodeEvents(t *testing.T, input string) []tui.Event {
	t.Helper()
	var out []tui.Event
	d := &decoder{emit: func(ev tui.Event) { out = append(out, ev) }}
	d.feedBytes([]byte(input))
	return out
}

func key(code rune, mods tui.Mods) tui.KeyEvent {
	return tui.KeyEvent{Code: code, Mods: mods}
}

func TestDecodeLegacyKeys(t *testing.T) {
	// Every ctlseqs sequence shape the decoder claims — arrows CSI/SS3,
	// Home/End both encodings, tilde keys, F-keys, xterm modifier encoding,
	// Alt-as-ESC-prefix, Ctrl+letter.
	cases := []struct {
		name  string
		input string
		want  []tui.Event
	}{
		{"printable", "a", []tui.Event{tui.KeyEvent{Code: 'a', Text: "a"}}},
		{"printable upper", "A", []tui.Event{tui.KeyEvent{Code: 'A', Text: "A"}}},
		{"utf8 print", "é", []tui.Event{tui.KeyEvent{Code: 'é', Text: "é"}}},
		{"enter", "\r", []tui.Event{key(tui.KeyEnter, 0)}},
		{"tab", "\t", []tui.Event{key(tui.KeyTab, 0)}},
		{"backspace del", "\x7f", []tui.Event{key(tui.KeyBackspace, 0)}},
		{"ctrl-a", "\x01", []tui.Event{key('a', tui.ModCtrl)}},
		{"ctrl-h", "\x08", []tui.Event{key('h', tui.ModCtrl)}},
		{"ctrl-x can", "\x18", []tui.Event{key('x', tui.ModCtrl)}},
		{"ctrl-z sub", "\x1a", []tui.Event{key('z', tui.ModCtrl)}},
		{"ctrl-space", "\x00", []tui.Event{key(' ', tui.ModCtrl)}},
		{"ctrl-backslash", "\x1c", []tui.Event{key('\\', tui.ModCtrl)}},

		{"csi up", "\x1b[A", []tui.Event{key(tui.KeyUp, 0)}},
		{"csi down", "\x1b[B", []tui.Event{key(tui.KeyDown, 0)}},
		{"csi right", "\x1b[C", []tui.Event{key(tui.KeyRight, 0)}},
		{"csi left", "\x1b[D", []tui.Event{key(tui.KeyLeft, 0)}},
		{"csi home", "\x1b[H", []tui.Event{key(tui.KeyHome, 0)}},
		{"csi end", "\x1b[F", []tui.Event{key(tui.KeyEnd, 0)}},
		{"ss3 up", "\x1bOA", []tui.Event{key(tui.KeyUp, 0)}},
		{"ss3 f1", "\x1bOP", []tui.Event{key(tui.KeyF1, 0)}},
		{"ss3 f4", "\x1bOS", []tui.Event{key(tui.KeyF4, 0)}},
		{"ss3 home", "\x1bOH", []tui.Event{key(tui.KeyHome, 0)}},

		{"tilde home", "\x1b[1~", []tui.Event{key(tui.KeyHome, 0)}},
		{"tilde insert", "\x1b[2~", []tui.Event{key(tui.KeyInsert, 0)}},
		{"tilde delete", "\x1b[3~", []tui.Event{key(tui.KeyDelete, 0)}},
		{"tilde end", "\x1b[4~", []tui.Event{key(tui.KeyEnd, 0)}},
		{"tilde pgup", "\x1b[5~", []tui.Event{key(tui.KeyPageUp, 0)}},
		{"tilde pgdn", "\x1b[6~", []tui.Event{key(tui.KeyPageDown, 0)}},
		{"tilde f5", "\x1b[15~", []tui.Event{key(tui.KeyF5, 0)}},
		{"tilde f10", "\x1b[21~", []tui.Event{key(tui.KeyF10, 0)}},
		{"tilde f12", "\x1b[24~", []tui.Event{key(tui.KeyF12, 0)}},

		// xterm modifier encoding: m = 1 + bitmask(shift=1, alt=2, ctrl=4).
		{"ctrl-up", "\x1b[1;5A", []tui.Event{key(tui.KeyUp, tui.ModCtrl)}},
		{"shift-up", "\x1b[1;2A", []tui.Event{key(tui.KeyUp, tui.ModShift)}},
		{"alt-left", "\x1b[1;3D", []tui.Event{key(tui.KeyLeft, tui.ModAlt)}},
		{"ctrl-shift-end", "\x1b[1;6F", []tui.Event{key(tui.KeyEnd, tui.ModCtrl|tui.ModShift)}},
		{"ctrl-delete", "\x1b[3;5~", []tui.Event{key(tui.KeyDelete, tui.ModCtrl)}},
		{"shift-f6", "\x1b[17;2~", []tui.Event{key(tui.KeyF6, tui.ModShift)}},
		{"modified f1", "\x1b[1;5P", []tui.Event{key(tui.KeyF1, tui.ModCtrl)}},
		{"back-tab", "\x1b[Z", []tui.Event{key(tui.KeyTab, tui.ModShift)}},

		{"alt-a", "\x1ba", []tui.Event{key('a', tui.ModAlt)}},
		{"alt-digit", "\x1b1", []tui.Event{key('1', tui.ModAlt)}},
		{"alt-utf8", "\x1bé", []tui.Event{key('é', tui.ModAlt)}},

		// Malformed-sequence recovery: CAN aborts, next input decodes clean.
		{"can recovery", "\x1b[12\x18\x1b[A", []tui.Event{key('x', tui.ModCtrl), key(tui.KeyUp, 0)}},
		{"esc restart recovery", "\x1b[99\x1b[B", []tui.Event{key(tui.KeyDown, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeEvents(t, tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decode(%q)\n got: %#v\nwant: %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestDecodeKittyKeys(t *testing.T) {
	// Kitty CSI u sequences with sub-parameters
	// (https://sw.kovidgoyal.net/kitty/keyboard-protocol/).
	cases := []struct {
		name  string
		input string
		want  []tui.Event
	}{
		{"esc key", "\x1b[27u", []tui.Event{key(tui.KeyEscape, 0)}},
		{"ctrl-a", "\x1b[97;5u", []tui.Event{key('a', tui.ModCtrl)}},
		{"super-space", "\x1b[32;9u", []tui.Event{key(' ', tui.ModSuper)}},
		{"release", "\x1b[97;1:3u",
			[]tui.Event{tui.KeyEvent{Kind: tui.KeyRelease, Code: 'a'}}},
		{"repeat with ctrl", "\x1b[97;5:2u",
			[]tui.Event{tui.KeyEvent{Kind: tui.KeyRepeat, Code: 'a', Mods: tui.ModCtrl}}},
		{"alternates shifted+base", "\x1b[97:65:97;2u",
			[]tui.Event{tui.KeyEvent{Code: 'a', Shifted: 'A', Base: 'a', Mods: tui.ModShift}}},
		{"text codepoints", "\x1b[97;;97u",
			[]tui.Event{tui.KeyEvent{Code: 'a', Text: "a"}}},
		{"functional pua f1", "\x1b[57364u", []tui.Event{key(tui.KeyF1, 0)}},
		{"functional pua up + arrows legacy form with event", "\x1b[1;1:3A",
			[]tui.Event{tui.KeyEvent{Kind: tui.KeyRelease, Code: tui.KeyUp}}},
		{"capslock bit", "\x1b[97;65u",
			[]tui.Event{tui.KeyEvent{Code: 'a', Mods: tui.ModCapsLock}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeEvents(t, tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decode(%q)\n got: %#v\nwant: %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestDecodeMouseSGR(t *testing.T) {
	// SGR press/release/motion/wheel with modifier
	// bits (0–2 button, +4 shift, +8 meta, +16 ctrl, +32 motion, +64 wheel).
	// Coordinates are 1-based on the wire, 0-based in events.
	cases := []struct {
		name  string
		input string
		want  []tui.Event
	}{
		{"left press", "\x1b[<0;1;1M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 0, Y: 0}}},
		{"left release", "\x1b[<0;10;5m",
			[]tui.Event{tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 9, Y: 4}}},
		{"right press", "\x1b[<2;3;4M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 2, Y: 3}}},
		{"middle press ctrl", "\x1b[<17;2;2M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseMiddle, X: 1, Y: 1, Mods: tui.ModCtrl}}},
		{"drag motion left held", "\x1b[<32;6;7M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 5, Y: 6}}},
		{"motion no button", "\x1b[<35;6;7M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseNone, X: 5, Y: 6}}},
		{"wheel up", "\x1b[<64;1;1M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelUp, X: 0, Y: 0}}},
		{"wheel down shift", "\x1b[<69;1;1M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 0, Y: 0, Mods: tui.ModShift}}},
		{"wheel left", "\x1b[<66;1;1M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelLeft, X: 0, Y: 0}}},
		{"press alt+shift", "\x1b[<12;1;1M",
			[]tui.Event{tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 0, Y: 0, Mods: tui.ModShift | tui.ModAlt}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeEvents(t, tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decode(%q)\n got: %#v\nwant: %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestDecodeBracketedPaste(t *testing.T) {
	// Paste framing: CR and CRLF normalized to \n, embedded
	// ESC captured literally, a 200~ opener inside a paste is literal text.
	cases := []struct {
		name  string
		input string
		want  []tui.Event
	}{
		{"simple", "\x1b[200~hello\x1b[201~",
			[]tui.Event{tui.PasteEvent{Text: "hello"}}},
		{"crlf normalized", "\x1b[200~a\r\nb\rc\nd\x1b[201~",
			[]tui.Event{tui.PasteEvent{Text: "a\nb\nc\nd"}}},
		{"embedded esc literal", "\x1b[200~a\x1bb\x1b[Ac\x1b[201~",
			[]tui.Event{tui.PasteEvent{Text: "a\x1bb\x1b[Ac"}}},
		{"nested opener literal", "\x1b[200~x\x1b[200~y\x1b[201~",
			[]tui.Event{tui.PasteEvent{Text: "x\x1b[200~y"}}},
		{"partial terminator literal", "\x1b[200~a\x1b[20zb\x1b[201~",
			[]tui.Event{tui.PasteEvent{Text: "a\x1b[20zb"}}},
		{"keys after paste", "\x1b[200~p\x1b[201~q",
			[]tui.Event{tui.PasteEvent{Text: "p"}, tui.KeyEvent{Code: 'q', Text: "q"}}},
		{"utf8 content", "\x1b[200~héllo 🎉\x1b[201~",
			[]tui.Event{tui.PasteEvent{Text: "héllo 🎉"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeEvents(t, tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decode(%q)\n got: %#v\nwant: %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestDecodeUnterminatedPasteFlushedOnFinish(t *testing.T) {
	// An unterminated paste is flushed as a paste on Stop
	// rather than dropped — including a dangling partial terminator.
	var out []tui.Event
	d := &decoder{emit: func(ev tui.Event) { out = append(out, ev) }}
	d.feedBytes([]byte("\x1b[200~lost\x1b[20"))
	if len(out) != 0 {
		t.Fatalf("premature events: %#v", out)
	}
	d.finish()
	want := []tui.Event{tui.PasteEvent{Text: "lost\x1b[20"}}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got %#v want %#v", out, want)
	}
}

func TestDecodePasteSplitAcrossChunks(t *testing.T) {
	// Split-boundary safety for the paste terminator matcher.
	full := "\x1b[200~ab\x1b[201~"
	for split := 1; split < len(full); split++ {
		var out []tui.Event
		d := &decoder{emit: func(ev tui.Event) { out = append(out, ev) }}
		d.feedBytes([]byte(full[:split]))
		d.feedBytes([]byte(full[split:]))
		want := []tui.Event{tui.PasteEvent{Text: "ab"}}
		if !reflect.DeepEqual(out, want) {
			t.Fatalf("split %d: got %#v want %#v", split, out, want)
		}
	}
}

func TestDecodeInBandResize(t *testing.T) {
	// The mode-2048 report: CSI 48 ; rows ; cols ; hpx ; wpx t.
	got := decodeEvents(t, "\x1b[48;30;120;600;1920t")
	want := []tui.Event{tui.ResizeEvent{W: 120, H: 30}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestDecodeFocus(t *testing.T) {
	focusIn := 0
	var out []tui.Event
	d := &decoder{
		emit:      func(ev tui.Event) { out = append(out, ev) },
		onFocusIn: func() { focusIn++ },
	}
	d.feedBytes([]byte("\x1b[I\x1b[O"))
	want := []tui.Event{
		tui.FocusEvent{Gained: true, Terminal: true},
		tui.FocusEvent{Gained: false, Terminal: true},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got %#v want %#v", out, want)
	}
	if focusIn != 1 {
		t.Fatalf("onFocusIn fired %d times, want 1", focusIn)
	}
}

func TestDecodeProbeRepliesAreNotEvents(t *testing.T) {
	// Probe replies route to the probe callback, never onto
	// the event stream.
	var evs []tui.Event
	var replies []probeReply
	d := &decoder{
		emit:  func(ev tui.Event) { evs = append(evs, ev) },
		probe: func(r probeReply) { replies = append(replies, r) },
	}
	d.feedBytes([]byte(
		"\x1b[?2026;1$y" + // DECRPM
			"\x1b[?0u" + // kitty query reply
			"\x1bP1+r524742=1;536D756C78=1\x1b\\" + // XTGETTCAP RGB+Smulx
			"\x1b]11;rgb:0000/0000/0000\x1b\\" + // OSC 11
			"\x1b[?62;22c")) // DA1 fence
	if len(evs) != 0 {
		t.Fatalf("probe replies leaked as events: %#v", evs)
	}
	if len(replies) != 5 {
		t.Fatalf("expected 5 probe replies, got %d: %#v", len(replies), replies)
	}
	if replies[0].kind != prDECRPM || replies[0].mode != 2026 || replies[0].value != 1 {
		t.Errorf("DECRPM decode wrong: %#v", replies[0])
	}
	if replies[1].kind != prKitty {
		t.Errorf("kitty reply wrong: %#v", replies[1])
	}
	if replies[2].kind != prTermcap || !replies[2].rgb || !replies[2].smulx {
		t.Errorf("XTGETTCAP decode wrong: %#v", replies[2])
	}
	if replies[3].kind != prOSCColor || replies[3].osc != 11 || !replies[3].color.Known {
		t.Errorf("OSC decode wrong: %#v", replies[3])
	}
	if replies[4].kind != prDA1 {
		t.Errorf("DA1 decode wrong: %#v", replies[4])
	}
}

func TestParseOSCColor(t *testing.T) {
	cases := []struct {
		in   string
		want tui.ProbedColor
		ok   bool
	}{
		{"rgb:ffff/0000/8080", tui.ProbedColor{R: 255, G: 0, B: 128, Known: true}, true},
		{"rgb:ff/00/80", tui.ProbedColor{R: 255, G: 0, B: 128, Known: true}, true},
		{"rgb:f/0/8", tui.ProbedColor{R: 255, G: 0, B: 136, Known: true}, true},
		{"rgba:ffff/0000/8080/ffff", tui.ProbedColor{R: 255, G: 0, B: 128, Known: true}, true},
		{"#1e2e3e", tui.ProbedColor{R: 0x1e, G: 0x2e, B: 0x3e, Known: true}, true},
		{"nonsense", tui.ProbedColor{}, false},
		{"rgb:zz/00/00", tui.ProbedColor{}, false},
		{"rgb:ff/00", tui.ProbedColor{}, false},
	}
	for _, tc := range cases {
		got, ok := parseOSCColor(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseOSCColor(%q) = %#v, %v; want %#v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDecodeEscResolution(t *testing.T) {
	// The decoder half of the ESC disambiguation: a pending lone ESC
	// resolves to the Escape key; a pending ESC O resolves to Alt+O.
	var out []tui.Event
	d := &decoder{emit: func(ev tui.Event) { out = append(out, ev) }}

	d.feedBytes([]byte("\x1b"))
	if !d.awaitingEsc() {
		t.Fatal("lone ESC should be pending")
	}
	if len(out) != 0 {
		t.Fatalf("lone ESC emitted early: %#v", out)
	}
	d.resolveEsc()
	if want := []tui.Event{key(tui.KeyEscape, 0)}; !reflect.DeepEqual(out, want) {
		t.Fatalf("got %#v want %#v", out, want)
	}

	// ESC followed by continuation in a later chunk is a sequence, not ESC.
	out = nil
	d.feedBytes([]byte("\x1b"))
	d.feedBytes([]byte("[A"))
	if d.awaitingEsc() {
		t.Fatal("completed sequence still pending")
	}
	if want := []tui.Event{key(tui.KeyUp, 0)}; !reflect.DeepEqual(out, want) {
		t.Fatalf("got %#v want %#v", out, want)
	}

	// Pending ESC O resolves to Alt+O.
	out = nil
	d.feedBytes([]byte("\x1bO"))
	if !d.awaitingEsc() {
		t.Fatal("ESC O should be pending")
	}
	d.resolveEsc()
	if want := []tui.Event{key('O', tui.ModAlt)}; !reflect.DeepEqual(out, want) {
		t.Fatalf("got %#v want %#v", out, want)
	}
}
