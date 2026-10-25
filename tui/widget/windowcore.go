package widget

import "github.com/yongjohnlee80/golib/tui"

// WindowGeometry is the placement owner's geometry, in one owner-local frame.
// Checkpoint restores the requested policy, including anchors and proportions.
type WindowGeometry interface {
	Bounds() tui.Rect
	RequestedBounds() tui.Rect
	Area() tui.Rect
	SetBounds(tui.Rect) bool
	Checkpoint() func()
}

// WindowZoom is the owner's existing maximize/restore or pane-zoom operation.
type WindowZoom interface {
	ToggleMaximize() bool
	Maximized() bool
}

// WindowVisibility conceals content without ending its mounted lifetime.
type WindowVisibility interface {
	SetWindowVisible(bool) bool
	WindowVisible() bool
	Modal() bool
}

// WindowDismissal supplies a target's explicitly chosen close policy.
type WindowDismissal interface{ CloseWindow() bool }

// WindowHandles lets a target choose, per gesture, which handle a resize drags. A target without it
// resizes from the configured handle (WithWindowResizeHandle), as before. A tile has up to four
// sides it shares with its neighbours, and the one a resize moves depends on where it starts.
type WindowHandles interface {
	// HandleAt is the handle a resize press at p, in the geometry's frame, drags; false refuses it.
	HandleAt(p tui.Point) (Handle, bool)
	// StepHandle is the handle a keyboard resize step (ResizeBy) moves; false refuses the step.
	StepHandle(dw, dh int) (Handle, bool)
}

// WindowGesture lets a target own a gesture's start and completion. A target without it keeps the
// core's rules: a release completes when RequestedBounds changed, and a lost capture keeps the
// reached geometry and publishes nothing.
//
// A target needs it when an operation can change its policy without changing a cell: a tile
// re-docked in a manager too small for its minimum lands in the rect it started in.
type WindowGesture interface {
	// GestureBegan is told the operation and handle before a drag captures (pointer, with at the
	// press in the geometry's frame) or before a keyboard step (MoveBy, ResizeBy) applies; false
	// refuses it.
	GestureBegan(op WindowOperation, h Handle, at tui.Point, pointer bool) bool
	// GestureChanged reports, at release, whether the drag changed the target's policy: true
	// publishes WindowChangedEvent, false restores the checkpoint. It replaces the core's
	// RequestedBounds comparison for this target.
	GestureChanged() bool
	// GestureLost is called once after a lost capture, the core's drag already cleared and no event
	// published.
	GestureLost(op WindowOperation)
}

// WindowState is a value snapshot of configuration and live state, not config.
type WindowState struct {
	Move, Resize, Maximize, Minimize, Close bool
	Maximized, Minimized, Closed            bool
}

// WindowCore holds window behavior apart from any view. Bind it to the component
// that receives its actions and captured events. Methods are loop-owned.
type WindowCore struct {
	move, resize, maximize, minimize, close bool
	customTarget, handlerSet                bool
	target                                  any
	geometry                                WindowGeometry
	zoom                                    WindowZoom
	visibility                              WindowVisibility
	dismissal                               WindowDismissal
	handles                                 WindowHandles
	gesture                                 WindowGesture
	collector                               WindowCollector
	moveBinding, resizeBinding              windowBinding
	handle                                  Handle
	minimum                                 tui.Size
	maximum                                 tui.Size // zero on an axis without one
	label, key                              string
	workArea                                func(tui.Rect) tui.Rect
	onChanged                               func(WindowChangedEvent)
	onMinimizedFocus                        func(*tui.Context)
	onClose                                 func() bool
	ctx                                     *tui.Context
	content                                 tui.Component
	pointerFrame                            func(tui.Point) tui.Point
	owner                                   tui.NodeID
	minimized, closed, busy                 bool
	drag                                    *windowDrag
	observers                               observers
}

