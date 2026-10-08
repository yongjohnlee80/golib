package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/vt"
)

// With a minimum contrast, a program's colour that washes out on the page is raised until it
// reads: ls's yellow on a light sepia page darkens to 4.5:1, a blue on a dark page lightens. A
// colour that already reads stays as written, ANSI and all; off, nothing changes.
func TestTerminalRaisesWashedOutColours(t *testing.T) {
	sepia := style.RGB(0xe6, 0xd9, 0xb9)
	term := NewTerminal()
	term.SetDefaultLook(style.New().Background(sepia).Foreground(style.RGB(0x5b, 0x46, 0x36)))
	ratioOf := func(fg, bg style.Color) float64 { r, _ := style.ContrastRatio(fg, bg); return r }

	yellow := vt.Cell{Content: "x", FG: style.ANSI(3)}
	if fg, _ := term.cellStyle(yellow).GetForeground(); fg != style.ANSI(3) {
		t.Errorf("off, yellow became %v", fg)
	}
	term.SetMinimumContrast(4.5)
	fg, _ := term.cellStyle(yellow).GetForeground()
	if r := ratioOf(fg, sepia); r < 4.5 {
		t.Errorf("yellow on sepia raised to %v: ratio %.2f, want 4.5", fg, r)
	}
	if r, g, b, _ := fg.RGBValues(); !(r > b && g > b) {
		t.Errorf("yellow's hue lost: %v", fg)
	}

	ink := vt.Cell{Content: "x", FG: style.ANSI(0)} // black reads on sepia already
	if fg, _ := term.cellStyle(ink).GetForeground(); fg != style.ANSI(0) {
		t.Errorf("a readable black became %v, want it as written", fg)
	}

	dark := style.RGB(0x1c, 0x1c, 0x1c)
	blue := vt.Cell{Content: "x", FG: style.ANSI(4), BG: dark} // the cell's own background counts
	fg, _ = term.cellStyle(blue).GetForeground()
	if r := ratioOf(fg, dark); r < 4.5 {
		t.Errorf("blue on a dark cell raised to %v: ratio %.2f, want 4.5", fg, r)
	}
}
