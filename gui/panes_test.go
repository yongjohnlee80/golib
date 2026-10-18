package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// stubPane is a pane's component: it lays out to whatever it is given.
type stubPane struct{ name string }

func (*stubPane) Init(*tui.Context)                 {}
func (*stubPane) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*stubPane) Render(tui.Surface)                {}
func (*stubPane) HandleEvent(tui.Event) bool        { return false }

// sidebarAndView is the example's shape: a 100dp cell sidebar beside a flex pixel view, over a
// flex cell log below a 40dp cell status bar.
func sidebarAndView() *Panes {
	return NewPanes(Column(
		Sized(Flex(1), Row(
			Sized(Fixed(100), Cells(&stubPane{"side"})),
			Sized(Flex(1), Pixels(&stubPane{"view"})),
		)),
		Sized(Fixed(40), Cells(&stubPane{"status"})),
	))
}

func TestFitTilesFixedAndFlexTracks(t *testing.T) {
	p := sidebarAndView()
	var gaps []image.Rectangle
	rects := fit(p.root, image.Rect(0, 0, 503, 301), 1.5, nil, &gaps)
	want := []image.Rectangle{
		image.Rect(0, 0, 150, 241),   // 100dp at 1.5
		image.Rect(150, 0, 503, 241), // the rest of the row
		image.Rect(0, 241, 503, 301), // 40dp at 1.5, at the bottom
	}
	if len(rects) != len(want) {
		t.Fatalf("rects %v", rects)
	}
	for i := range want {
		if rects[i] != want[i] {
			t.Errorf("pane %d at %v, want %v", i, rects[i], want[i])
		}
	}
	if len(gaps) != 0 {
		t.Errorf("gaps %v in a tree with flex tracks", gaps)
	}
}

func TestFitFlexSharesByWeightAndTheLastTakesTheRemainder(t *testing.T) {
	root := Row(Sized(Flex(1), Cells(&stubPane{})), Sized(Flex(2), Cells(&stubPane{})))
	rects := fit(root, image.Rect(0, 0, 100, 10), 1, nil, new([]image.Rectangle))
	if rects[0].Dx() != 33 || rects[1].Dx() != 67 || rects[1].Max.X != 100 {
		t.Fatalf("flex 1:2 of 100 is %v", rects)
	}
}

func TestFitOverconstrainedFixedTracksShrinkInOrder(t *testing.T) {
	root := Row(Sized(Fixed(80), Cells(&stubPane{})), Sized(Fixed(80), Cells(&stubPane{})), Sized(Fixed(80), Cells(&stubPane{})))
	var gaps []image.Rectangle
	rects := fit(root, image.Rect(0, 0, 100, 10), 1, nil, &gaps)
	if rects[0].Dx() != 80 || rects[1].Dx() != 20 || !rects[2].Empty() {
		t.Fatalf("80+80+80 in 100 is %v; want 80, 20, 0", rects)
	}
	if len(gaps) != 0 {
		t.Fatalf("gaps %v when the tracks overfill", gaps)
	}
}

func TestFitReportsTheSpareOfATreeWithoutFlex(t *testing.T) {
	root := Column(Sized(Fixed(30), Cells(&stubPane{})))
	var gaps []image.Rectangle
	fit(root, image.Rect(5, 5, 105, 105), 1, nil, &gaps)
	if len(gaps) != 1 || gaps[0] != image.Rect(5, 35, 105, 105) {
		t.Fatalf("gaps %v; want the 70px below the one fixed track", gaps)
	}
}

