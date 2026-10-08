package gui

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"

	"github.com/yongjohnlee80/golib/tui"
)

// A row's glyphs drawn as one path per colour land on the pixels each cell's own path drew, to
// a shade of antialiasing: the batch only merges draw calls. Rendered on Gio's headless GPU, which a machine with no
// GL context skips.
func TestARowsBatchDrawsWhatEachCellDrew(t *testing.T) {
	const w, h = 40, 1
	m := metrics{window: image.Pt(w*10, 24), cell: image.Pt(10, 22), ppem: 16, baseline: 16, scale: 1, grid: tui.Size{W: w, H: h}}
	hw, err := headless.NewWindow(m.window.X, m.window.Y)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	newR := func() *renderer {
		r := newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
		r.m = m
		return r
	}
	g := &grid{}
	g.resize(w, h)
	red, blue := tui.CellColor{Kind: tui.CellColorRGB, R: 0xc0}, tui.CellColor{Kind: tui.CellColorRGB, B: 0xc0}
	for i, ch := range []rune("func main() { x := 42 } // ok, Go!  ijl") {
		if i >= w {
			break
		}
		c := tui.Cell{Content: string(ch), Width: 1}
		if i%3 == 0 {
			c.Attrs.FG = red
		} else if i%5 == 0 {
			c.Attrs.FG = blue
			c.Attrs.Mask = tui.AttrBold
		}
		g.cells[i] = c
	}
	shot := func(record func(ops *op.Ops)) *image.RGBA {
		ops := new(op.Ops)
		paint.Fill(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		record(ops)
		if err := hw.Frame(ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: m.window})
		if err := hw.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	batched := shot(func(ops *op.Ops) { newR().recordRow(g, 0).Add(ops) })
	alone := shot(func(ops *op.Ops) {
		r := newR()
		for x := range w {
			c := g.at(x, 0)
			fg, _ := r.colors(c.Attrs)
			r.drawCell(ops, c, x, 0, fg)
		}
	})
	inked, differ, worst := 0, 0, 0
	for i := range batched.Pix {
		d := int(batched.Pix[i]) - int(alone.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > 0 {
			differ++
			worst = max(worst, d)
		}
		if batched.Pix[i] != 0xff {
			inked++
		}
	}
	// the same glyphs at the same places; a merged path's antialiasing may differ by a shade
	if worst > 8 || differ*20 > inked {
		t.Errorf("%d of %d inked bytes differ, the worst by %d: the batch did not draw what each cell drew", differ, inked, worst)
	}
	if inked == 0 {
		t.Fatal("nothing was drawn: the comparison proves nothing")
	}
}

// A row's border pieces merged into runs paint what each cell's own pieces painted: lines, corners
// and junctions, an underline across cells.
func TestARowsMergedLinesDrawWhatEachCellDrew(t *testing.T) {
	const w = 24
	m := metrics{window: image.Pt(w*10, 24), cell: image.Pt(10, 22), ppem: 16, baseline: 16, scale: 1, grid: tui.Size{W: w, H: 1}}
	hw, err := headless.NewWindow(m.window.X, m.window.Y)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	newR := func() *renderer {
		r := newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
		r.m = m
		return r
	}
	g := &grid{}
	g.resize(w, 1)
	for i, ch := range []rune("┌──┬───┐│ link ├══╪━━┘") {
		c := tui.Cell{Content: string(ch), Width: 1, Attrs: tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorRGB, R: 0x80, G: 0x40}}}
		if i >= 9 && i < 13 {
			c.Attrs.Mask = tui.AttrUnderline
		}
		g.cells[i] = c
	}
	shot := func(record func(ops *op.Ops)) *image.RGBA {
		ops := new(op.Ops)
		paint.Fill(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		record(ops)
		if err := hw.Frame(ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: m.window})
		if err := hw.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	merged := shot(func(ops *op.Ops) { newR().recordRow(g, 0).Add(ops) })
	alone := shot(func(ops *op.Ops) {
		r := newR()
		for x := range w {
			c := g.at(x, 0)
			fg, _ := r.colors(c.Attrs)
			r.drawCell(ops, c, x, 0, fg)
		}
	})
	inked, differ, worst := 0, 0, 0
	for i := range merged.Pix {
		d := int(merged.Pix[i]) - int(alone.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > 0 {
			differ++
			worst = max(worst, d)
		}
		if merged.Pix[i] != 0xff {
			inked++
		}
	}
	if inked == 0 || worst > 8 || differ*20 > inked {
		t.Errorf("%d of %d inked bytes differ, the worst by %d", differ, inked, worst)
	}
}
