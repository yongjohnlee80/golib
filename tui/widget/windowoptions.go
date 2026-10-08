package widget

import "github.com/yongjohnlee80/golib/tui"

// WindowModOption configures a capability widget or its shared behavior core.
type WindowModOption func(*WindowCore)

type windowBinding struct {
	mods   tui.Mods
	button tui.MouseButton
}

// WithWindowMove enables movement through the target's geometry policy.
func WithWindowMove() WindowModOption { return func(c *WindowCore) { c.move = true } }

// WithWindowResize enables resize without adding a grip or button.
func WithWindowResize() WindowModOption { return func(c *WindowCore) { c.resize = true } }

// WithWindowMaximize enables the target's maximize/restore or pane-zoom toggle.
func WithWindowMaximize() WindowModOption { return func(c *WindowCore) { c.maximize = true } }

// WithWindowMinimize enables retained, non-modal conceal into collector.
func WithWindowMinimize(collector WindowCollector) WindowModOption {
	return func(c *WindowCore) { c.minimize, c.collector = true, collector }
}

// WithWindowClose enables the target's default close policy, when it has one.
func WithWindowClose() WindowModOption { return func(c *WindowCore) { c.close = true } }

// WithWindowCloseHandler enables close with an owner-defined policy. Returning
// false vetoes dismissal; the handler may unmount its owner synchronously.
func WithWindowCloseHandler(fn func() bool) WindowModOption {
	return func(c *WindowCore) { c.close, c.handlerSet, c.onClose = true, true, fn }
}

// WithWindowTarget supplies an owner's geometry, zoom, visibility or dismissal
// facets. Only the interfaces required by enabled capabilities are bound.
func WithWindowTarget(target any) WindowModOption {
	return func(c *WindowCore) { c.customTarget, c.target = true, target }
}

// WithWindowLabel supplies collector and accessibility text, not a title bar.
func WithWindowLabel(title string) WindowModOption { return func(c *WindowCore) { c.label = title } }

// WithWindowKey supplies an optional stable identity for application persistence.
func WithWindowKey(key string) WindowModOption { return func(c *WindowCore) { c.key = key } }

// WithWindowChanged receives a completed change once, never for drag motion.
func WithWindowChanged(fn func(WindowChangedEvent)) WindowModOption {
	return func(c *WindowCore) { c.onChanged = fn }
}

// WithWindowMinimizedFocus optionally chooses focus after the runtime repairs it.
func WithWindowMinimizedFocus(fn func(*tui.Context)) WindowModOption {
	return func(c *WindowCore) { c.onMinimizedFocus = fn }
}

// WithWindowWorkArea reserves part of a default Float's viewport. fn is pure and
// its result is clipped to the viewport; it does not create or place a taskbar.
func WithWindowWorkArea(fn func(tui.Rect) tui.Rect) WindowModOption {
	return func(c *WindowCore) { c.workArea = fn }
}

// WithWindowMoveBinding replaces Alt/Option plus left-button drag.
func WithWindowMoveBinding(mods tui.Mods, button tui.MouseButton) WindowModOption {
	return func(c *WindowCore) { c.moveBinding = windowBinding{mods, button} }
}

// WithWindowResizeBinding replaces Alt/Option plus right-button drag.
func WithWindowResizeBinding(mods tui.Mods, button tui.MouseButton) WindowModOption {
	return func(c *WindowCore) { c.resizeBinding = windowBinding{mods, button} }
}

// WithWindowResizeHandle chooses the logical edge/corner changed by a resize drag.
// This configures geometry only and does not mount a visible resize grip.
func WithWindowResizeHandle(h Handle) WindowModOption { return func(c *WindowCore) { c.handle = h } }

// WithWindowMinSize sets the resize minimum before the placement owner's ceiling.
// The containing area still wins when it is smaller than this requested minimum.
func WithWindowMinSize(s tui.Size) WindowModOption { return func(c *WindowCore) { c.minimum = s } }
