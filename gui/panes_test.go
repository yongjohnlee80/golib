package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/io/event"
	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
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
		t.Fatalf("a placement across bands clips to %v; want its anchor pane %v", clip, side.clip)
	}
	if dev != image.Rect(80, 64, 240, 160) || dev.Intersect(clip) != image.Rect(80, 64, 150, 160) {
		t.Fatalf("crossing native at %v, visible %v; want from its anchor band, cut at x=150", dev, dev.Intersect(clip))
	}
}

// A Panes window's CellPixels is the admitted snapshot's cell, the one its
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

// Two layouts of the same grid are told apart by generation, and a frame is drawn
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
	c := newGioCanvas(new(op.Ops), r, g, m, CellRect{X: view.band.X, Y: view.band.Y, W: view.band.W, H: view.band.H}, tui.Rect{}, nil)
	want := Size{W: float32(view.clip.Dx()) / m.scale, H: float32(view.clip.Dy()) / m.scale}
	if c.Size() != want || c.origin != f32.Pt(float32(view.origin.X), float32(view.origin.Y)) {
		t.Fatalf("canvas %v at %v; want its pane, %v at %v (its band is rounded up past it)", c.Size(), c.origin, want, view.origin)
	}
}

func TestACursorInAHoleTellsNoCaret(t *testing.T) {
	m := stagedMetrics(t)
	b := NewBackend()
	b.grid.resize(m.grid.W, m.grid.H)
	var f frame
	b.frameCaret(&f, m, cursorState{visible: true, x: 8, y: 7}) // the hole beside the view
	if !f.caret.Empty() {
		t.Fatalf("a cursor in a hole told the IME %v", f.caret)
	}
	b.frameCaret(&f, m, cursorState{visible: true, x: 9, y: 0}) // positive control: the view's first cell
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

// A font change that leaves every band where it was is a new snapshot, admitted and
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

// A Panes tree is fixed once built. Replacing an item in the caller's slice afterwards
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

// A native whose top-left cell is a hole is anchored by the first band it overlaps and cut at that
// pane. Its canvas keeps the native's own origin, so the local position a pointer reconstructs
// from its cells is where the canvas draws, and its caret sits at that same origin.
func TestAHoleAnchoredNativeKeepsItsOwnOrigin(t *testing.T) {
	m := stagedMetrics(t)
	view := m.layout.panes[1]
	r := newRenderer(newTestShaper(), "Go Mono", DefaultTheme())
	g := &grid{}
	g.resize(m.grid.W, m.grid.H)
	cells := CellRect{X: 7, Y: 7, W: 4, H: 1} // (7, 7) and (8, 7) are holes; (9, 7) is the view's
	c := newGioCanvas(new(op.Ops), r, g, m, cells, tui.Rect{}, nil)
	if c.clip != image.Rect(150, 224, 182, 241).Intersect(view.clip) {
		t.Fatalf("clip %v; want the native cut at the view pane", c.clip)
	}
	// The pointer at device (158, 232): the view's cell (9, 7), 0.5 into it; local to the native
	// that is column 2.5, in dp 2.5 * 16 / 1.5.
	x, y := m.cellAt(158, 232)
	sx, _ := m.subCell(158, 232, x, y)
	local := (float32(x-cells.X) + sx) * float32(m.cell.X) / m.scale
	canvas := (158 - c.origin.X) / m.scale
	if x != 9 || y != 7 || local != canvas {
		t.Fatalf("pointer local %v dp at cell %d,%d; canvas local %v dp (origin %v): they must agree", local, x, y, canvas, c.origin)
	}
	// A caret where the pane cuts the view away (x 124 < 150) is hidden, so it is not told.
	hidden := tui.NativePlacement{View: caretView{r: Rect{X: 4, Y: 2, W: 1, H: 10}, base: 8, ok: true}, X: 7, Y: 7, Cols: 4, Rows: 1}
	if caret, ok := viewCaret(m, []tui.NativePlacement{hidden}); ok {
		t.Fatalf("a caret outside the painted clip was told at %v", caret.rect)
	}
	// Positive control: at x 30 dp the caret is inside the visible part, at the canvas origin
	// plus its dp offset.
	shown := hidden
	shown.View = caretView{r: Rect{X: 30, Y: 2, W: 1, H: 10}, base: 8, ok: true}
	caret, ok := viewCaret(m, []tui.NativePlacement{shown})
	if !ok || caret.rect.Min != image.Pt(int(c.origin.X)+45, int(c.origin.Y)+3) || !caret.rect.In(c.clip) {
		t.Fatalf("caret at %v; want at the canvas origin %v plus (30, 2) dp, inside the clip %v", caret.rect, c.origin, c.clip)
	}
}

// A native caret whose top its pane cuts keeps its baseline where it is on screen: the input
// method is told that baseline, with a non-negative ascent and descent. A caret whose baseline is
// cut away is not told at all.
func TestACutCaretKeepsItsBaseline(t *testing.T) {
	m := stagedMetrics(t)
	// Top-left (7, 7) is a hole; the status bar's band is the first it overlaps, at origin (112, 209).
	at := tui.NativePlacement{View: caretView{r: Rect{X: 4, Y: 16, W: 1, H: 10}, base: 8, ok: true}, X: 7, Y: 7, Cols: 1, Rows: 2}
	// Unclipped: y 233..248, baseline 233 + 8*1.5 = 245. The status pane starts at y 241.
	c, ok := viewCaret(m, []tui.NativePlacement{at})
	if !ok {
		t.Fatal("a caret whose baseline is visible was not told")
	}
	place := caretPlace(c.rect, c.base)
	if c.rect.Min.Y != 241 || place.Y != 245 || place.Ascent < 0 || place.Descent < 0 {
		t.Fatalf("cut caret %v base %d tells %+v; want top 241, baseline 245, ascent and descent >= 0", c.rect, c.base, place)
	}
	// A taller caret 6 dp higher: y 224..254 is partly visible, but its baseline (y 236) is above
	// the pane, so it is not told.
	at.View = caretView{r: Rect{X: 4, Y: 10, W: 1, H: 20}, base: 8, ok: true}
	if c, ok := viewCaret(m, []tui.NativePlacement{at}); ok {
		t.Fatalf("a caret whose baseline is cut away was told at %v base %d", c.rect, c.base)
	}
	// Bottom: a native in the view's last row (origin y 224); the pane ends at y 241.
	low := tui.NativePlacement{View: caretView{r: Rect{X: 4, Y: 8, W: 1, H: 10}, base: 2, ok: true}, X: 9, Y: 7, Cols: 2, Rows: 1}
	c, ok = viewCaret(m, []tui.NativePlacement{low}) // y 236..251, baseline 239: cut at the bottom
	if place := caretPlace(c.rect, c.base); !ok || c.rect.Max.Y != 241 || place.Y != 239 || place.Ascent < 0 || place.Descent < 0 {
		t.Fatalf("bottom-cut caret %v base %d tells %+v; want bottom 241, baseline 239, ascent and descent >= 0", c.rect, c.base, place)
	}
	low.View = caretView{r: Rect{X: 4, Y: 8, W: 1, H: 10}, base: 5, ok: true} // baseline 243, below the pane
	if c, ok := viewCaret(m, []tui.NativePlacement{low}); ok {
		t.Fatalf("a caret whose baseline is below its pane was told at %v base %d", c.rect, c.base)
	}
}

// resolveFixture is a Panes backend whose shown frame is snapshot E (503×301 at 1.5, 16×32 cells)
// and whose admitted snapshot is E' (a zoom to 20×40 cells in a 700×400 window: every band and
// origin moved).
func resolveFixture(t *testing.T) (*Backend, *frame, metrics, metrics) {
	t.Helper()
	p := sidebarAndView()
	b := NewBackend(WithPanes(p))
	e := b.publishLayout(nil, placePanes(p, metrics{window: image.Pt(503, 301), scale: 1.5, ppem: 24, cell: image.Pt(16, 32), baseline: 24}, 0))
	b.admitLayout(e.grid)
	shown := &frame{m: e}
	e2 := b.publishLayout(&e, placePanes(p, metrics{window: image.Pt(700, 400), scale: 1.5, ppem: 30, cell: image.Pt(20, 40), baseline: 30}, 0))
	b.admitLayout(e2.grid)
	if b.stage.admitted.layout != e2.layout {
		t.Fatal("fixture: E' not admitted")
	}
	return b, shown, e, e2
}

// A click read while frame E was shown, resolved after the App admitted E', lands in the same
// pane at the same distance into it in dp, on both axes, and CellPixels reconstructs that dp.
func TestAClickResolvesFromTheShownFrameIntoTheAdmittedLayout(t *testing.T) {
	b, shown, e, e2 := resolveFixture(t)
	view, view2 := e.layout.panes[1], e2.layout.panes[1]
	// Device (166, 52) in E: 16 px right, 52 px down into the view pane: (10.667, 34.667) dp.
	ev := b.ResolvePointer(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Source: tui.PointerSource{X: 166, Y: 52, Ref: shown}})
	cp := b.CellPixels()
	gotX := (float32(ev.X-view2.band.X) + ev.SubX) * cp.W
	gotY := (float32(ev.Y-view2.band.Y) + ev.SubY) * cp.H
	wantX, wantY := float32(166-view.origin.X)/1.5, float32(52-view.origin.Y)/1.5
	if d := gotX - wantX; d > 0.01 || d < -0.01 {
		t.Fatalf("x: %v dp into the view in E', want %v (event %+v, CellPixels %v)", gotX, wantX, ev, cp)
	}
	if d := gotY - wantY; d > 0.01 || d < -0.01 {
		t.Fatalf("y: %v dp into the view in E', want %v (event %+v)", gotY, wantY, ev)
	}
	if ev.X < view2.band.X || ev.X >= view2.band.X+view2.band.W {
		t.Fatalf("resolved to column %d, outside the view's band %v in E'", ev.X, view2.band)
	}
}