// NewWindowCore creates presentation-independent behavior for a WindowMod or GUI
// view. It allocates no controls, collector, taskbar, goroutine or global registry.
func NewWindowCore(opts ...WindowModOption) *WindowCore {
	c := &WindowCore{moveBinding: windowBinding{tui.ModAlt, tui.MouseLeft},
		resizeBinding: windowBinding{tui.ModAlt, tui.MouseRight}, handle: HandleBottomRight, minimum: tui.Size{W: 1, H: 1}}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if reason := c.configError(); reason != "" {
		panic(fatalOf("widget: NewWindowCore", reason, "configure capabilities before mounting"))
	}
	return c
}

func (c *WindowCore) configError() string {
	if c.minimum.W < 0 || c.minimum.H < 0 || c.minimum.W == tui.Unbounded || c.minimum.H == tui.Unbounded {
		return "window resize minimum must be finite and non-negative"
	}
	if c.maximum.W < 0 || c.maximum.H < 0 {
		return "window resize maximum must be non-negative"
	}
	if c.minimize && nilLike(c.collector) {
		return "minimize requires a collector"
	}
	if c.customTarget && nilLike(c.target) {
		return "nil window target"
	}
	if c.handlerSet && c.onClose == nil {
		return "nil close handler"
	}
	if !c.handle.resizes() {
		return "resize requires an edge or corner handle"
	}
	for _, b := range []windowBinding{c.moveBinding, c.resizeBinding} {
		if b.mods.Chord() != b.mods || b.button < tui.MouseLeft || b.button > tui.MouseRight {
			return "invalid window drag binding"
		}
	}
	if c.move && c.resize && c.moveBinding == c.resizeBinding {
		return "move and resize bindings overlap"
	}
	return ""
}

// Bind gives this core the actual event receiver's mount and unchanged content.
// It is called from that component's Init, once per mount; it mounts nothing.
func (c *WindowCore) Bind(ctx *tui.Context, content tui.Component) {
	c.bind(ctx, content, c.target, c.customTarget)
}

// BindTarget binds the view's placement collaborator during initialization.
func (c *WindowCore) BindTarget(ctx *tui.Context, content tui.Component, target any) {
	c.bind(ctx, content, target, true)
}

func (c *WindowCore) bind(ctx *tui.Context, content tui.Component, target any, explicit bool) {
	if c.live() && c.ctx != ctx {
		panic(fatalOf("widget: WindowCore.Bind", "core already bound to another live node", "one event receiver owns a behavior core"))
	}
	c.ctx, c.content, c.owner = ctx, content, ctx.ID()
	c.closed, c.minimized, c.drag = false, false, nil
	c.pointerFrame = nil
	needsTarget := c.move || c.resize || c.maximize || c.minimize || (c.close && c.onClose == nil) || c.workArea != nil
	if !explicit && needsTarget {
		if parent := ctx.Ancestor(func(v tui.Component) bool { _, ok := v.(WindowTargetProvider); return ok }); parent != nil {
			target, c.pointerFrame = parent.(WindowTargetProvider).WindowTargetFor(c, ctx)
		}
	}
	c.geometry, _ = target.(WindowGeometry)
	c.zoom, _ = target.(WindowZoom)
	c.visibility, _ = target.(WindowVisibility)
	c.dismissal, _ = target.(WindowDismissal)
	c.handles, _ = target.(WindowHandles)
	c.gesture, _ = target.(WindowGesture)
	if reason := c.bindError(explicit); reason != "" {
		panic(fatalOf("widget: WindowCore.Bind", reason, "supply the target facets required by enabled capabilities"))
	}
	owner := c.owner
	ctx.OnUnmount(func() {
		if c.collector != nil {
			c.collector.Remove(owner)
		}
		if c.owner == owner {
			c.ctx, c.content, c.drag = nil, nil, nil
			c.notifyObservers()
		}
	})
	c.notifyObservers()
}

