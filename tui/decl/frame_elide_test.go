package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// A Frame's titleElide is Qt's elide, from Tui's elide modes: Tui.ElideLeft keeps a long title's
// tail, its badge whole, and cuts the start; any other value is refused, naming the property.
func TestAFramesTitleElidesWhereItIsAsked(t *testing.T) {
	s := decltest.Run(t, 40, 4, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    Frame {
        title: "/tmp/a/long/project/path/notes.md  [no workspace]"
        titleElide: Tui.ElideLeft
        Text { text: "body" }
    }
}`)))
	s.WaitForText(t, "body")
	top := strings.Split(s.String(), "\n")[0]
	if !strings.Contains(top, "…") || !strings.Contains(top, "notes.md  [no workspace]") || strings.Contains(top, "/tmp/a") {
		t.Fatalf("the title row %q; want its start cut, its tail and badge kept", top)
	}
	if _, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow { Frame { titleElide: Tui.Horizontal; Text {} } }"))); err == nil || !strings.Contains(err.Error(), "titleElide") {
		t.Fatalf("titleElide: Tui.Horizontal: %v; want it refused, naming the property", err)
	}
}
