package panels_test

import (
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/panels"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
	"github.com/yongjohnlee80/golib/tui/widget"
)

const layout = `import tui 1.0
import tui.theme.dark 1.0
import tui.panels 1.0
Window {
    Text { text: "the page" }
    Panel { id: files; heading: "Files"; edge: "left"; Text { text: "in the panel" } }
}`

func collector() tuidecl.ProgramOption {
	return tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows()))
}

// A layout that imports the module, a theme and a window collector builds a Panel.
func TestAPanelBuilds(t *testing.T) {
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(layout)), panels.Offer(), themes.Offer(), collector()); err != nil {
		t.Fatalf("a Panel does not build: %v", err)
	}
}

// Panel is minimizable, so a program without a window collector is told so, not left with a panel
// its dock can't hide.
func TestAPanelNeedsAWindowCollector(t *testing.T) {
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(layout)), panels.Offer(), themes.Offer()); err == nil {
		t.Fatal("a Panel built with no window collector")
	}
}

// Panel binds the toolkit's theme vocabulary, so a layout with no theme is refused rather than drawn
// with an unbound surface.
func TestAPanelNeedsATheme(t *testing.T) {
	src := `import tui 1.0
import tui.panels 1.0
Window { Panel { heading: "Files"; edge: "left"; Text { text: "x" } } }`
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(src)), panels.Offer(), collector()); err == nil {
		t.Fatal("a Panel built with no theme imported")
	}
}
