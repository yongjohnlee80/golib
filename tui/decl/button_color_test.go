package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// A Button's color, bound to a source, paints its label, and a source that moves repaints it: how a
// document colours one button (a dock's icon lit or dimmed). A Button takes no palette roles of its
// own; palette.buttonText is a Dialog's and a FileDialog's.
func TestAButtonsColorBindsAndRepaints(t *testing.T) {
	s := decltest.Run(t, 30, 6, tuidecl.LayoutSource("main.qml", []byte(windowDoc(` Flex { Button { flat: true; text: "Dock"; color: App.tint } }`))),
		tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Sources(map[string]any{"App.tint": "#d03030"}))
	s.WaitForText(t, "Dock")
	fg := func() (uint8, uint8, uint8) {
		snap := s.Backend.Snapshot()
		for y, row := range strings.Split(s.String(), "\n") {
			if x := strings.Index(row, "Dock"); x >= 0 && y < len(snap) && x < len(snap[y]) {
				a := snap[y][x].Attrs
				return a.FG.R, a.FG.G, a.FG.B
			}
		}
		return 0, 0, 0
	}
	if r, g, b := fg(); r != 0xd0 || g != 0x30 || b != 0x30 {
		t.Fatalf("the label is painted %d,%d,%d; want App.tint's 208,48,48", r, g, b)
	}
	var err error
	onScreenLoop(t, s, func() { err = s.Program.Set("App.tint", "#30d030") })
	if err != nil {
		t.Fatal(err)
	}
	s.WaitFor(t, "the label repainted in the new colour", func(string) bool { r, g, b := fg(); return r == 0x30 && g == 0xd0 && b == 0x30 })
}
