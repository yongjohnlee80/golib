package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/unit"

	"github.com/yongjohnlee80/golib/tui"
)

// A cell's face is the typeface it is shaped in: the table's for its index, the window's for 0
// and for an index the table does not have. Each face's glyphs are cached apart.
func TestCellFacesShapeInTheirOwnTypeface(t *testing.T) {
	b := NewBackend()
	fs := b.font.Load()
	b.render.m = measure(fs.fm, fs.size, 0, image.Pt(200, 100), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	b.SetCellFaces([]string{"", "Go"}) // the window's is Go Mono
	window := b.render.shape("a", false, false, 0)
	faced := b.render.shape("a", false, false, 1)
	unknown := b.render.shape("a", false, false, 9)
	if len(window.glyphs) == 0 || len(faced.glyphs) == 0 || len(unknown.glyphs) == 0 {
		t.Fatal("nothing shaped")
	}
	if window.glyphs[0].ID == faced.glyphs[0].ID {
		t.Error("face 1 shaped in the window's typeface")
	}
	if unknown.glyphs[0].ID != window.glyphs[0].ID {
		t.Error("an index the table does not have is not the window's typeface")
	}
	if _, ok := b.render.glyphs[glyphKey{content: "a", face: 1}]; !ok {
		t.Error("face 1's glyph is not cached under its face")
	}
}

// A cell whose face alone changed is a changed cell: its row is drawn again.
func TestAFaceOnlyChangeRedrawsTheRow(t *testing.T) {
	var g grid
	g.resize(4, 2)
	cell := tui.Cell{Content: "a", Width: 1}
	g.apply([]tui.CellUpdate{{X: 1, Y: 1, Cell: cell}})
	g.dirty[1] = false
	cell.Face = 1
	g.apply([]tui.CellUpdate{{X: 1, Y: 1, Cell: cell}})
	if !g.dirty[1] || g.at(1, 1).Face != 1 {
		t.Fatalf("a face change: dirty %v, face %d", g.dirty[1], g.at(1, 1).Face)
	}
}
