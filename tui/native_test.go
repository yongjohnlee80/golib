package tui

import (
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// nativeBackend is a TestBackend that hosts native views, keeping each frame's placements, and
// optionally styles components (NativeStyler).
type nativeBackend struct {
	*TestBackend
	mu     sync.Mutex
	frames [][]NativePlacement
	style  func(Component) (any, NativeScope, bool)
}

func (b *nativeBackend) PlaceNatives(ps []NativePlacement) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames = append(b.frames, append([]NativePlacement(nil), ps...))
}

func (b *nativeBackend) last() []NativePlacement {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.frames) == 0 {
		return nil
	}
	return b.frames[len(b.frames)-1]
}

type styledBackend struct{ *nativeBackend }

func (b styledBackend) NativeFor(c Component) (any, NativeScope, bool) { return b.style(c) }

// box paints its rect with fill (or nothing), and optionally reports a native view.
type box struct {
	MultiChild
	fill   string
	view   any
	scope  NativeScope
	report bool
	rects  []Rect // where children go, in order
}

func (b *box) Layout(c Constraints) Size {
	sz := Size{W: c.MaxW, H: c.MaxH}
	if b.ctx != nil {
		i := 0
		for _, ch := range b.All() {
			r := b.rects[i]
			b.ctx.LayoutChild(ch, Tight(Size{W: r.W, H: r.H}))
			b.ctx.PlaceChild(ch, r)
			i++
		}
	}
	return c.Constrain(sz)
}

func (b *box) Render(s Surface) {
	if b.fill != "" {
		s.Fill(Rect{W: s.Size().W, H: s.Size().H}, b.fill, style.New())
	}
}

func (b *box) HandleEvent(Event) bool { return false }

type reporterBox struct{ *box }

func (r reporterBox) NativeView() (any, bool)  { return r.view, r.report }
func (r reporterBox) NativeScope() NativeScope { return r.scope }

func startNative(t *testing.T, root Component, w, h int, styled func(Component) (any, NativeScope, bool)) (*harness, *nativeBackend) {
	t.Helper()
	nb := &nativeBackend{TestBackend: NewTestBackend(w, h), style: styled}
	var be Backend = nb
	if styled != nil {
		be = styledBackend{nb}
	}
	app := NewApp(root, WithBackend(be), WithMinFrameInterval(0))
	return runApp(t, app, nb.TestBackend), nb
}

func placements(t *testing.T, nb *nativeBackend, want int) []NativePlacement {
	t.Helper()
	waitFor(t, "placements", func() bool { return len(nb.last()) == want })
	return nb.last()
}

// A reporter is placed over its screen cells; a later sibling painting over part of it covers
// exactly those cells.
func TestNative_LaterPaintCoversAView(t *testing.T) {
	view := &box{view: "graph", report: true}
	modal := &box{fill: "M"}
	root := &box{rects: []Rect{{X: 1, Y: 1, W: 8, H: 4}, {X: 3, Y: 2, W: 3, H: 2}}}
	root.Add(reporterBox{view}, modal)
	h, nb := startNative(t, root, 12, 6, nil)
	ps := placements(t, nb, 1)
	p := ps[0]
	if p.View != "graph" || p.X != 1 || p.Y != 1 || p.Cols != 8 || p.Rows != 4 || p.Scope != ScopeChrome {
		t.Fatalf("placement %+v", p)
	}
	want := []Rect{{X: 3, Y: 2, W: 3, H: 1}, {X: 3, Y: 3, W: 3, H: 1}}
	if !equalRects(p.Covered, want) {
		t.Fatalf("Covered %v; want %v", p.Covered, want)
	}
	_ = h
}

// A subtree-scoped view owns its children: a child painting its whole rect does not cover it, and
// a child that reports a view itself is not placed.
func TestNative_SubtreeScopeOwnsItsChildren(t *testing.T) {
	inner := &box{fill: "b", view: "button", report: true}
	parent := &box{view: "checkbox", report: true, scope: ScopeSubtree, rects: []Rect{{W: 6, H: 1}}}
	parent.Add(reporterBox{inner})
	root := &box{rects: []Rect{{X: 2, Y: 1, W: 6, H: 1}}}
	root.Add(reporterBox{parent})
	_, nb := startNative(t, root, 10, 3, nil)
	ps := placements(t, nb, 1)
	if ps[0].View != "checkbox" || len(ps[0].Covered) != 0 {
		t.Fatalf("placements %+v; want only the checkbox, uncovered", ps)
	}
}

