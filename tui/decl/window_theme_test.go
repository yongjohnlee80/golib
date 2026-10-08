package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// The root Window's palette is the App's token theme: a widget styled in tokens, not roles (a
// resizable Drawer's grip: TokenSurface under TokenBorder), wears the document's theme, the
// window role and the mid, not golib's default.
func TestTheWindowsPaletteIsTheAppsTokenTheme(t *testing.T) {
	s := decltest.Run(t, 60, 20,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    palette.highlight: "#8a5a2b"
    palette.window: "#dacca9"
    palette.mid: "#b5a380"
    palette.base: "#e6d9b9"
    palette.text: "#5b4636"
    Text { text: "the page underneath" }
    Drawer { id: d; resizable: true; edge: Tui.Bottom; size: 30; length: 100
        Frame { title: "panel"; Text { text: "inside" } } }
}`)))
	s.WaitForText(t, "the page underneath")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "panel")
	rgb := func(r, g, b uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: b} }
	s.WaitFor(t, "the grip in the palette's window and mid", func(string) bool {
		x, y, ok := find(s, "□")
		if !ok {
			return false
		}
		c := s.Backend.Snapshot()[y][x]
		return c.Attrs.BG == rgb(0xda, 0xcc, 0xa9) && c.Attrs.FG == rgb(0xb5, 0xa3, 0x80)
	})
}
