package widget

import (
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// THE EDITOR'S POINTER: a press, a drag with its auto-scroll at the edges, and the wheel.

// handleMouse implements the pointer contract.
//
// A press is a COMMAND BOUNDARY, not merely a cursor move, because the core holds
// modal state that a click has to resolve one way or the other (EditorCore.PressAt). The wheel
// scrolls the viewport and never moves the caret, so a reader can scroll while a caret
// stays where they left it.
func (e *Editor) handleMouse(m tui.MouseEvent) bool {
	switch {
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelUp:
		return e.wheel(-1)
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelDown:
		return e.wheel(1)
	case m.Kind == tui.MousePress && m.Button == tui.MouseLeft:
		x := max(m.X-e.cells.gutter, 0) // a press in the gutter is at the line's start
		return e.pressAt(x, m.Y)
	case m.Kind == tui.MousePress && m.Button == tui.MouseRight && e.menu.Enabled():
		// The selection is left as it is: the menu's Copy and Cut act on it.
		return e.menu.OpenAt(tui.Point{X: m.X, Y: m.Y})
	case m.Kind == tui.MouseMotion && m.Button == tui.MouseLeft && e.core.Dragging():
		return e.dragTo(m.X-e.cells.gutter, m.Y)
	case m.Kind == tui.MouseRelease && e.core.Dragging():
		e.endDrag()
		return true
	}
	return false
}

// pressAt places the caret at a clicked cell (EditorCore.PressAt), and keeps the pointer for a
// drag selection from there, so the drag goes on past the editor's edges.
func (e *Editor) pressAt(x, y int) bool {
	// The scroll-indicator column is not text. A press there is inert, and
	// consumed rather than bubbled: the column belongs to this widget.
	if e.scrollable() && x >= e.wrapWidth() {
		return true
	}
	ln, col := e.posAt(x, y)
	handled := e.core.PressAt(ln, col)
	if e.core.Dragging() {
		if ctx := e.Context(); ctx != nil {
			ctx.CapturePointer()
		}
	}
	return handled
}

// dragTo extends the drag-selection to the viewport cell (x, y), in visual mode: the same
// selection v makes, so y (vim) or Ctrl+C (Text mode) copies it. Past the top or bottom edge the
// view scrolls a line, and keeps scrolling while the pointer stays there (dragTick), as a GUI
// editor does.
func (e *Editor) dragTo(x, y int) bool {
	e.cells.dragX = x
	switch {
	case y < 0:
		e.setDragEdge(-1)
		e.cells.ScrollLines(-1)
	case e.cells.h > 0 && y >= e.cells.h:
		e.setDragEdge(1)
		e.cells.ScrollLines(1)
	default:
		e.setDragEdge(0)
	}
	e.extendDrag(x, min(max(y, 0), max(e.cells.h-1, 0)))
	return true
}

// dragTick is the auto-scroll's step while the pointer is held past an edge.
func (e *Editor) dragTick() {
	e.cells.ScrollLines(e.cells.dragEdge)
	row := 0
	if e.cells.dragEdge > 0 {
		row = max(e.cells.h-1, 0)
	}
	e.extendDrag(e.cells.dragX, row)
}

// setDragEdge starts or stops the auto-scroll as the pointer leaves or re-enters the view.
func (e *Editor) setDragEdge(edge int) {
	l := &e.cells
	l.dragEdge = edge
	switch {
	case edge != 0 && l.dragCancel == nil:
		if ctx := e.Context(); ctx != nil {
			l.dragCancel = ctx.Every(dragScrollInterval)
		}
	case edge == 0 && l.dragCancel != nil:
		l.dragCancel()
		l.dragCancel = nil
	}
}

// dragScrollInterval is the auto-scroll's pace: a line per tick while the pointer is past an edge.
const dragScrollInterval = 50 * time.Millisecond

// extendDrag moves the selection's moving end to the viewport cell (x, row).
func (e *Editor) extendDrag(x, row int) {
	ln, col := e.posAt(max(x, 0), row)
	e.core.DragTo(ln, col)
}

// endDrag ends the drag; the selection, if any, stays for the key combos to copy.
func (e *Editor) endDrag() {
	e.core.EndDrag()
	e.setDragEdge(0)
	if ctx := e.Context(); ctx != nil && ctx.HasPointerCapture() {
		ctx.ReleasePointer()
	}
}

// wheel scrolls by whole LOGICAL lines in both wrap modes, as the keys scroll (cellLayout.ScrollLines),
// and consumes the event.
//
// One MouseEvent is one step: the event carries a wheel direction and no
// magnitude, so N notches arrive as N events and this never multiplies. Visual
// rows are deliberately not the unit — the viewport stores a logical-line origin
// only (top/left), so an intra-line offset is not representable without new
// state and new invariants across render, Cursor, ensureVisible, click inversion
// and clamping. That is deferred to its own ADR.
func (e *Editor) wheel(delta int) bool {
	e.cells.ScrollLines(delta)
	return true
}
