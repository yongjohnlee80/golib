package widget

import "github.com/yongjohnlee80/golib/tui"

// WindowOperation identifies the completed operation, independent of its control.
type WindowOperation uint8

const (
	// WindowMove is a completed placement translation.
	WindowMove WindowOperation = iota
	// WindowResize is a completed size request.
	WindowResize
	// WindowMaximize is a maximize/restore or docked-pane zoom toggle.
	WindowMaximize
	// WindowMinimize is retained conceal into a collector.
	WindowMinimize
	// WindowRestore reveals a collector entry without remounting content.
	WindowRestore
	// WindowClose is an accepted owner dismissal.
	WindowClose
)

// String is the stable operation name used by declarative completion signals.
func (op WindowOperation) String() string {
	switch op {
	case WindowMove:
		return "move"
	case WindowResize:
		return "resize"
	case WindowMaximize:
		return "maximize"
	case WindowMinimize:
		return "minimize"
	case WindowRestore:
		return "restore"
	case WindowClose:
		return "close"
	}
	return "unknown"
}

// WindowChangedEvent reports one completed operation, not intermediate motion.
// Bounds is the normalized request; Owner is per mount and Key is consumer-owned.
type WindowChangedEvent struct {
	Owner                tui.NodeID
	Key                  string
	Operation            WindowOperation
	Bounds               tui.Rect
	Maximized, Minimized bool
}

// WindowDragBeginAction starts a gesture at At in the receiver's local cells.
type WindowDragBeginAction struct {
	Operation WindowOperation
	At        tui.Point
}

// ActionID names a window drag acquisition.
func (WindowDragBeginAction) ActionID() tui.ActionID { return "window.drag.begin" }

// WindowDragCancelAction restores the drag's original placement policy.
type WindowDragCancelAction struct{}

// ActionID names checkpoint cancellation.
func (WindowDragCancelAction) ActionID() tui.ActionID { return "window.drag.cancel" }

// WindowMoveByAction requests a cell translation through the target policy.
type WindowMoveByAction struct{ DX, DY int }

// ActionID names a discrete window move.
func (WindowMoveByAction) ActionID() tui.ActionID { return "window.move.by" }

// WindowResizeByAction requests a cell size delta through the target policy.
type WindowResizeByAction struct{ DW, DH int }

// ActionID names a discrete window resize.
func (WindowResizeByAction) ActionID() tui.ActionID { return "window.resize.by" }

// WindowMaximizeAction toggles the window or pane's owning zoom policy.
type WindowMaximizeAction struct{}

// ActionID names the owning maximize/restore toggle.
func (WindowMaximizeAction) ActionID() tui.ActionID { return "window.maximize.toggle" }

// WindowMinimizeAction requests non-modal retained conceal into the collector.
type WindowMinimizeAction struct{}

// ActionID names collector minimize.
func (WindowMinimizeAction) ActionID() tui.ActionID { return "window.minimize" }

// WindowRestoreAction reveals a minimized window through its owner.
type WindowRestoreAction struct{}

// ActionID names collector restore.
func (WindowRestoreAction) ActionID() tui.ActionID { return "window.restore" }

// WindowCloseAction requests the configured owner dismissal policy.
type WindowCloseAction struct{}

// ActionID names owner close.
func (WindowCloseAction) ActionID() tui.ActionID { return "window.close" }

func (c *WindowCore) applyAction(action tui.Action) bool {
	if nilLike(action) {
		return false
	}
	switch a := action.(type) {
	case WindowDragCancelAction, *WindowDragCancelAction:
		return c.cancelDrag()
	case WindowMoveByAction:
		return c.MoveBy(a.DX, a.DY)
	case *WindowMoveByAction:
		return c.MoveBy(a.DX, a.DY)
	case WindowResizeByAction:
		return c.ResizeBy(a.DW, a.DH)
	case *WindowResizeByAction:
		return c.ResizeBy(a.DW, a.DH)
	case WindowMaximizeAction, *WindowMaximizeAction:
		return c.ToggleMaximize()
	case WindowMinimizeAction, *WindowMinimizeAction:
		return c.Minimize()
	case WindowRestoreAction, *WindowRestoreAction:
		return c.Restore()
	case WindowCloseAction, *WindowCloseAction:
		return c.Close()
	}
	return false
}
