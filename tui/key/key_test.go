package key

import (
	"errors"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

func TestParseReadsQtPortableText(t *testing.T) {
	for _, c := range []struct {
		text string
		want Seq
	}{
		{"Ctrl+S", Seq{'s', Ctrl}},
		{"ctrl+shift+s", Seq{'s', Ctrl | Shift}},
		{"Shift+Down", Seq{Down, Shift}},
		{"F10", Seq{F10, 0}},
		{"Esc", Seq{Escape, 0}},
		{"escape", Seq{Escape, 0}},
		{"PgDown", Seq{PageDown, 0}},
		{"?", Seq{'?', 0}},
		{"G", Seq{'g', 0}},
		{"Shift+G", Seq{'g', Shift}},
		{"+", Seq{'+', 0}},
		{"Ctrl++", Seq{'+', Ctrl}},
		{",", Seq{',', 0}},
		{"Ctrl+,", Seq{',', Ctrl}},
		{"Space", Seq{' ', 0}},
		{"Super+Z", Seq{'z', Super}},
		{"Meta+X", Seq{'x', Alt}}, // the recorded deviation: meta is Alt
		{"Menu", Seq{Menu, 0}},
		{"Ctrl+Shift+/", Seq{'/', Ctrl | Shift}},
		{" Ctrl + Q ", Seq{'q', Ctrl}},
	} {
		got, err := Parse(c.text)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.text, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.text, got, c.want)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	for _, text := range []string{
		"", "Ctrl+", "+Q", "Ctrl++Q", "Hyper+Q", "Ctrl+Foo", "Shift+/", "Shift+Space", "Shift+?",
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("Parse(%q) accepted", text)
		} else if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("Parse(%q): %v is not ErrInvalidArgument", text, err)
		}
	}
	if _, err := Parse("Ctrl+K, Ctrl+C"); !errors.Is(err, ErrMultiChord) || !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("a multi-chord sequence: %v, want ErrMultiChord", err)
	}
}

