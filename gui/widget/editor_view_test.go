package widget

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// rowsOf is how many rows line ln is laid out on.
func rowsOf(h *edHarness, ln int) int {
	var n int
	h.onLoop(func() { n = len(h.e.layout.lay(h.e.layout.blockOf(ln), nil).bl.Lines[0].Para.Lines) })
	return n
}

// With WrapNone (the default) a long line keeps one row; the cursor at its end scrolls the view
// sideways, and a click on a cluster past the view's first width lands on that cluster.
func TestNoWrapScrollsSideways(t *testing.T) {
	long := strings.Repeat("abcdefghij", 20)
	h := startEditor(t, 40, 10, WithCore(tuiwidget.CoreInitialText("short\n"+long+"\nend")))
	h.paint()
	if got := h.e.Wrap(); got != tuiwidget.WrapNone {
		t.Fatalf("the default wrap is %v, want WrapNone as tui's Editor's", got)
	}
	if n := rowsOf(h, 1); n != 1 {
		t.Fatalf("an unwrapped long line took %d rows", n)
	}
	h.keys(key('j'), key('$'))
	h.paint()
	var left float32
	h.onLoop(func() { left = h.e.layout.left })
	if left <= 0 {
		t.Fatal("the cursor at the long line's end did not scroll the view sideways")
	}
	want := len(long) - 3
	x, y := h.caretPoint(1, want)
	if x <= 0 || x >= float32(40*cellW) {
		t.Fatalf("cluster %d is at x %v, outside the view after scrolling", want, x)
	}
	h.clickAt(x, y)
	if ln, col := h.line(); ln != 1 || col != want {
		t.Errorf("a click on cluster %d past the first width landed on %d:%d", want, ln, col)
	}
	h.onLoop(func() { h.e.SetWrap(tuiwidget.WrapSoft) })
	h.paint()
	if n := rowsOf(h, 1); n < 2 {
		t.Errorf("WrapSoft left the long line on %d row", n)
	}
	h.onLoop(func() { left = h.e.layout.left })
	if left != 0 {
		t.Errorf("wrapping kept a sideways scroll of %v", left)
	}
}

// Line numbers take a gutter as wide as tui's Editor's (four digits at least, two blank
// columns): the text starts after it, a click lands on the cluster under it, and the numbers
// are drawn in the gutter, in the colour SetLineNumberColor gives them.
func TestLineNumbersTakeAGutter(t *testing.T) {
	h := startEditor(t, 40, 10, WithLineNumbers(true), WithCore(tuiwidget.CoreInitialText("alpha\nbeta\ngamma")))
	rc := h.paint()
	var gutter, textX, cell float32
	h.onLoop(func() { gutter, textX, cell = h.e.layout.gutter, h.e.layout.textX(), h.e.layout.monoCell() })
	if h.e.GutterWidth() != 6 || gutter != 6*cell {
		t.Fatalf("gutter %d columns, %v px; want 6 columns of %v", h.e.GutterWidth(), gutter, cell)
	}
	if textX != padX+gutter {
		t.Errorf("the text starts at %v, want past the gutter at %v", textX, padX+gutter)
	}
	numbers := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" && c.Rect.X < textX-cell {
			numbers++
		}
	}
	if numbers != 3 {
		t.Errorf("%d numbers drawn in the gutter, want 3", numbers)
	}
	x, y := h.caretPoint(1, 2)
	h.clickAt(x, y)
	if ln, col := h.line(); ln != 1 || col != 2 {
		t.Errorf("a click past the gutter landed on %d:%d, want 1:2", ln, col)
	}

	h.onLoop(func() { h.e.SetLineNumberColor(style.RGB(0xff, 0, 0)) })
	rc = h.paint()
	red := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" && c.Rect.X < textX-cell && c.Brush.Color.R == 0xff && c.Brush.Color.G == 0 {
			red++
		}
	}
	if red != 3 {
		t.Errorf("%d numbers in the colour SetLineNumberColor gave, want 3", red)
	}
	h.onLoop(func() { h.e.SetLineNumbers(false) })
	h.paint()
	h.onLoop(func() { gutter = h.e.layout.gutter })
	if gutter != 0 {
		t.Errorf("hidden line numbers still take %v px", gutter)
	}
}

