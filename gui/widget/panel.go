package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// Floating Window & Panel Architecture:
//
//	+-------------------------------------------------------------+
//	| [Leading Widgets]  Window Title     (Optional: [_] [^] [X]) |  <- Title Bar Chrome
//	+-------------------------------------------------------------+
//	|                                                             |
//	|                                                             |
//	|                 Embedded Child Content                      |
//	|                 (Editor, HTMLView, Trees)                   |
//	|                                                             |
//	|                                                             |
//	+-------------------------------------------------------------+
//	|                                           (Optional:  /// ) |  <- Resize Grip
//	+-------------------------------------------------------------+
//
// Panel is a window inside the app: a title bar over its content.
// Every capability (moving, resizing, minimizing, maximizing, closing) is off by default until
// explicitly enabled by options (such as WithWindowMove, WithWindowResize, WithWindowClose).
//
// A movable or resizable Panel lives in a [tuiwidget.Float] (via InFloat); in an ordinary layout
// it ignores both. When hosted under a GUI backend, Panel renders hardware-accelerated vector title
// bars, crisp text, and anti-aliased control icons.
type Panel struct {
	tui.MultiChild
	ctx *tui.Context

	title    string
	content  tui.Component
	leading  []tui.Component
	trailing []tui.Component
	// leadingShare caps the leading widgets at this share of the bar's width; 0 leaves them the
	// whole bar (WithTitleLeadingShare)
	leadingShare float64

	movable, resizable, maximizable bool
	onClose                         func()

	float         *tuiwidget.Float
	rect          tui.Rect // where the Float places it, in the Float's area
	behavior      *tuiwidget.WindowCore
	windowOptions []tuiwidget.WindowModOption

	w, h   int
	titleX int // where the title starts: after the leading widgets, set by Layout
	// titleEnd is where the title's room ends: before the trailing widgets and the controls
	titleEnd int
	hover    control
	barSt    style.Style
	// the native bar's colours, from its last paint: what a leading widget draws in, so it sits
	// on the bar rather than on the cells' reversed row
	barText, barFill, barPage color.NRGBA
}

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
		if o != nil { // a nil option is no option
			o(p)
		}
	}
	var behavior []tuiwidget.WindowModOption
	if p.movable {
		behavior = append(behavior, tuiwidget.WithWindowMove())
	}
	if p.resizable {
		behavior = append(behavior, tuiwidget.WithWindowResize())
	}
	if p.maximizable {
		behavior = append(behavior, tuiwidget.WithWindowMaximize())
	}
	if p.onClose != nil {
		behavior = append(behavior, tuiwidget.WithWindowCloseHandler(func() bool {
			// WithClosable's onClose does not report whether it accepted the dismissal, so the
			// handler answers false and leaves the closing to onClose.
			p.onClose()
			return false
		}))
	}
	controls := 0
	if p.onClose != nil {
		controls++
	}
	if p.maximizable {
		controls++
	}
	if tuiwidget.NewWindowCore(p.windowOptions...).State().Minimize { // the capability, read before the core is built
		controls++
	}
	behavior = append(behavior, tuiwidget.WithWindowMinSize(tui.Size{W: controlCells*controls + 6, H: 3}))
	behavior = append(behavior, p.windowOptions...)
	p.behavior = tuiwidget.NewWindowCore(behavior...)
	p.Add(p.leading...)
	p.Add(p.trailing...)
	p.Add(content)
	return p
}

// WithTitle sets the title bar's text.
func WithTitle(s string) PanelOption { return func(p *Panel) { p.title = s } }

// WithTitleLeading puts ws at the title bar's left: tui children, with their own focus and keys.
func WithTitleLeading(ws ...tui.Component) PanelOption {
	return func(p *Panel) { p.leading = append(p.leading, ws...) }
}

// WithTitleTrailing puts ws at the title bar's right, before the window controls: tui children, with
// their own focus and keys, as WithTitleLeading's. The first given is the leftmost.
func WithTitleTrailing(ws ...tui.Component) PanelOption {
	return func(p *Panel) { p.trailing = append(p.trailing, ws...) }
}