// A chrome-scoped view stays beneath its children: their cells cover it.
func TestNative_ChromeScopeIsCoveredByChildren(t *testing.T) {
	label := &box{fill: "t"}
	frame := &box{view: "frame", report: true, rects: []Rect{{X: 1, Y: 1, W: 3, H: 1}}}
	frame.Add(label)
	root := &box{rects: []Rect{{W: 6, H: 3}}}
	root.Add(reporterBox{frame})
	_, nb := startNative(t, root, 6, 3, nil)
	ps := placements(t, nb, 1)
	if want := []Rect{{X: 1, Y: 1, W: 3, H: 1}}; !equalRects(ps[0].Covered, want) {
		t.Fatalf("Covered %v; want %v", ps[0].Covered, want)
	}
}

// A later write over half of a wide character covers both halves: the freed sibling is stamped too.
func TestNative_WideHalfOverwriteCoversBothHalves(t *testing.T) {
	// A wide character painted first, then the view over the row, then a later write onto the
	// wide character's right half.
	over := &box{fill: "x"}
	root := &box{rects: []Rect{{W: 4, H: 1}, {W: 4, H: 1}, {X: 1, Y: 0, W: 1, H: 1}}}
	root.Add(&wideView{}, reporterBox{&box{view: "v", report: true}}, over)
	_, nb := startNative(t, root, 4, 1, nil)
	ps := placements(t, nb, 1)
	if want := []Rect{{X: 0, Y: 0, W: 2, H: 1}}; !equalRects(ps[0].Covered, want) {
		t.Fatalf("Covered %v; want both halves of the wide character, %v", ps[0].Covered, want)
	}
}

// wideView paints a wide character at its origin.
type wideView struct{ MultiChild }

func (w *wideView) Layout(c Constraints) Size { return c.Constrain(Size{W: c.MaxW, H: c.MaxH}) }
func (w *wideView) Render(s Surface)          { s.SetCell(0, 0, "한", style.New()) }
func (w *wideView) HandleEvent(Event) bool    { return false }

// A NativeStyler draws components that did not ask; two overlapping views keep paint order, and a
// later view's footprint is not in an earlier one's Covered (order alone composites them).
func TestNative_StylerAndTwoViewsInOrder(t *testing.T) {
	a, b := &box{}, &box{}
	root := &box{rects: []Rect{{W: 5, H: 2}, {X: 2, Y: 0, W: 5, H: 2}}}
	root.Add(a, b)
	styled := func(c Component) (any, NativeScope, bool) {
		switch c {
		case a:
			return "a", ScopeChrome, true
		case b:
			return "b", ScopeChrome, true
		}
		return nil, 0, false
	}
	_, nb := startNative(t, root, 8, 2, styled)
	ps := placements(t, nb, 2)
	if ps[0].View != "a" || ps[1].View != "b" || ps[0].Layer >= ps[1].Layer {
		t.Fatalf("placements %+v; want a then b, in paint order", ps)
	}
	if len(ps[0].Covered) != 0 {
		t.Fatalf("a's Covered %v: a later view's footprint is not a cell write", ps[0].Covered)
	}
}

// No NativeHost: no stamping, no placements; the frame is the cells alone.
func TestNative_NothingOnAPlainBackend(t *testing.T) {
	root := &box{rects: []Rect{{W: 4, H: 1}}}
	root.Add(reporterBox{&box{view: "v", report: true, fill: "c"}})
	h := startApp(t, root, 4, 1)
	waitFor(t, "the cells", func() bool { return screenHas(h.tb, "cccc") })
	var stamping bool
	var n int
	h.onLoop(func() { stamping, n = h.app.buf.stamping, len(h.app.natives) })
	if stamping || n != 0 {
		t.Fatalf("stamping %v, %d placements on a backend that hosts nothing", stamping, n)
	}
}

func equalRects(a, b []Rect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