func (c *WindowCore) bindError(explicit bool) string {
	if (c.move || c.resize) && nilLike(c.geometry) {
		return "movement/resize requires window geometry"
	}
	if c.maximize && nilLike(c.zoom) {
		return "maximize requires a zoom target"
	}
	if c.minimize && (nilLike(c.visibility) || c.visibility.Modal()) {
		return "collector minimize requires a non-modal visibility target"
	}
	if c.close && c.onClose == nil && nilLike(c.dismissal) {
		return "close requires a target dismissal or owner handler"
	}
	if explicit && c.workArea != nil {
		return "a custom target owns its work area"
	}
	if c.workArea != nil && c.geometry == nil {
		return "a work area requires an enclosing Float"
	}
	return ""
}

func (c *WindowCore) live() bool { return c.ctx != nil && c.ctx.Mounted() && c.ctx.ID() == c.owner }

// State returns configured capabilities and current state; zoom stays owner-owned.
func (c *WindowCore) State() WindowState {
	s := WindowState{Move: c.move, Resize: c.resize, Maximize: c.maximize,
		Minimize: c.minimize, Close: c.close, Minimized: c.minimized, Closed: c.closed}
	if c.zoom != nil {
		s.Maximized = c.zoom.Maximized()
	}
	return s
}

// SetMaxSize changes the resize maximum (WithWindowMaxSize) live: a later resize stops there. A
// zero or negative dimension has no maximum. It moves nothing: an owner that holds its placement
// to the maximum (a FloatWindow's) reads MaxSize as it lays out.
func (c *WindowCore) SetMaxSize(s tui.Size) { c.maximum = tui.Size{W: max(s.W, 0), H: max(s.H, 0)} }

// MaxSize is the resize maximum; zero on an axis without one.
func (c *WindowCore) MaxSize() tui.Size { return c.maximum }

// Bounds reports the target's committed owner-local geometry, or zero without it.
func (c *WindowCore) Bounds() tui.Rect {
	if c.geometry != nil {
		return c.geometry.Bounds()
	}
	return tui.Rect{}
}

// RequestedBounds reports the target's normalized placement request.
func (c *WindowCore) RequestedBounds() tui.Rect {
	if c.geometry != nil {
		return c.geometry.RequestedBounds()
	}
	return tui.Rect{}
}

// PlacementCheckpoint captures a live placement policy for a view-owned
// compatibility transition, such as collapsing a legacy title bar.
func (c *WindowCore) PlacementCheckpoint() func() {
	if c.geometry != nil && c.live() {
		return c.geometry.Checkpoint()
	}
	return nil
}

// Observe watches settled state changes. The consumer cancels on its own unmount.
func (c *WindowCore) Observe(fn func()) func() { return c.observers.add(fn) }

func (c *WindowCore) notifyObservers() { c.observers.notify() }

func (c *WindowCore) publishWindowChanged(op WindowOperation, ctx *tui.Context, owner tui.NodeID) {
	s := c.State()
	e := WindowChangedEvent{Owner: owner, Key: c.key, Operation: op, Maximized: s.Maximized, Minimized: s.Minimized}
	if c.geometry != nil {
		e.Bounds = c.geometry.RequestedBounds()
	}
	if c.onChanged != nil {
		c.onChanged(e)
	}
	ctx.Bus().Publish(e)
	c.notifyObservers()
}

func (c *WindowCore) transition(op WindowOperation, fn func() bool) bool {
	if !c.live() || c.busy || c.closed {
		return false
	}
	c.busy = true
	defer func() { c.busy = false }()
	ctx, owner := c.ctx, c.owner
	if !fn() {
		return false
	}
	c.publishWindowChanged(op, ctx, owner)
	return true
}

// MoveBy moves the committed rectangle through its owner's policy.
func (c *WindowCore) MoveBy(dx, dy int) bool {
	if !c.move || c.minimized || c.State().Maximized || (dx == 0 && dy == 0) {
		return false
	}
	return c.transition(WindowMove, func() bool {
		if c.gesture != nil && !c.gesture.GestureBegan(WindowMove, c.handle, tui.Point{}, false) {
			return false
		}
		r := c.stepBounds()
		r.X = windowCellAdd(r.X, dx)
		r.Y = windowCellAdd(r.Y, dy)
		return c.geometry.SetBounds(r)
	})
}

