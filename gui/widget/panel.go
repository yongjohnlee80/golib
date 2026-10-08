// Package widget holds gui's native widgets: components that lay out in tui's cells and draw
// natively when a gui backend hosts them, and in cells on any other backend.
package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// Panel is a window inside the app: a title bar over its content. Every capability is off until
// an option turns it on. A movable or resizable Panel lives in a tui Float (InFloat); in an
// ordinary layout it ignores both.
type Panel struct {
	tui.MultiChild
	ctx *tui.Context

	title   string
	content tui.Component
	leading []tui.Component

	movable, resizable, minimizable, maximizable bool
	onClose                                      func()

	state   panelState
	float   *tuiwidget.Float
	rect    tui.Rect // where the Float places it, in the Float's area
	restore tui.Rect // the rect before maximizing

	w, h   int
	titleX int // where the title starts: after the leading widgets, set by Layout
	drag   panelDrag
	grab   tui.Point // the pointer where the drag began, in the Float's area
	from   tui.Rect  // the rect where the drag began
	hover  control
	barSt  style.Style
}

type panelState uint8

const (
	normal panelState = iota
	minimized
	maximized
)

type panelDrag uint8

const (
	noDrag panelDrag = iota
	moving
	resizeRight
	resizeBottom
	resizeCorner
)

// control is a title-bar button, by its place from the right edge.
type control int8

const (
	noControl control = iota - 1
	closeControl
	maximizeControl
	minimizeControl
)

// controlCells is each control's width on the title bar.
const controlCells = 3

// PanelOption sets up a Panel under construction.
type PanelOption func(*Panel)

// NewPanel is a panel over content.
func NewPanel(content tui.Component, opts ...PanelOption) *Panel {
	p := &Panel{content: content, hover: noControl, barSt: style.New().Reverse(true)}
	p.Label("Panel")
	for _, o := range opts {
		o(p)
	}
	p.Add(p.leading...)
	p.Add(content)
	return p
}

// WithTitle sets the title bar's text.
func WithTitle(s string) PanelOption { return func(p *Panel) { p.title = s } }

// TitleLeading puts ws at the title bar's left: tui children, with their own focus and keys.
func TitleLeading(ws ...tui.Component) PanelOption {
	return func(p *Panel) { p.leading = append(p.leading, ws...) }
}

// Movable lets a drag of the title bar move the panel.
func Movable() PanelOption { return func(p *Panel) { p.movable = true } }

// Resizable lets a drag of the right edge, the bottom edge or their corner resize the panel.
func Resizable() PanelOption { return func(p *Panel) { p.resizable = true } }

// Closable shows a close control that calls onClose.
func Closable(onClose func()) PanelOption { return func(p *Panel) { p.onClose = onClose } }

// Minimizable shows a control that collapses the panel to its title bar, and back.
func Minimizable() PanelOption { return func(p *Panel) { p.minimizable = true } }

// Maximizable shows a control, and takes a title-bar double-click, that fills the Float's area,
// and again restores.
func Maximizable() PanelOption { return func(p *Panel) { p.maximizable = true } }

// WithBarStyle sets the title bar's look in cells.
func WithBarStyle(st style.Style) PanelOption { return func(p *Panel) { p.barSt = st } }

// InFloat places the panel in f at r, the rect it moves and resizes from. f's child must be the
// panel. The panel sets f's anchor to AtRect(r).
func (p *Panel) InFloat(f *tuiwidget.Float, r tui.Rect) {
	p.float, p.rect = f, r
	f.SetAnchor(tuiwidget.AtRect(r))
}

// Bounds is where the panel is in its Float's area, as placed: its title row alone when
// minimized, the whole area when maximized. The zero Rect outside a Float.
func (p *Panel) Bounds() tui.Rect {
	switch {
	case p.float == nil:
		return tui.Rect{}
	case p.state == maximized:
		return tui.Rect{W: p.w, H: p.h}
	case p.state == minimized:
		return tui.Rect{X: p.rect.X, Y: p.rect.Y, W: p.rect.W, H: 1}
	}
	return p.rect
}

// Minimized and Maximized report the panel's state.
func (p *Panel) Minimized() bool { return p.state == minimized }
func (p *Panel) Maximized() bool { return p.state == maximized }

// Init keeps the panel's Context and mounts its children.
func (p *Panel) Init(ctx *tui.Context) {
	p.ctx = ctx
	p.MultiChild.Init(ctx)
}

// controls are the enabled controls, right to left.
func (p *Panel) controls() []control {
	var cs []control
	if p.onClose != nil {
		cs = append(cs, closeControl)
	}
	if p.maximizable {
		cs = append(cs, maximizeControl)
	}
	if p.minimizable {
		cs = append(cs, minimizeControl)
	}
	return cs
}

