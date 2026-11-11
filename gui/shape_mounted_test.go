package gui_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	guiwidget "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// cellFill is a widget that paints its cells' background in one colour: what lies under a shape,
// or what is painted over one.
type cellFill struct {
	bg   style.Color
	size tui.Size // zero takes the room it is given
}

func (*cellFill) Init(*tui.Context) {}
func (f *cellFill) Layout(c tui.Constraints) tui.Size {
	sz := f.size
	if sz.W == 0 && c.MaxW != tui.Unbounded {
		sz.W = c.MaxW
	}
	if sz.H == 0 && c.MaxH != tui.Unbounded {
		sz.H = c.MaxH
	}
	return c.Constrain(sz)
}
func (f *cellFill) Render(s tui.Surface) {
	sz := s.Size()
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", style.New().Background(f.bg))
}
func (*cellFill) HandleEvent(tui.Event) bool { return false }

// sceneHost runs a real App on a TestBackend whose native placements and flushes go to a real gui
// Backend, headless, as a window's do. It keeps the first frame's placements, and that frame's
// composited pixels (gui.HeadlessFrames.Snapshot, taken on the loop where Flush runs).
type sceneHost struct {
	*tui.TestBackend
	h      *gui.HeadlessFrames
	mu     sync.Mutex
	placed []tui.NativePlacement
	shot   chan struct{} // closed once the first frame's snapshot is taken
	img    *image.RGBA
	err    error
}

func (s *sceneHost) NativeFor(c tui.Component) (any, tui.NativeScope, bool) {
	return s.h.B.NativeFor(c)
}
func (s *sceneHost) CellPixels() tui.CellPixels { return s.h.B.CellPixels() }
func (s *sceneHost) PlaceNatives(ps []tui.NativePlacement) {
	s.h.B.PlaceNatives(ps)
	s.mu.Lock()
	if s.placed == nil {
		s.placed = append([]tui.NativePlacement{}, ps...)
	}
	s.mu.Unlock()
}

func (s *sceneHost) Flush(diff []tui.CellUpdate) error {
	if err := s.TestBackend.Flush(diff); err != nil {
		return err
	}
	s.h.Flush(diff)
	select {
	case <-s.shot:
	default:
		s.img, s.err = s.h.Snapshot()
		close(s.shot)
	}
	return nil
}

// A mounted App's shapes reach the window as a window's frame places them: each Shape is a native
// placement at its cells, a later sibling after an earlier one, with the cells painted after it as
// its Covered; and the composited frame shows them so, in pixels on a GPU: the later shape over the
// earlier where they overlap, both over the widget beneath, and the cells painted after a shape
// over it.
func TestAMountedAppsShapesAreComposited(t *testing.T) {
	red, blue := color.NRGBA{R: 0xc0, A: 0xff}, color.NRGBA{B: 0xc0, A: 0xff}
	a := guiwidget.NewShape(guiwidget.ShapeRect, guiwidget.WithFillColor(red), guiwidget.WithShapeSize(8, 3))
	b := guiwidget.NewShape(guiwidget.ShapeRect, guiwidget.WithFillColor(blue), guiwidget.WithShapeSize(8, 3))
	root := tui.NewStack()
	root.AddFilled(&cellFill{bg: style.RGB(0, 0xc0, 0)})                                   // the widget beneath: green cells
	root.AddAt(a, 2, 1)                                                                    // cols 2-9, rows 1-3
	root.AddAt(b, 6, 2)                                                                    // cols 6-13, rows 2-4: over a at cols 6-9, rows 2-3
	root.AddAt(&cellFill{bg: style.RGB(0xc0, 0xc0, 0), size: tui.Size{W: 4, H: 1}}, 12, 3) // yellow cells over b at cols 12-13, row 3

	// the App sees the window's own capabilities (true colour, native views), as it would in a window
	frames := gui.NewHeadlessFrames(20, 6)
	host := &sceneHost{TestBackend: tui.NewTestBackend(20, 6, tui.WithTestCapabilities(frames.B.Capabilities())),
		h: frames, shot: make(chan struct{})}
	app := tui.NewApp(root, tui.WithBackend(host), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-host.shot:
	case <-time.After(10 * time.Second):
		t.Fatal("the first frame never flushed")
	}

	host.mu.Lock()
	placed := append([]tui.NativePlacement{}, host.placed...)
	host.mu.Unlock()
	at := func(v any) (tui.NativePlacement, int) {
		for i, p := range placed {
			if p.View == v {
				return p, i
			}
		}
		return tui.NativePlacement{}, -1
	}
	av, _ := a.NativeView()
	bv, _ := b.NativeView()
	pa, ia := at(av)
	pb, ib := at(bv)
	if ia < 0 || ib < 0 {
		t.Fatalf("the shapes were not placed: %+v", placed)
	}
	if pa.X != 2 || pa.Y != 1 || pa.Cols != 8 || pa.Rows != 3 || pb.X != 6 || pb.Y != 2 || pb.Cols != 8 || pb.Rows != 3 {
		t.Errorf("placed at a %d,%d %dx%d and b %d,%d %dx%d; want a 2,1 8x3 and b 6,2 8x3",
			pa.X, pa.Y, pa.Cols, pa.Rows, pb.X, pb.Y, pb.Cols, pb.Rows)
	}
	if !(pa.Layer < pb.Layer) {
		t.Errorf("a's paint order %d is not before b's %d: the later sibling must be placed over the earlier", pa.Layer, pb.Layer)
	}
	covers := func(p tui.NativePlacement, x, y int) bool {
		for _, r := range p.Covered {
			if r.Contains(x, y) {
				return true
			}
		}
		return false
	}
	if !covers(pb, 12, 3) || !covers(pb, 13, 3) || covers(pb, 11, 3) || covers(pb, 7, 2) {
		t.Errorf("b's Covered %v: want the yellow cells (12,3) and (13,3) painted after it, and nothing else of it", pb.Covered)
	}
	if covers(pa, 7, 2) {
		t.Errorf("a's Covered %v holds b's cells: a native view over it is composited, not covered", pa.Covered)
	}

	if errors.Is(host.err, gui.ErrNoGPU) {
		t.Skipf("the placements are checked; no headless GPU here (gui.ErrNoGPU), so the pixels are not: %v", host.err)
	}
	if host.err != nil {
		t.Fatal(host.err)
	}
	// 8x16 pixel cells: a cell's centre is (8x+4, 16y+8)
	px := func(col, row int) color.RGBA { return host.img.RGBAAt(8*col+4, 16*row+8) }
	near := func(got color.RGBA, r, g, b uint8) bool {
		d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
		return d(got.R, r) <= 16 && d(got.G, g) <= 16 && d(got.B, b) <= 16
	}
	for _, c := range []struct {
		col, row int
		what     string
		r, g, b  uint8
	}{
		{3, 1, "a alone", 0xc0, 0, 0},
		{7, 2, "b over a", 0, 0, 0xc0},
		{11, 3, "b alone", 0, 0, 0xc0},
		{12, 3, "the yellow cells over b", 0xc0, 0xc0, 0},
		{17, 0, "the widget beneath", 0, 0xc0, 0},
	} {
		if got := px(c.col, c.row); !near(got, c.r, c.g, c.b) {
			t.Errorf("cell (%d,%d), %s: %v, want %02x%02x%02x", c.col, c.row, c.what, got, c.r, c.g, c.b)
		}
	}
}
