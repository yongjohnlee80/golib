package decl_test

import (
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// Qt's Window.minimumWidth / minimumHeight, in cells: on a smaller screen the program shows the
// too-small screen, and its layout again once the screen is large enough.
func TestWindowMinimumSize(t *testing.T) {
	doc := "import tui 1.0\nWindow {\n minimumWidth: 40\n minimumHeight: 10\n Text { text: \"laid out\" }\n}"
	s := decltest.Run(t, 30, 8, tuidecl.LayoutSource("main.qml", []byte(doc)))
	s.WaitForText(t, "Screen too small")
	s.WaitForText(t, "30 × 8 — needs 40 × 10")
	s.Backend.InjectResize(45, 12)
	s.WaitForText(t, "laid out")
}
