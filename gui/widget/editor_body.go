package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// editorBody is an Editor's text area: it routes input to the core, draws the text natively in
// pixels, and in cells on a backend without native views.
type editorBody struct {
	tuiwidget.Base
	e *Editor

	focused    bool
	w, h       int // in cells
	cellTop    int // the cell fallback's first line
	styleTheme *style.Theme

	caret     gui.Rect // the native caret, in the view's pixels, after a paint
	caretBase float32
	caretOn   bool
}

var (
	_ tui.Focusable      = (*editorBody)(nil)
	_ tui.NativeReporter = (*editorBody)(nil)
	_ tui.NativeScoper   = (*editorBody)(nil)
	_ tui.CursorReporter = (*editorBody)(nil)
	_ gui.CaretView      = bodyView{}
)

// Init binds the core to this body's context and pixel layout.
func (b *editorBody) Init(ctx *tui.Context) {
	b.Base.Init(ctx)
	b.e.core.Bind(ctx, b.e.layout)
	ctx.OnUnmount(b.e.layout.cancelDiagrams)
}

// AcceptsFocus: the text area takes the keys.
func (b *editorBody) AcceptsFocus() bool { return true }

// Layout is greedy on both axes.
func (b *editorBody) Layout(c tui.Constraints) tui.Size {
	w := c.MaxW
	if w == tui.Unbounded {
		w = max(c.MinW, 1)
	}
	h := c.MaxH
	if h == tui.Unbounded {
		h = max(c.MinH, b.e.core.LineCount())
	}
	b.w, b.h = w, h
	return c.Constrain(tui.Size{W: w, H: h})
}

// native reports whether a gui backend draws this body.
func (b *editorBody) native() bool {
	ctx := b.Context()
	return ctx != nil && ctx.App().Capabilities().NativeViews
}

// Render draws the lines in cells: what a backend without native views shows, and what a native
// view paints over. It keeps the cursor's line in view.
func (b *editorBody) Render(s tui.Surface) {
	b.styleTheme = s.Theme()
	sz := s.Size()
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", b.e.page)
	ln, _ := b.e.core.Line()
	if ln < b.cellTop {
		b.cellTop = ln
	}
	if sz.H > 0 && ln >= b.cellTop+sz.H {
		b.cellTop = ln - sz.H + 1
	}
	for y := range sz.H {
		i := b.cellTop + y
		if i >= b.e.core.LineCount() {
			break
		}
		x := 0
		for c := range tui.Graphemes(b.e.core.LineAt(i)) {
			if x >= sz.W {
				break
			}
			st := b.e.page
			if b.focused && b.e.core.Selected(i, clusterAt(b.e.core.LineAt(i), x)) {
				st = st.Reverse(true)
			}
			s.SetCell(x, y, c, st)
			x += max(s.StringWidth(c), 1)
		}
	}
}

// clusterAt is the cluster index at cell column x of s.
func clusterAt(s string, x int) int {
	col, i := 0, 0
	for c := range tui.Graphemes(s) {
		if col >= x {
			return i
		}
		col += max(tui.StringWidth(c), 1)
		i++
	}
	return i
}

// Cursor is the cell cursor on a backend without native views; a gui backend draws the native
// caret instead (bodyView.Caret), so there is none.
func (b *editorBody) Cursor() (int, int, bool) {
	if !b.focused || b.native() {
		return 0, 0, false
	}
	ln, col := b.e.core.Line()
	if ln < b.cellTop || ln >= b.cellTop+b.h {
		return 0, 0, false
	}
	x := 0
	i := 0
	for c := range tui.Graphemes(b.e.core.LineAt(ln)) {
		if i == col {
			break
		}
		x += max(tui.StringWidth(c), 1)
		i++
	}
	return min(x, max(b.w-1, 0)), ln - b.cellTop, true
}

// HandleEvent routes keys, paste, ticks and focus to the core, and the pointer through the
// pixel layout.
func (b *editorBody) HandleEvent(ev tui.Event) bool {
	c := b.e.core
	if r, ok := diagramResult(ev); ok {
		return b.e.layout.diagramReady(r)
	}
	switch t := ev.(type) {
	case tui.KeyEvent:
		if c.HandleKey(t) {
			b.MarkDirty()
			return true
		}
		return false
	case tui.PasteEvent:
		b.MarkDirty()
		return c.HandlePaste(t.Text)
	case tui.TickEvent:
		return c.HandleTick()
	case tui.FocusEvent:
		if b.focused != t.Gained {
			b.focused = t.Gained
			b.MarkDirty()
		}
		if !t.Gained {
			c.FocusLost()
		}
		return false
	case tui.PointerCaptureLostEvent:
		c.EndDrag()
		return true
	case tui.MouseEvent:
		return b.mouse(t)
	}
	return false
}

