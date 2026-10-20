package panels_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"

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

// A Panel's heading takes a catalog message as well as a string: the message reaches the Frame's
// title, so a host's translated panels share the one Panel. A declared type judges literals only, so
// this pins the substitution, not the var.
func TestAPanelsHeadingTakesAMessage(t *testing.T) {
	src := strings.Replace(layout, `heading: "Files"`, `heading: qsTrId("demo.panel.files")`, 1)
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(src)), panels.Offer(), themes.Offer(), collector()); err != nil {
		t.Fatalf("a Panel with a qsTrId heading does not build: %v", err)
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

// Panel is non-modal: a panel never traps focus or the pointer, which a Dialog
// does. Asserted on the shipped file itself, not inferred from minimizable's build rule.
func TestPanelIsNonModal(t *testing.T) {
	src, err := fs.ReadFile(panels.FS(), "Panel.qml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := qml.QML{File: "Panel.qml"}.Parse(src)
	if err != nil || tree.Root == nil || tree.Root.Type != "Drawer" {
		t.Fatalf("Panel.qml: %v, root %+v", err, tree.Root)
	}
	for _, p := range tree.Root.Props {
		if p.Name == "modal" {
			if p.Value.Raw != "false" {
				t.Fatalf("Panel's modal is %q; want false", p.Value.Raw)
			}
			return
		}
	}
	t.Fatal("Panel sets no modal: a Drawer's default is modal: true")
}

const tilesLayout = `import tui 1.0
import tui.theme.dark 1.0
import tui.panels 1.0
Window {
    TileManager {
        TilePanel { id: files; heading: "Files"; edge: "left"; edges: "left right"; size: 25; Text { text: "in the panel" } }
        TilePanel { id: page; heading: qsTrId("demo.panel.page"); edge: "center"; Text { text: "the page" } }
    }
}`

// TilePanels build in a TileManager, a heading taking a string or a catalog message.
func TestATilePanelBuilds(t *testing.T) {
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(tilesLayout)), panels.Offer(), themes.Offer(), collector()); err != nil {
		t.Fatalf("TilePanels do not build: %v", err)
	}
}

// TilePanel is the tiled frame over a Tile, never a Drawer: it shares the screen rather than lying
// over it. Asserted on the shipped file itself.
func TestTilePanelIsATile(t *testing.T) {
	src, err := fs.ReadFile(panels.FS(), "TilePanel.qml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := qml.QML{File: "TilePanel.qml"}.Parse(src)
	if err != nil || tree.Root == nil || tree.Root.Type != "Tile" {
		t.Fatalf("TilePanel.qml: %v, root %+v", err, tree.Root)
	}
}
