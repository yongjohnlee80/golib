package web

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// Criterion 7: every row of §2.9's table, asserted against the REAL struct
// fields. An unmapped browser key is dropped with no phantom event.
func TestDecodeKey_Table(t *testing.T) {
	t.Parallel()
	d := &decoder{}

	cases := []struct {
		name string
		in   KeyReport
		want tui.Event // nil means "emit nothing"
	}{
		// Rule 1 — reserved browser shortcuts. Stealing these is how a web
		// terminal becomes hostile.
		{"Ctrl+T", KeyReport{Key: "t", Ctrl: true}, nil},
		{"Cmd+T", KeyReport{Key: "t", Meta: true}, nil},
		{"Ctrl+N", KeyReport{Key: "n", Ctrl: true}, nil},
		{"Ctrl+W", KeyReport{Key: "w", Ctrl: true}, nil},
		{"Ctrl+L", KeyReport{Key: "l", Ctrl: true}, nil},
		{"Ctrl+R", KeyReport{Key: "r", Ctrl: true}, nil},
		{"Ctrl+Shift+R", KeyReport{Key: "R", Ctrl: true, Shift: true}, nil},
		{"Ctrl+Tab", KeyReport{Key: "Tab", Ctrl: true}, nil},
		{"F5", KeyReport{Key: "F5"}, nil},
		{"F11", KeyReport{Key: "F11"}, nil},
		{"F12", KeyReport{Key: "F12"}, nil},
		{"Cmd+Q", KeyReport{Key: "q", Meta: true}, nil},
		// Ctrl+Q is NOT reserved: only Cmd+Q quits a browser.
		{"Ctrl+Q reaches the app", KeyReport{Key: "q", Ctrl: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'q', Mods: tui.ModCtrl}},

		// Rule 2 — composing or AltGraph is never a chord.
		{"isComposing", KeyReport{Key: "a", Composing: true}, nil},
		{"isComposing with modifiers", KeyReport{Key: "a", Composing: true, Ctrl: true}, nil},
		{"AltGraph", KeyReport{Key: "€", AltGraph: true}, nil},
		// The crucial case: AltGraph reported alongside Ctrl+Alt must NOT
		// become a Ctrl+Alt command.
		{"AltGraph reported as Ctrl+Alt", KeyReport{Key: "€", AltGraph: true, Ctrl: true, Alt: true}, nil},
		// ...but without the AltGraph flag, Ctrl+Alt is a legitimate chord and
		// must not be swallowed by default.
		{"plain Ctrl+Alt is a chord by default", KeyReport{Key: "d", Ctrl: true, Alt: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'd', Mods: tui.ModCtrl | tui.ModAlt}},

		// Rule 3 — named keys.
		{"Enter", KeyReport{Key: "Enter"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}},
		{"Tab", KeyReport{Key: "Tab"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}},
		{"Escape", KeyReport{Key: "Escape"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}},
		{"Backspace", KeyReport{Key: "Backspace"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyBackspace}},
		{"Delete", KeyReport{Key: "Delete"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDelete}},
		{"Insert", KeyReport{Key: "Insert"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyInsert}},
		{"ArrowUp", KeyReport{Key: "ArrowUp"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyUp}},
		{"ArrowDown", KeyReport{Key: "ArrowDown"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}},
		{"ArrowLeft", KeyReport{Key: "ArrowLeft"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft}},
		{"ArrowRight", KeyReport{Key: "ArrowRight"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight}},
		{"Home", KeyReport{Key: "Home"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyHome}},
		{"End", KeyReport{Key: "End"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnd}},
		{"PageUp", KeyReport{Key: "PageUp"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageUp}},
		{"PageDown", KeyReport{Key: "PageDown"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageDown}},
		{"F1", KeyReport{Key: "F1"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF1}},
		{"F10", KeyReport{Key: "F10"}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10}},
		// A named key with modifiers stays a named key: a table lookup is what
		// makes Ctrl+Enter unambiguous.
		{"Ctrl+Enter", KeyReport{Key: "Enter", Ctrl: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter, Mods: tui.ModCtrl}},
		{"Shift+Tab", KeyReport{Key: "Tab", Shift: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab, Mods: tui.ModShift}},

		// repeat: true → KeyRepeat.
		{"held ArrowDown", KeyReport{Key: "ArrowDown", Repeat: true},
			tui.KeyEvent{Kind: tui.KeyRepeat, Code: tui.KeyDown}},

		// Rule 5 — modified printable.
		{"Ctrl+C", KeyReport{Key: "c", Ctrl: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'c', Mods: tui.ModCtrl}},
		// Lower-cased, so a component matches 'c' whether or not shift is held.
		{"Ctrl+Shift+C", KeyReport{Key: "C", Ctrl: true, Shift: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'c', Mods: tui.ModCtrl | tui.ModShift}},
		{"Alt+x", KeyReport{Key: "x", Alt: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'x', Mods: tui.ModAlt}},
		// metaKey maps to ModSuper, not ModMeta: tui.Mods is in kitty order and
		// the obvious mapping would break every Cmd chord on macOS.
		{"Cmd+S", KeyReport{Key: "s", Meta: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 's', Mods: tui.ModSuper}},
		{"Cmd+Shift+S", KeyReport{Key: "S", Meta: true, Shift: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 's', Mods: tui.ModSuper | tui.ModShift}},
		{"Ctrl+non-ASCII", KeyReport{Key: "é", Ctrl: true},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'é', Mods: tui.ModCtrl}},

		// Dropped: Code holds ONE rune, so a multi-scalar key has nothing
		// faithful to put in it.
		{"Ctrl+Dead", KeyReport{Key: "Dead", Ctrl: true}, nil},
		{"Ctrl+Unidentified", KeyReport{Key: "Unidentified", Ctrl: true}, nil},
		{"Ctrl+multi-codepoint emoji", KeyReport{Key: "👨‍👩‍👦", Ctrl: true}, nil},
		{"Ctrl+empty key", KeyReport{Key: "", Ctrl: true}, nil},

		// Rule 6 — everything else, dropped explicitly. Never a phantom key.
		{"unmodified printable is text", KeyReport{Key: "a"}, nil},
		{"unmodified space is text", KeyReport{Key: " "}, nil},
		{"bare Shift", KeyReport{Key: "Shift", Shift: true}, nil},
		{"bare Control", KeyReport{Key: "Control", Ctrl: true}, nil},
		{"bare Alt", KeyReport{Key: "Alt", Alt: true}, nil},
		{"bare Meta", KeyReport{Key: "Meta", Meta: true}, nil},
		{"Dead alone", KeyReport{Key: "Dead"}, nil},
		{"Unidentified alone", KeyReport{Key: "Unidentified"}, nil},
		{"CapsLock", KeyReport{Key: "CapsLock"}, nil},
		{"unknown named key", KeyReport{Key: "BrightnessUp"}, nil},
		{"F13 is not in the table", KeyReport{Key: "F13"}, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := d.decodeKey(c.in)
			if c.want == nil {
				if ok {
					t.Fatalf("emitted %#v, want nothing", got)
				}
				return
			}
			if !ok {
				t.Fatalf("emitted nothing, want %#v", c.want)
			}
			if got != c.want {
				t.Errorf("got  %#v\nwant %#v", got, c.want)
			}
			// Base and Shifted are ALWAYS zero: the DOM exposes no base-layout
			// or shifted codepoint, and 0 is exactly what a non-kitty terminal
			// produces.
			if k, isKey := got.(tui.KeyEvent); isKey && (k.Base != 0 || k.Shifted != 0) {
				t.Errorf("Base=%d Shifted=%d, both must be 0: the browser cannot supply them", k.Base, k.Shifted)
			}
		})
	}
}