// Layout puts the leading widgets on the title row, after one cell, and the content under it.
// Minimized, the content gets no rows.
func (p *Panel) Layout(c tui.Constraints) tui.Size {
	w := c.MaxW
	if w == tui.Unbounded {
		w = max(c.MinW, len(p.title)+4+controlCells*len(p.controls()))
	}
	x := 1
	right := w - controlCells*len(p.controls())
	for _, l := range p.leading {
		sz := p.ctx.LayoutChild(l, tui.Constraints{MaxW: max(right-x, 0), MaxH: 1})
		p.ctx.PlaceChild(l, tui.Rect{X: x, Y: 0, W: sz.W, H: 1})
		x += sz.W + 1
	}
	p.titleX = x
	h := 1
	if p.state == minimized {
		p.ctx.LayoutChild(p.content, tui.Constraints{})
		p.ctx.PlaceChild(p.content, tui.Rect{X: 0, Y: 1})
	} else {
		maxH := tui.Unbounded
		if c.MaxH != tui.Unbounded {
			maxH = max(c.MaxH-1, 0)
		}
		sz := p.ctx.LayoutChild(p.content, tui.Constraints{MinW: w, MaxW: w, MinH: max(c.MinH-1, 0), MaxH: maxH})
		p.ctx.PlaceChild(p.content, tui.Rect{X: 0, Y: 1, W: w, H: sz.H})
		h += sz.H
	}
	p.w, p.h = w, h
	return c.Constrain(tui.Size{W: w, H: h})
}

// controlAt is the control under column x of the title row.
func (p *Panel) controlAt(x int) control {
	cs := p.controls()
	for i, c := range cs {
		right := p.w - i*controlCells
		if x >= right-controlCells && x < right {
			return c
		}
	}
	return noControl
}

// Render paints the panel's rect, then the title bar in cells: the bar, the title, and
// [x] [□] [_] for the enabled controls. A gui backend draws its native bar over these cells (NativeView).
func (p *Panel) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	// A window is opaque: what lies under it never shows where its content paints nothing.
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", style.New())
	s.Fill(tui.Rect{W: sz.W, H: 1}, " ", p.barSt)
	putString(s, p.titleX, 0, p.title, p.barSt.Bold(true))
	for i, c := range p.controls() {
		x := sz.W - (i+1)*controlCells
		putString(s, x, 0, [...]string{"[x]", "[□]", "[_]"}[c], p.barSt)
	}
}

// putString sets s's cells from (x, y) to s's clusters, one cell each but for wide ones, and
// returns the column after them.
func putString(s tui.Surface, x, y int, str string, st style.Style) int {
	for c := range tui.Graphemes(str) {
		if x >= s.Size().W {
			return x
		}
		s.SetCell(x, y, c, st)
		x += max(s.StringWidth(c), 1)
	}
	return x
}

// HandleEvent takes the title bar's clicks and drags, and the edges' resizing drags.
func (p *Panel) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.MouseEvent:
		return p.mouse(e)
	case tui.PointerLeaveEvent:
		p.setHover(noControl)
	}
	return false
}

func (p *Panel) mouse(m tui.MouseEvent) bool {
	inFloat := p.float != nil && p.state != maximized
	switch m.Kind {
	case tui.MouseMotion:
		if p.drag != noDrag {
			p.dragTo(m)
			return true
		}
		if m.Y == 0 {
			p.setHover(p.controlAt(m.X))
		} else {
			p.setHover(noControl)
		}
		return false
	case tui.MouseRelease:
		if p.drag != noDrag {
			p.drag = noDrag
			p.ctx.ReleasePointer()
			return true
		}
		if m.Y == 0 && m.Button == tui.MouseLeft {
			if c := p.controlAt(m.X); c != noControl {
				p.act(c)
				return true
			}
		}
		return false
	case tui.MousePress:
		if m.Button != tui.MouseLeft {
			return false
		}
		if m.Y == 0 && p.controlAt(m.X) != noControl {
			return true // the control acts on release, as a button does
		}
		if m.Y == 0 && m.Count == 2 && p.maximizable {
			p.act(maximizeControl)
			return true
		}
		kind := noDrag
		switch {
		case p.resizable && inFloat && m.X == p.w-1 && m.Y == p.h-1:
			kind = resizeCorner
		case p.resizable && inFloat && m.X == p.w-1 && m.Y > 0:
			kind = resizeRight
		case p.resizable && inFloat && m.Y == p.h-1 && p.state != minimized:
			kind = resizeBottom
		case p.movable && inFloat && m.Y == 0:
			kind = moving
		}
		if kind == noDrag || !p.ctx.CapturePointer() {
			return false
		}
		p.drag, p.from = kind, p.rect
		p.grab = tui.Point{X: p.rect.X + m.X, Y: p.rect.Y + m.Y}
		return true
	}
	return false
}

