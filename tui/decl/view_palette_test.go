package decl_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

func viewCell(t *testing.T, s *decltest.Screen, label string) tui.CellAttrs {
	t.Helper()
	for y, line := range strings.Split(s.String(), "\n") {
		if pos := strings.Index(line, label); pos >= 0 {
			x := utf8.RuneCountInString(line[:pos])
			return s.Backend.Snapshot()[y][x].Attrs
		}
	}
	t.Fatalf("no painted %q in:\n%s", label, s.String())
	return tui.CellAttrs{}
}

func TestModelViewPalettesDressRowsAndRepaintActiveAndBlurredCursors(t *testing.T) {
	tree := tuidecl.NewTreeListModel("key", "label")
	tree.SetChildren(nil, []tuidecl.TreeRow{
		{Row: tuidecl.Row{"key": "a", "label": "alpha"}},
		{Row: tuidecl.Row{"key": "b", "label": "bravo"}},
	})
	flat := tuidecl.NewListModel("name")
	flat.Reset([]tuidecl.Row{{"name": "charlie"}, {"name": "delta"}})
	layout := []byte(`import tui 1.0
import demo 1.0
Window {
 palette.base: App.base; palette.text: "#eeeeee"
 palette.highlight: "#ffffff"; palette.highlightedText: "#000000"
 palette.inactive.highlight: "#333344"; palette.inactive.highlightedText: "#bbbbbb"
 Flex { direction: Tui.Vertical
  TreeView { id: tree; model: App.tree; textRole: "label"; focus: true; Layout.fillHeight: true }
  ListView { id: list; model: App.list; textRole: "name"; Layout.fillHeight: true }
 }
}`)
	s := decltest.Run(t, 40, 12,
		tuidecl.LayoutSource("main.qml", layout),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": tree, "App.list": flat, "App.base": "#101020"}))
	rgb := func(r, g, b uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: b} }
	active := func(c tui.CellAttrs) bool {
		return c.FG == rgb(0, 0, 0) && c.BG == rgb(255, 255, 255) && c.Mask&tui.AttrReverse == 0
	}
	blurred := func(c tui.CellAttrs) bool {
		return c.FG == rgb(0xbb, 0xbb, 0xbb) && c.BG == rgb(0x33, 0x33, 0x44) && c.Mask&tui.AttrReverse == 0
	}
	s.WaitFor(t, "focused tree cursor and blurred list cursor", func(sc string) bool {
		if !strings.Contains(sc, "alpha") || !strings.Contains(sc, "charlie") {
			return false
		}
		return active(viewCell(t, s, "alpha")) && blurred(viewCell(t, s, "charlie"))
	})
	if c := viewCell(t, s, "bravo"); c.FG != rgb(0xee, 0xee, 0xee) || c.BG != rgb(0x10, 0x10, 0x20) || c.Mask&tui.AttrReverse != 0 {
		t.Fatalf("ordinary tree row ignored base/text palette: %+v", c)
	}
	if c := viewCell(t, s, "delta"); c.FG != rgb(0xee, 0xee, 0xee) || c.BG != rgb(0x10, 0x10, 0x20) || c.Mask&tui.AttrReverse != 0 {
		t.Fatalf("ordinary list row ignored base/text palette: %+v", c)
	}
	s.Program.Post(func() { _ = s.Program.Call("list", "forceActiveFocus") })
	s.WaitFor(t, "focus exchange repainted both cursors", func(string) bool {
		return blurred(viewCell(t, s, "alpha")) && active(viewCell(t, s, "charlie"))
	})
	s.Program.Post(func() { _ = s.Program.Set("App.base", "#202030") })
	s.WaitFor(t, "live palette source changed both ordinary rows", func(string) bool {
		return viewCell(t, s, "bravo").BG == rgb(0x20, 0x20, 0x30) && viewCell(t, s, "delta").BG == rgb(0x20, 0x20, 0x30)
	})
	// A developer removing the role must not leave the last source value
	// painted on a view that survived transactional reload.
	reloaded := make(chan error, 1)
	s.Program.Post(func() {
		_, err := s.Program.Reload(bytes.Replace(layout, []byte("palette.base: App.base;"), nil, 1))
		reloaded <- err
	})
	if err := <-reloaded; err != nil {
		t.Fatal(err)
	}
	s.WaitFor(t, "removed base role cleared both row colors", func(string) bool {
		return viewCell(t, s, "bravo").BG != rgb(0x20, 0x20, 0x30) && viewCell(t, s, "delta").BG != rgb(0x20, 0x20, 0x30)
	})
}

func TestUnstyledTreeKeepsItsCursorWhenFocusLeaves(t *testing.T) {
	tree := tuidecl.NewTreeListModel("key", "label")
	tree.SetChildren(nil, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "a", "label": "alpha"}}})
	flat := tuidecl.NewListModel("name")
	flat.Reset([]tuidecl.Row{{"name": "other"}})
	s := decltest.Run(t, 30, 8,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window { Flex { direction: Tui.Vertical
 TreeView { id: tree; model: App.tree; textRole: "label"; focus: true; Layout.fillHeight: true }
 ListView { id: elsewhere; model: App.list; textRole: "name"; Layout.fillHeight: true }
} }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": tree, "App.list": flat}))
	s.WaitForText(t, "alpha")
	before := viewCell(t, s, "alpha")
	if before.Mask&tui.AttrReverse == 0 {
		t.Fatalf("default tree lost its reverse cursor: %+v", before)
	}
	flushes := s.Backend.Flushes()
	s.Program.Post(func() { _ = s.Program.Call("elsewhere", "forceActiveFocus") })
	s.WaitFor(t, "unstyled cursor stays visible while blurred", func(string) bool {
		return s.Backend.Flushes() > flushes && viewCell(t, s, "alpha") == before
	})
}
