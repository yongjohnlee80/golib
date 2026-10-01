package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// TestAWindowsColorFillsAroundAPage: Qt's Window.color paints what no widget
// covers — the columns either side of a page capped at 24 in a 60-column
// Window — while the page keeps its own palette. Unset, those columns keep the
// terminal's own background; a binding changes the colour while it runs.
func TestAWindowsColorFillsAroundAPage(t *testing.T) {
	doc := func(color string) []byte {
		return []byte("import tui 1.0\nimport demo 1.0\nWindow {\n" + color +
			" Frame { title: \"page\"; maximumWidth: 24; palette.window: \"#1c1c1c\"; Text { text: \"words\" } } }")
	}
	rgb := func(r, g, b uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: b} }
	page := rgb(0x1c, 0x1c, 0x1c)
	bg := func(s *decltest.Screen, x, y int) tui.CellColor { return s.Backend.Snapshot()[y][x].Attrs.BG }

	t.Run("bound", func(t *testing.T) {
		s := decltest.Run(t, 60, 6,
			tuidecl.LayoutSource("main.qml", doc(" color: App.backdrop\n")),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.backdrop": "#121212"}))
		s.WaitForText(t, "words")
		dim := rgb(0x12, 0x12, 0x12)
		s.WaitFor(t, "the margins in the Window's color", func(string) bool {
			return bg(s, 0, 0) == dim && bg(s, 17, 3) == dim && bg(s, 42, 3) == dim && bg(s, 59, 5) == dim
		})
		if got := bg(s, 20, 3); got != page {
			t.Fatalf("inside the page the background is %+v, want the page's %+v:\n%s", got, page, s)
		}
		onScreenLoop(t, s, func() {
			if err := s.Program.Set("App.backdrop", "#000080"); err != nil {
				t.Error(err)
			}
		})
		navy := rgb(0, 0, 0x80)
		s.WaitFor(t, "the margins in the new color", func(string) bool { return bg(s, 0, 0) == navy && bg(s, 59, 5) == navy })
	})

	t.Run("unset", func(t *testing.T) {
		s := decltest.Run(t, 60, 6, tuidecl.LayoutSource("main.qml", doc("")),
			tuidecl.Singleton("demo", "1.0", "App"))
		s.WaitForText(t, "words")
		if got := bg(s, 0, 0); got != (tui.CellColor{}) {
			t.Fatalf("with no color the margin's background is %+v, want the terminal's own:\n%s", got, s)
		}
		if got := bg(s, 20, 3); got != page {
			t.Fatalf("inside the page the background is %+v, want %+v", got, page)
		}
	})
}