func TestBandsFloorCellsCeilPixelsAndPackAsTheTree(t *testing.T) {
	p := sidebarAndView()
	rects := []image.Rectangle{image.Rect(0, 0, 150, 241), image.Rect(150, 0, 503, 241), image.Rect(0, 241, 503, 301)}
	grid, panes := bands(p.root, rects, image.Pt(16, 32))
	// side: 150/16 = 9 (floor) × 241/32 = 7; view: ceil(353/16) = 23 × ceil(241/32) = 8;
	// the row's band is 32 × 8, the status bar below it 503/16 = 31 × 60/32 = 1.
	want := []tui.Rect{{X: 0, Y: 0, W: 9, H: 7}, {X: 9, Y: 0, W: 23, H: 8}, {X: 0, Y: 8, W: 31, H: 1}}
	for i := range want {
		if panes[i].band != want[i] {
			t.Errorf("pane %d band %v, want %v", i, panes[i].band, want[i])
		}
		if panes[i].origin != rects[i].Min || panes[i].clip != rects[i] {
			t.Errorf("pane %d origin %v clip %v, want its rect %v", i, panes[i].origin, panes[i].clip, rects[i])
		}
	}
	if grid != (tui.Size{W: 32, H: 9}) {
		t.Fatalf("grid %v, want 32x9", grid)
	}
	if !panes[1].pixels || panes[0].pixels {
		t.Fatal("pane kinds lost")
	}
}

// stagedMetrics is a snapshot of sidebarAndView in a 503×301 window at scale 1.5, 16×32 cells.
func stagedMetrics(t *testing.T) metrics {
	t.Helper()
	m := metrics{window: image.Pt(503, 301), scale: 1.5, cell: image.Pt(16, 32), baseline: 24}
	return placePanes(sidebarAndView(), m, 0)
}

func TestNoLayoutMapsAsTheSingleGridDid(t *testing.T) {
	m := metrics{window: image.Pt(200, 100), origin: image.Pt(4, 6), cell: image.Pt(10, 20), grid: tui.Size{W: 19, H: 4}}
	for _, c := range []struct{ x, y, w, h int }{{0, 0, 1, 1}, {3, 2, 5, 1}, {18, 3, 1, 1}, {19, 4, 2, 2}, {-1, -1, 1, 1}} {
		want := image.Rect(c.x*10, c.y*20, (c.x+c.w)*10, (c.y+c.h)*20).Add(m.origin)
		if got := m.cellRect(c.x, c.y, c.w, c.h); got != want {
			t.Errorf("cellRect%v = %v, want %v", c, got, want)
		}
	}
	if p, ok := m.cellPoint(2, 1); !ok || p != image.Pt(24, 26) {
		t.Errorf("cellPoint(2,1) = %v %v", p, ok)
	}
	if fs := m.fragments(tui.Rect{X: 1, Y: 1, W: 3, H: 2}); len(fs) != 1 || fs[0].dev != m.cellRect(1, 1, 3, 2) {
		t.Errorf("fragments without a layout %v", fs)
	}
}

func TestPanesMapEachBandAtItsPanesOrigin(t *testing.T) {
	m := stagedMetrics(t)
	view := m.layout.panes[1]
	if got := m.cellRect(9, 0, 1, 1); got != image.Rect(150, 0, 166, 32) {
		t.Errorf("the view's first cell at %v; want the view pane's origin (150, 0)", got)
	}
	if got := m.cellRect(8, 0, 1, 1); got != image.Rect(128, 0, 144, 32) {
		t.Errorf("the sidebar's last cell at %v", got)
	}
	if got := m.cellRect(0, 8, 2, 1); got != image.Rect(0, 241, 32, 273) {
		t.Errorf("the status bar's first cells at %v; want at its pane's top, 241", got)
	}
	if p, ok := m.cellPoint(9, 0); !ok || p != view.origin {
		t.Errorf("cellPoint at the view's origin = %v %v; want %v (a zero-size caret keeps its origin)", p, ok, view.origin)
	}
	if _, ok := m.pane(8, 7); ok {
		t.Error("cell (8, 7) is a hole (the sidebar's band is 7 rows, the row's 8); pane reported it")
	}
	if _, ok := m.cellPoint(8, 7); ok {
		t.Error("cellPoint in a hole reported a position")
	}
}

