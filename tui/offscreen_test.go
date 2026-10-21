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
		if inner[0].Face != got[0].Face { // a nested call with no face inherits the enclosing call's
			t.Errorf("face %q: the nested grid's face %d, want the enclosing grid's %d", own, inner[0].Face, got[0].Face)
		}
	}
}

// restoreProbe renders off the window three times in one Render: a call with a face of its own whose
// draw panics (recovered), then one with no face: that one is in the node's face again, the
// panicked call's face undone; the App's offscreen state is back to none.
type restoreProbe struct {
	MultiChild
	a, b      Offscreen
	after     []Cell
	depth     int
	recovered bool
}

func (p *restoreProbe) Layout(c Constraints) Size { return c.Constrain(Size{W: c.MaxW, H: c.MaxH}) }
func (p *restoreProbe) Render(s Surface) {
	s.SetCell(0, 0, "w", style.New())
	func() {
		defer func() { p.recovered = recover() != nil }()
		p.ctx.RenderOffscreen(&p.a, Size{W: 2, H: 1}, "Other Mono", func(Surface) { panic("a draw that fails") })
	}()
	p.after = slices.Clone(p.ctx.RenderOffscreen(&p.b, Size{W: 2, H: 1}, "", func(o Surface) { o.SetCell(0, 0, "x", style.New()) }))
	p.depth = p.ctx.app.faces.offDepth
}
func (p *restoreProbe) HandleEvent(Event) bool { return false }

func TestRenderOffscreenRestoresTheFaceInEffect(t *testing.T) {
	p := &restoreProbe{}
	term := &faceBox{box: box{rects: []Rect{{X: 0, Y: 0, W: 4, H: 1}}}, family: "Iosevka"}
	term.Add(p)
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 2}}}
	root.Add(term)
	h, _ := startFaced(t, root, 8, 3)
	h.sync()
	var after []Cell
	var depth int
	var recovered bool
	var window uint16
	h.onLoop(func() { after, depth, recovered, window = p.after, p.depth, p.recovered, p.ctx.app.buf.curr[0].Face })
	if !recovered {
		t.Fatal("the draw did not panic")
	}
	if depth != 0 || len(after) != 2 || after[0].Face != window {
		t.Fatalf("after a panicked call: depth %d, face %v; want 0 and the node's face %d", depth, after, window)
	}
}
