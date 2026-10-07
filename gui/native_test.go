package gui

import (
	"context"
	"image"
	"image/color"
	"slices"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// styled is a TestBackend that is a NativeHost, styled by a real Backend's NativeFor, and keeps
// the latest frame's placements.
type styled struct {
	*tui.TestBackend
	b      *Backend
	mu     sync.Mutex
	placed []tui.NativePlacement
}

func (s *styled) NativeFor(c tui.Component) (any, tui.NativeScope, bool) { return s.b.NativeFor(c) }

func (s *styled) PlaceNatives(ps []tui.NativePlacement) {
	s.mu.Lock()
	s.placed = slices.Clone(ps)
	s.mu.Unlock()
}

func (s *styled) placements() []tui.NativePlacement {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.placed)
}

// styledApp runs root on a w×h styled backend with st.
func styledApp(t *testing.T, root tui.Component, w, h int, st *Style) (*tui.App, *styled) {
	t.Helper()
	sb := &styled{TestBackend: tui.NewTestBackend(w, h), b: &Backend{cfg: config{style: st}}}
	app := tui.NewApp(root, tui.WithBackend(sb))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	waitUntil(t, "a frame", func() bool { return sb.Flushes() > 0 })
	return app, sb
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 300 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// onLoop runs fn on the App's loop goroutine and waits for it.
func onLoop(t *testing.T, app *tui.App, fn func()) {
	t.Helper()
	done := make(chan struct{})
	app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the loop did not run the update")
	}
}

// paintOn paints p's view on a recording canvas over the App's cells, on the loop goroutine.
func paintOn(t *testing.T, app *tui.App, sb *styled, p tui.NativePlacement) *RecordingCanvas {
	t.Helper()
	cell := Size{W: 8, H: 16}
	rc := NewRecordingCanvas(Size{W: float32(p.Cols) * cell.W, H: float32(p.Rows) * cell.H}, cell)
	r := newRenderer(nil, "Go Mono", DefaultTheme())
	rc.Colors = func(col, row int) (fg, bg color.NRGBA) {
		snap := sb.Snapshot()
		return r.colors(snap[p.Y+row][p.X+col].Attrs)
	}
	onLoop(t, app, func() { p.View.(View).Paint(rc) })
	return rc
}

// placementOf is the placement of node id, or fails.
func placementOf(t *testing.T, sb *styled, id tui.NodeID) tui.NativePlacement {
	t.Helper()
	for _, p := range sb.placements() {
		if p.Node == id {
			return p
		}
	}
	t.Fatalf("node %d has no native placement; placed: %+v", id, sb.placements())
	return tui.NativePlacement{}
}

func TestStyleChoosesByTypeThenRole(t *testing.T) {
	btn := widget.NewButton("OK")
	byRole := func(tui.Component) (View, tui.NativeScope, bool) {
		return ViewFunc(func(Canvas) {}), tui.ScopeChrome, true
	}
	st := NewStyle().ForRole(tui.RolePushButton, byRole)
	if st.painterFor(btn) == nil {
		t.Fatal("a role registration did not match")
	}
	var typed bool
	ForType(st, func(*widget.Button) (View, tui.NativeScope, bool) { typed = true; return nil, 0, false })
	st.painterFor(btn)(btn)
	if !typed {
		t.Fatal("an exact-type registration did not beat the role's")
	}
	if CellStyle.painterFor(btn) != nil {
		t.Fatal("CellStyle painted a button")
	}
	if st.ForRole(tui.RolePushButton, nil); st.Clone().byRole[tui.RolePushButton] != nil {
		t.Fatal("ForRole(nil) left the painter")
	}
	a, b := NativeStyle(), NativeStyle()
	a.ForRole(tui.RolePushButton, nil)
	if b.painterFor(btn) == nil {
		t.Fatal("changing one NativeStyle changed another")
	}
}

