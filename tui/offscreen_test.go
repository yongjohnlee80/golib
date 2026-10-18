package tui

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// offProbe draws a fixed picture (draw) on the window and, in the same Render, off it at the same size:
// what the window shows and what RenderOffscreen gives are to be the same cells.
type offProbe struct {
	MultiChild
	off, nested Offscreen
	got, inner  []Cell
	size        Size
	face        string
}

// offDraw is the picture: plain, a theme token's colour, reverse with an attribute, a wide cluster
// at the right edge (dropped, as the frame buffer drops it) and one inside.
func offDraw(s Surface) {
	s.SetCell(0, 0, "a", style.New())
	s.SetCell(1, 0, "b", style.New().Foreground(style.ANSI(1)).Bold(true))
	s.SetCell(2, 0, "c", style.New().Reverse(true))
	s.SetCell(0, 1, "界", style.New())
	s.SetCell(s.Size().W-1, 1, "界", style.New())
}

func (p *offProbe) Layout(c Constraints) Size { return c.Constrain(Size{W: c.MaxW, H: c.MaxH}) }
func (p *offProbe) Render(s Surface) {
	offDraw(s)
	p.size = s.Size()
	got := p.ctx.RenderOffscreen(&p.off, p.size, p.face, func(o Surface) {
		offDraw(o)
		// a nested call, its own Offscreen: the outer one's cells are kept
		p.inner = slices.Clone(p.ctx.RenderOffscreen(&p.nested, Size{W: 2, H: 1}, "", func(n Surface) { n.SetCell(0, 0, "n", style.New()) }))
	})
	p.got = slices.Clone(got)
}
func (p *offProbe) HandleEvent(Event) bool { return false }

// RenderOffscreen gives the frame buffer's cells for the same drawing: content, width, resolved
// attributes and the face in effect (inherited from a CellFacer above it, or its own when given);
// a nested call has its own grid; the window's frame is untouched by it.
func TestRenderOffscreenIsTheFrameBuffersCells(t *testing.T) {
	for _, own := range []string{"", "Other Mono"} {
		p := &offProbe{face: own}
		term := &faceBox{box: box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 2}}}, family: "Iosevka"}
		term.Add(p)
		root := &box{rects: []Rect{{X: 0, Y: 0, W: 8, H: 3}}}
		root.Add(term)
		h, _ := startFaced(t, root, 10, 4)
		h.sync()
		var got, inner, frame []Cell
		var size Size
		var fw int
		h.onLoop(func() { // the frame buffer itself: a backend's snapshot rebuilds continuation cells
			got, inner, size = p.got, p.inner, p.size
			frame, fw = slices.Clone(p.ctx.app.buf.curr), p.ctx.app.buf.w
		})
		if size != (Size{W: 6, H: 2}) || len(got) != 12 {
			t.Fatalf("face %q: size %v, %d cells", own, size, len(got))
		}
		for y := 0; y < size.H; y++ {
			for x := 0; x < size.W; x++ {
				w, o := frame[y*fw+x], got[y*size.W+x]
				if own != "" {
					w.Face = o.Face // its own face: compared below
				}
				if w != o {
					t.Errorf("face %q (%d,%d): offscreen %+v, window %+v", own, x, y, o, w)
				}
			}
		}
		if own == "" && got[0].Face != frame[0].Face || own != "" && got[0].Face == frame[0].Face {
			t.Errorf("face %q: offscreen face %d, window %d", own, got[0].Face, frame[0].Face)
		}
		if len(inner) != 2 || inner[0].Content != "n" {
			t.Errorf("the nested grid: %+v", inner)
		}
	}
}
