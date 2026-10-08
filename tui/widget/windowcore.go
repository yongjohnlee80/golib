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
	collector                               WindowCollector
	moveBinding, resizeBinding              windowBinding
	handle                                  Handle
	minimum                                 tui.Size
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
	observers                               map[uint64]func()
	nextObserver                            uint64
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
		if parent := ctx.Ancestor(func(v tui.Component) bool { _, ok := v.(*Float); return ok }); parent != nil {
			f := parent.(*Float)
			if f.windowTarget == nil {
				f.windowTarget = &floatWindowTarget{float: f}
			}
			if old := f.windowTarget.core; old != nil && old != c && old.live() {
				panic(fatalOf("widget: WindowCore.Bind", "Float already has a live window behavior owner", "compose capabilities on one widget"))
			}
			f.windowTarget.core, f.windowTarget.workArea = c, c.workArea
			target = f.windowTarget
			c.pointerFrame = func(at tui.Point) tui.Point {
				if r, ok := f.ctx.ResolveAnchor(ctx.NodeAnchor()); ok {
					return tui.Point{X: r.X + at.X, Y: r.Y + at.Y}
				}
				return at
			}
		}
	}
	c.geometry, _ = target.(WindowGeometry)
	c.zoom, _ = target.(WindowZoom)
	c.visibility, _ = target.(WindowVisibility)
	c.dismissal, _ = target.(WindowDismissal)
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
func (c *WindowCore) Observe(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	if c.observers == nil {
		c.observers = make(map[uint64]func())
	}
	c.nextObserver++
	id := c.nextObserver
	c.observers[id] = fn
	return func() { delete(c.observers, id) }
}

func (c *WindowCore) notifyObservers() {
	callbacks := make([]func(), 0, len(c.observers))
	for _, fn := range c.observers {
		callbacks = append(callbacks, fn)
	}
	for _, fn := range callbacks {
		fn()
	}
}

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
		r = c.resizedBounds(r, dw, dh, c.handle)
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
			switch a := inv.Action.(type) {
			case WindowDragBeginAction:
				return c.beginDrag(a, source.Button, c.handle)
			case *WindowDragBeginAction:
				if a != nil {
					return c.beginDrag(*a, source.Button, c.handle)
				}
			}
		}
	}
	return c.applyAction(inv.Action)
}
