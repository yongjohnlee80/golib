package widget

import (
	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
	"image/color"
)

// WindowButtons is the optional circular GUI appearance of ordinary TUI window
// control Buttons. The real children retain hit-testing, focus and activation.
// On a terminal the same component draws the controls' labeled cell fallback.
type WindowButtons struct {
	buttons       *tuiwidget.WindowButtons
	core          *tuiwidget.WindowCore
	ctx           *tui.Context
	colors        [3]color.NRGBA
	widgetOptions []tuiwidget.WindowButtonsOption
}

// WindowButtonsOption configures the GUI appearance, not window capabilities.
type WindowButtonsOption func(*WindowButtons)

// WithWindowButtonColors replaces close, maximize and minimize circle colors.
func WithWindowButtonColors(close, maximize, minimize color.NRGBA) WindowButtonsOption {
	return func(w *WindowButtons) { w.colors = [3]color.NRGBA{close, maximize, minimize} }
}

// WithWindowButtonWidgets configures labels and fallback styles on the real Buttons.
func WithWindowButtonWidgets(opts ...tuiwidget.WindowButtonsOption) WindowButtonsOption {
	return func(w *WindowButtons) { w.widgetOptions = append(w.widgetOptions, opts...) }
}

// NewWindowButtons creates circular presentation over an existing shared core.
func NewWindowButtons(core *tuiwidget.WindowCore, opts ...WindowButtonsOption) *WindowButtons {
	w := &WindowButtons{core: core, colors: [3]color.NRGBA{closeRed, maximizeGreen, minimizeAmber}}
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}
	w.buttons = tuiwidget.NewWindowButtons(core, w.widgetOptions...)
	return w
}

// Init mounts ordinary Buttons under this actual GUI component node.
func (w *WindowButtons) Init(ctx *tui.Context) { w.ctx = ctx; w.buttons.Init(ctx) }

// Layout uses the same measured cells as the real Button hit regions.
func (w *WindowButtons) Layout(c tui.Constraints) tui.Size { return w.buttons.Layout(c) }

// Render supplies the cell fallback; a GUI backend covers it with the native view.
func (w *WindowButtons) Render(s tui.Surface) { w.buttons.Render(s) }

// HandleEvent leaves input behavior to the real Button children.
func (w *WindowButtons) HandleEvent(tui.Event) bool { return false }

// NativeView returns circular appearance without replacing the input components.
func (w *WindowButtons) NativeView() (any, bool) { return gui.ViewFunc(w.paint), true }

// NativeScope covers the cell fallback; its Button nodes still receive input.
func (w *WindowButtons) NativeScope() tui.NativeScope { return tui.ScopeSubtree }

func (w *WindowButtons) paint(c gui.Canvas) {
	if w.ctx == nil {
		return
	}
	_, bg := c.CellColors(0, 0)
	c.FillRect(gui.Rect{W: c.Size().W, H: c.Size().H}, gui.Solid(bg))
	s := w.core.State()
	var colors []color.NRGBA
	if s.Minimize {
		colors = append(colors, w.colors[2])
	}
	if s.Maximize {
		colors = append(colors, w.colors[1])
	}
	if s.Close {
		colors = append(colors, w.colors[0])
	}
	cell := c.CellSize()
	for i, child := range w.buttons.All() {
		button, ok := child.(*tuiwidget.Button)
		if !ok || i >= len(colors) {
			continue
		}
		r, ok := w.ctx.ResolveAnchor(button.Context().NodeAnchor())
		if !ok || r.Empty() {
			continue
		}
		col := colors[i]
		if !button.ActivationAvailable() {
			col.A = 90
		}
		radius := min(cell.H*0.3, float32(r.W)*cell.W*0.45)
		cx, cy := (float32(r.X)+float32(r.W)/2)*cell.W, (float32(r.Y)+float32(r.H)/2)*cell.H
		dot := gui.Rect{X: cx - radius, Y: cy - radius, W: radius * 2, H: radius * 2}
		c.FillEllipse(dot, gui.Solid(col))
		if button.Armed() {
			c.StrokeRRect(dot, radius, 1, gui.Solid(color.NRGBA{A: 160}))
		}
	}
}