func TestCellAtClampsIntoThePaneUnderThePointer(t *testing.T) {
	m := stagedMetrics(t)
	cases := []struct {
		x, y   float32
		cx, cy int
	}{
		{0, 0, 0, 0},
		{149, 10, 8, 0},   // the sidebar's leftover pixels (144..150): its last cell
		{150, 10, 9, 0},   // the view's first pixel: its band's first cell
		{502, 240, 31, 7}, // the view's last pixel
		{10, 250, 0, 8},   // the status bar
		{-50, 400, 0, 8},  // past the window: the nearest pane
	}
	for _, c := range cases {
		if cx, cy := m.cellAt(c.x, c.y); cx != c.cx || cy != c.cy {
			t.Errorf("cellAt(%v, %v) = %d, %d; want %d, %d", c.x, c.y, cx, cy, c.cx, c.cy)
		}
	}
	if sx, _ := m.subCell(158, 10, 9, 0); sx != 0.5 {
		t.Errorf("subCell half into the view's first cell = %v, want 0.5", sx)
	}
}

func TestFragmentsSplitARectByBandAndSkipHoles(t *testing.T) {
	m := stagedMetrics(t)
	// Rows 6..8, columns 7..11: the sidebar's last row, the view's, the hole (8, 7), the bar's.
	fs := m.fragments(tui.Rect{X: 7, Y: 6, W: 5, H: 3})
	want := map[tui.Rect]image.Rectangle{
		{X: 7, Y: 6, W: 2, H: 1}: image.Rect(112, 192, 144, 224),
		{X: 9, Y: 6, W: 3, H: 2}: image.Rect(150, 192, 198, 256),
		{X: 7, Y: 8, W: 5, H: 1}: image.Rect(112, 241, 192, 273),
	}
	if len(fs) != len(want) {
		t.Fatalf("fragments %+v", fs)
	}
	for _, f := range fs {
		if dev, ok := want[f.cells]; !ok || dev != f.dev {
			t.Errorf("fragment %v at %v; want %v", f.cells, f.dev, dev)
		}
	}
}

func TestAnchoredClipsANativeToItsAnchorPane(t *testing.T) {
	m := stagedMetrics(t)
	side, view := m.layout.panes[0], m.layout.panes[1]
	dev, clip, ok := m.anchored(view.band)
	if !ok || clip != view.clip || dev.Intersect(clip) != view.clip {
		t.Fatalf("the view's own band: dev %v clip %v; want exactly its pane %v", dev, clip, view.clip)
	}
	// A dialog from the sidebar across the view: drawn from its top-left band, cut at the sidebar.
	dev, clip, ok = m.anchored(tui.Rect{X: 5, Y: 2, W: 10, H: 3})
	if !ok || clip != side.clip {
		t.Fatalf("a placement across bands clips to %v; want its anchor pane %v (ADR 1792306654 §4.3c)", clip, side.clip)
	}
	if dev != image.Rect(80, 64, 240, 160) || dev.Intersect(clip) != image.Rect(80, 64, 150, 160) {
		t.Fatalf("crossing native at %v, visible %v; want from its anchor band, cut at x=150", dev, dev.Intersect(clip))
	}
}

// Lector #359 R3: a Panes window's CellPixels is the admitted snapshot's cell, the one its
// layout and paint use, not a newer published one.
func TestCellPixelsIsTheAdmittedSnapshots(t *testing.T) {
	p := sidebarAndView()
	b := NewBackend(WithPanes(p))
	small := metrics{window: image.Pt(503, 301), scale: 1.5, ppem: 24, cell: image.Pt(16, 32), baseline: 24}
	a := b.publishLayout(nil, placePanes(p, small, 0))
	b.admitLayout(a.grid)
	big := small
	big.cell, big.ppem = image.Pt(20, 40), 30 // a zoom, published but not yet admitted
	nb := b.publishLayout(&a, placePanes(p, big, 0))
	b.metrics.Store(&nb)
	if got, want := b.CellPixels(), (tui.CellPixels{W: 16 / 1.5, H: 32 / 1.5}); got != want {
		t.Fatalf("CellPixels %v; want the admitted %v, not the newest", got, want)
	}
	b.admitLayout(nb.grid) // positive control: once admitted, the zoom is what it reports
	if got, want := b.CellPixels(), (tui.CellPixels{W: 20 / 1.5, H: 40 / 1.5}); got != want {
		t.Fatalf("after admission CellPixels %v; want %v", got, want)
	}
}