func TestButtonAndFieldPlacements(t *testing.T) {
	btn := widget.NewButton("OK")
	in := widget.NewTextInput(widget.WithInitialValue("hello"))
	root := tui.NewFlex(tui.Vertical)
	root.Add(btn, in)
	app, sb := styledApp(t, root, 20, 4, NativeStyle())
	waitUntil(t, "two placements", func() bool { return len(sb.placements()) == 2 })

	bp := placementOf(t, sb, btn.NodeID())
	if bp.Scope != tui.ScopeSubtree || len(bp.Covered) != 0 {
		t.Fatalf("button placement %+v; want subtree scope, nothing covering it", bp)
	}
	ops := paintOn(t, app, sb, bp).Ops()
	if !slices.Equal(ops, []string{"FillRect", "FillRRect", "StrokeRRect", "DrawText"}) {
		t.Fatalf("button painted %v", ops)
	}

	fp := placementOf(t, sb, in.NodeID())
	rc := paintOn(t, app, sb, fp)
	var cellsCall *PaintCall
	for i := range rc.Calls {
		if rc.Calls[i].Op == "PaintCells" {
			cellsCall = &rc.Calls[i]
		}
	}
	if cellsCall == nil || !cellsCall.GlyphsOnly || cellsCall.Cells != (CellRect{W: fp.Cols, H: fp.Rows}) {
		t.Fatalf("the field did not keep its text in its cells: %+v", rc.Calls)
	}
}

// The selection is tui's own cells at the columns tui wrote them, after the field's scroll.
func TestTextFieldSelectionKeepsTuisColumns(t *testing.T) {
	in := widget.NewTextInput()
	root := tui.NewFlex(tui.Vertical)
	root.Add(in)
	app, sb := styledApp(t, root, 20, 1, NativeStyle())
	sb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	for _, r := range "abcdefghijklmnopqrstuvwxy" {
		sb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)})
	}
	for range 3 {
		sb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft, Mods: tui.ModShift})
	}
	waitUntil(t, "the selection", func() bool {
		var ok bool
		onLoop(t, app, func() { _, _, ok = in.Selection(); ok = ok && in.Scroll() == 6 })
		return ok
	})
	rc := paintOn(t, app, sb, placementOf(t, sb, in.NodeID()))
	want := PaintCall{Op: "PaintCells", Cells: CellRect{X: 16, W: 3, H: 1}}
	if !slices.ContainsFunc(rc.Calls, func(p PaintCall) bool { return p.Op == want.Op && p.Cells == want.Cells && !p.GlyphsOnly }) {
		t.Fatalf("the selection (clusters 22-25, scrolled by 6) was not drawn at cells 16-19: %+v", rc.Calls)
	}
}

type fakeCheck struct {
	tui.MultiChild
	checked bool
}

func (f *fakeCheck) Layout(cs tui.Constraints) tui.Size { return cs.Constrain(tui.Size{W: 10, H: 1}) }
func (f *fakeCheck) Render(tui.Surface)                 {}
func (f *fakeCheck) HandleEvent(tui.Event) bool         { return false }
func (f *fakeCheck) AccessibleRole() tui.AccessibleRole { return tui.RoleCheckBox }
func (f *fakeCheck) Checked() bool                      { return f.checked }

// The box is found in the cells, wherever centring put it: here one cell in, " [x] Lexical".
func TestCheckBoxDrawsItsBoxAndKeepsItsText(t *testing.T) {
	row := []string{" ", "[", "x", "]", " ", "L", "e", "x", "i", "c"}
	paint := func(checked bool) *RecordingCanvas {
		v, scope, ok := paintCheckBox(&fakeCheck{checked: checked})
		if !ok || scope != tui.ScopeSubtree {
			t.Fatalf("ok=%v scope=%v", ok, scope)
		}
		rc := NewRecordingCanvas(Size{W: 80, H: 16}, Size{W: 8, H: 16})
		rc.Content = func(col, r int) string {
			if r == 0 && col >= 0 && col < len(row) {
				return row[col]
			}
			return ""
		}
		v.Paint(rc)
		return rc
	}
	rc := paint(true)
	if !slices.Contains(rc.Ops(), "StrokePath") {
		t.Fatalf("a checked box has no tick: %v", rc.Ops())
	}
	var kept []CellRect
	for _, p := range rc.Calls {
		if p.Op == "PaintCells" {
			kept = append(kept, p.Cells)
		}
		if p.Op == "FillRect" && p.Rect.X != 8 {
			t.Fatalf("the box is drawn at x=%v; want over the [ at cell 1 (x=8)", p.Rect.X)
		}
	}
	if !slices.Equal(kept, []CellRect{{W: 1, H: 1}, {X: 4, W: 6, H: 1}}) {
		t.Fatalf("kept cells %+v; want everything but the box's three", kept)
	}
	if slices.Contains(paint(false).Ops(), "StrokePath") {
		t.Fatal("an unchecked box has a tick")
	}
}

