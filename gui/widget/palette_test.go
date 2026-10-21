package widget

import (
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// A view draws a palette colour in the window's theme, as the cells beside it are drawn: a
// DataTable cursor styled ANSI red fills in the theme's red, not xterm's (gui-D1).
func TestAViewDrawsAPaletteColourInTheWindowsTheme(t *testing.T) {
	_, l := pixelTable(t, cities(3))
	l.t.SetStyles(tuiwidget.DataTableStyles{CursorBlurred: style.New().Background(style.ANSI(1))})
	th := gui.DefaultTheme()
	themeRed := color.NRGBA{R: 0xbf, G: 0x61, B: 0x6a, A: 0xff}
	th.ANSI[1] = themeRed

	c := gui.NewRecordingCanvas(gui.Size{W: 600, H: 120}, gui.Size{W: 8, H: 16})
	c.TextPx = 14
	c.Theme = &th
	c.Colors = func(int, int) (color.NRGBA, color.NRGBA) {
		return color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
	}
	l.Paint(c)
	var fills []color.NRGBA
	for _, f := range calls(c, "FillRect") {
		fills = append(fills, f.Brush.Color)
		if f.Brush.Color == themeRed {
			return
		}
	}
	t.Fatalf("no fill in the window theme's ANSI red %v; fills %v", themeRed, fills)
}