func TestADragMapsThroughThePaneItsPressLandedIn(t *testing.T) {
	m := stagedMetrics(t)
	var p pointerState
	press := p.translate(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(140, 10)}, m)
	if len(press) != 1 || press[0].X != 8 {
		t.Fatalf("press %+v", press)
	}
	// Dragged into the view: cells keep counting in the sidebar's mapping, 160/16 = 10.
	drag := p.translate(pointer.Event{Kind: pointer.Drag, Buttons: pointer.ButtonPrimary, Position: f32.Pt(160, 10)}, m)
	if len(drag) != 1 || drag[0].X != 10 {
		t.Fatalf("drag past the sidebar maps to %+v; want column 10 in the press pane's mapping", drag)
	}
	p.translate(pointer.Event{Kind: pointer.Release, Position: f32.Pt(160, 10)}, m)
	move := p.translate(pointer.Event{Kind: pointer.Move, Position: f32.Pt(160, 10)}, m)
	if len(move) != 1 || move[0].X != 9 {
		t.Fatalf("after the release a move maps to %+v; want the view's own column 9", move)
	}
}

// Lector r0 R1: two layouts of the same grid are told apart by generation, and a frame is drawn
// only with the snapshot its layout was admitted with.
func TestALayoutIsPairedWithItsPaintByGenerationNotSize(t *testing.T) {
	p := sidebarAndView()
	b := NewBackend(WithPanes(p))
	base := metrics{scale: 1.5, cell: image.Pt(16, 32), baseline: 24}
	snap := func(w, h int) metrics {
		m := base
		m.window = image.Pt(w, h)
		return placePanes(p, m, 0)
	}
	a := b.publishLayout(nil, snap(503, 301))
	la := b.admitLayout(a.grid)
	if la == nil || la.gen != a.layout.gen {
		t.Fatalf("admitted %v; want A", la)
	}
	// B: a wider sidebar's worth of window, the same virtual grid (32×9) but the view moved.
	bm := snap(510, 301)
	if bm.grid != a.grid || bm.layout.sameShape(a.layout) {
		t.Fatalf("fixture: B must keep A's grid %v (got %v) and move a band or origin", a.grid, bm.grid)
	}
	bm = b.publishLayout(&a, bm)
	if bm.layout.gen == a.layout.gen {
		t.Fatal("B was not given a new generation")
	}
	// A Flush the App laid out with A arrives after B is published: it is drawn with A.
	b.grid.resize(a.grid.W, a.grid.H)
	if err := b.FlushSized(a.grid, nil); err != nil {
		t.Fatal(err)
	}
	if b.latest == nil || b.latest.m.layout != a.layout {
		t.Fatalf("the old-layout Flush drew with gen %d; want A's %d", b.latest.m.layout.gen, a.layout.gen)
	}
	// The App lays out again (B's ResizeEvent): B is admitted and the next Flush draws with it.
	if l := b.admitLayout(bm.grid); l != bm.layout {
		t.Fatalf("admitted gen %d; want B's %d", l.gen, bm.layout.gen)
	}
	if err := b.FlushSized(bm.grid, nil); err != nil {
		t.Fatal(err)
	}
	if b.latest.m.layout != bm.layout {
		t.Fatal("the Flush after B's admission did not draw with B")
	}
}

func TestCoalescedResizesAdmitTheNewestOfTheAppsSize(t *testing.T) {
	p := sidebarAndView()
	b := NewBackend(WithPanes(p))
	base := metrics{scale: 1.5, cell: image.Pt(16, 32), baseline: 24}
	snap := func(w, h int) metrics {
		m := base
		m.window = image.Pt(w, h)
		return placePanes(p, m, 0)
	}
	a1 := b.publishLayout(nil, snap(503, 301))
	bb := b.publishLayout(&a1, snap(700, 301))
	a2 := b.publishLayout(&bb, snap(510, 301)) // A's grid again, other origins
	if a2.grid != a1.grid || bb.grid == a1.grid {
		t.Fatalf("fixture grids %v %v %v", a1.grid, bb.grid, a2.grid)
	}
	if l := b.admitLayout(a1.grid); l != a2.layout {
		t.Fatalf("A→B→A admitted gen %d; want the last A's %d", l.gen, a2.layout.gen)
	}
	if len(b.stage.published) != 0 {
		t.Fatalf("%d snapshots left after admitting the newest", len(b.stage.published))
	}
	// A then C published, the App still at A's size (C's ResizeEvent not yet drained): A is
	// admitted, not the newer C of another size, and C waits for its own Layout.
	a3 := b.publishLayout(&a2, snap(520, 301))
	c := b.publishLayout(&a3, snap(800, 301))
	if c.grid == a3.grid {
		t.Fatal("fixture: C must have another grid")
	}
	if l := b.admitLayout(a3.grid); l != a3.layout {
		t.Fatalf("admitted gen %d at A's size; want A's %d, not the newer C's %d", l.gen, a3.layout.gen, c.layout.gen)
	}
	if l := b.admitLayout(c.grid); l != c.layout {
		t.Fatal("C was not kept for its own Layout")
	}
}

