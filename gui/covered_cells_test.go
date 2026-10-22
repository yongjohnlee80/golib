package gui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/unit"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// The colours of a covered-cell test: the view's own sepia, and the dark terminal panel raised over
// some of its cells (AutoDoc's agent panel over the todos tab bar).
var (
	sepiaFG = color.NRGBA{R: 0x5b, G: 0x46, B: 0x36, A: 0xff}
	sepiaBG = color.NRGBA{R: 0xf4, G: 0xec, B: 0xd8, A: 0xff}
	darkFG  = color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}
	darkBG  = color.NRGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff}
	redFG   = color.NRGBA{R: 0xff, A: 0xff}
	redBG   = color.NRGBA{R: 0x80, A: 0xff}
)

// overView is a view of cols cells by rows whose cells covered are a later layer's; the others are
// the view's, in own (else sepia) and text.
type overView struct {
	cols, rows int
	covered    func(col, row int) bool
	own        func(col, row int) (fg, bg color.NRGBA)
	text       func(col, row int) string
}

// canvas is the view's recording canvas with the covering layer in fg, bg and text overText; ""
// gives the covered cells the text the view's would have, so they read as the view's.
func (v overView) canvas(fg, bg color.NRGBA, overText string) *RecordingCanvas {
	rc := NewRecordingCanvas(Size{W: float32(v.cols) * 8, H: float32(v.rows) * 16}, Size{W: 8, H: 16})
	rc.Covers = v.covered
	rc.Colors = func(col, row int) (color.NRGBA, color.NRGBA) {
		if v.covered(col, row) {
			return fg, bg
		}
		if v.own != nil {
			return v.own(col, row)
		}
		return sepiaFG, sepiaBG
	}
	rc.Content = func(col, row int) string {
		if v.covered(col, row) && overText != "" {
			return overText
		}
		if v.text != nil {
			return v.text(col, row)
		}
		return ""
	}
	return rc
}

// drawn is a paint call with what can be compared: no shaped text, the brush's colour.
type drawn struct {
	Op                   string
	Rect                 Rect
	Radius, Width, Alpha float32
	Color                color.NRGBA
	Cells                CellRect
	GlyphsOnly           bool
}

func drawnOf(rc *RecordingCanvas) []drawn {
	out := make([]drawn, len(rc.Calls))
	for i, p := range rc.Calls {
		out[i] = drawn{p.Op, p.Rect, p.Radius, p.Width, p.Opacity, p.Brush.Color, p.Cells, p.GlyphsOnly}
	}
	return out
}

// paintUnder paints view v under a dark layer and under a red one, fails unless the two are drawn
// alike (the painter read nothing of the covering layer: its colours or its text), and returns the
// dark one's canvas.
func paintUnder(t *testing.T, view View, v overView) *RecordingCanvas {
	t.Helper()
	dark := v.canvas(darkFG, darkBG, "")
	view.Paint(dark)
	red := v.canvas(redFG, redBG, "]")
	view.Paint(red)
	if a, b := drawnOf(dark), drawnOf(red); !slices.Equal(a, b) {
		t.Fatalf("the paint follows the covering layer's cells:\nunder dark %+v\nunder red  %+v", a, b)
	}
	for _, d := range drawnOf(dark) {
		if d.Color == darkBG || d.Color == darkFG {
			t.Fatalf("%s drawn in the covering layer's %v: %+v", d.Op, d.Color, d)
		}
	}
	return dark
}

func at(cols ...int) func(col, row int) bool {
	return func(col, row int) bool { return row == 0 && slices.Contains(cols, col) }
}

// fakeTabs is a tab strip of spans, as TerminalTabs reports them.
type fakeTabs struct {
	tui.MultiChild
	spans  []widget.TabSpan
	active int
}

func (f *fakeTabs) Layout(cs tui.Constraints) tui.Size { return cs.Constrain(tui.Size{W: 20, H: 1}) }
func (f *fakeTabs) Render(tui.Surface)                 {}
func (f *fakeTabs) HandleEvent(tui.Event) bool         { return false }
func (f *fakeTabs) TabSpans() []widget.TabSpan         { return f.spans }
func (f *fakeTabs) ActiveTabIndex() int                { return f.active }

