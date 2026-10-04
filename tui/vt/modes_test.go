package vt

import (
	"strings"
	"testing"
)

func TestModes(t *testing.T) {
	cases := []struct {
		in    string
		check func(Modes) bool
		name  string
	}{
		{"\x1b[?1h", func(m Modes) bool { return m.AppCursor }, "DECCKM on"},
		{"\x1b[?1h\x1b[?1l", func(m Modes) bool { return !m.AppCursor }, "DECCKM off"},
		{"\x1b=", func(m Modes) bool { return m.AppKeypad }, "DECKPAM"},
		{"\x1b=\x1b>", func(m Modes) bool { return !m.AppKeypad }, "DECKPNM"},
		{"\x1b[?66h", func(m Modes) bool { return m.AppKeypad }, "DECNKM"},
		{"\x1b[?2004h", func(m Modes) bool { return m.BracketedPaste }, "bracketed paste"},
		{"\x1b[?1004h", func(m Modes) bool { return m.FocusEvents }, "focus events"},
		{"\x1b[?25l", func(m Modes) bool { return !m.CursorVisible }, "DECTCEM off"},
		{"\x1b[?12h", func(m Modes) bool { return m.CursorBlink }, "blink"},
		{"\x1b[?2026h", func(m Modes) bool { return m.Synchronized }, "synchronized output"},
		{"\x1b[?9h", func(m Modes) bool { return m.Mouse == MouseX10 }, "X10 mouse"},
		{"\x1b[?1000h", func(m Modes) bool { return m.Mouse == MouseClick }, "click mouse"},
		{"\x1b[?1002h", func(m Modes) bool { return m.Mouse == MouseDrag }, "drag mouse"},
		{"\x1b[?1003h\x1b[?1006h", func(m Modes) bool { return m.Mouse == MouseMotion && m.MouseSGR }, "motion mouse, SGR"},
		{"\x1b[?1003h\x1b[?1000l", func(m Modes) bool { return m.Mouse == MouseOff }, "resetting any mouse mode ends reporting"},
		{"\x1b[?1000;1006h", func(m Modes) bool { return m.Mouse == MouseClick && m.MouseSGR }, "several modes in one sequence"},
		{"\x1b[4h\x1b[20h", func(m Modes) bool { return m.Insert && m.NewLine }, "IRM and LNM"},
		{"\x1b[?6h", func(m Modes) bool { return m.Origin }, "DECOM"},
		{"\x1b[?7l", func(m Modes) bool { return !m.Autowrap }, "DECAWM off"},
		{"", func(m Modes) bool { return m.Autowrap && m.CursorVisible && !m.AltScreen }, "power-on"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if m := feed(New(3, 10), c.in).Modes(); !c.check(m) {
				t.Errorf("modes = %+v", m)
			}
		})
	}
}

func TestCursorShapes(t *testing.T) {
	for _, c := range []struct {
		in    string
		shape CursorShape
		blink bool
	}{
		{"\x1b[1 q", CursorBlock, true},
		{"\x1b[2 q", CursorBlock, false},
		{"\x1b[3 q", CursorUnderline, true},
		{"\x1b[4 q", CursorUnderline, false},
		{"\x1b[5 q", CursorBar, true},
		{"\x1b[6 q", CursorBar, false},
		{"\x1b[6 q\x1b[0 q", CursorDefault, false},
		{"\x1b[6 q\x1b[9 q", CursorBar, false},
	} {
		s := feed(New(3, 10), c.in)
		if _, _, _, shape := s.Cursor(); shape != c.shape || s.Modes().CursorBlink != c.blink {
			t.Errorf("%q: shape %d blink %v, want %d %v", c.in, shape, s.Modes().CursorBlink, c.shape, c.blink)
		}
	}
}