func TestScrollbarOnlyWhenContentOverflows(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e", "f"}
	l := widget.NewList(widget.WithSource(widget.SliceSource(items), func(s string) string { return s }))
	root := tui.NewFlex(tui.Vertical)
	root.Add(l)
	app, sb := styledApp(t, root, 10, 3, NativeStyle())
	p := placementOf(t, sb, l.NodeID())
	rc := paintOn(t, app, sb, p)
	if len(rc.Calls) != 1 || rc.Calls[0].Op != "FillRRect" || rc.Calls[0].Rect.X+rc.Calls[0].Rect.W > 80 {
		t.Fatalf("scrollbar painted %+v", rc.Calls)
	}
	if _, _, ok := paintScrollbar(widget.NewList(widget.WithSource(widget.SliceSource([]string{"a"}), func(s string) string { return s }))); ok {
		t.Fatal("a list that fits has a scrollbar")
	}
}

// A Modal over a native button covers it: the dialog's cells are written after the button began.
func TestModalCoversANativeButton(t *testing.T) {
	btn := widget.NewButton("Under")
	host := widget.NewOverlayHost(btn)
	app, sb := styledApp(t, host, 30, 8, NativeStyle())
	waitUntil(t, "the button's placement", func() bool { return len(sb.placements()) > 0 })
	m := widget.NewModal(widget.NewText("over"), widget.WithModalTitle("Dialog"))
	onLoop(t, app, func() {
		if err := m.Open(host); err != nil {
			t.Error(err)
		}
	})
	waitUntil(t, "the button covered", func() bool {
		for _, p := range sb.placements() {
			if p.Node == btn.NodeID() && len(p.Covered) > 0 {
				return true
			}
		}
		return false
	})
}

// The Gio canvas records every primitive into ops without a window.
func TestGioCanvasRecords(t *testing.T) {
	m := measure(testFont, 14, 0, image.Pt(200, 100), unit.Metric{PxPerDp: 2, PxPerSp: 2})
	var g grid
	g.resize(m.grid.W, m.grid.H)
	g.apply([]tui.CellUpdate{{X: 1, Y: 0, Cell: cell("a")}, {X: 2, Y: 0, Cell: tui.Cell{Content: "한", Width: 2}}})
	r := newRenderer(newTestShaper(), "Go Mono", DefaultTheme())
	r.m = m
	ops := new(op.Ops)
	c := newGioCanvas(ops, r, &g, m, CellRect{X: 1, W: 4, H: 1})
	if c.Size() != (Size{W: 4 * float32(m.cell.X) / 2, H: float32(m.cell.Y) / 2}) || c.CellSize().W != float32(m.cell.X)/2 {
		t.Fatalf("canvas %v, cell %v, for 4 cells at scale 2", c.Size(), c.CellSize())
	}
	c.PushOpacity(0.5)
	c.PushTransform(Affine{}.Offset(f32pt(1, 1)))
	c.FillRRect(Rect{W: 10, H: 5}, 2, Solid(color.NRGBA{A: 255}))
	c.StrokeRRect(Rect{W: 10, H: 5}, 2, 1, Brush{Gradient: &LinearGradient{To: Pt(10, 0), ToColor: color.NRGBA{R: 255, A: 255}}})
	c.FillEllipse(Rect{W: 4, H: 4}, Solid(color.NRGBA{A: 255}))
	c.DrawText(c.Text().Layout("Hi there", Font{Size: 12}, 20), Pt(0, 0), Solid(color.NRGBA{A: 255}))
	c.Pop()
	c.Pop()
	c.Sub(Rect{X: 2, Y: 0, W: 4, H: 4}).FillRect(Rect{W: 100, H: 100}, Solid(color.NRGBA{A: 255}))
	c.PaintCells(CellRect{X: 2, W: 1, H: 1}, false) // the continuation half of 한: drawn from its head
	if fg, _ := c.CellColors(0, 0); fg != DefaultTheme().FG {
		t.Fatalf("CellColors(0,0) fg = %v; want the theme's default", fg)
	}
	if fg, bg := c.CellColors(99, 0); fg != (color.NRGBA{}) || bg != (color.NRGBA{}) {
		t.Fatal("a cell outside the grid has colours")
	}
	if len(c.stack) != 0 {
		t.Fatal("Push and Pop do not balance")
	}
}