func TestStringRoundTrips(t *testing.T) {
	codes := []rune{'a', 'z', '?', '+', ',', '/', '1', ' ', Escape, Enter, Tab, Backspace, Up, Down, Left, Right,
		Home, End, PageUp, PageDown, Insert, Delete, F1, F10, F12, Menu}
	for _, code := range codes {
		for m := Mods(0); m <= bindable; m++ {
			if m&^bindable != 0 {
				continue
			}
			s := Seq{code, m}
			got, err := Parse(s.String())
			if m&Shift != 0 && m&heavy == 0 && printable(code) && code != ' ' && !isLetter(code) {
				if err == nil {
					t.Errorf("%+v (%q) parsed; Shift with a non-letter character must be refused", s, s.String())
				}
				continue
			}
			if code == ' ' && m&Shift != 0 && m&heavy == 0 {
				continue // Shift+Space is refused as the character Shift types; covered above
			}
			if err != nil || got != s {
				t.Errorf("Parse(%q) = %+v, %v; want %+v", s.String(), got, err, s)
			}
		}
	}
	if got := (Seq{'s', Ctrl | Alt | Super | Shift}).String(); got != "Ctrl+Alt+Super+Shift+S" {
		t.Errorf("canonical order: %q", got)
	}
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// matches is the lookup every binding uses: Of, then the first of Candidates that is s.
func matches(s Seq, code rune, mods Mods, text string) bool {
	ev, ok := Of(code, mods, text)
	return ok && slices.Contains(Candidates(ev), s)
}

// The rule tui/decl's Shortcut has always applied, carried over case for case: a letter's case is its
// Shift, whichever way the terminal delivers the capital; with Ctrl or Alt case is not relied on.
func TestALettersCaseIsItsShift(t *testing.T) {
	for _, c := range []struct {
		seq  string
		code rune
		mods Mods
		want bool
	}{
		{"C", 'c', 0, true},
		{"C", 'C', 0, false},
		{"c", 'C', 0, false},
		{"Shift+C", 'C', 0, true},
		{"Shift+C", 'c', Shift, true},
		{"Shift+C", 'c', 0, false},
		{"Ctrl+Q", 'q', Ctrl, true},
		{"Ctrl+Q", 'Q', Ctrl, true},
		{"Ctrl+Q", 'q', Ctrl | Shift, true},
		{"Alt+H", 'H', Alt, true},
		{"?", '?', 0, true},
	} {
		s, err := Parse(c.seq)
		if err != nil {
			t.Fatal(err)
		}
		if got := matches(s, c.code, c.mods, ""); got != c.want {
			t.Errorf("%q matches %q (mods %v): %v, want %v", c.seq, string(c.code), c.mods, got, c.want)
		}
	}
}

func TestOfNormalizesTheEvent(t *testing.T) {
	for _, c := range []struct {
		name string
		code rune
		mods Mods
		text string
		want Seq
		ok   bool
	}{
		{"kitty ? is its text", '/', Shift, "?", Seq{'?', 0}, true},
		{"legacy ?", '?', 0, "", Seq{'?', 0}, true},
		{"kitty capital", 'g', Shift, "G", Seq{'g', Shift}, true},
		{"legacy capital", 'G', 0, "", Seq{'g', Shift}, true},
		{"num lock is not a modifier", Down, NumLock, "", Seq{Down, 0}, true},
		{"caps lock is not a modifier", 'j', CapsLock, "j", Seq{'j', 0}, true},
		{"shift on a functional key stays", Down, Shift, "", Seq{Down, Shift}, true},
		{"ctrl letter", 's', Ctrl, "", Seq{'s', Ctrl}, true},
		{"ctrl ignores text", 's', Ctrl, "s", Seq{'s', Ctrl}, true},
		{"ctrl capital is not shift", 'S', Ctrl, "", Seq{'s', Ctrl}, true},
		{"super is a modifier", 'z', Super, "", Seq{'z', Super}, true},
		{"hyper matches nothing", 'j', Hyper, "", Seq{}, false},
		{"meta matches nothing", 'j', Meta, "", Seq{}, false},
		{"space text", ' ', 0, " ", Seq{' ', 0}, true},
		{"enter has no text key", Enter, 0, "\r", Seq{Enter, 0}, true},
	} {
		got, ok := Of(c.code, c.mods, c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: Of = %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestCandidatesTryTheExactChordFirst(t *testing.T) {
	ctrlShiftS, ctrlS := Seq{'s', Ctrl | Shift}, Seq{'s', Ctrl}
	if got := Candidates(ctrlShiftS); !slices.Equal(got, []Seq{ctrlShiftS, ctrlS}) {
		t.Errorf("Candidates(Ctrl+Shift+S) = %v", got)
	}
	for _, s := range []Seq{ctrlS, {Down, Ctrl | Shift}, {'?', 0}, {'g', Shift}, {'s', Super | Shift}} {
		if got := Candidates(s); !slices.Equal(got, []Seq{s}) {
			t.Errorf("Candidates(%v) = %v: only a Ctrl or Alt letter with Shift widens", s, got)
		}
	}
}

func TestAcceptsIsTheInverseOfCandidates(t *testing.T) {
	var all []Seq
	for _, code := range []rune{'s', '?', Down, Tab} {
		for m := Mods(0); m <= bindable; m++ {
			if m&^bindable == 0 {
				all = append(all, Seq{code, m})
			}
		}
	}
	for _, s := range all {
		for _, ev := range all {
			want := slices.Contains(Candidates(ev), s)
			got := slices.Contains(Accepts(s), ev)
			if got != want {
				t.Errorf("event %v reaches %v: Candidates says %v, Accepts says %v", ev, s, want, got)
			}
		}
	}
}

func TestOverlapsIsSymmetricAndCoversTheFallback(t *testing.T) {
	ctrlS, ctrlShiftS, altS := Seq{'s', Ctrl}, Seq{'s', Ctrl | Shift}, Seq{'s', Alt}
	for _, c := range []struct {
		a, b Seq
		want bool
	}{
		{ctrlS, ctrlS, true},
		{ctrlS, ctrlShiftS, true},
		{ctrlShiftS, ctrlS, true},
		{ctrlS, altS, false},
		{Seq{Down, Ctrl}, Seq{Down, Ctrl | Shift}, false},
	} {
		if got := Overlaps(c.a, c.b); got != c.want {
			t.Errorf("Overlaps(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