// ResizeBy resizes the committed rectangle through its owner's policy.
func (c *WindowCore) ResizeBy(dw, dh int) bool {
	if !c.resize || c.minimized || c.State().Maximized || (dw == 0 && dh == 0) {
		return false
	}
	return c.transition(WindowResize, func() bool {
		r := c.stepBounds()
		h := c.handle
		if c.handles != nil {
			var ok bool
			if h, ok = c.handles.StepHandle(dw, dh); !ok {
				return false
			}
		}
		if c.gesture != nil && !c.gesture.GestureBegan(WindowResize, h, tui.Point{}, false) {
			return false
		}
		r = c.resizedBounds(r, dw, dh, h)
		return c.geometry.SetBounds(r)
	})
}

// ToggleMaximize delegates to the layout owner's existing zoom operation.
func (c *WindowCore) ToggleMaximize() bool {
	if !c.maximize || c.minimized {
		return false
	}
	return c.transition(WindowMaximize, func() bool { c.cancelDrag(); return c.zoom.ToggleMaximize() })
}

// Minimize retains a non-modal window's lifetime while putting it in its collector.
func (c *WindowCore) Minimize() bool {
	if !c.minimize || c.minimized {
		return false
	}
	return c.transition(WindowMinimize, func() bool {
		c.cancelDrag()
		if !c.visibility.SetWindowVisible(false) {
			return false
		}
		c.ctx.InvalidateFocusability()
		c.minimized = true
		c.collector.Put(MinimizedWindow{Owner: c.owner, Key: c.key, Title: c.label, core: c})
		if c.onMinimizedFocus != nil {
			c.onMinimizedFocus(c.ctx)
		}
		return true
	})
}

// Restore reveals the same mount and placement. It is an application-owned call.
func (c *WindowCore) Restore() bool {
	if !c.minimize || !c.minimized {
		return false
	}
	return c.transition(WindowRestore, func() bool {
		if !c.visibility.SetWindowVisible(true) && !c.visibility.WindowVisible() {
			return false
		}
		c.minimized = false
		c.collector.Remove(c.owner)
		c.ctx.FocusInto(c.content)
		return true
	})
}

// Close invokes the configured dismissal; the default Float is hidden, not detached.
func (c *WindowCore) Close() bool {
	if !c.close {
		return false
	}
	return c.transition(WindowClose, func() bool {
		owner, collector := c.owner, c.collector
		c.cancelDrag()
		accepted := false
		if c.onClose != nil {
			accepted = c.onClose()
		} else {
			accepted = c.dismissal.CloseWindow()
		}
		if !accepted {
			return false
		}
		if c.owner == owner {
			c.closed, c.minimized = true, false
		}
		if collector != nil {
			collector.Remove(owner)
		}
		return true
	})
}

// InvokeInput applies a control's action only when this window is inside the
// active input scope. It does not manufacture runtime input provenance.
func (c *WindowCore) InvokeInput(a tui.Action) bool {
	return c.live() && c.ctx.InInputScope() && c.applyAction(a)
}

// HandleAction checks scope for input origins, not for application-owned actions.
func (c *WindowCore) HandleAction(inv tui.ActionInvocation) bool {
	if !c.live() || (inv.Origin != tui.OriginProgrammatic && !c.ctx.InInputScope()) {
		return false
	}
	if inv.Origin == tui.OriginPointer {
		if source, ok := inv.Source.(tui.MouseEvent); ok && source.Kind == tui.MousePress {
			if a, ok := actionValue(inv.Action).(WindowDragBeginAction); ok {
				return c.beginDrag(a, source.Button, a.handleOr(c.handle))
			}
		}
	}
	return c.applyAction(inv.Action)
}