// A press latches its pane: a drag past the pane's edge keeps counting in that pane, and after the
// release a move maps through the pane under the pointer again.
func TestResolvePointerLatchesThePressPane(t *testing.T) {
	b, shown, _, e2 := resolveFixture(t)
	src := func(x, y float32) tui.PointerSource { return tui.PointerSource{X: x, Y: y, Ref: shown} }
	side2, view2 := e2.layout.panes[0], e2.layout.panes[1]
	press := b.ResolvePointer(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Source: src(140, 10)})
	if press.X < side2.band.X || press.X >= side2.band.X+side2.band.W {
		t.Fatalf("press resolved to column %d, outside the sidebar %v", press.X, side2.band)
	}
	// Device x 200 in E is 200 px into the sidebar's mapping: 133.3 dp, 10 of E''s 20 px cells
	// (through the view's mapping it would be 50 px in: column 7+2 = 9).
	drag := b.ResolvePointer(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, Source: src(200, 10)})
	if drag.X != side2.band.X+10 {
		t.Fatalf("drag over the view resolved to column %d; want %d, counted on in the sidebar's mapping", drag.X, side2.band.X+10)
	}
	b.ResolvePointer(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, Source: src(200, 10)})
	move := b.ResolvePointer(tui.MouseEvent{Kind: tui.MouseMotion, Source: src(200, 10)})
	if move.X < view2.band.X {
		t.Fatalf("after the release a move resolved to column %d; want in the view %v", move.X, view2.band)
	}
}