// altCase drives one alternate-screen mode in and out and checks the
// primary's text survives, where the cursor lands, and what the alternate
// screen holds on re-entry.
func TestAlternateScreens(t *testing.T) {
	cases := []struct {
		mode           string
		restoresCursor bool // 1049 saves and restores the cursor
		clearsOnEnter  bool // 1049 clears on the way in
		clearsOnLeave  bool // 1047 clears on the way out
	}{
		{"47", false, false, false},
		{"1047", false, false, true},
		{"1049", true, true, false},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			on, off := "\x1b[?"+c.mode+"h", "\x1b[?"+c.mode+"l"
			s := feed(New(3, 10), "primary\x1b[2;3H")
			feed(s, on)
			if !s.Modes().AltScreen {
				t.Fatal("not on the alternate screen")
			}
			if strings.Join(screenRows(s), "") != "" {
				t.Fatalf("alternate screen shows %q", screenRows(s))
			}
			feed(s, "\x1b[3;5Halt")
			wantRows(t, s, "", "", "    alt")
			feed(s, off)
			if s.Modes().AltScreen {
				t.Fatal("still on the alternate screen")
			}
			wantRows(t, s, "primary")
			if c.restoresCursor {
				wantCursor(t, s, 1, 2)
			} else {
				wantCursor(t, s, 2, 7) // the cursor stays where the alternate screen left it
			}
			// Back in without 1049's clear: what the alternate screen kept.
			feed(s, "\x1b[?47h")
			kept := strings.TrimSpace(strings.Join(screenRows(s), ""))
			if want := map[bool]string{true: "", false: "alt"}[c.clearsOnLeave]; kept != want {
				t.Errorf("alternate screen kept %q, want %q", kept, want)
			}
			feed(s, "\x1b[?47l")
			if c.clearsOnEnter {
				feed(s, on)
				wantRows(t, s)
			}
		})
	}
}

func TestAlternateScreenSplitAcrossWrites(t *testing.T) {
	in := "primary\x1b[?1049h\x1b[Halt\x1b[?1049l"
	for cut := 1; cut < len(in); cut++ {
		s := New(3, 10)
		s.Write([]byte(in[:cut]))
		s.Write([]byte(in[cut:]))
		if s.Modes().AltScreen {
			t.Fatalf("cut %d: left on the alternate screen", cut)
		}
		if got := screenRows(s)[0]; got != "primary" {
			t.Fatalf("cut %d: primary row %q", cut, got)
		}
		wantCursor(t, s, 0, 7)
	}
}

func TestAlternateScreenKeepsNoScrollback(t *testing.T) {
	s := feed(New(2, 5), "\x1b[?1049h1\r\n2\r\n3\r\n4")
	if n := s.Scrollback(); n != 0 {
		t.Errorf("alternate screen scrolled %d lines into the scrollback", n)
	}
}

func TestRepeated1049IsIdempotent(t *testing.T) {
	s := feed(New(3, 10), "p\x1b[2;2H\x1b[?1049h\x1b[3;3H\x1b[?1049hx\x1b[?1049l\x1b[?1049l")
	wantRows(t, s, "p")
	wantCursor(t, s, 1, 1)
}

func TestReplies(t *testing.T) {
	cases := []struct {
		in, want, name string
	}{
		{"\x1b[c", "\x1b[?62;22c", "DA1"},
		{"\x1b[0c", "\x1b[?62;22c", "DA1 with 0"},
		{"\x1b[>c", "\x1b[>1;10;0c", "DA2"},
		{"\x1b[5n", "\x1b[0n", "DSR"},
		{"\x1b[3;7H\x1b[6n", "\x1b[3;7R", "CPR"},
		{"\x1b[3;7H\x1b[?6n", "\x1b[?3;7R", "DECXCPR"},
		{"\x1b[2;4r\x1b[?6h\x1b[2;3H\x1b[6n", "\x1b[2;3R", "CPR under DECOM is region-relative"},
		{"\x1b[?2004h\x1b[?2004$p", "\x1b[?2004;1$y", "DECRQM set"},
		{"\x1b[?2004$p", "\x1b[?2004;2$y", "DECRQM reset"},
		{"\x1b[?1049h\x1b[?1049$p", "\x1b[?1049;1$y", "DECRQM alternate screen"},
		{"\x1b[?1002h\x1b[?1002$p\x1b[?1000$p", "\x1b[?1002;1$y\x1b[?1000;2$y", "DECRQM mouse"},
		{"\x1b[?9999$p", "\x1b[?9999;0$y", "DECRQM unknown"},
		{"\x1b[4h\x1b[4$p", "\x1b[4;1$y", "DECRQM ANSI IRM"},
		{"\x1b[20$p", "\x1b[20;2$y", "DECRQM ANSI LNM"},
		{"\x1b[3$p", "\x1b[3;0$y", "DECRQM ANSI unknown"},
		{"\x1b[?5n\x1b[>4;1m\x1b[?u\x1b[1c", "", "unanswered queries"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []byte
			s := New(5, 10, WithReply(func(b []byte) { got = append(got, b...) }))
			feed(s, c.in)
			if string(got) != c.want {
				t.Errorf("reply %q, want %q", got, c.want)
			}
		})
	}
	// Without WithReply, queries are dropped.
	feed(New(2, 2), "\x1b[c\x1b[6n")
}

