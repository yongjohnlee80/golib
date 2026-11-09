package widget

import (
	"github.com/yongjohnlee80/gio/io/system"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TitleBar is an app's own title bar for its desktop window: one row with the title, trailing
// widgets (a status, a workspace name), and the window's close, maximize and minimize controls.
// Open the window with gui.WithDecorations(false) and put the bar at the top of the App's root.
//
// On the GUI backend the bar is drawn natively, its empty part is the window's drag area
// (Canvas.WindowMoveArea), a double-click on it maximizes or restores the window, and its controls
// act on the window (gui.Window). On a terminal there is no window to control: the bar is a row in
// cells with the title and the trailing widgets, and no controls.
type TitleBar struct {
	tui.MultiChild
	gui.Base
	ctx      *tui.Context
	window   gui.WindowControls // nil on a terminal
	title    string
	trailing []tui.Component

	w        int
	titleEnd int // the title's room ends here: before the trailing widgets and the controls
	hover    control
	pressed  control // the control a left press landed on: only a release on it acts
	barSt    style.Style
}

// titleX is where a title bar's title starts.
const titleX = 1

// NewTitleBar is a title bar showing title, with trailing at its right, before the controls: tui
// children, with their own focus and keys.
func NewTitleBar(title string, trailing ...tui.Component) *TitleBar {
	b := &TitleBar{title: title, trailing: trailing, hover: noControl, pressed: noControl, barSt: style.New().Reverse(true)}
	b.Label("TitleBar")
	b.Bind(b)
	b.Add(trailing...)
	return b
}

// SetTitle changes the title.
func (b *TitleBar) SetTitle(s string) {
	if s == b.title {
		return
	}
	b.title = s
	if b.ctx != nil {
		b.ctx.MarkDirty()
	}
}

// Title is the title shown.
func (b *TitleBar) Title() string { return b.title }

// Init keeps the bar's Context, finds its window, and mounts its children.
func (b *TitleBar) Init(ctx *tui.Context) {
	b.ctx = ctx
	if w, ok := gui.Window(ctx.App()); ok {
		b.window = w
	}
	b.MultiChild.Init(ctx)
}

// controls are the window's controls, right to left; none on a terminal.
func (b *TitleBar) controls() []control {
	if b.window == nil {
		return nil
	}
	return []control{closeControl, maximizeControl, minimizeControl}
}

// Layout is one row across the width: the trailing widgets right to left before the controls.
func (b *TitleBar) Layout(c tui.Constraints) tui.Size {
	w := c.MaxW
	if w == tui.Unbounded {
		w = max(c.MinW, len(b.title)+2+controlCells*len(b.controls()))
	}
	right := w - controlCells*len(b.controls())
	for i := len(b.trailing) - 1; i >= 0; i-- {
		l := b.trailing[i]
		sz := b.ctx.LayoutChild(l, tui.Constraints{MaxW: max(right-1, 0), MaxH: 1})
		right -= sz.W
		b.ctx.PlaceChild(l, tui.Rect{X: right, Y: 0, W: sz.W, H: 1})
		right--
	}
	b.w, b.titleEnd = w, right
	return c.Constrain(tui.Size{W: w, H: 1})
}

// Render draws the terminal's bar (RenderTUI) where native views aren't painted.
func (b *TitleBar) Render(s tui.Surface) { b.RenderFallback(b.ctx, s) }

// RenderTUI is the bar in cells: a reversed row with the title in bold.
func (b *TitleBar) RenderTUI(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	s.Fill(tui.Rect{W: sz.W, H: 1}, " ", b.barSt)
	if titleX < b.titleEnd {
		tui.DrawText(s.Sub(tui.Rect{W: b.titleEnd, H: 1}), titleX, 0, b.title, b.barSt.Bold(true))
	}
}

// Paint is the native bar (titleRow), with the window's drag area over the bar's empty part: from
// its left edge to the first trailing widget or control.
func (b *TitleBar) Paint(c gui.Canvas) {
	row := titleRow{title: b.title, titleX: titleX, titleEnd: b.titleEnd, fit: len(b.trailing) > 0,
		controls: b.controls(), hover: b.hover}
	if _, _, _, ok := row.paint(c); !ok {
		return
	}
	if b.window != nil && b.titleEnd > 0 {
		c.WindowMoveArea(gui.Rect{W: float32(b.titleEnd) * c.CellSize().W, H: c.CellSize().H})
	}
}

// NativeScope: the bar is the title bar's own look, beneath its trailing widgets.
func (b *TitleBar) NativeScope() tui.NativeScope { return tui.ScopeChrome }

// controlAt is the control under column x.
func (b *TitleBar) controlAt(x int) control {
	for i, c := range b.controls() {
		right := b.w - i*controlCells
		if x >= right-controlCells && x < right {
			return c
		}
	}
	return noControl
}

// HandleEvent takes the controls' hover and clicks, and a double-click on the bar.
func (b *TitleBar) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.MouseEvent:
		return b.mouse(e)
	case tui.PointerLeaveEvent:
		b.setHover(noControl)
	case tui.PointerCaptureLostEvent:
		b.pressed = noControl // the gesture was taken away: nothing acts
	}
	return false
}

// mouse acts as a button does: a left press on a control arms it and captures the pointer, so its
// release comes here wherever it lands, and the control acts only if that release is on it too. A
// release anywhere else, one no press here armed, or a lost capture does nothing.
func (b *TitleBar) mouse(m tui.MouseEvent) bool {
	if b.window == nil {
		return false
	}
	switch m.Kind {
	case tui.MouseMotion:
		if m.Y == 0 {
			b.setHover(b.controlAt(m.X))
		} else {
			b.setHover(noControl)
		}
	case tui.MousePress:
		if m.Button != tui.MouseLeft || m.Y != 0 {
			return false
		}
		if c := b.controlAt(m.X); c != noControl {
			b.pressed = c
			b.ctx.CapturePointer()
			return true
		}
		if m.Count == 2 {
			b.act(maximizeControl)
			return true
		}
	case tui.MouseRelease:
		if m.Button != tui.MouseLeft {
			return false
		}
		armed := b.pressed
		b.pressed = noControl
		if b.ctx.HasPointerCapture() {
			b.ctx.ReleasePointer()
		}
		if armed == noControl {
			return false
		}
		if m.Y == 0 && b.controlAt(m.X) == armed {
			b.act(armed)
		}
		return true
	}
	return false
}

// act performs control c on the window: maximize restores a maximized window.
func (b *TitleBar) act(c control) {
	switch c {
	case closeControl:
		b.window.Perform(system.ActionClose)
	case minimizeControl:
		b.window.Perform(system.ActionMinimize)
	case maximizeControl:
		if b.window.Maximized() {
			b.window.Perform(system.ActionUnmaximize)
		} else {
			b.window.Perform(system.ActionMaximize)
		}
	}
}

func (b *TitleBar) setHover(c control) {
	if c != b.hover {
		b.hover = c
		if b.ctx != nil {
			b.ctx.MarkDirty()
		}
	}
}

var (
	_ tui.NativeReporter = (*TitleBar)(nil)
	_ tui.NativeScoper   = (*TitleBar)(nil)
	_ gui.TUIMock        = (*TitleBar)(nil)
)