func TestAFlushForAnUnadmittedSizeIsNotPresented(t *testing.T) {
	p := sidebarAndView()
	b := NewBackend(WithPanes(p))
	m := metrics{window: image.Pt(503, 301), scale: 1.5, cell: image.Pt(16, 32), baseline: 24}
	a := b.publishLayout(nil, placePanes(p, m, 0))
	b.admitLayout(a.grid)
	other := tui.Size{W: a.grid.W + 3, H: a.grid.H}
	if err := b.FlushSized(other, nil); err != nil {
		t.Fatal(err)
	}
	if b.latest != nil {
		t.Fatal("a Flush laid out for a size no admitted snapshot has was presented")
	}
	if b.grid.w != other.W {
		t.Fatalf("the grid did not follow the App's size: %d columns, want %d", b.grid.w, other.W)
	}
	// Positive control: the admitted size presents.
	if err := b.FlushSized(a.grid, nil); err != nil || b.latest == nil {
		t.Fatalf("the admitted size was not presented (err %v)", err)
	}
}

func TestAPixelsPanesCanvasIsExactlyItsPane(t *testing.T) {
	m := stagedMetrics(t)
	view := m.layout.panes[1]
	r := newRenderer(newTestShaper(), "Go Mono", DefaultTheme())
	g := &grid{}
	g.resize(m.grid.W, m.grid.H)
	c := newGioCanvas(new(op.Ops), r, g, m, CellRect{X: view.band.X, Y: view.band.Y, W: view.band.W, H: view.band.H}, nil)
	want := Size{W: float32(view.clip.Dx()) / m.scale, H: float32(view.clip.Dy()) / m.scale}
	if c.Size() != want || c.origin != f32.Pt(float32(view.origin.X), float32(view.origin.Y)) {
		t.Fatalf("canvas %v at %v; want its pane, %v at %v (its band is rounded up past it)", c.Size(), c.origin, want, view.origin)
	}
}

func TestACursorInAHoleTellsNoCaret(t *testing.T) {
	m := stagedMetrics(t)
	b := NewBackend()
	b.grid.resize(m.grid.W, m.grid.H)
	b.cursor = cursorState{visible: true, x: 8, y: 7} // the hole beside the view
	var f frame
	b.frameCaret(&f, m)
	if !f.caret.Empty() {
		t.Fatalf("a cursor in a hole told the IME %v", f.caret)
	}
	b.cursor = cursorState{visible: true, x: 9, y: 0} // positive control: the view's first cell
	b.frameCaret(&f, m)
	if f.caret != image.Rect(150, 0, 166, 32) {
		t.Fatalf("the caret in the view's first cell is %v; want at the view's origin", f.caret)
	}
}

