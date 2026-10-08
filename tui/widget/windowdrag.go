package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"math"
)

type windowDrag struct {
	operation        WindowOperation
	button           tui.MouseButton
	origin           tui.Point
	start, requested tui.Rect
	restore          func()
	handle           Handle
	changed          bool
}

func (c *WindowCore) pointerPosition(at tui.Point) tui.Point {
	if c.pointerFrame != nil {
		return c.pointerFrame(at)
	}
	r := c.geometry.Bounds()
	return tui.Point{X: windowCellAdd(r.X, at.X), Y: windowCellAdd(r.Y, at.Y)}
}

// ResolvePointerPress reserves only an enabled, currently usable drag capability.
func (c *WindowCore) ResolvePointerPress(e tui.MouseEvent) (tui.Action, bool) {
	if e.Kind != tui.MousePress || !c.live() || c.busy || c.closed || c.minimized || c.drag != nil || c.State().Maximized || c.geometry == nil || c.geometry.Bounds().Empty() {
		return nil, false
	}
	b := windowBinding{e.Mods.Chord(), e.Button}
	op := WindowMove
	switch {
	case c.move && b == c.moveBinding:
	case c.resize && b == c.resizeBinding:
		op = WindowResize
	default:
		return nil, false
	}
	return WindowDragBeginAction{Operation: op, At: tui.Point{X: e.X, Y: e.Y}}, true
}

// ResolveCaptureEscape cancels the held gesture without a content key binding.
func (c *WindowCore) ResolveCaptureEscape(e tui.KeyEvent) (tui.Action, bool) {
	if c.drag != nil && e.Kind != tui.KeyRelease && e.Code == tui.KeyEscape && e.Mods.Chord() == 0 {
		return WindowDragCancelAction{}, true
	}
	return nil, false
}

// BeginPointerDrag is the compatibility entry for a view's own raw pointer
// handler. It must run in the bound node's HandleEvent/HandleAction; capture
// still enforces the actual handler identity rather than borrowing another's.
func (c *WindowCore) BeginPointerDrag(op WindowOperation, e tui.MouseEvent, handle Handle) bool {
	if e.Kind != tui.MousePress || e.Button < tui.MouseLeft || e.Button > tui.MouseRight {
		return false
	}
	return c.beginDrag(WindowDragBeginAction{Operation: op, At: tui.Point{X: e.X, Y: e.Y}}, e.Button, handle)
}

func (c *WindowCore) beginDrag(a WindowDragBeginAction, button tui.MouseButton, handle Handle) bool {
	if !c.canBeginWindowDrag() {
		return false
	}
	switch a.Operation {
	case WindowMove:
		if !c.move {
			return false
		}
	case WindowResize:
		if !c.resize || !handle.resizes() {
			return false
		}
	default:
		return false
	}
	start := c.geometry.Bounds()
	if start.Empty() {
		return false
	}
	ctx, owner := c.ctx, c.owner
	if !ctx.FocusWithin(c.content) {
		ctx.FocusInto(c.content)
		if ctx.HoldsFocusable(c.content) && !ctx.FocusWithin(c.content) {
			return false
		}
	}
	if !c.canBeginWindowDrag() || c.owner != owner || c.ctx != ctx || !ctx.InInputScope() || !ctx.CapturePointer() {
		return false
	}
	restore := c.geometry.Checkpoint()
	if restore == nil {
		ctx.ReleasePointer()
		return false
	}
	c.drag = &windowDrag{operation: a.Operation, button: button,
		origin: c.pointerPosition(a.At),
		start:  start, requested: c.geometry.RequestedBounds(), restore: restore, handle: handle}
	return true
}

func (c *WindowCore) canBeginWindowDrag() bool {
	return c.live() && !c.busy && !c.closed && !c.minimized && c.drag == nil &&
		c.geometry != nil && !c.State().Maximized && c.ctx.Visible() &&
		(c.visibility == nil || c.visibility.WindowVisible())
}

func (c *WindowCore) stepBounds() tui.Rect {
	// the geometry as last asked for, not as last laid out: two steps before a layout add up
	r := c.geometry.RequestedBounds()
	if c.drag != nil {
		r = c.drag.start
		c.cancelDrag()
	}
	return r
}

func (c *WindowCore) cancelDrag() bool {
	d := c.drag
	if d == nil {
		return false
	}
	c.drag = nil
	d.restore()
	if c.live() {
		c.ctx.ReleasePointer()
	}
	c.notifyObservers()
	return true
}

// HandleEvent updates only the captured pointer gesture; loss keeps reached geometry.
func (c *WindowCore) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.PointerCaptureLostEvent); ok {
		c.drag = nil
		return false
	}
	e, ok := ev.(tui.MouseEvent)
	if !ok || c.drag == nil || !c.live() {
		return false
	}
	d := c.drag
	switch e.Kind {
	case tui.MouseMotion:
		at := c.pointerPosition(tui.Point{X: e.X, Y: e.Y})
		dx, dy := windowCellSubtract(at.X, d.origin.X), windowCellSubtract(at.Y, d.origin.Y)
		if dx == 0 && dy == 0 {
			if d.changed {
				d.restore()
				d.changed = false
			}
			return true
		}
		r := d.start
		if d.operation == WindowMove {
			r.X = windowCellAdd(r.X, dx)
			r.Y = windowCellAdd(r.Y, dy)
		} else {
			dw, dh := dx, dy
			if d.handle.dx() < 0 {
				dw = windowCellSubtract(0, dx)
			}
			if d.handle.dy() < 0 {
				dh = windowCellSubtract(0, dy)
			}
			r = c.resizedBounds(r, dw, dh, d.handle)
		}
		if c.geometry.SetBounds(r) {
			d.changed = true
		}
		return true
	case tui.MouseRelease:
		if e.Button != d.button {
			return true
		}
		c.drag = nil
		ctx, owner := c.ctx, c.owner
		ctx.ReleasePointer()
		if c.geometry.RequestedBounds() == d.requested {
			d.restore()
		} else {
			c.publishWindowChanged(d.operation, ctx, owner)
		}
		return true
	case tui.MousePress:
		return true
	}
	return false
}

func (c *WindowCore) resizedBounds(r tui.Rect, dw, dh int, handle Handle) tui.Rect {
	area := c.geometry.Area()
	if handle.dx() != 0 {
		limit := max(area.X+area.W-r.X, 0)
		if handle.dx() < 0 {
			limit = max(r.X+r.W-area.X, 0)
		}
		width := clampInt(windowCellAdd(r.W, dw), min(c.minimum.W, limit), limit)
		if handle.dx() < 0 {
			r.X += r.W - width
		}
		r.W = width
	}
	if handle.dy() != 0 {
		limit := max(area.Y+area.H-r.Y, 0)
		if handle.dy() < 0 {
			limit = max(r.Y+r.H-area.Y, 0)
		}
		height := clampInt(windowCellAdd(r.H, dh), min(c.minimum.H, limit), limit)
		if handle.dy() < 0 {
			r.Y += r.H - height
		}
		r.H = height
	}
	return r
}

func windowCellAdd(n, d int) int {
	if d > 0 && n > math.MaxInt-d {
		return math.MaxInt
	}
	if d < 0 && n < math.MinInt-d {
		return math.MinInt
	}
	return n + d
}

func windowCellSubtract(n, d int) int {
	if d > 0 && n < math.MinInt+d {
		return math.MinInt
	}
	if d < 0 && n > math.MaxInt+d {
		return math.MaxInt
	}
	return n - d
}
