package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// tiles_tier_test.go: a governed member stacks beneath the floating windows.

// A Drawer opened over the tiled page stays over it when the page takes the keyboard: the page is
// raised only among the tiled windows. Typing reaches the page, and the drawer is still drawn.
func TestADrawerOverATiledPageStaysOverItWhenThePageTakesTheKeyboard(t *testing.T) {
	s := decltest.Run(t, 60, 10, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
 TileManager {
  FloatWindow { id: pw; TileManager.edge: Tui.Center; Frame { title: "PAGE"; Editor { id: page } } }
 }
 Drawer { id: d; modal: false; edge: Tui.Right; size: 50; closeOnQ: false; Frame { title: "DRAWER"; Text { text: "d" } } }
}`)))
	s.WaitForText(t, "PAGE")
	onScreenLoop(t, s, func() { _ = s.Program.Call("d", "open") })
	s.WaitForText(t, "DRAWER")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("page", "forceActiveFocus"); err != nil {
			t.Error(err)
		}
	})
	s.Keys(t, decltest.Rune('i'))
	s.Keys(t, decltest.Type("typed")...)
	s.WaitFor(t, "typing reached the page, the drawer over it", func(sc string) bool {
		return strings.Contains(sc, "typed") && strings.Contains(sc, "DRAWER")
	})
}