// pixel is a pointer event's position in the view's logical pixels.
func (b *editorBody) pixel(m tui.MouseEvent) (float32, float32) {
	cell := b.e.layout.cell
	if cell.W == 0 {
		if ctx := b.Context(); ctx != nil {
			cp := ctx.App().CellPixels()
			cell = gui.Size{W: cp.W, H: cp.H}
		}
	}
	return (float32(m.X) + m.SubX) * cell.W, (float32(m.Y) + m.SubY) * cell.H
}

// at is the buffer position under the pointer: through the pixel layout once painted natively,
// else the cell fallback's.
func (b *editorBody) at(m tui.MouseEvent) (int, int) {
	if b.native() && b.e.layout.sh != nil {
		return b.e.layout.at(b.pixel(m))
	}
	ln := min(b.cellTop+max(m.Y, 0), max(b.e.core.LineCount()-1, 0))
	return ln, clusterAt(b.e.core.LineAt(ln), max(m.X, 0))
}

func (b *editorBody) mouse(m tui.MouseEvent) bool {
	c := b.e.core
	l := b.e.layout
	switch {
	case m.Kind == tui.MouseWheel && (m.Button == tui.WheelUp || m.Button == tui.WheelDown):
		d := float32(3) * l.lineHeight(l.monoFont())
		if m.Button == tui.WheelUp {
			d = -d
		}
		if b.native() && l.scrollBy(d) {
			b.MarkDirty()
		}
		return true
	case m.Kind == tui.MousePress && m.Button == tui.MouseLeft:
		ln, col := b.at(m)
		handled := c.PressAt(ln, col)
		if c.Dragging() {
			b.Context().CapturePointer()
		}
		b.MarkDirty()
		return handled
	case m.Kind == tui.MousePress && m.Button == tui.MouseRight && b.e.menu.Enabled():
		return b.e.menu.OpenAt(tui.Point{X: m.X, Y: m.Y})
	case m.Kind == tui.MouseMotion && c.Dragging():
		if b.native() {
			_, y := b.pixel(m)
			switch {
			case y < 0:
				l.scrollBy(-l.lineHeight(l.monoFont()))
			case y > l.height:
				l.scrollBy(l.lineHeight(l.monoFont()))
			}
		}
		ln, col := b.at(m)
		c.DragTo(ln, col)
		b.MarkDirty()
		return true
	case m.Kind == tui.MouseRelease && c.Dragging():
		c.EndDrag()
		if ctx := b.Context(); ctx != nil && ctx.HasPointerCapture() {
			ctx.ReleasePointer()
		}
		return true
	}
	return false
}

// NativeView is the text drawn in pixels.
func (b *editorBody) NativeView() (any, bool) { return bodyView{b}, true }

// NativeScope: the view draws the whole body.
func (b *editorBody) NativeScope() tui.NativeScope { return tui.ScopeSubtree }

// bodyView paints an editorBody, and reports its caret for the input method.
type bodyView struct{ b *editorBody }

func (v bodyView) Caret() (gui.Rect, float32, bool) { return v.b.caret, v.b.caretBase, v.b.caretOn }