// An event with no frame is the producer's: resolution leaves it alone.
func TestResolvePointerLeavesAnUnmarkedEvent(t *testing.T) {
	b, _, _, _ := resolveFixture(t)
	in := tui.MouseEvent{Kind: tui.MousePress, X: 3, Y: 2, SubX: 0.5}
	if out := b.ResolvePointer(in); out != in {
		t.Fatalf("resolved an unmarked event: %+v", out)
	}
}

func TestResolvePointerAllocatesNothing(t *testing.T) {
	b, shown, _, _ := resolveFixture(t)
	ev := tui.MouseEvent{Kind: tui.MouseMotion, Source: tui.PointerSource{X: 166, Y: 52, Ref: shown}}
	if n := testing.AllocsPerRun(100, func() { _ = b.ResolvePointer(ev) }); n != 0 {
		t.Fatalf("ResolvePointer allocates %v times per event", n)
	}
}

// The pointer path from a Gio event to the resolved MouseEvent allocates nothing: translate into
// its reused buffer, the Source the window attaches, and ResolvePointer, for every kind of event.
// (The App's queue then holds each as a tui.Event, past this path.)
func TestThePointerPathAllocatesNothing(t *testing.T) {
	b, shown, _, _ := resolveFixture(t)
	var ptr pointerState
	var resolved tui.MouseEvent
	through := func(pe pointer.Event) {
		for _, me := range ptr.translate(pe, shown.m) {
			me.Source = tui.PointerSource{X: pe.Position.X, Y: pe.Position.Y, Ref: shown}
			resolved = b.ResolvePointer(me)
		}
	}
	at := f32.Pt(166, 52)
	step := float32(shown.m.cell.Y) / 2 // one wheel step
	kinds := []struct {
		name string
		do   func()
	}{
		{"move", func() { through(pointer.Event{Kind: pointer.Move, Position: at}) }},
		{"click", func() {
			through(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: at})
			through(pointer.Event{Kind: pointer.Release, Position: at})
		}},
		{"drag", func() {
			through(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: at})
			through(pointer.Event{Kind: pointer.Drag, Buttons: pointer.ButtonPrimary, Position: f32.Pt(300, 60)})
			through(pointer.Event{Kind: pointer.Release, Position: f32.Pt(300, 60)})
		}},
		{"scroll", func() { through(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, step), Position: at}) }},
	}
	for _, k := range kinds {
		k.do() // the buffer grows to its size once
		if n := testing.AllocsPerRun(100, k.do); n != 0 {
			t.Errorf("a %s through the pointer path allocates %v times", k.name, n)
		}
	}
	if resolved.Kind != tui.MouseWheel {
		t.Fatalf("the last event resolved to %+v; want the wheel step (the path ran)", resolved)
	}
}