// The bar is filled in its own colour with the panel raised over its right end: AutoDoc's todos tab
// bar turned the agent terminal's black while the agent panel, raised by its focus, covered its
// last cell.
func TestTheTabBarIsNotTheColourOfAPanelOverItsEnd(t *testing.T) {
	tabs := &fakeTabs{spans: []widget.TabSpan{{X: 0, W: 5}, {X: 6, W: 4}, {X: 17, W: 3}}}
	view, _, ok := paintTabs(tabs)
	if !ok {
		t.Fatal("no view")
	}
	// the panel covers the bar's last four cells: the last tab and the gap before it
	rc := paintUnder(t, view, overView{cols: 20, rows: 1, covered: at(16, 17, 18, 19)})
	bar := rc.Calls[0]
	if bar.Op != "FillRect" || bar.Brush.Color != sepiaBG || bar.Rect.W != 160 {
		t.Fatalf("the bar was filled %+v; want across it in its own %v", bar, sepiaBG)
	}
	var boxes int
	for _, p := range rc.Calls {
		if p.Op == "FillRRect" {
			boxes++
		}
	}
	if boxes != 2 {
		t.Fatalf("%d tabs boxed; want the two the panel leaves open: %+v", boxes, rc.Calls)
	}

	// covered at its first cell only, a tab's box takes the colour of a cell of its own still open
	tabs.spans = []widget.TabSpan{{X: 0, W: 5}}
	rc = paintUnder(t, view, overView{cols: 20, rows: 1, covered: at(0)})
	if i := slices.IndexFunc(rc.Calls, func(p PaintCall) bool { return p.Op == "FillRRect" }); i < 0 || rc.Calls[i].Brush.Color != sepiaBG {
		t.Fatalf("the tab's box %+v; want its own %v", rc.Calls, sepiaBG)
	}

	// a bar wholly covered fills nothing: the host paints the panel's cells over it
	rc = paintUnder(t, view, overView{cols: 20, rows: 1, covered: func(int, int) bool { return true }})
	if slices.Contains(rc.Ops(), "FillRect") || slices.Contains(rc.Ops(), "FillRRect") {
		t.Fatalf("a wholly covered bar was filled: %v", rc.Ops())
	}
}

// A divider stands in the cell between two neighbouring tabs, a thin line darker than the bar
// (Johno: which tab a × belongs to), and none where a panel covers that cell or the tabs do not
// meet across one cell.
func TestTabsHaveADividerBetweenThem(t *testing.T) {
	tabs := &fakeTabs{spans: []widget.TabSpan{{X: 0, W: 5}, {X: 6, W: 4}, {X: 11, W: 3}, {X: 17, W: 3}}}
	view, _, ok := paintTabs(tabs)
	if !ok {
		t.Fatal("no view")
	}
	dividers := func(rc *RecordingCanvas) []PaintCall {
		var out []PaintCall
		for _, p := range rc.Calls {
			if p.Op == "FillRect" && p.Rect.W == 1 {
				out = append(out, p)
			}
		}
		return out
	}
	rc := paintUnder(t, view, overView{cols: 20, rows: 1, covered: func(int, int) bool { return false }})
	got := dividers(rc)
	if len(got) != 2 { // after the cells 5 and 10; 14..16 is no single cell
		t.Fatalf("%d dividers; want 2, in cells 5 and 10: %+v", len(got), rc.Calls)
	}
	for i, cell := range []float32{5, 10} {
		if want := (cell + 0.5) * 8; got[i].Rect.X != want {
			t.Fatalf("divider %d at x %v; want %v, the middle of cell %v", i, got[i].Rect.X, want, cell)
		}
		if got[i].Brush.Color == sepiaBG {
			t.Fatalf("divider %d is the bar's own %v: it shows nothing", i, sepiaBG)
		}
	}
	rc = paintUnder(t, view, overView{cols: 20, rows: 1, covered: at(5)})
	if got := dividers(rc); len(got) != 1 || got[0].Rect.X != (10+0.5)*8 {
		t.Fatalf("with cell 5 covered: dividers %+v; want the one in cell 10 alone", got)
	}
}

