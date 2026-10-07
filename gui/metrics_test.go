package gui

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/unit"
	"github.com/yongjohnlee80/golib/tui"
)

var testFont = fontMetrics{advance: 0.6, ascent: 0.8, descent: 0.25}

func TestMeasureScales(t *testing.T) {
	for _, scale := range []float32{1, 1.25, 1.5, 2} {
		win := image.Pt(1000, 700)
		m := measure(testFont, 14, 0, win, unit.Metric{PxPerDp: scale, PxPerSp: scale})
		if m.cell.X < 1 || m.cell.Y < 1 {
			t.Fatalf("scale %v: cell %v", scale, m.cell)
		}
		// The grid is the whole cells that fit, never more.
		if want := (tui.Size{W: win.X / m.cell.X, H: win.Y / m.cell.Y}); m.grid != want {
			t.Errorf("scale %v: grid %v; want %v", scale, m.grid, want)
		}
		if m.grid.W*m.cell.X > win.X || m.grid.H*m.cell.Y > win.Y {
			t.Errorf("scale %v: grid %v of %v overflows the window %v", scale, m.grid, m.cell, win)
		}
		if m.baseline <= 0 || m.baseline >= m.cell.Y {
			t.Errorf("scale %v: baseline %d outside the cell height %d", scale, m.baseline, m.cell.Y)
		}
	}
	at1 := measure(testFont, 14, 0, image.Pt(1000, 700), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	at2 := measure(testFont, 14, 0, image.Pt(1000, 700), unit.Metric{PxPerDp: 2, PxPerSp: 2})
	if at2.cell.X < 2*at1.cell.X-1 {
		t.Errorf("cells at 2× (%v) are not twice those at 1× (%v)", at2.cell, at1.cell)
	}
}

func TestMeasureTinyWindow(t *testing.T) {
	m := measure(testFont, 14, 0, image.Pt(3, 3), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	if m.grid != (tui.Size{W: 1, H: 1}) {
		t.Fatalf("a window smaller than a cell has grid %v; want 1×1", m.grid)
	}
}

func TestCellAtClamps(t *testing.T) {
	m := metrics{cell: image.Pt(8, 16), grid: tui.Size{W: 10, H: 5}}
	cases := []struct {
		x, y   float32
		cx, cy int
	}{
		{0, 0, 0, 0},
		{7.9, 15.9, 0, 0},
		{8, 16, 1, 1},
		{79.5, 79.5, 9, 4},
		{-20, -20, 0, 0}, // a captured drag left of the window
		{500, 500, 9, 4}, // and past its far corner
	}
	for _, c := range cases {
		if cx, cy := m.cellAt(c.x, c.y); cx != c.cx || cy != c.cy {
			t.Errorf("cellAt(%v, %v) = %d, %d; want %d, %d", c.x, c.y, cx, cy, c.cx, c.cy)
		}
	}
}

func TestSameCellsIgnoresTheWindow(t *testing.T) {
	a := measure(testFont, 14, 0, image.Pt(1000, 700), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	b := measure(testFont, 14, 0, image.Pt(1003, 705), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	if a.grid == b.grid && !a.sameCells(b) {
		t.Fatal("a window resized within the same grid redraws every row")
	}
	c := measure(testFont, 14, 0, image.Pt(1000, 700), unit.Metric{PxPerDp: 2, PxPerSp: 2})
	if a.sameCells(c) {
		t.Fatal("a scale change kept the old cells")
	}
}

func TestPointerTranslate(t *testing.T) {
	m := metrics{cell: image.Pt(8, 16), grid: tui.Size{W: 100, H: 40}}
	var p pointerState
	at := func(kind pointer.Kind, b pointer.Buttons, x, y float32) pointer.Event {
		return pointer.Event{Kind: kind, Buttons: b, Position: f32.Pt(x, y)}
	}
	expect := func(got []tui.MouseEvent, want ...tui.MouseEvent) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %+v; want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("event %d: got %+v; want %+v", i, got[i], want[i])
			}
		}
	}
	expect(p.translate(at(pointer.Move, 0, 20, 40), m), tui.MouseEvent{Kind: tui.MouseMotion, X: 2, Y: 2})
	expect(p.translate(at(pointer.Press, pointer.ButtonPrimary, 20, 40), m), tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 2})
	expect(p.translate(at(pointer.Drag, pointer.ButtonPrimary, 40, 40), m), tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 5, Y: 2})
	// A second button pressed during the drag is reported as itself.
	expect(p.translate(at(pointer.Press, pointer.ButtonPrimary|pointer.ButtonSecondary, 40, 40), m), tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 5, Y: 2})
	expect(p.translate(at(pointer.Release, pointer.ButtonPrimary, 40, 40), m), tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 5, Y: 2})
	expect(p.translate(at(pointer.Release, 0, 40, 40), m), tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 5, Y: 2})
}

