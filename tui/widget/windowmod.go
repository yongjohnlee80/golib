package widget

import "github.com/yongjohnlee80/golib/tui"

// WindowMod gives unchanged content optional window operations without supplying
// any appearance. Its default target is its enclosing Float; docked layouts can
// supply their own target. All methods are owned by the application loop.
type WindowMod struct {
	Base
	child tui.Component
	core  *WindowCore
}

// NewWindowMod wraps content in independently optional window capabilities.
func NewWindowMod(content tui.Component, opts ...WindowModOption) *WindowMod {
	if nilLike(content) {
		panic(fatalOf("widget: NewWindowMod", "nil content", "a window capability wraps content"))
	}
	return &WindowMod{child: content, core: NewWindowCore(opts...)}
}

// Core returns the behavior shared with custom controls and GUI presentations.
func (w *WindowMod) Core() *WindowCore { return w.core }

// Init binds behavior to this mount and mounts the unchanged content.
func (w *WindowMod) Init(ctx *tui.Context) {
	w.Base.Init(ctx)
	w.core.Bind(ctx, w.child)
	ctx.Mount(w.child)
}

// Layout passes through the content's constraints and adds no chrome cells.
func (w *WindowMod) Layout(c tui.Constraints) tui.Size {
	s := w.ctx.LayoutChild(w.child, c)
	w.ctx.PlaceChild(w.child, tui.Rect{W: s.W, H: s.H})
	return s
}

// Render draws nothing; appearance belongs to the content and optional controls.
func (w *WindowMod) Render(tui.Surface) {}

// ResolvePointerPress reserves only enabled, configured window gestures.
func (w *WindowMod) ResolvePointerPress(e tui.MouseEvent) (tui.Action, bool) {
	return w.core.ResolvePointerPress(e)
}

// ResolveCaptureEscape resolves cancellation on this capture owner, not its content.
func (w *WindowMod) ResolveCaptureEscape(e tui.KeyEvent) (tui.Action, bool) {
	return w.core.ResolveCaptureEscape(e)
}

// HandleAction invokes the shared behavior with runtime-supplied input provenance.
func (w *WindowMod) HandleAction(inv tui.ActionInvocation) bool { return w.core.HandleAction(inv) }

// HandleEvent continues a captured drag and handles involuntary capture loss.
func (w *WindowMod) HandleEvent(ev tui.Event) bool { return w.core.HandleEvent(ev) }

// MoveBy moves through the target's placement policy, from application code.
func (w *WindowMod) MoveBy(dx, dy int) bool { return w.core.MoveBy(dx, dy) }

// ResizeBy resizes through the target's placement policy, from application code.
func (w *WindowMod) ResizeBy(dw, dh int) bool { return w.core.ResizeBy(dw, dh) }

// ToggleMaximize invokes the same zoom operation a keymap or control invokes.
func (w *WindowMod) ToggleMaximize() bool { return w.core.ToggleMaximize() }

// Minimize conceals a non-modal window into its explicitly supplied collector.
func (w *WindowMod) Minimize() bool { return w.core.Minimize() }

// Restore reveals a minimized window without remounting its content.
func (w *WindowMod) Restore() bool { return w.core.Restore() }

// Close runs the owner's dismissal policy; it never implicitly closes the app.
func (w *WindowMod) Close() bool { return w.core.Close() }

// InvokeInput applies a control's operation with the window's input-scope check.
func (w *WindowMod) InvokeInput(a tui.Action) bool { return w.core.InvokeInput(a) }