// keyup is never forwarded: KeyRelease is kitty-only and must not be
// synthesized here, or a component would see releases from one backend and not
// another.
func TestDecodeKey_NeverSynthesizesRelease(t *testing.T) {
	t.Parallel()
	d := &decoder{}
	for _, key := range []string{"Enter", "a", "ArrowUp", "F1"} {
		ev, ok := d.decodeKey(KeyReport{Key: key, Ctrl: true})
		if !ok {
			continue
		}
		if k, isKey := ev.(tui.KeyEvent); isKey && k.Kind == tui.KeyRelease {
			t.Errorf("%s produced KeyRelease, which is kitty-only", key)
		}
	}
}

// The AltGraph opt-in exists because some browsers do not report the modifier.
// It must stay OFF by default, or every legitimate Ctrl+Alt chord is swallowed.
func TestDecodeKey_TreatCtrlAltAsAltGraph(t *testing.T) {
	t.Parallel()
	report := KeyReport{Key: "d", Ctrl: true, Alt: true}

	off := &decoder{}
	got, ok := off.decodeKey(report)
	if !ok {
		t.Fatal("with the heuristic OFF, Ctrl+Alt must remain a chord")
	}
	want := tui.KeyEvent{Kind: tui.KeyPress, Code: 'd', Mods: tui.ModCtrl | tui.ModAlt}
	if got != want {
		t.Errorf("got %#v, want %#v", got, want)
	}

	on := &decoder{treatCtrlAltAsAltGraph: true}
	if _, ok := on.decodeKey(report); ok {
		t.Error("with the heuristic ON, Ctrl+Alt must go to the text path")
	}
	// The opt-in must not affect anything else.
	if _, ok := on.decodeKey(KeyReport{Key: "c", Ctrl: true}); !ok {
		t.Error("the opt-in swallowed a plain Ctrl chord")
	}
}

