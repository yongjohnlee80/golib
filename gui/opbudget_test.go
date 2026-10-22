//go:build gui_opcount

package gui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/io/input"
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
	resetOps()
	r.frame(g, m, cursorState{}, nil, nil, true)
	want := budget(r, g)
	fills, batches, cells := int(ops.fills.Load()), int(ops.batches.Load()), ops.cells.Load()
	if got := fills + batches; got > want || cells != 0 {
		t.Fatalf("recorded %d fills + %d batches = %d and %d lone glyphs (%s); the batched budget is %d and 0", fills, batches, got, cells, lone(r, g), want)
	}
	if n := ops.rows.Load(); n != 67 {
		t.Fatalf("recorded %d rows; want 67 (the counter must observe)", n)
	}
	t.Logf("%d fills + %d batches within a budget of %d (%d cells)", fills, batches, want, 240*67)
}

// paneWindow is a Backend whose grid is a 1920×1080 Panes window: a full cell pane of mixed text
// beside a 480dp pixel pane, given as its band. Nothing is published yet.
func paneWindow(t *testing.T) (*Backend, metrics, tui.Rect) {
	t.Helper()
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	p := NewPanes(Row(Sized(Flex(1), Cells(&stubPane{"cells"})), Sized(Fixed(480), Pixels(&stubPane{"stream"}))))
	m := placePanes(p, metrics{window: image.Pt(1920, 1080), scale: 1, ppem: 14, cell: image.Pt(8, 16), baseline: 12}, 0)
	if m.layout == nil || len(m.layout.panes) != 2 {
		t.Fatal("fixture: no two-pane layout")
	}
	b.grid = *fullGrid(m.grid.W, m.grid.H)
	return b, m, m.layout.panes[1].band
}

// streamWindow is paneWindow with a view in the pixel pane that draws a backdrop (a letterbox),
// one stream filling the pane, and an overlay after it (as a frame rate badge is): drawing before
// and after the slot. The flushed frame is published.
func streamWindow(t *testing.T) (*Backend, *Stream, metrics) {
	t.Helper()
	b, m, band := paneWindow(t)
	st := b.NewStream()
	b.natives = []tui.NativePlacement{{View: ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: c.Size().W, H: c.Size().H}, Brush{Color: color.NRGBA{A: 0xff}})
		DrawStream(c, st, Rect{W: c.Size().W, H: c.Size().H})
		c.FillRRect(Rect{X: 8, Y: 8, W: 96, H: 24}, 4, Brush{Color: color.NRGBA{A: 0xb0}})
	}), X: band.X, Y: band.Y, Cols: band.W, Rows: band.H}}
	b.publishFrame(m)
	return b, st, m
}

// presentAt runs the real present for b's frame at now, the toolkit taking the ops as they are.
func presentAt(b *Backend, m metrics, now time.Time) {
	router := new(input.Router)
	b.present(app.FrameEvent{Now: now, Size: m.window, Source: router.Source(), Frame: func(*op.Ops) {}}, &b.gio, m, b.font.load())
}

// One stream frame makes Gio walk at most the flushed frame's ops plus 4 per slot, through the
// real present: the flushed frame's present walks its recorded ops plus the window's own three
// (the background's clip and fill, the input area's clip), and a pushed picture adds its slot's
// clip, transforms and image. It records no row and paints no view.
func TestAStreamFrameWalksTheFlushedFramePlusFourPerSlot(t *testing.T) {
	b, st, m := streamWindow(t)
	defer st.Close()
	b.mu.Lock()
	f := b.latest
	b.mu.Unlock()
	flushed := f.drawCost
	for _, p := range f.parts {
		flushed += p.cost
	}
	if len(f.parts) != 1 || f.parts[0].cost == 0 || flushed < 67 {
		t.Fatalf("fixture: %d slots, %d recorded ops; want 1 slot with drawing after it, and a counted frame", len(f.parts), flushed)
	}
	const window = 3
	resetOps()
	presentAt(b, m, time.Unix(1, 0))
	w0 := walkNow()
	if w0 != flushed+window {
		t.Fatalf("the flushed frame's present walks %d ops; want its recorded %d + the window's %d", w0, flushed, window)
	}
	var rel int
	st.Push(memFrame(t, 64, 64, [4]byte{1, 2, 3, 255}, &rel))
	resetOps()
	presentAt(b, m, time.Unix(2, 0))
	w1 := walkNow()
	if budget := w0 + 4*len(f.parts); w1 > budget || w1 == w0 {
		t.Fatalf("a stream frame walks %d ops; want more than the flushed frame's %d (it drew the picture) and at most %d", w1, w0, budget)
	}
	if rows, paints := ops.rows.Load(), ops.paints.Load(); rows != 0 || paints != 0 {
		t.Fatalf("a stream frame recorded %d rows and painted %d views; want 0 and 0", rows, paints)
	}
	t.Logf("flushed frame: %d recorded ops, %d walked; a stream frame walks %d", flushed, w0, w1)
}

// A stream's frames redraw at the display's pace without the cell pipeline: 100 pushed pictures,
// each presented, record no row and paint no view. A tui flush, the positive control, does both.
func TestStreamFramesRecordNoRowAndPaintNoView(t *testing.T) {
	b, st, m := streamWindow(t)
	defer st.Close()
	resetOps()
	var rel int
	for i := range 100 {
		st.Push(memFrame(t, 4, 4, [4]byte{1, 2, 3, 255}, &rel))
		presentAt(b, m, time.Unix(int64(i), 0))
	}
	if rows, paints := ops.rows.Load(), ops.paints.Load(); rows != 0 || paints != 0 {
		t.Fatalf("100 stream frames recorded %d rows and painted %d views; want 0 and 0", rows, paints)
	}
	b.grid.touchAll()
	b.publishFrame(m)
	if rows, paints := ops.rows.Load(), ops.paints.Load(); rows == 0 || paints == 0 {
		t.Fatalf("a tui flush recorded %d rows and painted %d views; the counters do not observe", rows, paints)
	}
}

// A frame with no stream slot is one segment, and present credits all of it: the first frame a
// renderer publishes, and one published after a split frame (whose first segment's count must not
// carry over), each walk their own recorded ops plus the window's three.
func TestAnUnsplitFrameIsCreditedWhenPresented(t *testing.T) {
	latest := func(b *Backend) *frame {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.latest
	}
	fresh, m, _ := paneWindow(t)
	fresh.publishFrame(m) // its renderer's first frame, unsplit
	want := latest(fresh).drawCost
	if want == 0 {
		t.Fatal("a renderer's first frame, unsplit, is credited 0 ops")
	}
	b, st, _ := streamWindow(t) // its first frame is split at a slot
	defer st.Close()
	b.natives = nil
	b.grid.touchAll()
	b.publishFrame(m)
	if got := latest(b).drawCost; got != want {
		t.Fatalf("an unsplit frame after a split one is credited %d ops; want its own %d", got, want)
	}
	resetOps()
	presentAt(b, m, time.Unix(1, 0))
	if w := walkNow(); w != want+3 {
		t.Fatalf("presenting it walks %d ops; want its %d + the window's 3", w, want)
	}
}