func TestPointerWheelSteps(t *testing.T) {
	m := metrics{cell: image.Pt(8, 16), grid: tui.Size{W: 100, H: 40}}
	var p pointerState
	scroll := func(dy float32) []tui.MouseEvent {
		return p.translate(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, dy), Position: f32.Pt(8, 16)}, m)
	}
	// A step is half a cell (8px): small touchpad deltas add up rather than vanish.
	if got := scroll(5); len(got) != 0 {
		t.Fatalf("5px scrolled = %+v; want no step yet", got)
	}
	if got := scroll(5); len(got) != 1 || got[0].Button != tui.WheelDown || got[0].X != 1 || got[0].Y != 1 {
		t.Fatalf("10px scrolled = %+v; want one WheelDown at 1,1", got)
	}
	if got := scroll(-34); len(got) != 4 || got[0].Button != tui.WheelUp {
		t.Fatalf("-34px scrolled = %+v; want four WheelUp", got)
	}
}

// Padding keeps the cells off the window's edge: the grid is what fits inside it, cell (0,0) starts
// at the padding, and a pointer maps back through it.
func TestMeasurePadding(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		win := image.Pt(1000, 700)
		m := measure(testFont, 14, 4, win, unit.Metric{PxPerDp: scale, PxPerSp: scale})
		pad := int(4*scale + 0.5)
		if m.origin != image.Pt(pad, pad) {
			t.Fatalf("scale %v: origin %v; want %d,%d", scale, m.origin, pad, pad)
		}
		if want := (tui.Size{W: (win.X - 2*pad) / m.cell.X, H: (win.Y - 2*pad) / m.cell.Y}); m.grid != want {
			t.Errorf("scale %v: grid %v; want %v", scale, m.grid, want)
		}
		if r := m.cellRect(0, 0, 1, 1); r.Min != m.origin {
			t.Errorf("scale %v: cell (0,0) at %v; want the origin %v", scale, r.Min, m.origin)
		}
		if x, y := m.cellAt(float32(pad), float32(pad)); x != 0 || y != 0 {
			t.Errorf("scale %v: the first pixel inside the padding is cell %d,%d", scale, x, y)
		}
		if x, y := m.cellAt(float32(pad+m.cell.X), float32(pad)); x != 1 || y != 0 {
			t.Errorf("scale %v: one cell in is cell %d,%d", scale, x, y)
		}
	}
}

// A glyph that fits is centred in its span; one wider than its span is scaled to the span's
// width, not clipped to part of itself.
func TestGlyphPlacementFitsTheSpan(t *testing.T) {
	m := metrics{cell: image.Pt(9, 18), baseline: 14}
	rect := image.Rect(90, 36, 99, 54) // one cell
	fit := glyphPlacement(rect, m, 7)
	if p := fit.Transform(f32.Pt(0, 0)); p != f32.Pt(91, 50) {
		t.Fatalf("a 7px glyph in a 9px cell starts at %v; want 91,50 (centred, on the baseline)", p)
	}
	wide := glyphPlacement(rect, m, 18) // a colour emoji in a one-cell span
	left, right := wide.Transform(f32.Pt(0, 0)), wide.Transform(f32.Pt(18, 0))
	if left.X != 90 || right.X != 99 {
		t.Fatalf("an 18px glyph spans %v..%v; want exactly the cell, 90..99", left.X, right.X)
	}
	if top := wide.Transform(f32.Pt(0, -14)); top.Y < 36 {
		t.Fatalf("the scaled glyph's top is at %v, above its cell (36)", top.Y)
	}
}