// Emission is per RUNE, matching tui/term's actPrint. A component must not be
// able to tell the two backends apart.
func TestDecodeText_PerRune(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in    string
		codes []rune
	}{
		"ascii":                 {"abc", []rune{'a', 'b', 'c'}},
		"accented":              {"héllo", []rune{'h', 'é', 'l', 'l', 'o'}},
		"CJK":                   {"漢字", []rune{'漢', '字'}},
		"single emoji":          {"🙂", []rune{'🙂'}},
		"multi-codepoint emoji": {"👨‍👩‍👦", []rune{'👨', 0x200d, '👩', 0x200d, '👦'}},
		"combining sequence":    {"é", []rune{'e', 0x0301}},
		"empty":                 {"", nil},
		"newline":               {"a\nb", []rune{'a', '\n', 'b'}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := decodeText(c.in)
			if len(got) != len(c.codes) {
				t.Fatalf("%d events, want %d: %#v", len(got), len(c.codes), got)
			}
			for i, want := range c.codes {
				k, ok := got[i].(tui.KeyEvent)
				if !ok {
					t.Fatalf("event %d is %#v, want KeyEvent", i, got[i])
				}
				if k.Code != want {
					t.Errorf("event %d Code = %q, want %q", i, k.Code, want)
				}
				if k.Text != string(want) {
					t.Errorf("event %d Text = %q, want %q", i, k.Text, string(want))
				}
				if k.Kind != tui.KeyPress {
					t.Errorf("event %d Kind = %v, want KeyPress", i, k.Kind)
				}
				if k.Mods != 0 || k.Base != 0 || k.Shifted != 0 {
					t.Errorf("event %d = %#v: text carries no modifiers and no Base/Shifted", i, k)
				}
			}
		})
	}
}

// Invalid UTF-8 from a client is dropped rather than forwarded as a replacement
// character the user never typed.
func TestDecodeText_DropsInvalidUTF8(t *testing.T) {
	t.Parallel()
	got := decodeText("a\xffb")
	if len(got) != 2 {
		t.Fatalf("%d events, want 2 (the invalid byte dropped): %#v", len(got), got)
	}
	for i, want := range []rune{'a', 'b'} {
		if k := got[i].(tui.KeyEvent); k.Code != want {
			t.Errorf("event %d = %q, want %q", i, k.Code, want)
		}
	}
}