// Each pane's cells are drawn at its own origin: a cell coloured red in the view's first column
// lands at the view's left edge, not one cell after the sidebar's last, and the sidebar's leftover
// pixels between them are the page colour. Needs a headless GPU (skips without one).
func TestEachBandIsDrawnAtItsPanesOrigin(t *testing.T) {
	m := stagedMetrics(t)
	hw, err := headless.NewWindow(m.window.X, m.window.Y)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	m.ppem = 24
	r := newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	g := &grid{}
	g.resize(m.grid.W, m.grid.H)
	red := tui.CellColor{Kind: tui.CellColorRGB, R: 0xff}
	g.cells[0*g.w+9] = tui.Cell{Content: " ", Width: 1, Attrs: tui.CellAttrs{BG: red}} // the view's (0, 0)
	g.cells[0*g.w+8] = tui.Cell{Content: " ", Width: 1, Attrs: tui.CellAttrs{BG: red}} // the sidebar's last
	ops := new(op.Ops)
	r.frame(g, m, cursorState{}, nil, nil, true).Add(ops)
	if err := hw.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: m.window})
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	isRed := func(x, y int) bool { c := img.RGBAAt(x, y); return c.R > 0xe0 && c.G < 0x20 && c.B < 0x20 }
	for _, c := range []struct {
		x, y int
		red  bool
		what string
	}{
		{150, 10, true, "the view's first cell, at its pane's left edge"},
		{165, 10, true, "the view's first cell, its right side"},
		{166, 10, false, "the view's second cell"},
		{130, 10, true, "the sidebar's last cell"},
		{147, 10, false, "the sidebar's leftover pixels (144..150)"},
	} {
		if isRed(c.x, c.y) != c.red {
			t.Errorf("(%d, %d), %s: red %v, want %v", c.x, c.y, c.what, !c.red, c.red)
		}
	}
}

// Lector #359: a font change that leaves every band where it was is a new snapshot, admitted and
// painted with, not deduplicated by its pane shape.
func TestAFontChangeWithTheSameBandsIsANewSnapshot(t *testing.T) {
	p := sidebarAndView()
	b := NewBackend(WithPanes(p))
	m := metrics{window: image.Pt(503, 301), scale: 1.5, ppem: 24, cell: image.Pt(16, 32), baseline: 24, fontGen: 1}
	a := b.publishLayout(nil, placePanes(p, m, 0))
	b.admitLayout(a.grid)
	m.ppem, m.fontGen, m.baseline = 23, 2, 23 // another face at the same cell size
	f := b.publishLayout(&a, placePanes(p, m, 0))
	if !f.layout.sameShape(a.layout) {
		t.Fatal("fixture: the bands must not move")
	}
	if f.layout.gen == a.layout.gen {
		t.Fatal("the font change kept A's generation: it is never admitted")
	}
	b.admitLayout(f.grid)
	if err := b.FlushSized(f.grid, nil); err != nil {
		t.Fatal(err)
	}
	if b.latest.m.fontGen != 2 || b.latest.m.ppem != 23 {
		t.Fatalf("painted with font gen %d ppem %v; want the new face's 2, 23", b.latest.m.fontGen, b.latest.m.ppem)
	}
	// Positive control: the same snapshot again keeps its generation.
	if g := b.publishLayout(&f, placePanes(p, m, 0)); g.layout != f.layout {
		t.Fatal("an unchanged snapshot was given a new generation")
	}
}

// Lector #359: a Panes tree is fixed once built. Replacing an item in the caller's slice afterwards
// changes neither the panes Layout places nor the tree fit resolves.
func TestATreeIsFixedAgainstItsCallersSlice(t *testing.T) {
	for _, c := range []struct {
		name  string
		build func(...Item) Node
		len   func(image.Rectangle) int
	}{
		{"Row", Row, func(r image.Rectangle) int { return r.Dx() }},
		{"Column", Column, func(r image.Rectangle) int { return r.Dy() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			side, view := &stubPane{"side"}, &stubPane{"view"}
			items := []Item{Sized(Fixed(100), Cells(side)), Sized(Flex(1), Pixels(view))}
			p := NewPanes(c.build(items...))
			items[0] = Sized(Fixed(300), Pixels(&stubPane{"intruder"}))
			rects := fit(p.root, image.Rect(0, 0, 500, 500), 1, nil, new([]image.Rectangle))
			if len(p.leaves) != 2 || p.leaves[0].c != side || c.len(rects[0]) != 100 {
				t.Fatalf("the tree followed its caller's slice: leaf 0 %v, pane 0 %v", p.leaves[0].c, rects[0])
			}
			_, panes := bands(p.root, rects, image.Pt(10, 20))
			if panes[0].pixels {
				t.Fatal("pane 0 became a Pixels pane after construction")
			}
		})
	}
}