func TestReplyModesTracked(t *testing.T) {
	var got []byte
	s := New(3, 10, WithReply(func(b []byte) { got = append(got, b...) }))
	for _, m := range []string{"1", "6", "7", "9", "12", "25", "47", "66", "1003", "1004", "1006", "2026"} {
		got = got[:0]
		feed(s, "\x1b[?"+m+"h\x1b[?"+m+"$p")
		if want := "\x1b[?" + m + ";1$y"; string(got) != want {
			t.Errorf("mode %s: %q, want %q", m, got, want)
		}
		feed(s, "\x1b[?"+m+"l")
	}
}

func TestTitle(t *testing.T) {
	s := feed(New(2, 10), "\x1b]0;first\x07")
	if s.Title() != "first" {
		t.Errorf("title %q", s.Title())
	}
	feed(s, "\x1b]2;second\x1b\\")
	if s.Title() != "second" {
		t.Errorf("title %q", s.Title())
	}
	feed(s, "\x1b]52;c;aGk=\x07\x1b]1;icon\x07\x1b]7;file:///\x07\x1b]nonsense\x07")
	if s.Title() != "second" {
		t.Errorf("OSC 52/1/7 changed the title to %q", s.Title())
	}
	feed(s, "\x1b]2;a\x01b\xffc\u0085d\x07")
	if s.Title() != "abcd" {
		t.Errorf("title %q, want the printable runes", s.Title())
	}
	wantRows(t, s) // none of it printed
}

func TestScrollbackCapAndOrder(t *testing.T) {
	s := New(2, 5, WithScrollback(3))
	for i := 0; i < 10; i++ {
		feed(s, string(rune('0'+i))+"\r\n")
	}
	if s.Scrollback() != 3 {
		t.Fatalf("scrollback %d", s.Scrollback())
	}
	for i, want := range []string{"6", "7", "8"} {
		if got := rowText(s.Line(i)); got != want {
			t.Errorf("Line(%d) = %q, want %q", i, got, want)
		}
	}
	if s.Line(-1) != nil || s.Line(3) != nil {
		t.Error("Line out of range is not nil")
	}
	feed(s, "\x1b[3J")
	if s.Scrollback() != 0 {
		t.Errorf("ED 3 left %d lines", s.Scrollback())
	}
	if n := feed(New(2, 5, WithScrollback(0)), "1\r\n2\r\n3").Scrollback(); n != 0 {
		t.Errorf("WithScrollback(0) kept %d", n)
	}
	if n := feed(New(2, 5, WithScrollback(-4)), "1\r\n2\r\n3").Scrollback(); n != 0 {
		t.Errorf("WithScrollback(-4) kept %d", n)
	}
}

func TestRegionBelowTheTopKeepsNoScrollback(t *testing.T) {
	s := feed(New(4, 5), "\x1b[2;4r\x1b[4;1H\n\n\n")
	if s.Scrollback() != 0 {
		t.Errorf("a region below the top fed %d lines to the scrollback", s.Scrollback())
	}
	feed(s, "\x1b[1;3r\x1b[3;1Hx\n")
	if s.Scrollback() != 1 {
		t.Errorf("a region at the top fed %d lines, want 1", s.Scrollback())
	}
}