func (v bodyView) Paint(c gui.Canvas) {
	b, e := v.b, v.b.e
	l := e.layout
	fg, bg := c.CellColors(0, 0)
	l.sh, l.cell = c.Text(), c.CellSize()
	th := e.theme(fg, bg, l.cell, b.styleTheme)
	w, h := c.Size().W-2*padX, c.Size().H
	if w != l.width || th != l.th {
		l.invalidate()
	}
	l.th, l.width, l.height = th, max(w, 1), h
	l.ensure()
	c.FillRect(gui.Rect{W: c.Size().W, H: h}, gui.Solid(bg))

	var frame *tuiwidget.HighlightFrame
	top := 0
	var y float32
	for ; top < len(l.blocks); top++ {
		hh := l.heightOf(top)
		if y+hh > l.scroll {
			break
		}
		y += hh
	}
	if e.mode == Raw && top < len(l.blocks) {
		frame = e.core.BeginHighlight(l.blocks[top].From)
		if frame.Behind {
			if ctx := b.Context(); ctx != nil {
				ctx.App().Update(b.MarkDirty)
			}
		}
	}
	b.caretOn = false
	cl, ccol := e.core.Line()
	visualLine := e.core.Mode() == tuiwidget.ModeVisualLine
	sel := selColor(th)
	for i := top; i < len(l.blocks) && y-l.scroll < h; i++ {
		lb := l.lay(i, frame)
		oy := padY + y - l.scroll
		if lb.bl.Background.A > 0 {
			c.FillRect(gui.Rect{X: padX / 2, Y: oy, W: l.width + padX, H: lb.bl.Height}, gui.Solid(lb.bl.Background))
		}
		for _, ll := range lb.bl.Lines {
			at := gui.Pt(padX, oy+ll.Y)
			if b.focused {
				for _, r := range selection(e.core, ll, visualLine, l.width) {
					c.FillRect(gui.Rect{X: at.X + r.X, Y: at.Y + r.Y, W: r.W, H: r.H}, gui.Solid(sel))
				}
			}
			ll.Para.Paint(c, at, ll.Spans)
		}
		if lb.bl.Picture != nil {
			ph := float32(0)
			if n := len(lb.bl.Lines); n > 0 {
				last := lb.bl.Lines[n-1]
				ph = last.Y + last.Para.Height
			}
			lb.bl.Picture.Paint(c.Sub(gui.Rect{X: padX, Y: oy + ph, W: lb.bl.PictureSize.W, H: lb.bl.PictureSize.H}))
		}
		if cl >= l.blocks[i].From && cl < l.blocks[i].To {
			if li, pos, ok := posIn(lb, cl, ccol); ok {
				ll := lb.bl.Lines[li]
				r := ll.Para.Caret(pos)
				r.X += padX
				r.Y += oy + ll.Y
				b.caret, b.caretBase, b.caretOn = r, r.H*0.8, b.focused
			}
		}
		y += lb.bl.Height
	}
	if b.caretOn {
		paintCaret(c, b.caret, e.core.Mode(), th)
	}
}

// selection is a line's selected clusters as highlight rects, in the paragraph's coordinates. A
// Visual-line selection covers each row of the line, the width of the text.
func selection(core *tuiwidget.EditorCore, ll LineLayout, wholeLine bool, width float32) []gui.Rect {
	if len(ll.Spans) == 0 || ll.Spans[0].Line < 0 {
		return nil
	}
	ln := ll.Spans[0].Line
	if wholeLine {
		if !core.Selected(ln, 0) {
			return nil
		}
		var out []gui.Rect
		for _, row := range ll.Para.Lines {
			out = append(out, gui.Rect{Y: row.Y, W: width, H: row.H})
		}
		return out
	}
	var out []gui.Rect
	for si, sp := range ll.Spans {
		n := clusters(sp.Text)
		start := -1
		for k := 0; k <= n; k++ {
			on := k < n && core.Selected(ln, sp.Col+k)
			switch {
			case on && start < 0:
				start = k
			case !on && start >= 0:
				out = append(out, ll.Para.Rects(flow.Pos{Span: si, Offset: byteOfCluster(sp.Text, start)},
					flow.Pos{Span: si, Offset: byteOfCluster(sp.Text, k)})...)
				start = -1
			}
		}
	}
	return out
}

// paintCaret draws the caret: a bar in Insert mode, a block over the cluster otherwise.
func paintCaret(c gui.Canvas, r gui.Rect, m tuiwidget.EditorMode, th Theme) {
	col := th.Text
	if th.Caret.A > 0 {
		col = th.Caret
	}
	if m == tuiwidget.ModeInsert {
		c.FillRect(gui.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, gui.Solid(col))
		return
	}
	col.A = 0x80
	c.FillRect(gui.Rect{X: r.X, Y: r.Y, W: max(th.Mono.Size*0.6, 2), H: r.H}, gui.Solid(col))
}

// selColor is the selection's fill: the accent, faint over the page.
func selColor(th Theme) color.NRGBA { return mix(th.Accent, th.Background, 0.35) }