// WithTitleLeadingShare caps the leading widgets at share of the bar's width (two thirds: 2.0/3), so
// the title and the trailing widgets keep the rest; 0, the default, caps them only by the room
// left.
func WithTitleLeadingShare(share float64) PanelOption {
	return func(p *Panel) { p.leadingShare = share }
}

// WithMovable lets a drag of the title bar move the panel.
func WithMovable() PanelOption { return func(p *Panel) { p.movable = true } }

// WithResizable lets a drag of the right edge, the bottom edge or their corner resize the panel.
func WithResizable() PanelOption { return func(p *Panel) { p.resizable = true } }

// WithClosable shows a close control that calls onClose.
func WithClosable(onClose func()) PanelOption { return func(p *Panel) { p.onClose = onClose } }

// WithMaximizable shows a control, and takes a title-bar double-click, that fills the Float's area,
// and again restores.
func WithMaximizable() PanelOption { return func(p *Panel) { p.maximizable = true } }

// WithBarStyle sets the title bar's look in cells.
func WithBarStyle(st style.Style) PanelOption { return func(p *Panel) { p.barSt = st } }

// WithWindowCapabilities supplies independent, presentation-free window options.
// Move/resize use configurable Alt/Option drag, and a plain drag on the title bar or a border
// (WindowCore.BeginFrameDrag). Minimizing is one of them:
// tuiwidget.WithWindowMinimize(collector) shows the minimize control, which hands
// the panel to the collector (a taskbar or a dock) until it is restored.
func WithWindowCapabilities(opts ...tuiwidget.WindowModOption) PanelOption {
	return func(p *Panel) { p.windowOptions = append(p.windowOptions, opts...) }
}

// WindowBehavior is the shared core a custom GUI presentation can invoke.
func (p *Panel) WindowBehavior() *tuiwidget.WindowCore { return p.behavior }

// InFloat places the panel in f at r, the rect it moves and resizes from. f's child must be the
// panel. The panel sets f's anchor to AtRect(r).
func (p *Panel) InFloat(f *tuiwidget.Float, r tui.Rect) {
	p.float, p.rect = f, r
	f.SetAnchor(tuiwidget.AtRect(r))
}

// Bounds is where the panel is in its Float's area, as placed: the whole area when maximized. The
// zero Rect outside a Float.
func (p *Panel) Bounds() tui.Rect {
	if r := p.behavior.Bounds(); !r.Empty() {
		return r
	}
	if p.float == nil {
		return tui.Rect{}
	}
	return p.rect
}

// Minimized and Maximized report the panel's window state: minimized is handed to its collector
// (WithWindowCapabilities with tuiwidget.WithWindowMinimize), and hidden.
func (p *Panel) Minimized() bool { return p.behavior.State().Minimized }
func (p *Panel) Maximized() bool { return p.behavior.State().Maximized }

// Init keeps the panel's Context and mounts its children.
func (p *Panel) Init(ctx *tui.Context) {
	p.ctx = ctx
	// a container that places windows (a Float, or any WindowTargetProvider) gives the panel its target
	parent := ctx.Ancestor(func(c tui.Component) bool { _, ok := c.(tuiwidget.WindowTargetProvider); return ok })
	if parent == nil && len(p.windowOptions) == 0 {
		p.behavior.BindTarget(ctx, p.content, staticPanelTarget{})
	} else {
		p.behavior.Bind(ctx, p.content)
	}
	p.MultiChild.Init(ctx)
}

// controls are the enabled controls, right to left.
func (p *Panel) controls() []control {
	var cs []control
	if p.behavior.State().Close {
		cs = append(cs, closeControl)
	}
	if p.behavior.State().Maximize {
		cs = append(cs, maximizeControl)
	}
	if p.behavior.State().Minimize {
		cs = append(cs, minimizeControl)
	}
	return cs
}