// queuedMouse is the MouseEvents b has pushed for the App.
func queuedMouse(b *Backend) []tui.MouseEvent {
	b.q.mu.Lock()
	defer b.q.mu.Unlock()
	var out []tui.MouseEvent
	for _, ev := range b.q.queue {
		if m, ok := ev.(tui.MouseEvent); ok {
			out = append(out, m)
		}
	}
	return out
}

// pressThrough feeds one press at (x, y) through Gio's router into b.readInput.
func pressThrough(t *testing.T, b *Backend, m metrics, x, y float32) []tui.MouseEvent {
	t.Helper()
	r := new(input.Router)
	tag := &b.gio
	b.readInput(r.Source(), tag, m) // registers the filters
	ops := new(op.Ops)
	area := clip.Rect(image.Rectangle{Max: m.window}).Push(ops)
	event.Op(ops, tag)
	area.Pop()
	r.Frame(ops)
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, y)})
	b.readInput(r.Source(), tag, m)
	return queuedMouse(b)
}

// A Panes window hands the App each pointer event with its device position and the frame on
// screen, for resolution at dispatch; a cell-root window sends none.
func TestAPanesWindowSendsThePointerWithTheShownFrame(t *testing.T) {
	m := stagedMetrics(t)
	b := NewBackend(WithPanes(sidebarAndView()))
	shown := &frame{m: m}
	b.gio.shown = shown
	got := pressThrough(t, b, m, 166, 52)
	if len(got) != 1 || got[0].Source.Ref != shown || got[0].Source.X != 166 || got[0].Source.Y != 52 {
		t.Fatalf("pushed %+v; want one press carrying (166, 52) and the shown frame", got)
	}
	plain := NewBackend()
	plain.gio.shown = &frame{m: metrics{window: m.window, cell: m.cell, scale: 1, grid: tui.Size{W: 31, H: 9}}}
	if got := pressThrough(t, plain, plain.gio.shown.m, 166, 52); len(got) != 1 || got[0].Source != (tui.PointerSource{}) {
		t.Fatalf("a cell-root window pushed %+v; want one press with no Source", got)
	}
}

// A cancel that ends a chord releases every button, so the dispatch latch empties and the next
// free motion resolves through the pane under it; a new press after it latches afresh.
func TestACancelledChordReleasesEveryButtonAndTheLatch(t *testing.T) {
	b, shown, _, e2 := resolveFixture(t)
	view2 := e2.layout.panes[1]
	var ptr pointerState
	m := shown.m
	resolve := func(pe pointer.Event) []tui.MouseEvent {
		var out []tui.MouseEvent
		for _, me := range ptr.translate(pe, m) {
			me.Source = tui.PointerSource{X: pe.Position.X, Y: pe.Position.Y, Ref: shown}
			out = append(out, b.ResolvePointer(me))
		}
		return out
	}
	at := f32.Pt(140, 10) // the sidebar
	resolve(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: at})
	resolve(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary | pointer.ButtonSecondary, Position: at})
	rel := resolve(pointer.Event{Kind: pointer.Cancel, Position: at})
	if len(rel) != 2 || rel[0].Kind != tui.MouseRelease || rel[1].Kind != tui.MouseRelease || rel[0].Button == rel[1].Button {
		t.Fatalf("the cancel released %+v; want one release for each of the two buttons", rel)
	}
	if b.stage.latch.held != 0 {
		t.Fatalf("latch still holds %#b after the cancel", b.stage.latch.held)
	}
	// Free motion over the view resolves through the view: column 9, half into it.
	move := resolve(pointer.Event{Kind: pointer.Move, Position: f32.Pt(200, 10)})
	if len(move) != 1 || move[0].X < view2.band.X {
		t.Fatalf("free motion after the cancel resolved to %+v; want the view's own band %v", move, view2.band)
	}
	// A new press there latches the view, not the old sidebar.
	press := resolve(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(200, 10)})
	if len(press) != 1 || press[0].X < view2.band.X || b.stage.latch.pane != 1 {
		t.Fatalf("a new press resolved to %+v with the latch on pane %d; want the view", press, b.stage.latch.pane)
	}
}
