package widget

import "github.com/yongjohnlee80/golib/tui"

// drag.go: one pointer gesture's lifecycle, shared by every widget a drag reshapes or scrolls (a
// Resizable's grip, a Split's divider, an Image's pan): begun with what a cancel restores, the
// pointer captured through the node that took it, released when the gesture ends or is cancelled,
// and only forgotten when the runtime revokes the capture. A revoked capture is not a cancellation:
// the state reached so far stands, since silently reverting the drag would be a change the user
// never made.

// resizeGesture maps a primary-button pointer event onto the resize vocabulary a grip or a divider
// shares: a press on what grab accepts begins (at at, in the owner's frame), and while dragging,
// motion updates and the release ends. A non-primary release does not end a primary drag.
func resizeGesture(e tui.MouseEvent, at tui.Point, dragging bool, grab func() (Handle, bool)) (tui.Action, bool) {
	if e.Button != tui.MouseLeft && e.Kind != tui.MouseMotion {
		return nil, false
	}
	switch e.Kind {
	case tui.MousePress:
		if h, ok := grab(); ok {
			return ResizeBeginAction{Handle: h, At: at}, true
		}
	case tui.MouseMotion:
		if dragging {
			return ResizeUpdateAction{At: at}, true
		}
	case tui.MouseRelease:
		if dragging {
			return ResizeEndAction{}, true
		}
	}
	return nil, false
}

// stepCellsOf is how many cells one keyboard step of step moves along an axis of length cells: step
// itself in cells, or that percentage of the axis, never under one cell (a percentage of a narrow
// axis rounds to zero, and a keypress that provably cannot move anything is worse than a slow one).
func stepCellsOf(step int, unit StepUnit, cells int) int {
	if unit == StepPercent {
		return max(cells*step/100, 1)
	}
	return step
}
