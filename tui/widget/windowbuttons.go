package widget

import (
	"maps"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// WindowButtons is an optional right-aligned row of ordinary close, maximize,
// and minimize Buttons. A WindowMod never creates it. No move/resize UI is added.
type WindowButtons struct {
	tui.MultiChild
	core    *WindowCore
	buttons []*Button
	ops     []WindowOperation // each button's, in the same order
	labels  map[WindowOperation]string
	styles  map[WindowOperation]*ButtonStyle
}

// windowControlLabels are the window controls' default labels, one cell each, as WindowButtons and
// a tab strip's controls (SetWindowButtons) draw them.
var windowControlLabels = map[WindowOperation]string{WindowClose: "x", WindowMaximize: "+", WindowMinimize: "_"}

// windowControlTokens are their default colours: close an error's, maximize a success's, minimize a
// warning's.
var windowControlTokens = map[WindowOperation]style.Token{
	WindowClose: style.TokenError, WindowMaximize: style.TokenSuccess, WindowMinimize: style.TokenWarning,
}

// WindowControl is one of WindowButtons' buttons and the operation it invokes.
type WindowControl struct {
	Op     WindowOperation
	Button *Button
}

// Controls are the buttons in their order on the bar, each with its operation: a native painter
// colours each by what it does, not by where it sits.
func (w *WindowButtons) Controls() []WindowControl {
	out := make([]WindowControl, len(w.buttons))
	for i, b := range w.buttons {
		out[i] = WindowControl{Op: w.ops[i], Button: b}
	}
	return out
}

// WindowButtonsOption configures appearance independently of window behavior.
type WindowButtonsOption func(*WindowButtons)

// WithWindowButtonLabels replaces the close, maximize and minimize labels.
func WithWindowButtonLabels(close, maximize, minimize string) WindowButtonsOption {
	return func(w *WindowButtons) {
		w.labels = map[WindowOperation]string{WindowClose: close, WindowMaximize: maximize, WindowMinimize: minimize}
	}
}

// WithWindowButtonStyle replaces one control's semantic default style.
func WithWindowButtonStyle(op WindowOperation, st *ButtonStyle) WindowButtonsOption {
	return func(w *WindowButtons) { w.styles[op] = st }
}

// NewWindowButtons constructs only the enabled presentation controls. The caller
// places the row in its frame; behavior and its bindings remain in core.
func NewWindowButtons(core *WindowCore, opts ...WindowButtonsOption) *WindowButtons {
	if core == nil {
		panic(fatalOf("widget: NewWindowButtons", "nil core", "controls need a window behavior target"))
	}
	w := &WindowButtons{core: core, styles: make(map[WindowOperation]*ButtonStyle), labels: maps.Clone(windowControlLabels)}
	w.Label("WindowButtons")
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}
	s := core.State()
	for _, control := range []struct {
		op      WindowOperation
		enabled bool
		action  tui.Action
	}{
		{WindowMinimize, s.Minimize, WindowMinimizeAction{}},
		{WindowMaximize, s.Maximize, WindowMaximizeAction{}},
		{WindowClose, s.Close, WindowCloseAction{}},
	} {
		if !control.enabled {
			continue
		}
		st := w.styles[control.op]
		if st == nil {
			normal := style.New().Foreground(windowControlTokens[control.op])
			st = NewButtonStyle(normal, normal.Reverse(true))
		}
		action := control.action
		button := NewButton(w.labels[control.op], WithButtonDecoration("", ""), WithButtonStyle(st),
			WithOnActivate(func() { core.InvokeInput(action) }))
		w.buttons = append(w.buttons, button)
		w.ops = append(w.ops, control.op)
		w.Add(button)
	}
	return w
}

// Init mounts real Button controls and observes state only for this UI lifetime.
func (w *WindowButtons) Init(ctx *tui.Context) {
	w.MultiChild.Init(ctx)
	w.refresh()
	ctx.OnUnmount(w.core.Observe(func() {
		if ctx.Mounted() {
			w.refresh()
			ctx.MarkDirty()
		}
	}))
}

func (w *WindowButtons) refresh() {
	s := w.core.State()
	for _, button := range w.buttons {
		enabled := w.core.live() && !s.Closed && !s.Minimized
		button.SetEnabled(enabled)
	}
}

// Layout right-aligns controls with geometry measured by each Button.
func (w *WindowButtons) Layout(c tui.Constraints) tui.Size {
	width := c.MaxW
	if width == tui.Unbounded {
		width = 0
		for _, b := range w.buttons {
			sz := w.Ctx().LayoutChild(b, tui.Constraints{MaxW: tui.Unbounded, MaxH: 1})
			width += sz.W + 1
		}
		width = max(width-1, 0)
	}
	x, height := width, 0
	for i := len(w.buttons) - 1; i >= 0; i-- {
		b := w.buttons[i]
		sz := w.Ctx().LayoutChild(b, tui.Constraints{MaxW: max(x, 0), MaxH: min(c.MaxH, 1)})
		x -= sz.W
		w.Ctx().PlaceChild(b, tui.Rect{X: max(x, 0), W: sz.W, H: sz.H})
		height = max(height, sz.H)
		x--
	}
	return c.Constrain(tui.Size{W: width, H: height})
}

// Render draws no synthetic control regions; the Button children paint themselves.
func (w *WindowButtons) Render(tui.Surface) {}

// HandleEvent leaves ordinary keyboard traversal to the Button children.
func (w *WindowButtons) HandleEvent(tui.Event) bool { return false }