// dragTo follows a drag: the pointer in the Float's area is the panel's origin plus its local
// position, and the rect is the drag's start moved or grown by the pointer's travel.
func (p *Panel) dragTo(m tui.MouseEvent) {
	at := tui.Point{X: p.rect.X + m.X, Y: p.rect.Y + m.Y}
	dx, dy := at.X-p.grab.X, at.Y-p.grab.Y
	r := p.from
	switch p.drag {
	case moving:
		r.X, r.Y = max(r.X+dx, 0), max(r.Y+dy, 0)
	case resizeRight:
		r.W = max(r.W+dx, p.minW())
	case resizeBottom:
		r.H = max(r.H+dy, 3)
	case resizeCorner:
		r.W, r.H = max(r.W+dx, p.minW()), max(r.H+dy, 3)
	}
	if r != p.rect {
		p.rect = r
		p.float.SetAnchor(tuiwidget.AtRect(r))
	}
}

// minW is the narrowest the panel resizes to: its controls and a little title.
func (p *Panel) minW() int { return controlCells*len(p.controls()) + 6 }

func (p *Panel) act(c control) {
	switch c {
	case closeControl:
		p.onClose()
	case minimizeControl:
		if p.state == minimized {
			p.state = normal
		} else {
			p.state = minimized
		}
		// In a Float the rect is the panel's size: minimized, the Float holds the title row
		// alone, so what was under the content shows and takes the pointer again.
		if p.float != nil {
			p.float.SetAnchor(tuiwidget.AtRect(p.Bounds()))
		}
		p.relayout()
	case maximizeControl:
		if p.float == nil {
			return
		}
		if p.state == maximized {
			p.state = normal
			p.float.SetSizeFraction(0, 0)
			p.rect = p.restore
			p.float.SetAnchor(tuiwidget.AtRect(p.rect))
		} else {
			p.state, p.restore = maximized, p.rect
			p.float.SetAnchor(tuiwidget.Center)
			p.float.SetSizeFraction(100, 100)
		}
		p.relayout()
	}
}

func (p *Panel) relayout() {
	if p.ctx != nil {
		p.ctx.RequestLayout()
	}
}

func (p *Panel) setHover(c control) {
	if c != p.hover {
		p.hover = c
		if p.ctx != nil {
			p.ctx.MarkDirty()
		}
	}
}

// The native title bar's control colours, macOS's.
var (
	closeRed      = color.NRGBA{R: 0xff, G: 0x5f, B: 0x57, A: 0xff}
	minimizeAmber = color.NRGBA{R: 0xfe, G: 0xbc, B: 0x2e, A: 0xff}
	maximizeGreen = color.NRGBA{R: 0x28, G: 0xc8, B: 0x40, A: 0xff}
)

// NativeView is the title bar a gui backend draws over the panel's own cells: the bar in the
// colours its cells were painted in, the title in the UI font, and round controls. Children's
// cells (the leading widgets, the content) cover it.
func (p *Panel) NativeView() (any, bool) {
	return gui.ViewFunc(p.paintBar), true
}

// NativeScope: the bar is the panel's own look, beneath its children.
func (p *Panel) NativeScope() tui.NativeScope { return tui.ScopeChrome }

func (p *Panel) paintBar(c gui.Canvas) {
	cell := c.CellSize()
	fg, bg := c.CellColors(0, 0)
	c.FillRect(gui.Rect{W: c.Size().W, H: cell.H}, gui.Solid(bg))
	if p.title != "" {
		t := c.Text().Layout(p.title, gui.Font{Size: cell.H * 0.62, Bold: true}, 0)
		c.DrawText(t, gui.Pt(float32(p.titleX)*cell.W, (cell.H-t.Height)/2), gui.Solid(fg))
	}
	r := min(cell.H*0.32, cell.W*1.2)
	for i, ctl := range p.controls() {
		cx := c.Size().W - (float32(i)+0.5)*controlCells*cell.W
		col := [...]color.NRGBA{closeRed, maximizeGreen, minimizeAmber}[ctl]
		dot := gui.Rect{X: cx - r, Y: cell.H/2 - r, W: 2 * r, H: 2 * r}
		c.FillEllipse(dot, gui.Solid(col))
		if p.hover == ctl {
			c.StrokeRRect(dot, r, 1, gui.Solid(color.NRGBA{A: 0x60}))
		}
	}
}

var (
	_ tui.NativeReporter = (*Panel)(nil)
	_ tui.NativeScoper   = (*Panel)(nil)
)
