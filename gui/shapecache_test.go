package gui

import (
	"fmt"
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

func testShaper() *text.Shaper {
	return text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
}

// glyphsOf is s shaped on one line, as a renderer hands a run to Shape.
func glyphsOf(s *text.Shaper, str string) []text.Glyph {
	s.LayoutString(text.Parameters{Font: fontOf("Go Mono"), PxPerEm: fixed.I(16), MaxWidth: 1 << 20}, str)
	var gs []text.Glyph
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		gs = append(gs, g)
	}
	return gs
}

// More runs than Gio's own cache holds, in the same order each frame: the first frame shapes each
// once, and the frames after shape none.
func TestARunIsShapedOnceWhileOnScreen(t *testing.T) {
	s := testShaper()
	c := newShapeCache(s)
	runs := make([][]text.Glyph, 1500)
	for i := range runs {
		runs[i] = glyphsOf(s, fmt.Sprintf("run %d of the screen", i))
	}
	for frame := range 3 {
		for _, gs := range runs {
			c.Shape(gs)
		}
		c.endFrame()
		if c.shaped != len(runs) {
			t.Fatalf("after frame %d: %d runs shaped, want %d: each once", frame, c.shaped, len(runs))
		}
	}
}

// A run is the same run anywhere on the screen; the same glyphs spaced otherwise are another.
func TestARunIsKeyedByItsGlyphsAndTheirSpacing(t *testing.T) {
	s := testShaper()
	c := newShapeCache(s)
	gs := glyphsOf(s, "the same words")
	c.Shape(gs)
	moved := append([]text.Glyph(nil), gs...)
	for i := range moved {
		moved[i].X += fixed.I(640)
	}
	c.Shape(moved)
	if c.shaped != 1 {
		t.Fatalf("the run moved along the row was shaped again: %d shapes", c.shaped)
	}
	spaced := append([]text.Glyph(nil), gs...)
	spaced[len(spaced)-1].X += fixed.I(3)
	c.Shape(spaced)
	if c.shaped != 2 {
		t.Fatalf("the same glyphs spaced otherwise were taken for the run: %d shapes", c.shaped)
	}
}

// A run no frame has used for keepFrames is dropped, and shaped again when it returns; one used
// every frame stays.
func TestARunOffScreenIsDropped(t *testing.T) {
	s := testShaper()
	c := newShapeCache(s)
	kept, gone := glyphsOf(s, "kept"), glyphsOf(s, "gone")
	c.Shape(kept)
	c.Shape(gone)
	c.endFrame()
	for range keepFrames {
		c.Shape(kept)
		c.endFrame()
	}
	c.Shape(kept)
	if c.shaped != 2 {
		t.Fatalf("a run used every frame was shaped again: %d shapes", c.shaped)
	}
	c.Shape(gone)
	if c.shaped != 3 {
		t.Fatalf("a run off screen for %d frames was kept: %d shapes", keepFrames, c.shaped)
	}
	c.clear()
	c.Shape(kept)
	if c.shaped != 4 {
		t.Fatalf("clear kept a run: %d shapes", c.shaped)
	}
}

// A window of rows, every one recorded again (a repaint of the whole grid): the second frame shapes
// no run the first already shaped.
func TestARepaintedGridShapesNothingTwice(t *testing.T) {
	const w, h = 80, 60
	m := metrics{window: image.Pt(w*10, h*22), cell: image.Pt(10, 22), ppem: 16, baseline: 16, scale: 1, grid: tui.Size{W: w, H: h}}
	r := newRenderer(testShaper(), "Go Mono", DefaultTheme())
	g := &grid{}
	g.resize(w, h)
	colours := []tui.CellColor{{Kind: tui.CellColorRGB, R: 0xc0}, {Kind: tui.CellColorRGB, B: 0xc0}, {Kind: tui.CellColorRGB, G: 0xc0}}
	for y := range h {
		for x, ch := range []rune(fmt.Sprintf("row %02d: the quick brown fox %d jumps over the lazy dog", y, y*7)) {
			if x >= w {
				break
			}
			c := tui.Cell{Content: string(ch), Width: 1}
			c.Attrs.FG = colours[(x/8)%len(colours)]
			g.cells[y*w+x] = c
		}
	}
	g.touchAll()
	r.frame(g, m, cursorState{}, nil, nil, true)
	first := r.paths.shaped
	if first < h {
		t.Fatalf("the first frame shaped %d runs; want at least one per row", first)
	}
	g.touchAll()
	r.frame(g, m, cursorState{}, nil, nil, true)
	if r.paths.shaped != first {
		t.Fatalf("the repainted grid shaped %d runs again", r.paths.shaped-first)
	}
}