// Every gap and every divider is drawn alike, whatever a cell's width: the gaps between neighbouring
// boxes all one width, and each divider on a whole pixel, one pixel wide, the same height and colour.
// A cell 9.6 pixels wide drifted the boxes a pixel from one gap to the next, and put the middle of a
// gap on a half pixel, where the line came and went (Johno: consistent gaps, a thin divider).
func TestEveryDividerIsDrawnAlike(t *testing.T) {
	// gaps after the cells 2, 6 and 13: placed by their cells, at 9.6 pixels a cell, they come out
	// 6, 5 and 5 pixels wide
	tabs := &fakeTabs{spans: []widget.TabSpan{{X: 0, W: 2}, {X: 3, W: 3}, {X: 7, W: 6}, {X: 14, W: 4}}}
	view, _, _ := paintTabs(tabs)
	rc := NewRecordingCanvas(Size{W: 20 * 9.6, H: 16}, Size{W: 9.6, H: 16})
	rc.Covers = func(int, int) bool { return false }
	rc.Colors = func(int, int) (color.NRGBA, color.NRGBA) { return sepiaFG, sepiaBG }
	rc.Content = func(int, int) string { return "" }
	view.Paint(rc)
	var lines []PaintCall
	for _, p := range rc.Calls {
		if p.Op == "FillRect" && p.Rect.W < 2 {
			lines = append(lines, p)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("%d dividers; want 3: %+v", len(lines), rc.Calls)
	}
	var boxes []Rect
	for _, p := range rc.Calls {
		if p.Op == "FillRRect" {
			boxes = append(boxes, p.Rect)
		}
	}
	if len(boxes) != 4 {
		t.Fatalf("%d boxes; want 4", len(boxes))
	}
	for i := 1; i < len(boxes); i++ {
		gap, first := boxes[i].X-(boxes[i-1].X+boxes[i-1].W), boxes[1].X-(boxes[0].X+boxes[0].W)
		if gap != first || boxes[i].X != float32(math.Round(float64(boxes[i].X))) {
			t.Errorf("gap %d is %v, the first %v; box at x %v: want every gap alike, on whole pixels", i, gap, first, boxes[i].X)
		}
	}
	for i, l := range lines {
		if l.Rect.X != float32(math.Floor(float64(l.Rect.X))) || l.Rect.W != 1 {
			t.Errorf("divider %d at x %v, %v wide: want a whole pixel, one wide", i, l.Rect.X, l.Rect.W)
		}
		if l.Rect.W != lines[0].Rect.W || l.Rect.H != lines[0].Rect.H || l.Brush.Color != lines[0].Brush.Color {
			t.Errorf("divider %d %+v is drawn unlike the first %+v", i, l, lines[0])
		}
	}
}

// A strip on a panel's title bar (OnPanelBar, as the Editor's) fills its bar in the panel's own
// colour, so the title bar is one shade from end to end, the strip's cells under it whatever colour
// they are; before the panel has painted, in its cells' as any strip. Another strip keeps its cells'.
func TestAStripOnAPanelBarIsThePanelsColour(t *testing.T) {
	open := func(int, int) bool { return false }
	tint := color.NRGBA{R: 0xe9, G: 0xdf, B: 0xc8, A: 0xff}
	barOf := func(rc *RecordingCanvas) (color.NRGBA, bool) {
		for _, p := range rc.Calls {
			if p.Op == "FillRect" && p.Rect.W == 160 {
				return p.Brush.Color, true
			}
		}
		return color.NRGBA{}, false
	}
	strip := widget.NewTabStrip()
	strip.SetTabs([]widget.StripTab{{ID: "a", Label: "aa"}, {ID: "b", Label: "bb"}})
	painted := false
	OnPanelBar(strip, func() (color.NRGBA, bool) { return tint, painted })
	view, _, ok := paintTabs(strip)
	if !ok {
		t.Fatal("no view")
	}
	if got, ok := barOf(paintUnder(t, view, overView{cols: 20, rows: 1, covered: open})); !ok || got != sepiaBG {
		t.Fatalf("before the panel painted, the bar %v (%v); want its cells' %v", got, ok, sepiaBG)
	}
	painted = true
	if got, ok := barOf(paintUnder(t, view, overView{cols: 20, rows: 1, covered: open})); !ok || got != tint {
		t.Fatalf("on the panel's bar, the bar %v (%v); want the panel's %v", got, ok, tint)
	}
	view, _, _ = paintTabs(&fakeTabs{spans: []widget.TabSpan{{X: 0, W: 4}, {X: 5, W: 4}}})
	if got, ok := barOf(paintUnder(t, view, overView{cols: 20, rows: 1, covered: open})); !ok || got != sepiaBG {
		t.Fatalf("another strip's bar %v (%v); want its cells' %v", got, ok, sepiaBG)
	}
}

// A button, a text field and a scroll bar under a panel over one of their cells take their colours
// from their own cells.
func TestButtonFieldAndScrollbarAreNotThePanelsColour(t *testing.T) {
	t.Run("button", func(t *testing.T) {
		view, _, _ := paintButton(widget.NewButton("Save"))
		rc := paintUnder(t, view, overView{cols: 8, rows: 1, covered: at(0)})
		i := slices.IndexFunc(rc.Calls, func(p PaintCall) bool { return p.Op == "FillRRect" })
		if i < 0 || rc.Calls[i].Brush.Color != sepiaBG {
			t.Fatalf("the button's fill %+v; want its own %v", rc.Calls, sepiaBG)
		}
	})
	t.Run("button's key under the panel", func(t *testing.T) {
		// the panel's cell reads as the mnemonic: its colour must not become the key's. The button is
		// a dark one, so the panel's light text would read on it and not fall back to the label's.
		row := []string{"S", " ", "S", "a", "v", "e", " ", " "}
		navy := color.NRGBA{R: 0x10, G: 0x18, B: 0x40, A: 0xff}
		white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		view, _, _ := paintButton(widget.NewButton("Save", widget.WithMnemonic('S')))
		paintUnder(t, view, overView{cols: 8, rows: 1, covered: at(0), text: func(col, _ int) string { return row[col] },
			own: func(int, int) (color.NRGBA, color.NRGBA) { return white, navy }})
	})
	t.Run("button between two panels", func(t *testing.T) {
		// both ends covered, the first open cell is the mnemonic's, in a theme's accent. The button is
		// mounted, so the key's cell is where tui draws it (Button.MnemonicColumn), and the row is
		// what tui drew.
		accent := color.NRGBA{R: 0xc0, G: 0x30, B: 0x20, A: 0xff}
		b := widget.NewButton("Save", widget.WithMnemonic('S'), widget.WithButtonDecoration("", ""))
		_, sb := styledApp(t, b, 6, 1, NativeStyle())
		var row []string
		waitUntil(t, "the button drawn", func() bool {
			row = row[:0]
			for _, c := range sb.Snapshot()[0] {
				row = append(row, c.Content)
			}
			return strings.Contains(strings.Join(row, ""), "Save")
		})
		key := slices.Index(row, "S")
		if key <= 0 || key >= 5 {
			t.Fatalf("the key's cell %d in %q: want one inside the ends", key, row)
		}
		view, _, _ := paintButton(b)
		rc := paintUnder(t, view, overView{cols: 6, rows: 1, covered: at(0, 5), text: func(col, _ int) string { return row[col] },
			own: func(col, _ int) (color.NRGBA, color.NRGBA) {
				if col == key {
					return accent, sepiaBG
				}
				return sepiaFG, sepiaBG
			}})
		i := slices.IndexFunc(rc.Calls, func(p PaintCall) bool { return p.Op == "StrokeRRect" })
		if i < 0 || rc.Calls[i].Brush.Color != alpha(sepiaFG, 0.35) {
			t.Fatalf("the button's border %+v; want in its label's %v, not the key's accent", rc.Calls, sepiaFG)
		}
	})
	t.Run("text field", func(t *testing.T) {
		view, _, _ := paintTextField(widget.NewTextInput(widget.WithInitialValue("hello")))
		rc := paintUnder(t, view, overView{cols: 10, rows: 1, covered: at(0)})
		if rc.Calls[0].Op != "FillRect" || rc.Calls[0].Brush.Color != sepiaBG {
			t.Fatalf("the field's fill %+v; want its own %v", rc.Calls, sepiaBG)
		}
	})
	t.Run("scroll bar", func(t *testing.T) {
		view, _, ok := paintScrollbar(&fakeScroller{view: 3, content: 10})
		if !ok {
			t.Fatal("no view")
		}
		rc := paintUnder(t, view, overView{cols: 10, rows: 3, covered: at(0)})
		if len(rc.Calls) != 1 || rc.Calls[0].Brush.Color != alpha(sepiaFG, 0.4) {
			t.Fatalf("the thumb %+v; want in the list's own %v", rc.Calls, alpha(sepiaFG, 0.4))
		}
	})
}

// A check box whose box is under a panel has no box to draw, whatever its cells read: one found
// under the panel would be drawn in the panel's colours, and one further on is its text's ("[x] On"
// beside its own "[x] "). Its cells are shown as they are.
func TestACheckBoxIsNotFoundUnderAPanel(t *testing.T) {
	row := []string{"[", "x", "]", " ", "[", "x", "]", " ", "O", "n"}
	text := func(col, row0 int) string {
		if row0 == 0 && col < len(row) {
			return row[col]
		}
		return ""
	}
	view, _, _ := paintCheckBox(&fakeCheck{checked: true})
	rc := paintUnder(t, view, overView{cols: 10, rows: 1, covered: at(0, 1, 2), text: text})
	if want := []PaintCall{{Op: "PaintCells", Cells: CellRect{W: 10, H: 1}}}; !slices.EqualFunc(rc.Calls, want, func(a, b PaintCall) bool {
		return a.Op == b.Op && a.Cells == b.Cells && a.GlyphsOnly == b.GlyphsOnly
	}) {
		t.Fatalf("a check box with its box covered drew %+v; want its cells as they are, the label's brackets text", rc.Calls)
	}

	// with the panel over the text instead, the box before it is drawn, in its own colours
	rc = paintUnder(t, view, overView{cols: 10, rows: 1, covered: at(8, 9), text: text})
	i := slices.IndexFunc(rc.Calls, func(p PaintCall) bool { return p.Op == "FillRect" })
	if i < 0 || rc.Calls[i].Rect.X != 0 || rc.Calls[i].Brush.Color != sepiaBG {
		t.Fatalf("the box %+v; want its own at cell 0 in %v", rc.Calls, sepiaBG)
	}
}

// A progress bar takes its filled colour from a filled cell and its track's from an empty one,
// each one nothing covers.
func TestAProgressBarIsNotThePanelsColour(t *testing.T) {
	p := widget.NewProgressBar()
	p.SetProgress(0.5)
	view, _, ok := paintProgress(p)
	if !ok {
		t.Fatal("no view")
	}
	green := color.NRGBA{G: 0x80, A: 0xff}
	rc := paintUnder(t, view, overView{cols: 10, rows: 1, covered: at(0, 9),
		own: func(col, _ int) (color.NRGBA, color.NRGBA) {
			if col < 5 {
				return green, sepiaBG
			}
			return sepiaFG, sepiaBG
		}})
	var fills []color.NRGBA
	for _, c := range rc.Calls {
		if c.Op == "FillRRect" {
			fills = append(fills, c.Brush.Color)
		}
	}
	if want := []color.NRGBA{Mix(sepiaBG, sepiaFG, 0.25), green}; !slices.Equal(fills, want) {
		t.Fatalf("track and fill %v; want %v", fills, want)
	}
}

type fakeScroller struct {
	tui.MultiChild
	view, content int
}

func (f *fakeScroller) Layout(cs tui.Constraints) tui.Size {
	return cs.Constrain(tui.Size{W: 10, H: 3})
}
func (f *fakeScroller) Render(tui.Surface)           {}
func (f *fakeScroller) HandleEvent(tui.Event) bool   { return false }
func (f *fakeScroller) ScrollState() (int, int, int) { return 0, f.view, f.content }

// The composite hands each view a canvas that knows the cells painted after it: its placement's
// Covered, in the view's own cells, and so does a Sub of it.
func TestCompositeHandsAViewItsCoveredCells(t *testing.T) {
	m := measure(testFont, 14, 0, image.Pt(200, 100), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	var g grid
	g.resize(m.grid.W, m.grid.H)
	r := newRenderer(newTestShaper(), "Go Mono", DefaultTheme())
	r.m = m
	var covered, subCovered []int
	view := ViewFunc(func(c Canvas) {
		for x := range 4 {
			if CellCovered(c, x, 0) {
				covered = append(covered, x)
			}
			if CellCovered(c.Sub(Rect{X: 1, W: 4, H: 4}), x, 0) {
				subCovered = append(subCovered, x)
			}
		}
	})
	natives := []tui.NativePlacement{{View: view, X: 2, Y: 1, Cols: 4, Rows: 1, Covered: []tui.Rect{{X: 5, Y: 1, W: 3, H: 2}}}}
	r.composite(new(op.Ops), &g, m, natives, nil)
	if !slices.Equal(covered, []int{3}) || !slices.Equal(subCovered, []int{3}) {
		t.Fatalf("covered %v, in a Sub %v; want the view's cell 3 (grid 5,1)", covered, subCovered)
	}
}

// AutoDoc's case whole: a Tabs under a dark panel, a non-modal Float raised by focusing it over the
// bar's last column. The bar keeps its own colour.
func TestARaisedPanelOverTheTabBarLeavesTheBarItsColour(t *testing.T) {
	bar := style.New().Background(style.RGB(sepiaBG.R, sepiaBG.G, sepiaBG.B)).Foreground(style.RGB(sepiaFG.R, sepiaFG.G, sepiaFG.B))
	tabs := widget.NewTabs(widget.WithTab("Open", widget.NewText("open")), widget.WithTab("Done", widget.NewText("done")),
		widget.WithTabsStyles(bar, bar, bar))
	host := widget.NewOverlayHost(tabs)
	dark := style.New().Background(style.RGB(darkBG.R, darkBG.G, darkBG.B)).Foreground(style.RGB(darkFG.R, darkFG.G, darkFG.B))
	in := widget.NewTextInput()
	body := tui.NewFlex(tui.Vertical)
	body.Add(widget.NewText("agent", widget.WithTextStyle(dark)), in)
	const w = 30
	panel := widget.NewFloat(body, widget.WithAnchor(widget.AtRect(tui.Rect{X: w - 5, Y: 0, W: 5, H: 2})))
	host.Attach(panel)
	app, sb := styledApp(t, host, w, 6, NativeStyle())
	onLoop(t, app, panel.Show)
	waitUntil(t, "the panel's field", func() bool {
		var focused bool
		onLoop(t, app, func() { focused = app.FocusInto(in) }) // the focus raises the panel, as the agent panel's does
		return focused
	})
	var p tui.NativePlacement
	waitUntil(t, "the panel over the bar's end", func() bool {
		for _, q := range sb.placements() {
			if q.Node == tabs.NodeID() && slices.ContainsFunc(q.Covered, func(r tui.Rect) bool { return r.Contains(w-1, 0) }) {
				p = q
				return true
			}
		}
		return false
	})
	r := newRenderer(nil, "Go Mono", DefaultTheme())
	var last color.NRGBA
	onLoop(t, app, func() { _, last = r.colors(sb.Snapshot()[0][w-1].Attrs) })
	if last != darkBG {
		t.Fatalf("the bar's last cell is %v on screen; want the panel's %v over it", last, darkBG)
	}
	rc := paintOn(t, app, sb, p)
	if fill := rc.Calls[0]; fill.Op != "FillRect" || fill.Brush.Color != sepiaBG {
		t.Fatalf("the bar was filled %+v; want its own %v, not the panel's %v", fill, sepiaBG, darkBG)
	}
}

// With the bar overflowing, the active tab's underline is under the active tab's label: the
// painter reads the tab's place among the spans drawn (tabs in view, < and > around them), not its
// place among all the tabs, which is another span or none once the bar scrolls.
func TestAnOverflowingTabBarUnderlinesTheActiveTab(t *testing.T) {
	var opts []widget.TabsOption
	for i := range 8 {
		opts = append(opts, widget.WithTab(fmt.Sprintf("tab%d", i), widget.NewText(fmt.Sprint(i))))
	}
	tabs := widget.NewTabs(opts...)
	app, sb := styledApp(t, tabs, 20, 3, NativeStyle())
	onLoop(t, app, func() { tabs.Select(7) })
	var row string
	label := -1 // the cell tab7's label starts at
	waitUntil(t, "tab7 on the bar", func() bool {
		cells := sb.Snapshot()[0]
		var b strings.Builder
		for _, c := range cells {
			b.WriteString(c.Content)
		}
		row = b.String()
		for x := 0; x+3 < len(cells); x++ {
			if cells[x].Content+cells[x+1].Content+cells[x+2].Content+cells[x+3].Content == "tab7" {
				label = x
				return true
			}
		}
		return false
	})
	rc := paintOn(t, app, sb, placementOf(t, sb, tabs.NodeID()))
	cell := float32(8)
	for _, c := range rc.Calls {
		if c.Op == "FillRect" && c.Rect.H == 2 { // the active tab's underline
			if x := float32(label) * cell; c.Rect.X > x || c.Rect.X+c.Rect.W < x+4*cell {
				t.Fatalf("the underline %+v is not under tab7, at cells %d-%d of %q", c.Rect, label, label+4, row)
			}
			return
		}
	}
	t.Fatalf("no underline drawn; tab7 is at cell %d of %q: %+v", label, row, rc.Calls)
}