// The ruler is a one-pixel guide at its column of the Raw view, counted in monospace cells; the
// Rendered view, whose text is not in source columns, draws none.
func TestRulerAtItsColumn(t *testing.T) {
	h := startEditor(t, 60, 8, WithRuler(5), WithRenderer(NewMarkdownRenderer()), WithCore(tuiwidget.CoreInitialText("# title\nbody")))
	rc := h.paint()
	var want, cell float32
	h.onLoop(func() { cell = h.e.layout.monoCell(); want = h.e.layout.textX() + 4*cell })
	guide := func(rc *gui.RecordingCanvas) bool {
		for _, c := range rc.Calls {
			if c.Op == "FillRect" && c.Rect.W == 1 && c.Rect.X == want {
				return true
			}
		}
		return false
	}
	if !guide(rc) {
		t.Errorf("no guide at x %v (column 5)", want)
	}
	h.onLoop(func() { h.e.SetMode(Rendered) })
	if guide(h.paint()) {
		t.Error("the Rendered view drew the ruler")
	}
	h.onLoop(func() { h.e.SetMode(Raw); h.e.SetRuler(0) })
	if guide(h.paint()) {
		t.Error("SetRuler(0) left the guide")
	}
}

// The native switch draws each label inside the cells HandleEvent hit-tests for it, the current
// one in the bar's text colour and the other muted; Rendered unavailable is fainter still.
func TestModeSwitchSegmentsSitOnTheirHitCells(t *testing.T) {
	h := startEditor(t, 60, 8, WithRenderer(NewMarkdownRenderer()), WithCore(tuiwidget.CoreInitialText("# hi")))
	w := len(switchRaw) + len(switchSep) + len(switchRendered)
	labels := func() []gui.PaintCall { // Raw's, then Rendered's
		var out []gui.PaintCall
		h.onLoop(func() {
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(w) * cellW, H: cellH}, h.cell)
			rc.Colors = func(int, int) (fg, bg color.NRGBA) {
				return color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, color.NRGBA{A: 0xff}
			}
			h.e.sw.paint(rc)
			for _, c := range rc.Calls {
				if c.Op == "DrawText" {
					out = append(out, c)
				}
			}
		})
		return out
	}
	mid := func(c gui.PaintCall) float32 { return c.Rect.X + c.Rect.W/2 }
	l := labels()
	if len(l) != 2 {
		t.Fatalf("%d labels drawn, want 2", len(l))
	}
	raw, ren := l[0], l[1]
	if m := mid(raw); m < 0 || m >= float32(len(switchRaw))*cellW {
		t.Errorf("Raw drawn at x %v, outside its hit cells [0, %d)", m, len(switchRaw))
	}
	if m := mid(ren); m <= float32(len(switchRaw)+1)*cellW || m >= float32(w)*cellW {
		t.Errorf("Rendered drawn at x %v, outside its hit cells", m)
	}
	if raw.Brush.Color == ren.Brush.Color {
		t.Error("the current and the other segment drawn alike")
	}
	h.onLoop(func() { h.e.SetMode(Rendered) })
	l2 := labels()
	if l2[1].Brush.Color != raw.Brush.Color {
		t.Errorf("Rendered current drawn %v, want the current colour %v", l2[1].Brush.Color, raw.Brush.Color)
	}
}

// The text is the window's text size (Canvas.TextSize), not a fraction of the cell's height.
func TestTextAtTheWindowsTextSize(t *testing.T) {
	h := startEditor(t, 60, 8, WithCore(tuiwidget.CoreInitialText("body")))
	var mono, prose float32
	h.onLoop(func() {
		rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
		rc.TextPx = 20
		v, _ := h.e.body.NativeView()
		v.(gui.View).Paint(rc)
		mono, prose = h.e.layout.th.Mono.Size, h.e.layout.th.Prose.Size
	})
	if mono != 20 || prose != 21 {
		t.Errorf("mono %v, prose %v at a 20px window text size; want 20 and 21", mono, prose)
	}
}