// The web backend's scalar sequence for a given text must match what the
// terminal decoder would produce, which is the parity claim in criterion 7.
func TestDecodeText_MatchesTerminalScalarSequence(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"hello", "héllo wörld", "漢字テスト", "🙂🙃", "👨‍👩‍👦"} {
		got := decodeText(text)
		// The terminal decoder emits one KeyEvent{Code: r, Text: string(r)} per
		// rune (decoder.go's actPrint), so the reference is simply the rune
		// sequence of the same string.
		var want []tui.Event
		for _, r := range text {
			want = append(want, tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)})
		}
		if len(got) != len(want) {
			t.Fatalf("%q: %d events, want %d", text, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q event %d: got %#v want %#v", text, i, got[i], want[i])
			}
		}
	}
}

func TestDecodePaste_NormalizesLineEndings(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a\r\nb":     "a\nb",
		"a\rb":       "a\nb",
		"a\nb":       "a\nb",
		"a\r\n\r\nb": "a\n\nb",
		"a\n\rb":     "a\n\nb",
		"":           "",
	}
	for in, want := range cases {
		ev := decodePaste(in)
		p, ok := ev.(tui.PasteEvent)
		if !ok {
			t.Fatalf("%q produced %#v, want PasteEvent", in, ev)
		}
		if p.Text != want {
			t.Errorf("%q -> %q, want %q", in, p.Text, want)
		}
	}
}

func TestDecodeMouse_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   MouseReport
		want tui.Event
	}{
		{"left down", MouseReport{Kind: "down", Button: 0, X: 3, Y: 4},
			tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 3, Y: 4}},
		{"middle down", MouseReport{Kind: "down", Button: 1, X: 1, Y: 1},
			tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseMiddle, X: 1, Y: 1}},
		{"right up", MouseReport{Kind: "up", Button: 2, X: 9, Y: 0},
			tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 9, Y: 0}},
		{"motion carries no button", MouseReport{Kind: "move", Button: 0, X: 5, Y: 6},
			tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseNone, X: 5, Y: 6}},
		{"wheel up", MouseReport{Kind: "wheel", Dir: "up", X: 2, Y: 2},
			tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelUp, X: 2, Y: 2}},
		{"wheel down", MouseReport{Kind: "wheel", Dir: "down", X: 2, Y: 2},
			tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 2}},
		{"wheel left", MouseReport{Kind: "wheel", Dir: "left", X: 2, Y: 2},
			tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelLeft, X: 2, Y: 2}},
		{"wheel right", MouseReport{Kind: "wheel", Dir: "right", X: 2, Y: 2},
			tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelRight, X: 2, Y: 2}},
		{"modifiers", MouseReport{Kind: "down", Button: 0, X: 1, Y: 1, Ctrl: true, Shift: true, Meta: true},
			tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 1,
				Mods: tui.ModCtrl | tui.ModShift | tui.ModSuper}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := decodeMouse(c.in)
			if !ok {
				t.Fatalf("emitted nothing, want %#v", c.want)
			}
			if got != c.want {
				t.Errorf("got  %#v\nwant %#v", got, c.want)
			}
		})
	}
}

// An unknown button index is refused rather than mapped to left, which would
// turn a stray back/forward button into a click.
func TestDecodeMouse_UnknownInputsAreDropped(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]MouseReport{
		"button 3":        {Kind: "down", Button: 3},
		"button 4":        {Kind: "up", Button: 4},
		"negative button": {Kind: "down", Button: -1},
		"unknown kind":    {Kind: "hover"},
		"empty kind":      {},
		"wheel no dir":    {Kind: "wheel"},
		"wheel bad dir":   {Kind: "wheel", Dir: "diagonal"},
	} {
		if got, ok := decodeMouse(in); ok {
			t.Errorf("%s produced %#v, want nothing", name, got)
		}
	}
}

