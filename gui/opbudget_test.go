//go:build gui_opcount

package gui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// fullGrid is a w×h grid of mixed text: words in three colours, two background colours in runs.
func fullGrid(w, h int) *grid {
	g := &grid{}
	g.resize(w, h)
	fgs := []tui.CellColor{
		{Kind: tui.CellColorRGB, R: 0xd0, G: 0x40, B: 0x40},
		{Kind: tui.CellColorRGB, R: 0x40, G: 0xd0, B: 0x40},
		{Kind: tui.CellColorRGB, R: 0x40, G: 0x40, B: 0xd0},
	}
	bg := tui.CellColor{Kind: tui.CellColorRGB, R: 0x20, G: 0x20, B: 0x30}
	text := "func main() { fmt.Println(\"hello, world\") } // a line of code "
	for y := range h {
		for x := range w {
			c := tui.Cell{Content: string(text[(x+y)%len(text)]), Width: 1}
			c.Attrs.FG = fgs[(x/7)%3]
			if (x/40)%2 == 1 {
				c.Attrs.BG = bg
			}
			g.cells[y*w+x] = c
		}
	}
	return g
}

// lone names the contents of g's first row that batchable refuses, with their advance.
func lone(r *renderer, g *grid) string {
	var out []string
	seen := map[string]bool{}
	for x := range g.w {
		c := g.at(x, 0)
		if _, ok := r.batchable(c, x, 0); ok || seen[c.Content] || c.Content == " " {
			continue
		}
		seen[c.Content] = true
		gs := r.shape(c.Content, false, false, c.Face)
		out = append(out, fmt.Sprintf("%q advance %d empty %v colour %v", c.Content, gs.advance, gs.empty, gs.colour))
	}
	return strings.Join(out, "; ")
}

// budget is what the batched drawing of g may record: per row, a fill per run of an off-theme
// background and a glyph batch per foreground colour, plus the four margins.
func budget(r *renderer, g *grid) int {
	n := 4
	for y := range g.h {
		var lastBG color.NRGBA
		inRun := false
		colours := map[color.NRGBA]bool{}
		for x := range g.w {
			c := g.at(x, y)
			fg, bg := r.colors(c.Attrs)
			if bg != r.theme.BG && (!inRun || bg != lastBG) {
				n++
			}
			inRun, lastBG = bg != r.theme.BG, bg
			if c.Content != " " && c.Content != "" {
				colours[fg] = true
			}
		}
		n += len(colours)
	}
	return n
}

// A full-window cell pane at 1920×1080 (240×67 cells of 8×16): the flushed frame stays within
// the batched budget its grid allows, and draws no glyph alone.
func TestAFlushedFrameStaysWithinTheBatchedBudget(t *testing.T) {
	r := newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	g := fullGrid(240, 67)
	m := metrics{window: image.Pt(1920, 1080), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 240, H: 67}}
	ops = opCounts{}
	r.frame(g, m, cursorState{}, nil, nil, true)
	want := budget(r, g)
	if got := ops.fills + ops.batches; got > want || ops.cells != 0 {
		t.Fatalf("recorded %d fills + %d batches = %d and %d lone glyphs (%s); the batched budget is %d and 0", ops.fills, ops.batches, got, ops.cells, lone(r, g), want)
	}
	if ops.rows != 67 {
		t.Fatalf("recorded %d rows; want 67 (the counter must observe)", ops.rows)
	}
	t.Logf("%d fills + %d batches within a budget of %d (%d cells)", ops.fills, ops.batches, want, 240*67)
}

// A stream's frames redraw at the display's pace without the cell pipeline: 100 pushed and
// presented frames record no row and paint no view. A tui flush, the positive control, does both.
func TestStreamFramesRecordNoRowAndPaintNoView(t *testing.T) {
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	st := b.NewStream()
	view := ViewFunc(func(c Canvas) { DrawStream(c, st, Rect{W: 64, H: 64}) })
	g := fullGrid(40, 10)
	m := metrics{window: image.Pt(320, 160), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 40, H: 10}}
	natives := []tui.NativePlacement{{View: view, Cols: 8, Rows: 4}}
	draw, parts := b.render.frameParts(g, m, cursorState{}, natives, nil, true)
	ops = opCounts{}
	var rel int
	for range 100 {
		st.Push(memFrame(t, 4, 4, [4]byte{1, 2, 3, 255}, &rel))
		frameOps := new(op.Ops)
		draw.Add(frameOps)
		for _, p := range parts {
			p.slot.add(frameOps)
			p.seg.Add(frameOps)
		}
		b.streamsPresented(time.Now())
	}
	if ops.rows != 0 || ops.paints != 0 {
		t.Fatalf("100 stream frames recorded %d rows and painted %d views; want 0 and 0", ops.rows, ops.paints)
	}
	g.touchAll()
	b.render.frameParts(g, m, cursorState{}, natives, nil, true)
	if ops.rows == 0 || ops.paints == 0 {
		t.Fatalf("a tui flush recorded %d rows and painted %d views; the counters do not observe", ops.rows, ops.paints)
	}
}
