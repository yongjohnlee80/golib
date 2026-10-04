package vt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// rowText renders cells as text: a blank is a space, a wide cluster's
// continuation adds nothing, and trailing spaces are trimmed.
func rowText(cells []Cell) string {
	var b strings.Builder
	for _, c := range cells {
		switch {
		case c.Width == 0:
		case c.Content == "":
			b.WriteByte(' ')
		default:
			b.WriteString(c.Content)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// screenRows is every visible row's text.
func screenRows(s *Screen) []string {
	rows, cols := s.Size()
	out := make([]string, rows)
	for r := range out {
		cells := make([]Cell, cols)
		for c := range cells {
			cells[c] = s.Cell(r, c)
		}
		out[r] = rowText(cells)
	}
	return out
}

func feed(s *Screen, in string) *Screen {
	s.Write([]byte(in))
	return s
}

func wantRows(t *testing.T, s *Screen, want ...string) {
	t.Helper()
	got := screenRows(s)
	for len(want) < len(got) {
		want = append(want, "")
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("screen\n got: %q\nwant: %q", got, want)
	}
}

func wantCursor(t *testing.T, s *Screen, row, col int) {
	t.Helper()
	if r, c, _, _ := s.Cursor(); r != row || c != col {
		t.Errorf("cursor = (%d, %d), want (%d, %d)", r, c, row, col)
	}
}

// grid is one D3 cell: input on a fresh rows x cols screen, the rows it
// leaves and where the cursor ends.
type grid struct {
	name       string
	rows, cols int
	in         string
	want       []string
	row, col   int
}

var grids = []grid{
	// printing
	{"print", 3, 10, "hi", []string{"hi"}, 0, 2},
	{"autowrap", 3, 5, "abcdefg", []string{"abcde", "fg"}, 1, 2},
	{"pending wrap holds the cursor", 3, 5, "abcde", []string{"abcde"}, 0, 4},
	{"pending wrap cancelled by CR", 3, 5, "abcde\rX", []string{"Xbcde"}, 0, 1},
	{"pending wrap then LF keeps the column", 3, 5, "abcde\nX", []string{"abcde", "    X"}, 1, 4},
	{"no autowrap overwrites the last column", 3, 5, "\x1b[?7labcdefg", []string{"abcdg"}, 0, 4},
	{"wide chars", 3, 6, "\u4e16\u754c", []string{"\u4e16\u754c"}, 0, 4},
	{"wide char at the margin wraps", 3, 3, "ab\u4e16", []string{"ab", "\u4e16"}, 1, 2},
	{"overwriting a wide char's first half", 3, 6, "\u4e16\x1b[1GX", []string{"X"}, 0, 1},
	{"overwriting a wide char's second half", 3, 6, "\u4e16\x1b[2GX", []string{" X"}, 0, 2},
	{"combining mark joins", 3, 6, "e\u0301x", []string{"e\u0301x"}, 0, 2},
	{"zwj sequence is one cell", 3, 6, "\U0001F469\u200d\U0001F4BBx", []string{"\U0001F469\u200d\U0001F4BBx"}, 0, 3},
	{"vs16 widens", 3, 6, "\u2602\ufe0fx", []string{"\u2602\ufe0fx"}, 0, 3},
	{"flag pair is one cell", 3, 6, "\U0001F1F0\U0001F1F7x", []string{"\U0001F1F0\U0001F1F7x"}, 0, 3},
	{"combining after a move does not join", 3, 6, "e\x1b[C\u0301", []string{"e"}, 0, 2},
	{"dec line drawing", 3, 10, "\x1b(0lqk\x1b(Bq", []string{"\u250c\u2500\u2510q"}, 0, 4},
	{"g1 with SO and SI", 3, 10, "\x1b)0\x0eq\x0fq", []string{"\u2500q"}, 0, 2},
	{"uk charset", 3, 10, "\x1b(A#\x1b(B#", []string{"\u00a3#"}, 0, 2},
	{"insert mode", 3, 10, "abc\x1b[1G\x1b[4hX", []string{"Xabc"}, 0, 1},
	{"insert mode off", 3, 10, "abc\x1b[1G\x1b[4h\x1b[4lX", []string{"Xbc"}, 0, 1},
	{"backspace", 3, 10, "ab\bX", []string{"aX"}, 0, 2},
	{"backspace at column 0", 3, 10, "\bX", []string{"X"}, 0, 1},

	// cursor
	{"CUP", 4, 10, "\x1b[2;3HX", []string{"", "  X"}, 1, 3},
	{"CUP defaults home", 4, 10, "abc\x1b[HX", []string{"Xbc"}, 0, 1},
	{"CUP clamps", 4, 10, "\x1b[99;99HX", []string{"", "", "", "         X"}, 3, 9},
	{"HVP", 4, 10, "\x1b[3;2fX", []string{"", "", " X"}, 2, 2},
	{"CUU CUD CUF CUB", 5, 10, "\x1b[3;3H\x1b[AX\x1b[2BY\x1b[2CZ\x1b[4DW", []string{"", "  X", "", "   W  Z"}, 3, 4},
	{"CUU stops at row 0", 3, 10, "\x1b[2;1H\x1b[9AX", []string{"X"}, 0, 1},
	{"CUF stops at the margin", 3, 5, "\x1b[9CX", []string{"    X"}, 0, 4},
	{"CNL CPL", 4, 10, "ab\x1b[2EX\x1b[FY", []string{"ab", "Y", "X"}, 1, 1},
	{"CHA HPA", 3, 10, "\x1b[5GX\x1b[2`Y", []string{" Y  X"}, 0, 2},
	{"VPA keeps the column", 4, 10, "ab\x1b[3dX", []string{"ab", "", "  X"}, 2, 3},
	{"HPR VPR", 4, 10, "\x1b[2a\x1b[2eX", []string{"", "", "  X"}, 2, 3},
	{"DECSC DECRC", 3, 10, "\x1b[2;3H\x1b7\x1b[HX\x1b8Y", []string{"X", "  Y"}, 1, 3},
	{"SCOSC SCORC", 3, 10, "\x1b[2;3H\x1b[s\x1b[HX\x1b[uY", []string{"X", "  Y"}, 1, 3},
	{"DECRC without a save goes home", 3, 10, "ab\x1b8X", []string{"Xb"}, 0, 1},
	{"tab", 3, 20, "\tX", []string{"        X"}, 0, 9},
	{"tab stops at the margin", 3, 10, "\t\t\tX", []string{"         X"}, 0, 9},
	{"HTS and TBC", 3, 20, "\x1b[3G\x1bH\x1b[1G\tX\x1b[3G\x1b[g\x1b[1G\tY", []string{"  X     Y"}, 0, 9},
	{"TBC 3 clears all", 3, 20, "\x1b[3g\tX", []string{"                   X"}, 0, 19},
	{"CHT CBT", 3, 30, "\x1b[2IX\x1b[2ZY", []string{"        Y       X"}, 0, 9},
	{"CBT stops at 0", 3, 30, "ab\x1b[5ZX", []string{"Xb"}, 0, 1},

	// editing
	{"ED 0", 3, 5, "aaaaa\r\nbbbbb\r\nccccc\x1b[2;3H\x1b[J", []string{"aaaaa", "bb"}, 1, 2},
	{"ED 1", 3, 5, "aaaaa\r\nbbbbb\r\nccccc\x1b[2;3H\x1b[1J", []string{"", "   bb", "ccccc"}, 1, 2},
	{"ED 2", 3, 5, "aaaaa\r\nbbbbb\x1b[2J", []string{}, 1, 4},
	{"EL 0", 3, 5, "abcde\x1b[3G\x1b[K", []string{"ab"}, 0, 2},
	{"EL 1", 3, 5, "abcde\x1b[3G\x1b[1K", []string{"   de"}, 0, 2},
	{"EL 2", 3, 5, "abcde\x1b[3G\x1b[2K", []string{}, 0, 2},
	{"ECH", 3, 6, "abcdef\x1b[2G\x1b[2X", []string{"a  def"}, 0, 1},
	{"ECH past the margin", 3, 6, "abcdef\x1b[5G\x1b[9X", []string{"abcd"}, 0, 4},
	{"ICH", 3, 6, "abcdef\x1b[2G\x1b[2@", []string{"a  bcd"}, 0, 1},
	{"DCH", 3, 6, "abcdef\x1b[2G\x1b[2P", []string{"adef"}, 0, 1},
	{"DCH splits a wide char", 3, 6, "a\u4e16b\x1b[3G\x1b[P", []string{"a b"}, 0, 2},
	{"IL", 4, 5, "a\r\nb\r\nc\r\nd\x1b[2;3H\x1b[L", []string{"a", "", "b", "c"}, 1, 0},
	{"DL", 4, 5, "a\r\nb\r\nc\r\nd\x1b[2;3H\x1b[2M", []string{"a", "d"}, 1, 0},
	{"ICH pushes a wide char off the edge", 3, 4, "ab\u4e16\x1b[2G\x1b[@", []string{"a b"}, 0, 1},
	{"ICH inside a wide char", 3, 4, "\u4e16b\x1b[2G\x1b[@", []string{"   b"}, 0, 1},
	{"DECSED", 3, 5, "ab\r\ncd\x1b[?2J", []string{}, 1, 2},
	{"DECSEL", 3, 5, "abcd\x1b[2G\x1b[?2K", []string{}, 0, 1},
	{"RI above the region", 5, 5, "\x1b[2;4r\x1b[1;1H\x1bMX", []string{"X"}, 0, 1},
	{"REP", 3, 10, "ab\x1b[3b", []string{"abbbb"}, 0, 5},
	{"REP a wide char", 3, 10, "\u4e16\x1b[2b", []string{"\u4e16\u4e16\u4e16"}, 0, 6},
	{"REP after a control repeats nothing", 3, 10, "a\r\x1b[3b", []string{"a"}, 0, 0},

	// scrolling
	{"LF scrolls at the bottom", 3, 5, "1\r\n2\r\n3\r\n4", []string{"2", "3", "4"}, 2, 1},
	{"region scrolls alone", 5, 5, "1\r\n2\r\n3\r\n4\r\n5\x1b[2;4r\x1b[4;1H\nX", []string{"1", "3", "4", "X", "5"}, 3, 1},
	{"RI at the top margin", 5, 5, "1\r\n2\r\n3\r\n4\r\n5\x1b[2;4r\x1b[2;1H\x1bMX", []string{"1", "X", "2", "3", "5"}, 1, 1},
	{"RI at row 0 scrolls down", 3, 5, "1\r\n2\x1b[H\x1bMX", []string{"X", "1", "2"}, 0, 1},
	{"IND NEL", 3, 5, "ab\x1bDX\x1bEY", []string{"ab", "  X", "Y"}, 2, 1},
	{"SU SD", 4, 5, "1\r\n2\r\n3\r\n4\x1b[2S", []string{"3", "4"}, 3, 1},
	{"SD", 4, 5, "1\r\n2\r\n3\r\n4\x1b[T", []string{"", "1", "2", "3"}, 3, 1},
	{"DECSTBM homes the cursor", 5, 5, "ab\x1b[2;4rX", []string{"Xb"}, 0, 1},
	{"DECSTBM rejects a one-row region", 5, 5, "\x1b[3;3r\x1b[5;1H\nX", []string{"", "", "", "", "X"}, 4, 1},
	{"IL outside the region does nothing", 5, 5, "a\x1b[2;3r\x1b[5;1H\x1b[L", []string{"a"}, 4, 0},
	{"origin mode", 6, 5, "\x1b[2;4r\x1b[?6h\x1b[1;1HX\x1b[9;1HY", []string{"", "X", "", "Y"}, 3, 1},
	{"DECALN", 2, 3, "\x1b#8", []string{"EEE", "EEE"}, 0, 0},
	{"LNM", 3, 5, "\x1b[20hab\ncd", []string{"ab", "cd"}, 1, 2},
	{"RIS", 3, 5, "abc\x1b[?7l\x1bc", []string{}, 0, 0},
}

func TestGrids(t *testing.T) {
	for _, g := range grids {
		t.Run(g.name, func(t *testing.T) {
			s := feed(New(g.rows, g.cols), g.in)
			wantRows(t, s, g.want...)
			wantCursor(t, s, g.row, g.col)
		})
	}
}

// TestGridsSplit replays every grid byte by byte: the output must match the
// contiguous write.
func TestGridsSplit(t *testing.T) {
	for _, g := range grids {
		t.Run(g.name, func(t *testing.T) {
			whole := feed(New(g.rows, g.cols), g.in)
			s := New(g.rows, g.cols)
			for i := 0; i < len(g.in); i++ {
				s.Write([]byte{g.in[i]})
			}
			if a, b := screenRows(whole), screenRows(s); strings.Join(a, "|") != strings.Join(b, "|") {
				t.Errorf("split %q != whole %q", b, a)
			}
		})
	}
}

func TestWrappedRows(t *testing.T) {
	s := feed(New(3, 5), "abcdefg\r\nxy")
	if !s.Wrapped(0) || s.Wrapped(1) || s.Wrapped(2) || s.Wrapped(-1) || s.Wrapped(3) {
		t.Errorf("wrapped = %v %v %v", s.Wrapped(0), s.Wrapped(1), s.Wrapped(2))
	}
	feed(s, "\x1b[1;1H\x1b[K")
	if s.Wrapped(0) {
		t.Error("EL 0 left the row wrapped")
	}
}

func TestCellOutsideTheGridIsBlank(t *testing.T) {
	s := New(2, 2)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {2, 0}, {0, 2}} {
		if c := s.Cell(p[0], p[1]); !c.isBlank() {
			t.Errorf("Cell%v = %+v", p, c)
		}
	}
	if s := New(0, 0); fmt.Sprint(s.Size()) != "1 1" {
		t.Errorf("New(0, 0) size = %v", fmt.Sprint(s.Size()))
	}
}

func TestSGR(t *testing.T) {
	cases := []struct {
		in       string
		attr     Attr
		fg, bg   style.Color
		describe string
	}{
		{"\x1b[1mX", AttrBold, style.Color{}, style.Color{}, "bold"},
		{"\x1b[1;2;3;4;5;7;8;9mX", AttrBold | AttrFaint | AttrItalic | AttrUnderline | AttrBlink | AttrReverse | AttrInvisible | AttrStrike, style.Color{}, style.Color{}, "all attributes"},
		{"\x1b[1;2;3;4;5;7;8;9;22;23;24;25;27;28;29mX", 0, style.Color{}, style.Color{}, "all attributes off"},
		{"\x1b[6mX", AttrBlink, style.Color{}, style.Color{}, "rapid blink"},
		{"\x1b[21mX", AttrUnderline, style.Color{}, style.Color{}, "double underline"},
		{"\x1b[4:3mX", AttrUnderline, style.Color{}, style.Color{}, "curly underline"},
		{"\x1b[4m\x1b[4:0mX", 0, style.Color{}, style.Color{}, "4:0 ends underline"},
		{"\x1b[31;42mX", 0, style.ANSI(1), style.ANSI(2), "ansi"},
		{"\x1b[91;107mX", 0, style.ANSI(9), style.ANSI(15), "bright"},
		{"\x1b[38;5;100;48;5;7mX", 0, style.ANSI256(100), style.ANSI256(7), "256"},
		{"\x1b[38;2;1;2;3mX", 0, style.RGB(1, 2, 3), style.Color{}, "truecolor"},
		{"\x1b[38:2::1:2:3mX", 0, style.RGB(1, 2, 3), style.Color{}, "truecolor colon with colour space"},
		{"\x1b[38:2:1:2:3mX", 0, style.RGB(1, 2, 3), style.Color{}, "truecolor colon"},
		{"\x1b[48:5:9mX", 0, style.Color{}, style.ANSI256(9), "256 colon"},
		{"\x1b[38;2;300;0;3mX", 0, style.RGB(255, 0, 3), style.Color{}, "clamped"},
		{"\x1b[38;5;300mX", 0, style.Color{}, style.Color{}, "256 out of range"},
		{"\x1b[31;38;5mX", 0, style.ANSI(1), style.Color{}, "truncated 256 keeps the colour"},
		{"\x1b[38;9;1mX", AttrBold, style.Color{}, style.Color{}, "unknown extended kind skips one"},
		{"\x1b[38:9mX", 0, style.Color{}, style.Color{}, "unknown colon kind"},
		{"\x1b[58;5;3;1mX", AttrBold, style.Color{}, style.Color{}, "underline colour is consumed"},
		{"\x1b[31;41m\x1b[39;49mX", 0, style.Color{}, style.Color{}, "defaults"},
		{"\x1b[1;31m\x1b[mX", 0, style.Color{}, style.Color{}, "bare reset"},
		{"\x1b[1;31m\x1b[0mX", 0, style.Color{}, style.Color{}, "reset"},
	}
	for _, c := range cases {
		t.Run(c.describe, func(t *testing.T) {
			got := feed(New(2, 10), c.in).Cell(0, 0)
			if got.Attr != c.attr || got.FG != c.fg || got.BG != c.bg {
				t.Errorf("cell = attr %b fg %+v bg %+v; want attr %b fg %+v bg %+v", got.Attr, got.FG, got.BG, c.attr, c.fg, c.bg)
			}
		})
	}
}

func TestEraseWearsThePenBackground(t *testing.T) {
	s := feed(New(2, 4), "abcd\x1b[44m\x1b[1G\x1b[K")
	for c := 0; c < 4; c++ {
		if got := s.Cell(0, c); got.BG != style.ANSI(4) || got.Content != "" {
			t.Errorf("cell %d = %+v", c, got)
		}
	}
}

func TestDECSCSavesPenAndCharset(t *testing.T) {
	s := feed(New(2, 10), "\x1b[31m\x1b(0\x1b7\x1b[0m\x1b(B\x1b8q")
	if c := s.Cell(0, 0); c.Content != "\u2500" || c.FG != style.ANSI(1) {
		t.Errorf("after DECRC: %+v", c)
	}
}

func TestDECSTRSoftReset(t *testing.T) {
	s := feed(New(4, 10), "\x1b[2;3r\x1b[?6h\x1b[4h\x1b[?7l\x1b[?1h\x1b=\x1b[?25l\x1b[31m\x1b[!p")
	m := s.Modes()
	if m.Insert || m.Origin || !m.Autowrap || m.AppCursor || m.AppKeypad || !m.CursorVisible {
		t.Errorf("modes after DECSTR: %+v", m)
	}
	feed(s, "\x1b[4;1H\nX")
	wantRows(t, s, "", "", "", "X") // the region is the whole screen again
	if c := s.Cell(3, 0); !c.FG.IsDefault() {
		t.Errorf("pen survived DECSTR: %+v", c)
	}
}
