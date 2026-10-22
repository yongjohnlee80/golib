package widget

import (
	"math"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// TERMINAL VIEWS — a Terminal's text at a size of its own: a scale of the window's text size, so
// the window's Fonts size and Zoom move it too. The view sizes the terminal in its own cells, maps
// the pointer to them, and paints the frame the terminal hands it with gui.CellPainter, the window's
// cell code at that size; the native style's Terminal painter reads it back (Terminal.TerminalView).

// The bounds of a scale: smaller or larger is clamped.
const (
	minTerminalScale = 0.5
	maxTerminalScale = 2
)

// TerminalViews makes the views of one panel's terminals, all at one scale: pass Make to
// tuiwidget.TerminalTabs.SetTerminalViews. Loop goroutine.
type TerminalViews struct {
	scale float32
	views []*terminalView
}

// NewTerminalViews is views at scale of the window's text size (clamped to 0.5–2).
func NewTerminalViews(scale float32) *TerminalViews { return &TerminalViews{scale: clampScale(scale)} }

func clampScale(f float32) float32 {
	if math.IsNaN(float64(f)) {
		return 1
	}
	return min(max(f, minTerminalScale), maxTerminalScale)
}

// Scale is the views' scale.
func (v *TerminalViews) Scale() float32 { return v.scale }

// SetScale changes every view's scale: each terminal is laid out again at its next layout, in its
// new cells.
func (v *TerminalViews) SetScale(f float32) {
	f = clampScale(f)
	if f == v.scale {
		return
	}
	v.scale = f
	for _, tv := range v.views {
		tv.cell = gui.Size{} // measured again at its next Paint; until then the window's × scale
		tv.relayout(tv.owner.Context())
	}
}

// Make is a view bound to terminal t: SetTerminalViews' newView.
func (v *TerminalViews) Make(t *tuiwidget.Terminal) tuiwidget.TerminalView {
	tv := &terminalView{views: v, owner: t, painter: gui.NewCellPainter()}
	v.views = append(v.views, tv)
	return tv
}

// terminalView is one terminal's view: a tuiwidget.TerminalView, and the gui.View (with its caret)
// the native style paints it with.
type terminalView struct {
	views   *TerminalViews
	owner   *tuiwidget.Terminal
	painter *gui.CellPainter

	cell     gui.Size // measured at the last Paint; zero before the first, or after a scale change
	baseline float32
	cells    []tui.Cell // the last frame, copied
	cols     int
	rows     int
	caret    gui.CellCaret
	retired  bool // Detached: it draws and posts nothing more
}

var (
	_ tuiwidget.TerminalView = (*terminalView)(nil)
	_ gui.CaretView          = (*terminalView)(nil)
)

// cellPx is the view's cell in logical pixels: the measured one, or before it the window's cell
// (ctx.CellPixels) at the scale, as the Editor guesses its mono font before its first Paint.
func (v *terminalView) cellPx(ctx *tui.Context) (w, h float32) {
	if v.cell.W > 0 && v.cell.H > 0 {
		return v.cell.W, v.cell.H
	}
	cp := ctx.CellPixels()
	return max(cp.W*v.views.scale, 1), max(cp.H*v.views.scale, 1)
}

// Cells is the area's pixels divided by the view's cell: at least 1×1. With its owner not mounted,
// the area as it is.
func (v *terminalView) Cells(w, h int) (int, int) {
	ctx := v.owner.Context()
	if ctx == nil || !ctx.Mounted() {
		return w, h
	}
	cp := ctx.CellPixels()
	if cp.W <= 0 || cp.H <= 0 {
		return w, h
	}
	cw, ch := v.cellPx(ctx)
	return max(int(float32(w)*cp.W/cw), 1), max(int(float32(h)*cp.H/ch), 1)
}

// Pointer maps the event's pixel, (X+SubX)·CellPixels as the Editor's body maps it, to the view's
// cells.
func (v *terminalView) Pointer(e tui.MouseEvent) tui.MouseEvent {
	ctx := v.owner.Context()
	if ctx == nil || !ctx.Mounted() {
		return e
	}
	cp := ctx.CellPixels()
	if cp.W <= 0 || cp.H <= 0 {
		return e
	}
	cw, ch := v.cellPx(ctx)
	px, py := eventPixel(e, cp.W, cp.H)
	fx, fy := px/cw, py/ch
	e.X, e.Y = int(fx), int(fy)
	e.SubX, e.SubY = fx-float32(e.X), fy-float32(e.Y)
	return e
}

// Frame keeps a copy of the terminal's frame and its caret for Paint.
func (v *terminalView) Frame(cells []tui.Cell, cols, rows int, cursor tui.Point, shape tui.CursorShape, show bool) {
	if v.retired {
		return
	}
	v.cells = append(v.cells[:0], cells...)
	v.cols, v.rows = cols, rows
	v.caret = gui.CellCaret{X: cursor.X, Y: cursor.Y, Shape: shape, Show: show}
}

// Detached drops the frame and the caret, and retires the view: it posts no layout after.
func (v *terminalView) Detached() {
	v.retired, v.cells, v.caret = true, nil, gui.CellCaret{}
	for i, tv := range v.views.views {
		if tv == v {
			v.views.views = append(v.views.views[:i], v.views.views[i+1:]...)
			break
		}
	}
}

// Paint draws the last frame at the scale of c's text size. A cell measured different from the one
// the terminal was laid out with posts a layout to the loop (never laying out from Paint): through
// the owner's Context of now, dropped if that mount is gone or the view retired by then.
func (v *terminalView) Paint(c gui.Canvas) {
	if v.retired {
		return
	}
	px := c.TextSize() * v.views.scale
	cell, base := v.painter.Cell(c, px)
	if cell != v.cell && cell.W > 0 {
		v.cell, v.baseline = cell, base
		v.relayout(v.owner.Context())
	}
	v.painter.Paint(c, px, v.cells, v.cols, v.rows, v.caret)
}

// relayout asks ctx's App for a layout on its loop, unless the view is retired or ctx unmounted by
// then.
func (v *terminalView) relayout(ctx *tui.Context) {
	if ctx == nil || !ctx.Mounted() {
		return
	}
	ctx.Update(func() {
		if !v.retired && ctx.Mounted() {
			ctx.RequestLayout()
		}
	})
}

// Caret is the terminal's caret in the view's pixels, for the input method: only while the frame
// said to show it (the terminal focused, its cursor shown).
func (v *terminalView) Caret() (gui.Rect, float32, bool) {
	if v.retired || !v.caret.Show || v.cell.W <= 0 {
		return gui.Rect{}, 0, false
	}
	return gui.Rect{X: float32(v.caret.X) * v.cell.W, Y: float32(v.caret.Y) * v.cell.H, W: v.cell.W, H: v.cell.H}, v.baseline, true
}