// Layout puts the trailing widgets on the title row before the controls, the leading ones after
// one cell, within their share, and the content under it.
func (p *Panel) Layout(c tui.Constraints) tui.Size {
	w := c.MaxW
	if w == tui.Unbounded {
		w = max(c.MinW, len(p.title)+4+controlCells*len(p.controls()))
	}
	right := w - controlCells*len(p.controls())
	for i := len(p.trailing) - 1; i >= 0; i-- {
		l := p.trailing[i]
		sz := p.ctx.LayoutChild(l, tui.Constraints{MaxW: max(right-1, 0), MaxH: 1})
		right -= sz.W
		p.ctx.PlaceChild(l, tui.Rect{X: right, Y: 0, W: sz.W, H: 1})
		right--
	}
	x, end := 1, right
	if p.leadingShare > 0 {
		end = min(end, 1+int(p.leadingShare*float64(w)))
	}
	for _, l := range p.leading {
		sz := p.ctx.LayoutChild(l, tui.Constraints{MaxW: max(end-x, 0), MaxH: 1})
		p.ctx.PlaceChild(l, tui.Rect{X: x, Y: 0, W: sz.W, H: 1})
		x += sz.W + 1
	}
	p.titleX, p.titleEnd = x, right
	maxH := tui.Unbounded
	if c.MaxH != tui.Unbounded {
		maxH = max(c.MaxH-1, 0)
	}
	sz := p.ctx.LayoutChild(p.content, tui.Constraints{MinW: w, MaxW: w, MinH: max(c.MinH-1, 0), MaxH: maxH})
	p.ctx.PlaceChild(p.content, tui.Rect{X: 0, Y: 1, W: w, H: sz.H})
	h := 1 + sz.H
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
	if p.titleX < p.titleEnd {
		tui.DrawText(s.Sub(tui.Rect{W: p.titleEnd, H: 1}), p.titleX, 0, p.title, p.barSt.Bold(true))
	}
	for i, c := range p.controls() {
		x := sz.W - (i+1)*controlCells
		tui.DrawText(s, x, 0, [...]string{"[x]", "[□]", "[_]"}[c], p.barSt)
	}
}

// HandleEvent takes the title bar's control clicks; the frame's drags are HandleUnclaimedPress's.
func (p *Panel) HandleEvent(ev tui.Event) bool {
	if p.behavior.HandleEvent(ev) {
		return true
	}
	switch e := ev.(type) {
	case tui.MouseEvent:
		return p.mouse(e)
	case tui.PointerLeaveEvent:
		p.setHover(noControl)
	}
	return false
}

// mouse handles mouse events for the panel's title bar controls: their hover, press and release.
func (p *Panel) mouse(m tui.MouseEvent) bool {
	switch m.Kind {
	case tui.MouseMotion:
		if m.Y == 0 {
			p.setHover(p.controlAt(m.X))
		} else {
			p.setHover(noControl)
		}
		return false
	case tui.MouseRelease:
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
		return m.Y == 0 && p.controlAt(m.X) != noControl // the control acts on release, as a button does
	}
	return false
}

// HandleUnclaimedPress takes a press on its frame that nothing in it wanted
// (tui.UnclaimedPressHandler): the rule every window shares, its title moving it and its sides and
// bottom resizing it (WindowCore.BeginFrameDrag), while it floats.
func (p *Panel) HandleUnclaimedPress(m tui.MouseEvent) bool {
	return p.float != nil && p.behavior.BeginFrameDrag(m, tui.Size{W: p.w, H: p.h})
}

// SubtreePointerShapes is a floating panel's resize arrows (tui.SubtreePointerShaper), over the
// frame cells a press resizes from (WindowCore.FrameShapes): its sides, its bottom row and its two
// bottom corners, over its content's cells too. The title row, which moves it and holds its
// controls, keeps the arrow.
func (p *Panel) SubtreePointerShapes(dst []tui.PointerRegion) []tui.PointerRegion {
	if p.float == nil {
		return dst
	}
	return p.behavior.FrameShapes(dst, tui.Size{W: p.w, H: p.h})
}

// ResolvePointerPress delegates configured Alt/Option gestures to shared behavior.
func (p *Panel) ResolvePointerPress(e tui.MouseEvent) (tui.Action, bool) {
	return p.behavior.ResolvePointerPress(e)
}

