package vt

import (
	"strings"
	"testing"
)

// allText is the scrollback then the screen, as rows of text.
func allText(s *Screen) []string {
	var out []string
	for i := 0; i < s.Scrollback(); i++ {
		out = append(out, rowText(s.Line(i)))
	}
	return append(out, screenRows(s)...)
}

func TestResizeReflowsWider(t *testing.T) {
	s := feed(New(4, 5), "abcdefghij\r\nxy")
	wantRows(t, s, "abcde", "fghij", "xy")
	s.Resize(4, 12)
	wantRows(t, s, "abcdefghij", "xy")
	wantCursor(t, s, 1, 2)
	if s.Wrapped(0) {
		t.Error("joined row still wrapped")
	}
}

func TestResizeReflowsNarrowerIntoScrollback(t *testing.T) {
	s := feed(New(3, 10), "abcdefghij\r\n0123456789\r\nok")
	s.Resize(3, 5)
	got := allText(s)
	want := []string{"abcde", "fghij", "01234", "56789", "ok"}
	if strings.Join(got[:5], "|") != strings.Join(want, "|") {
		t.Errorf("text %q, want %q", got, want)
	}
	wantRows(t, s, "01234", "56789", "ok")
	wantCursor(t, s, 2, 2)
	if !s.Wrapped(0) || s.Wrapped(1) {
		t.Errorf("wrapped flags %v %v", s.Wrapped(0), s.Wrapped(1))
	}
}

func TestResizeRoundTrip(t *testing.T) {
	in := "the quick brown fox\r\njumps over\r\nthe lazy dog"
	s := feed(New(5, 20), in)
	before := allText(s)
	s.Resize(5, 7)
	s.Resize(5, 20)
	if a, b := strings.Join(before, "|"), strings.Join(allText(s), "|"); strings.TrimRight(a, "|") != strings.TrimRight(b, "|") {
		t.Errorf("round trip\n got: %q\nwant: %q", allText(s), before)
	}
	wantCursor(t, s, 2, 12)
}

func TestResizeKeepsBlankRowsAboveTheCursor(t *testing.T) {
	s := feed(New(5, 10), "a\r\n\r\n\r\nb")
	s.Resize(5, 8)
	wantRows(t, s, "a", "", "", "b")
	wantCursor(t, s, 3, 1)
}

func TestResizeDropsBlankRowsBelowTheCursor(t *testing.T) {
	s := feed(New(6, 10), "abcdefghij")
	s.Resize(6, 3)
	if n := s.Scrollback(); n != 0 {
		t.Errorf("blank rows pushed %d lines into the scrollback", n)
	}
	wantRows(t, s, "abc", "def", "ghi", "j")
}

func TestResizeShorterKeepsTheCursorRow(t *testing.T) {
	s := feed(New(5, 10), "1\r\n2\r\n3\r\n4\r\n5")
	s.Resize(2, 10)
	wantRows(t, s, "4", "5")
	wantCursor(t, s, 1, 1)
	if s.Scrollback() != 3 {
		t.Errorf("scrollback %d, want 3", s.Scrollback())
	}
}

func TestResizeCursorAboveText(t *testing.T) {
	// A full-screen program on the primary screen: the cursor moved up, text
	// below it. The cursor's row must stay on the screen.
	s := feed(New(4, 10), "1\r\n2\r\n3\r\n4\x1b[1;1H")
	s.Resize(2, 10)
	r, _, _, _ := s.Cursor()
	if r < 0 || r > 1 {
		t.Fatalf("cursor row %d off a 2-row screen", r)
	}
	if got := screenRows(s)[r]; got != "1" {
		t.Errorf("cursor's row shows %q, want %q", got, "1")
	}
}

func TestResizeWideCharsRewrap(t *testing.T) {
	s := feed(New(3, 6), "a世界世")
	s.Resize(3, 4)
	wantRows(t, s, "a世", "界世")
	s.Resize(3, 3)
	wantRows(t, s, "a世", "界", "世")
}

func TestResizePendingWrapStaysPending(t *testing.T) {
	s := feed(New(3, 5), "abcde")
	s.Resize(3, 5) // no-op
	s.Resize(3, 10)
	wantCursor(t, s, 0, 5)
	feed(s, "X")
	wantRows(t, s, "abcdeX")

	s = feed(New(3, 5), "abcde")
	s.Resize(3, 6)
	s.Resize(3, 5)
	wantCursor(t, s, 0, 4)
	feed(s, "X")
	wantRows(t, s, "abcde", "X")
}

func TestResizeCursorPastTheText(t *testing.T) {
	s := feed(New(3, 10), "ab\x1b[8G")
	s.Resize(3, 5)
	// Offset 7 of the logical line: row 1, column 2 at width 5.
	wantCursor(t, s, 1, 2)
	feed(s, "X")
	wantRows(t, s, "ab", "  X")
}

func TestResizeAlternateScreenCuts(t *testing.T) {
	s := feed(New(4, 6), "primary text\x1b[?1049h\x1b[1;1Hab世cd\x1b[4;1Hlast")
	s.Resize(3, 3)
	wantRows(t, s, "", "", "las") // rows above the cursor left from the top
	wantCursor(t, s, 2, 2)
	feed(s, "\x1b[1;1H\x1b[?1049l")
	// Back on the reflowed primary, at the cursor 1049 saved.
	if got := strings.Join(allText(s), ""); !strings.HasPrefix(got, "primary text") && !strings.Contains(got, "pri") {
		t.Errorf("primary lost: %q", allText(s))
	}
	s = feed(New(2, 4), "\x1b[?1049hab世")
	s.Resize(2, 3)
	if c := s.Cell(0, 2); c.Width == 2 {
		t.Errorf("half a wide char kept at the cut: %+v", c)
	}
}

func TestResizeAlternateScreenGrows(t *testing.T) {
	s := feed(New(2, 3), "\x1b[?1049habc")
	s.Resize(4, 6)
	wantRows(t, s, "abc")
	if r, c := s.Size(); r != 4 || c != 6 {
		t.Errorf("size %d x %d", r, c)
	}
}

func TestResizeResetsTheRegionAndExtendsTabs(t *testing.T) {
	s := feed(New(4, 10), "\x1b[2;3r")
	s.Resize(5, 20)
	feed(s, "\x1b[5;1H\nX\x1b[1;1H\t\tY")
	wantRows(t, s, "                Y", "", "", "", "X")
}

func TestResizeTracksTheSavedCursor(t *testing.T) {
	s := feed(New(3, 10), "abcdefghij\x1b[1;8H\x1b7\x1b[3;1H")
	s.Resize(3, 5)
	feed(s, "\x1b8X")
	if got := allText(s); !strings.Contains(strings.Join(got, "|"), "fgX") {
		t.Errorf("DECRC after reflow wrote at the wrong place: %q", got)
	}
}

func TestResizeMinimum(t *testing.T) {
	s := feed(New(3, 3), "abc")
	s.Resize(0, -1)
	if r, c := s.Size(); r != 1 || c != 1 {
		t.Errorf("size %d x %d", r, c)
	}
}