func TestTextLayoutWraps(t *testing.T) {
	ts := &TextShaper{s: newTestShaper(), scale: 1}
	one := ts.Layout("hello world", Font{Size: 12}, 0)
	two := ts.Layout("hello world", Font{Size: 12}, one.Width*0.7)
	if len(one.lines) != 1 || len(two.lines) != 2 {
		t.Fatalf("lines %d and %d; want 1 and 2", len(one.lines), len(two.lines))
	}
	if two.Height <= one.Height || two.Width >= one.Width {
		t.Fatalf("wrapped %vx%v vs one line %vx%v", two.Width, two.Height, one.Width, one.Height)
	}
}

func TestSubCell(t *testing.T) {
	m := metrics{origin: image.Pt(4, 4), cell: image.Pt(10, 20), grid: tui.Size{W: 5, H: 5}}
	x, y := m.cellAt(4+15, 4+30)
	sx, sy := m.subCell(4+15, 4+30, x, y)
	if x != 1 || y != 1 || sx != 0.5 || sy != 0.5 {
		t.Fatalf("cell %d,%d sub %v,%v; want 1,1 and 0.5,0.5", x, y, sx, sy)
	}
	x, y = m.cellAt(500, 500) // past the edge: the edge cell, at its far side
	if sx, sy = m.subCell(500, 500, x, y); sx >= 1 || sy >= 1 {
		t.Fatalf("sub %v,%v past the edge; want below 1", sx, sy)
	}
}

func newTestShaper() *text.Shaper {
	return text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
}

func f32pt(x, y float32) f32.Point { return f32.Pt(x, y) }

// The backdrop is what borders the view: here a blue panel around a one-row field, not the
// field's own red cells, and the page's colour where nothing borders it.
func TestBackdropIsWhatSurroundsTheView(t *testing.T) {
	blue := tui.CellAttrs{BG: tui.CellColor{Kind: tui.CellColorRGB, B: 200}}
	red := tui.CellAttrs{BG: tui.CellColor{Kind: tui.CellColorRGB, R: 200}}
	var g grid
	g.resize(6, 3)
	var diff []tui.CellUpdate
	for y := range 3 {
		for x := range 6 {
			a := blue
			if y == 1 && x >= 1 && x <= 4 {
				a = red
			}
			diff = append(diff, tui.CellUpdate{X: x, Y: y, Cell: tui.Cell{Content: " ", Width: 1, Attrs: a}})
		}
	}
	g.apply(diff)
	r := newRenderer(nil, "Go Mono", DefaultTheme())
	r.pageBG = color.NRGBA{G: 9, A: 255}
	if got := r.backdrop(&g, CellRect{X: 1, Y: 1, W: 4, H: 1}); got != (color.NRGBA{B: 200, A: 255}) {
		t.Fatalf("backdrop %v; want the blue around the field", got)
	}
	if got := r.backdrop(&g, CellRect{W: 6, H: 3}); got != r.pageBG {
		t.Fatalf("backdrop of the whole grid %v; want the page's", got)
	}
}