// ResolveCaptureEscape lets the actual capture owner cancel without content focus.
func (p *Panel) ResolveCaptureEscape(e tui.KeyEvent) (tui.Action, bool) {
	return p.behavior.ResolveCaptureEscape(e)
}

// HandleAction uses the same operation path as custom appearance and keymaps.
func (p *Panel) HandleAction(inv tui.ActionInvocation) bool { return p.behavior.HandleAction(inv) }

// act executes the action associated with the clicked title bar control.
func (p *Panel) act(c control) {
	switch c {
	case closeControl:
		p.behavior.InvokeInput(tuiwidget.WindowCloseAction{})
	case minimizeControl:
		p.behavior.InvokeInput(tuiwidget.WindowMinimizeAction{})
	case maximizeControl:
		p.behavior.InvokeInput(tuiwidget.WindowMaximizeAction{})
		p.relayout()
	}
}

// relayout requests a layout recalculation from the widget context.
func (p *Panel) relayout() {
	if p.ctx != nil {
		p.ctx.RequestLayout()
	}
}

// setHover updates the currently hovered title bar control and repaints if changed.
func (p *Panel) setHover(c control) {
	if c != p.hover {
		p.hover = c
		if p.ctx != nil {
			p.ctx.MarkDirty()
		}
	}
}

// The native title bar's control colours (gui.WindowControlColor).
var (
	closeRed      = gui.WindowControlColor(tuiwidget.WindowClose)
	minimizeAmber = gui.WindowControlColor(tuiwidget.WindowMinimize)
	maximizeGreen = gui.WindowControlColor(tuiwidget.WindowMaximize)
)

// NativeView is the title bar a gui backend draws over the panel's own cells: the bar in the
// colours its cells were painted in, the title in the UI font, and round controls. Children's
// cells (the leading widgets, the content) cover it.
func (p *Panel) NativeView() (any, bool) {
	return gui.ViewFunc(p.paintBar), true
}

// NativeScope: the bar is the panel's own look, beneath its children.
func (p *Panel) NativeScope() tui.NativeScope { return tui.ScopeChrome }

// The bar is a surface of the page, not the cells' row: a terminal marks the bar by reversing
// it, which in a window is a bright band across the panel. So the bar is the page colour around
// the panel tinted a little toward the text, with a hairline under it and the title muted. The
// text colour is whichever of the bar cells' colours stands off the page more, so a reversed or
// a plain bar style both give it.
func (p *Panel) paintBar(c gui.Canvas) {
	cell := c.CellSize()
	page := c.Backdrop()
	fg, bg, ok := openColors(c, 1)
	if !ok {
		return // the bar is wholly covered: the host paints the covering cells, and BarColors keep the last
	}
	text := fg
	if contrast(bg, page) > contrast(fg, page) {
		text = bg
	}
	fill := mix(text, page, 0.06)
	p.barText, p.barFill, p.barPage = text, fill, page
	w := c.Size().W
	c.FillRect(gui.Rect{W: w, H: cell.H}, gui.Solid(fill))
	c.FillRect(gui.Rect{Y: cell.H - 1, W: w, H: 1}, gui.Solid(mix(text, page, 0.16)))
	if p.title != "" {
		// with trailing widgets, the title keeps to its room left of them: none if it does not fit
		t := c.Text().Layout(p.title, gui.Font{Size: c.TextSize() * 0.92}, 0)
		if len(p.trailing) == 0 || t.Width <= float32(p.titleEnd-p.titleX)*cell.W {
			c.DrawText(t, gui.Pt(float32(p.titleX)*cell.W, (cell.H-t.Height)/2), gui.Solid(mix(text, page, 0.72)))
		}
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

// BarColors are the native bar's text, fill and page colours from its last paint: a widget on
// the bar (WithTitleLeading) draws in them. Zero before the first paint.
func (p *Panel) BarColors() (text, fill, page color.NRGBA) { return p.barText, p.barFill, p.barPage }

var (
	_ tui.NativeReporter = (*Panel)(nil)
	_ tui.NativeScoper   = (*Panel)(nil)
)