// Window focus is TERMINAL-level focus, not component focus, which the App's
// focus manager owns.
func TestDecodeFocus(t *testing.T) {
	t.Parallel()
	for _, gained := range []bool{true, false} {
		ev := decodeFocus(gained)
		f, ok := ev.(tui.FocusEvent)
		if !ok {
			t.Fatalf("%#v is not a FocusEvent", ev)
		}
		if f.Gained != gained {
			t.Errorf("Gained = %v, want %v", f.Gained, gained)
		}
		if !f.Terminal {
			t.Error("Terminal must be true: this is terminal-level focus")
		}
	}
}

// The client decides preventDefault synchronously, so it needs the same tables.
// Exporting them is what keeps the two sides from drifting apart.
func TestNamedKeys_IsTheSameTableTheDecoderUses(t *testing.T) {
	t.Parallel()
	names := NamedKeys()
	if len(names) != len(namedKeys) {
		t.Fatalf("NamedKeys returned %d entries, the decoder has %d", len(names), len(namedKeys))
	}
	for _, n := range names {
		if _, ok := namedKeys[n]; !ok {
			t.Errorf("NamedKeys reported %q, which the decoder does not forward", n)
		}
	}
	// Sorted, so the generated client asset is byte-stable across builds.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Fatalf("NamedKeys is not sorted at %d (%q before %q)", i, names[i-1], names[i])
		}
	}
}

func TestSingleScalar(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		want rune
		ok   bool
	}{
		"a":     {'a', true},
		"A":     {'a', true}, // lower-cased for stable matching
		"é":     {'é', true},
		"É":     {'é', true},
		"漢":     {'漢', true},
		"🙂":     {'🙂', true},
		"1":     {'1', true},
		" ":     {' ', true},
		"":      {0, false},
		"ab":    {0, false},
		"Dead":  {0, false},
		"👨‍👩‍👦": {0, false},
		"İ":     {'i', true},
	}
	for in, c := range cases {
		got, ok := singleScalar(in)
		if ok != c.ok {
			t.Errorf("singleScalar(%q) ok = %v, want %v", in, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("singleScalar(%q) = %q, want %q", in, got, c.want)
		}
	}

	// Go's simple case mapping never expands: İ lower-cases to a single 'i'
	// here, not to i + combining dot above, because neither strings nor unicode
	// applies Unicode's full locale-sensitive mappings. Pinned so that a future
	// switch to full case folding — which WOULD expand and so would not fit
	// Code's single rune — fails here rather than silently truncating.
	if got, ok := singleScalar("İ"); !ok || got != 'i' {
		t.Errorf(`singleScalar("İ") = %q, %v — want 'i' from Go's simple case mapping`, got, ok)
	}
	// The real protection for Code's single rune is the multi-scalar rejection,
	// which is asserted in the table above.
}

func TestMods_MetaMapsToSuper(t *testing.T) {
	t.Parallel()
	// The obvious-looking metaKey->ModMeta mapping would silently break every
	// Cmd chord on macOS, because tui.Mods is in kitty order where super is
	// Cmd/Windows and meta is the historical Meta.
	if got := mods(false, false, false, true); got != tui.ModSuper {
		t.Errorf("metaKey mapped to %v, want ModSuper", got)
	}
	if got := mods(false, false, false, true); got&tui.ModMeta != 0 {
		t.Error("metaKey must not set ModMeta")
	}
	all := mods(true, true, true, true)
	for name, bit := range map[string]tui.Mods{
		"ModCtrl": tui.ModCtrl, "ModAlt": tui.ModAlt,
		"ModShift": tui.ModShift, "ModSuper": tui.ModSuper,
	} {
		if all&bit == 0 {
			t.Errorf("%s missing from %v", name, all)
		}
	}
	if all&tui.ModMeta != 0 {
		t.Error("ModMeta must never be set by this backend")
	}
	if got := mods(false, false, false, false); got != 0 {
		t.Errorf("no modifiers = %v, want 0", got)
	}
}
