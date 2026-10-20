package tiles_test

import (
	"io/fs"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
	"github.com/yongjohnlee80/golib/tui/decl/tiles"
	"github.com/yongjohnlee80/golib/tui/widget"
)

const layout = `import tui 1.0
import tui.theme.dark 1.0
import tui.tiles 1.0
Window {
    TileManager {
        TilePanel { id: files; heading: "Files"; edge: "left"; edges: "left right"; size: 25; Text { text: "in the panel" } }
        TilePanel { id: page; heading: qsTrId("demo.panel.page"); edge: "center"; Text { text: "the page" } }
    }
}`

func collector() tuidecl.ProgramOption {
	return tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows()))
}

// TilePanels build in a TileManager, a heading taking a string or a catalog message.
func TestATilePanelBuilds(t *testing.T) {
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(layout)), tiles.Offer(), themes.Offer(), collector()); err != nil {
		t.Fatalf("TilePanels do not build: %v", err)
	}
}

// TilePanel binds the toolkit's theme vocabulary, so a layout with no theme is refused rather than
// drawn with an unbound surface.
func TestATilePanelNeedsATheme(t *testing.T) {
	src := `import tui 1.0
import tui.tiles 1.0
Window { TileManager { TilePanel { heading: "Files"; edge: "center"; Text { text: "x" } } } }`
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(src)), tiles.Offer(), collector()); err == nil {
		t.Fatal("a TilePanel built with no theme imported")
	}
}

// TilePanel is the tiled frame over a Tile, never a Drawer: it shares the screen rather than lying
// over it. Asserted on the shipped file itself.
func TestTilePanelIsATile(t *testing.T) {
	src, err := fs.ReadFile(tiles.FS(), "TilePanel.qml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := qml.QML{File: "TilePanel.qml"}.Parse(src)
	if err != nil || tree.Root == nil || tree.Root.Type != "Tile" {
		t.Fatalf("TilePanel.qml: %v, root %+v", err, tree.Root)
	}
}
